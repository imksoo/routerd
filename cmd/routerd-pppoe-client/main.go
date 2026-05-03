package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	routerotel "routerd/pkg/otel"
	"routerd/pkg/platform"
	"routerd/pkg/pppoeclient"
)

const daemonKind = pppoeclient.DaemonKind

type options struct {
	resource        string
	ifname          string
	username        string
	password        string
	passwordFile    string
	authMethod      string
	mtu             int
	mru             int
	serviceName     string
	acName          string
	lcpEchoInterval int
	lcpEchoFailure  int
	runtimeDir      string
	socketPath      string
	stateFile       string
	eventFile       string
	connect         bool
}

type daemon struct {
	opts      options
	spec      api.PPPoESessionSpec
	startedAt time.Time

	mu         sync.Mutex
	snapshot   pppoeclient.Snapshot
	events     []daemonapi.DaemonEvent
	nextCursor uint64
	cmd        *exec.Cmd

	telemetry     *routerotel.Runtime
	phaseGauge    metric.Int64Gauge
	bytesCounter  metric.Int64Counter
	sessionLength metric.Float64Histogram
}

type eventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "selftest":
			return selftestCommand(args[1:], stdout)
		case "daemon":
			return daemonCommand(args[1:])
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	return daemonCommand(args)
}

func parseOptions(name string, args []string) (options, error) {
	defaults, _ := platform.Current()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.resource, "resource", "wan-pppoe", "resource name")
	fs.StringVar(&opts.ifname, "interface", "", "underlying Ethernet interface name")
	fs.StringVar(&opts.username, "username", "", "PPPoE username")
	fs.StringVar(&opts.password, "password", "", "PPPoE password")
	fs.StringVar(&opts.passwordFile, "password-file", "", "file containing PPPoE password")
	fs.StringVar(&opts.authMethod, "auth-method", "both", "auth method: chap, pap, both")
	fs.IntVar(&opts.mtu, "mtu", 1454, "PPP MTU")
	fs.IntVar(&opts.mru, "mru", 1454, "PPP MRU")
	fs.StringVar(&opts.serviceName, "service-name", "", "PPPoE service name")
	fs.StringVar(&opts.acName, "ac-name", "", "PPPoE access concentrator name")
	fs.IntVar(&opts.lcpEchoInterval, "lcp-echo-interval", 30, "LCP echo interval")
	fs.IntVar(&opts.lcpEchoFailure, "lcp-echo-failure", 4, "LCP echo failures before down")
	fs.StringVar(&opts.runtimeDir, "runtime-dir", "", "runtime work directory")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.stateFile, "state-file", "", "state JSON path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	fs.BoolVar(&opts.connect, "connect", false, "selftest: run the platform PPPoE command instead of only rendering config")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.ifname == "" {
		return options{}, errors.New("--interface is required")
	}
	if opts.username == "" {
		return options{}, errors.New("--username is required")
	}
	if opts.password == "" && opts.passwordFile != "" {
		data, err := os.ReadFile(opts.passwordFile)
		if err != nil {
			return options{}, err
		}
		opts.password = strings.TrimSpace(string(data))
	}
	if opts.password == "" {
		return options{}, errors.New("--password or --password-file is required")
	}
	if opts.runtimeDir == "" {
		opts.runtimeDir = filepath.Join(defaults.RuntimeDir, "pppoe-client", opts.resource)
	}
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join(defaults.RuntimeDir, "pppoe-client", opts.resource+".sock")
	}
	if opts.stateFile == "" {
		opts.stateFile = filepath.Join(defaults.StateDir, "pppoe-client", opts.resource, "state.json")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join(defaults.StateDir, "pppoe-client", opts.resource, "events.jsonl")
	}
	return opts, nil
}

func selftestCommand(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	if !opts.connect && (strings.HasPrefix(opts.runtimeDir, "/run/") || strings.HasPrefix(opts.runtimeDir, "/var/run/")) {
		opts.runtimeDir = filepath.Join(os.TempDir(), "routerd-pppoe-client-selftest", opts.resource)
	}
	d := newDaemon(opts, &routerotel.Runtime{})
	if err := d.renderRuntimeConfig(); err != nil {
		return err
	}
	if !opts.connect {
		name, argv := pppoeclient.Command(d.config())
		return writeJSON(stdout, map[string]any{
			"resource": opts.resource,
			"phase":    "Rendered",
			"command":  append([]string{name}, argv...),
			"peer":     string(pppoeclient.LinuxPeer(d.config())),
			"freebsd":  string(pppoeclient.FreeBSDPPPConf(d.config())),
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := d.startSession(ctx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		d.stopSession()
	case <-time.After(20 * time.Second):
		d.stopSession()
	}
	d.mu.Lock()
	snapshot := d.snapshot
	d.mu.Unlock()
	return writeJSON(stdout, snapshot)
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	telemetry, err := routerotel.Setup(ctx, daemonKind, attribute.String("routerd.resource.name", opts.resource))
	if err != nil {
		return err
	}
	defer telemetry.Shutdown(context.Background())
	d := newDaemon(opts, telemetry)
	return d.Run(ctx)
}

func newDaemon(opts options, telemetry *routerotel.Runtime) *daemon {
	if telemetry == nil {
		telemetry = &routerotel.Runtime{}
	}
	telemetry.ServiceName = daemonKind
	telemetry.Ensure()
	spec := api.PPPoESessionSpec{
		Interface:       opts.ifname,
		AuthMethod:      opts.authMethod,
		Username:        opts.username,
		Password:        opts.password,
		MTU:             opts.mtu,
		MRU:             opts.mru,
		ServiceName:     opts.serviceName,
		ACName:          opts.acName,
		LCPEchoInterval: opts.lcpEchoInterval,
		LCPEchoFailure:  opts.lcpEchoFailure,
	}
	now := time.Now().UTC()
	return &daemon{
		opts:          opts,
		spec:          spec,
		startedAt:     now,
		snapshot:      pppoeclient.Snapshot{Resource: opts.resource, Interface: opts.ifname, IfName: pppoeclient.DefaultIfName(opts.resource), Phase: pppoeclient.PhaseIdle, UpdatedAt: now},
		telemetry:     telemetry,
		phaseGauge:    telemetry.Gauge("routerd.pppoe.client.session.state"),
		bytesCounter:  telemetry.Counter("routerd.pppoe.client.bytes"),
		sessionLength: mustHistogram(telemetry),
	}
}

func mustHistogram(telemetry *routerotel.Runtime) metric.Float64Histogram {
	histogram, _ := telemetry.Meter.Float64Histogram("routerd.pppoe.client.session.duration")
	return histogram
}

func (d *daemon) Run(ctx context.Context) error {
	if err := d.prepareFilesystem(); err != nil {
		return err
	}
	_ = d.restoreState()
	_ = d.restoreEvents()
	server, listener, err := d.httpServer()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "PPPoE client daemon started", nil)
	if err := d.renderRuntimeConfig(); err != nil {
		return err
	}
	if err := d.startSession(ctx); err != nil {
		d.publish("routerd.pppoe.client.session.failed", daemonapi.SeverityWarning, "StartFailed", err.Error(), nil)
	}
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "PPPoE client daemon is ready", nil)
	<-ctx.Done()
	d.stopSession()
	d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "PPPoE client daemon stopped", nil)
	return ctx.Err()
}

func (d *daemon) startSession(ctx context.Context) error {
	if err := d.renderRuntimeConfig(); err != nil {
		return err
	}
	name, argv := pppoeclient.Command(d.config())
	cmd := exec.CommandContext(ctx, name, argv...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.snapshot.Phase = pppoeclient.PhaseConnecting
	d.snapshot.UpdatedAt = time.Now().UTC()
	d.cmd = cmd
	d.mu.Unlock()
	d.recordPhase()
	if err := cmd.Start(); err != nil {
		d.markFailed(err.Error())
		return err
	}
	d.publish("routerd.pppoe.client.session.connecting", daemonapi.SeverityInfo, "Connecting", "PPPoE session command started", map[string]string{"command": name})
	go d.scanLog(stdout)
	go d.scanLog(stderr)
	go func() {
		err := cmd.Wait()
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.cmd == cmd {
			d.cmd = nil
		}
		if err != nil && d.snapshot.Phase != pppoeclient.PhaseDisconnecting && d.snapshot.ConnectedAt.IsZero() {
			d.snapshot.Phase = pppoeclient.PhaseFailed
			d.snapshot.LastError = err.Error()
			d.snapshot.UpdatedAt = time.Now().UTC()
			_ = d.saveStateLocked()
			d.publishLocked("routerd.pppoe.client.session.failed", daemonapi.SeverityWarning, "Exited", err.Error(), nil)
			return
		}
		d.snapshot.Phase = pppoeclient.PhaseIdle
		if !d.snapshot.ConnectedAt.IsZero() {
			d.snapshot.LastError = ""
		}
		d.snapshot.UpdatedAt = time.Now().UTC()
		_ = d.saveStateLocked()
		d.publishLocked("routerd.pppoe.client.session.disconnected", daemonapi.SeverityInfo, "Exited", "PPPoE session command exited", nil)
	}()
	return nil
}

func (d *daemon) scanLog(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		d.mu.Lock()
		prev := d.snapshot.Phase
		d.snapshot = pppoeclient.ParseLogLine(d.snapshot, line, time.Now())
		_ = d.saveStateLocked()
		if prev != d.snapshot.Phase {
			d.recordPhaseLocked()
			d.publishLocked("routerd.pppoe.client.session."+strings.ToLower(d.snapshot.Phase), daemonapi.SeverityInfo, d.snapshot.Phase, line, d.eventAttrsLocked())
		}
		d.mu.Unlock()
	}
}

func (d *daemon) stopSession() {
	d.mu.Lock()
	cmd := d.cmd
	d.snapshot.Phase = pppoeclient.PhaseDisconnecting
	d.snapshot.UpdatedAt = time.Now().UTC()
	_ = d.saveStateLocked()
	d.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		time.Sleep(time.Second)
		_ = cmd.Process.Kill()
	}
}

func (d *daemon) markFailed(message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshot.Phase = pppoeclient.PhaseFailed
	d.snapshot.LastError = message
	d.snapshot.UpdatedAt = time.Now().UTC()
	_ = d.saveStateLocked()
}

func (d *daemon) renderRuntimeConfig() error {
	if err := os.MkdirAll(d.opts.runtimeDir, 0700); err != nil {
		return err
	}
	cfg := d.config()
	peer := pppoeclient.LinuxPeer(cfg)
	if platform.CurrentOS() == platform.OSFreeBSD {
		peer = pppoeclient.FreeBSDPPPConf(cfg)
	}
	return os.WriteFile(filepath.Join(d.opts.runtimeDir, "peer.conf"), peer, 0600)
}

func (d *daemon) config() pppoeclient.Config {
	return pppoeclient.Config{Resource: d.opts.resource, Interface: d.opts.ifname, IfName: pppoeclient.DefaultIfName(d.opts.resource), Spec: d.spec, Password: d.opts.password, RuntimeDir: d.opts.runtimeDir}
}

func (d *daemon) prepareFilesystem() error {
	for _, dir := range []string{filepath.Dir(d.opts.socketPath), filepath.Dir(d.opts.stateFile), filepath.Dir(d.opts.eventFile), d.opts.runtimeDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	_ = os.Remove(d.opts.socketPath)
	return nil
}

func (d *daemon) httpServer() (*http.Server, net.Listener, error) {
	listener, err := net.Listen("unix", d.opts.socketPath)
	if err != nil {
		return nil, nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", d.handleStatus)
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeHTTPJSON(w, map[string]string{"status": daemonapi.HealthOK})
	})
	mux.HandleFunc("/v1/events", d.handleEvents)
	mux.HandleFunc("/v1/commands/", d.handleCommand)
	return &http.Server{Handler: mux}, listener, nil
}

func (d *daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	snapshot := d.snapshot
	d.recordPhaseLocked()
	d.mu.Unlock()
	status := daemonapi.NewStatus(d.daemonRef())
	status.Phase = daemonapi.PhaseRunning
	status.Health = daemonapi.HealthOK
	status.Since = d.startedAt
	health := daemonapi.HealthUnknown
	if snapshot.Phase == pppoeclient.PhaseConnected {
		health = daemonapi.HealthOK
	} else if snapshot.Phase == pppoeclient.PhaseFailed {
		health = daemonapi.HealthFailed
	}
	status.Resources = []daemonapi.ResourceStatus{{
		Resource:   daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: d.opts.resource},
		Phase:      snapshot.Phase,
		Health:     health,
		Since:      snapshot.UpdatedAt,
		Conditions: []daemonapi.Condition{{Type: "SessionConnected", Status: conditionStatus(snapshot.Phase == pppoeclient.PhaseConnected), Reason: snapshot.Phase, LastTransitionTime: snapshot.UpdatedAt}},
		Observed: map[string]string{
			"interface":      snapshot.Interface,
			"ifname":         snapshot.IfName,
			"sessionID":      snapshot.SessionID,
			"currentAddress": snapshot.CurrentAddress,
			"peerAddress":    snapshot.PeerAddress,
			"dnsServers":     jsonStringList(snapshot.DNSServers),
			"connectedAt":    formatTime(snapshot.ConnectedAt),
			"bytesIn":        strconv.FormatUint(snapshot.BytesIn, 10),
			"bytesOut":       strconv.FormatUint(snapshot.BytesOut, 10),
		},
	}}
	writeHTTPJSON(w, status)
}

func (d *daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	since := r.URL.Query().Get("since")
	d.mu.Lock()
	events, cursor := d.eventsSinceLocked(since, topic)
	d.mu.Unlock()
	writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
}

func (d *daemon) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	command := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
	result := daemonapi.CommandResult{TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult}, Command: command, Accepted: true}
	switch command {
	case "connect", daemonapi.CommandRenew, daemonapi.CommandStart:
		if err := d.startSession(r.Context()); err != nil {
			result.Accepted = false
			result.Message = err.Error()
		}
	case "disconnect", daemonapi.CommandRelease, daemonapi.CommandStop:
		d.stopSession()
	case daemonapi.CommandReload:
		result.Accepted = d.renderRuntimeConfig() == nil
	default:
		result.Accepted = false
		result.Message = "unsupported command"
	}
	writeHTTPJSON(w, result)
}

func (d *daemon) restoreState() error {
	data, err := os.ReadFile(d.opts.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot pppoeclient.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	d.mu.Lock()
	d.snapshot = snapshot
	d.mu.Unlock()
	return nil
}

func (d *daemon) saveStateLocked() error {
	data, err := json.MarshalIndent(d.snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.opts.stateFile + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, d.opts.stateFile)
}

func (d *daemon) restoreEvents() error {
	file, err := os.Open(d.opts.eventFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event daemonapi.DaemonEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		d.events = append(d.events, event)
		if id, err := strconv.ParseUint(event.Cursor, 10, 64); err == nil && id > d.nextCursor {
			d.nextCursor = id
		}
	}
	return scanner.Err()
}

func (d *daemon) publish(topic, severity, reason, message string, attrs map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.publishLocked(topic, severity, reason, message, attrs)
}

func (d *daemon) publishLocked(topic, severity, reason, message string, attrs map[string]string) {
	d.nextCursor++
	event := daemonapi.NewEvent(d.daemonRef(), topic, severity)
	event.Cursor = strconv.FormatUint(d.nextCursor, 10)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: d.opts.resource}
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
	}
	_ = os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755)
	file, err := os.OpenFile(d.opts.eventFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		_ = json.NewEncoder(file).Encode(event)
		_ = file.Close()
	}
}

func (d *daemon) eventsSinceLocked(since, topic string) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	cursor := since
	sinceID, _ := strconv.ParseUint(since, 10, 64)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if id <= sinceID {
			continue
		}
		if topic != "" && topic != event.Type {
			continue
		}
		out = append(out, event)
		cursor = event.Cursor
	}
	return out, cursor
}

func (d *daemon) recordPhase() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recordPhaseLocked()
}

func (d *daemon) recordPhaseLocked() {
	if d.phaseGauge != nil {
		d.phaseGauge.Record(context.Background(), phaseValue(d.snapshot.Phase), metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource), attribute.String("routerd.pppoe.phase", d.snapshot.Phase)))
	}
	if d.bytesCounter != nil && (d.snapshot.BytesIn > 0 || d.snapshot.BytesOut > 0) {
		d.bytesCounter.Add(context.Background(), int64(d.snapshot.BytesIn+d.snapshot.BytesOut), metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource)))
	}
	if d.sessionLength != nil && !d.snapshot.ConnectedAt.IsZero() {
		d.sessionLength.Record(context.Background(), time.Since(d.snapshot.ConnectedAt).Seconds(), metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource)))
	}
}

func (d *daemon) eventAttrsLocked() map[string]string {
	return map[string]string{
		"currentAddress": d.snapshot.CurrentAddress,
		"peerAddress":    d.snapshot.PeerAddress,
	}
}

func (d *daemon) daemonRef() daemonapi.DaemonRef {
	return daemonapi.DaemonRef{Name: daemonKind + "-" + d.opts.resource, Kind: daemonKind, Instance: d.opts.resource}
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func phaseValue(phase string) int64 {
	switch phase {
	case pppoeclient.PhaseConnecting:
		return 1
	case pppoeclient.PhaseAuthenticating:
		return 2
	case pppoeclient.PhaseIPCP:
		return 3
	case pppoeclient.PhaseConnected:
		return 4
	case pppoeclient.PhaseDisconnecting:
		return 5
	case pppoeclient.PhaseFailed:
		return 6
	default:
		return 0
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func jsonStringList(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-pppoe-client [daemon|selftest] --interface IFNAME --username USER --password PASSWORD")
}
