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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/healthcheck"
	routerotel "routerd/pkg/otel"
)

const daemonKind = healthcheck.DaemonKind

type options struct {
	resource           string
	target             string
	protocol           string
	addressFamily      string
	via                string
	sourceInterface    string
	sourceAddress      string
	port               int
	interval           time.Duration
	timeout            time.Duration
	healthyThreshold   int
	unhealthyThreshold int
	socketPath         string
	stateFile          string
	eventFile          string
}

type daemon struct {
	opts      options
	spec      api.HealthCheckSpec
	resource  api.Resource
	startedAt time.Time
	state     healthcheck.State

	mu         sync.Mutex
	cond       *sync.Cond
	events     []daemonapi.DaemonEvent
	nextCursor uint64
	cancel     context.CancelFunc

	telemetry    *routerotel.Runtime
	probeCounter metric.Int64Counter
	phaseGauge   metric.Int64Gauge
}

type eventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
	More   bool                    `json:"more,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
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
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.resource, "resource", "internet", "resource name")
	fs.StringVar(&opts.target, "target", "", "probe target")
	fs.StringVar(&opts.protocol, "protocol", healthcheck.ProtocolTCP, "probe protocol: icmp, tcp, dns, http")
	fs.StringVar(&opts.addressFamily, "address-family", "", "address family: ipv4 or ipv6")
	fs.StringVar(&opts.via, "via", "", "gateway IP used by the selected path")
	fs.StringVar(&opts.sourceInterface, "source-interface", "", "source interface for probes")
	fs.StringVar(&opts.sourceAddress, "source-address", "", "source IP address for probes")
	fs.IntVar(&opts.port, "port", 0, "probe port")
	fs.DurationVar(&opts.interval, "interval", 30*time.Second, "probe interval")
	fs.DurationVar(&opts.timeout, "timeout", 3*time.Second, "probe timeout")
	fs.IntVar(&opts.healthyThreshold, "healthy-threshold", 1, "consecutive passes needed for Healthy")
	fs.IntVar(&opts.unhealthyThreshold, "unhealthy-threshold", 3, "consecutive failures needed for Unhealthy")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.stateFile, "state-file", "", "state JSON path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.target) == "" {
		return options{}, errors.New("--target is required")
	}
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join("/run/routerd/healthcheck", opts.resource+".sock")
	}
	if opts.stateFile == "" {
		opts.stateFile = filepath.Join("/var/lib/routerd/healthcheck", opts.resource, "state.json")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join("/var/lib/routerd/healthcheck", opts.resource, "events.jsonl")
	}
	return opts, nil
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
	d.cancel = cancel
	return d.Run(ctx)
}

func newDaemon(opts options, telemetry *routerotel.Runtime) *daemon {
	spec := api.HealthCheckSpec{
		Target:             opts.target,
		Protocol:           opts.protocol,
		AddressFamily:      opts.addressFamily,
		Via:                opts.via,
		SourceInterface:    opts.sourceInterface,
		SourceAddress:      opts.sourceAddress,
		Port:               opts.port,
		Interval:           opts.interval.String(),
		Timeout:            opts.timeout.String(),
		HealthyThreshold:   opts.healthyThreshold,
		UnhealthyThreshold: opts.unhealthyThreshold,
		Daemon:             daemonKind,
	}
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: opts.resource},
		Spec:     spec,
	}
	if telemetry == nil {
		telemetry = &routerotel.Runtime{}
	}
	telemetry.ServiceName = daemonKind
	telemetry.Ensure()
	d := &daemon{
		opts:         opts,
		spec:         spec,
		resource:     resource,
		startedAt:    time.Now().UTC(),
		state:        healthcheck.State{Phase: healthcheck.PhaseUnknown},
		telemetry:    telemetry,
		probeCounter: telemetry.Counter("routerd.healthcheck.probes"),
		phaseGauge:   telemetry.Gauge("routerd.healthcheck.phase"),
	}
	d.cond = sync.NewCond(&d.mu)
	return d
}

func (d *daemon) Run(ctx context.Context) error {
	if err := d.prepareFilesystem(); err != nil {
		return err
	}
	if err := d.restoreState(); err != nil {
		return err
	}
	if err := d.restoreEvents(); err != nil {
		return err
	}
	server, listener, err := d.httpServer()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "healthcheck daemon started", nil)
	if err := d.probeOnce(ctx); err != nil {
		return err
	}
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "healthcheck daemon is ready", nil)

	ticker := time.NewTicker(d.opts.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := d.probeOnce(ctx); err != nil {
				return err
			}
		case <-ctx.Done():
			d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "healthcheck daemon stopped", nil)
			return ctx.Err()
		}
	}
}

func (d *daemon) probeOnce(ctx context.Context) error {
	timeout := d.opts.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	spanCtx, span := d.telemetry.Tracer.Start(probeCtx, "healthcheck.probe", trace.WithAttributes(
		attribute.String("routerd.resource.kind", "HealthCheck"),
		attribute.String("routerd.resource.name", d.opts.resource),
		attribute.String("network.protocol.name", d.spec.Protocol),
		attribute.String("server.address", d.spec.Target),
		attribute.String("network.interface.name", d.spec.SourceInterface),
	))
	result := healthcheck.Probe(spanCtx, d.spec)
	if probeCtx.Err() == context.DeadlineExceeded && !result.OK {
		result.Timeout = true
	}
	span.End()
	now := time.Now().UTC()
	d.mu.Lock()
	evaluation := healthcheck.ApplyResult(d.resource, d.spec, d.state, result, now)
	d.state = evaluation.State
	if err := d.saveStateLocked(); err != nil {
		d.mu.Unlock()
		return err
	}
	d.mu.Unlock()
	d.recordMetrics(spanCtx, evaluation)
	d.publishEvent(evaluation.Event)
	return nil
}

func (d *daemon) recordMetrics(ctx context.Context, evaluation healthcheck.Evaluation) {
	attrs := metric.WithAttributes(
		attribute.String("routerd.resource.name", d.opts.resource),
		attribute.String("routerd.healthcheck.result", evaluation.Result),
		attribute.String("server.address", d.spec.Target),
		attribute.String("network.protocol.name", d.spec.Protocol),
	)
	d.probeCounter.Add(ctx, 1, attrs)
	d.phaseGauge.Record(ctx, phaseValue(evaluation.State.Phase), metric.WithAttributes(
		attribute.String("routerd.resource.name", d.opts.resource),
		attribute.String("routerd.healthcheck.phase", evaluation.State.Phase),
	))
}

func (d *daemon) prepareFilesystem() error {
	if err := os.MkdirAll(filepath.Dir(d.opts.socketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.stateFile), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755); err != nil {
		return err
	}
	_ = os.Remove(d.opts.socketPath)
	return nil
}

func (d *daemon) restoreState() error {
	data, err := os.ReadFile(d.opts.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state healthcheck.State
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Phase == "" {
		state.Phase = healthcheck.PhaseUnknown
	}
	d.state = state
	return nil
}

func (d *daemon) saveStateLocked() error {
	data, err := json.MarshalIndent(d.state, "", "  ")
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
		if len(d.events) > 1000 {
			d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
		}
		if id, err := strconv.ParseUint(event.Cursor, 10, 64); err == nil && id > d.nextCursor {
			d.nextCursor = id
		}
	}
	return scanner.Err()
}

func (d *daemon) httpServer() (*http.Server, net.Listener, error) {
	listener, err := net.Listen("unix", d.opts.socketPath)
	if err != nil {
		return nil, nil, err
	}
	_ = os.Chmod(d.opts.socketPath, 0660)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", d.handleStatus)
	mux.HandleFunc("/v1/healthz", d.handleHealthz)
	mux.HandleFunc("/v1/events", d.handleEvents)
	mux.HandleFunc("/v1/commands/", d.handleCommand)
	return &http.Server{Handler: mux}, listener, nil
}

func (d *daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	status := d.statusLocked()
	d.mu.Unlock()
	writeHTTPJSON(w, status)
}

func (d *daemon) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	phase := d.state.Phase
	d.mu.Unlock()
	health := daemonapi.HealthOK
	if phase == healthcheck.PhaseUnhealthy {
		health = daemonapi.HealthFailed
	}
	writeHTTPJSON(w, map[string]string{"status": health})
}

func (d *daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	since := r.URL.Query().Get("since")
	wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
	deadline := time.Now().Add(wait)
	if wait > 0 {
		timer := time.AfterFunc(wait, func() {
			d.mu.Lock()
			d.cond.Broadcast()
			d.mu.Unlock()
		})
		defer timer.Stop()
	}
	for {
		d.mu.Lock()
		events, cursor := d.eventsSinceLocked(since, topic)
		if r.URL.Query().Get("tail") == "true" {
			d.mu.Unlock()
			writeHTTPJSON(w, eventsResponse{Cursor: cursor})
			return
		}
		if len(events) > 0 || wait == 0 || time.Now().After(deadline) {
			d.mu.Unlock()
			writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
			return
		}
		d.cond.Wait()
		d.mu.Unlock()
	}
}

func (d *daemon) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	command := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
	result := daemonapi.CommandResult{
		TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult},
		Command:  command,
	}
	d.publish(daemonapi.EventCommandReceived, daemonapi.SeverityInfo, "CommandReceived", command, map[string]string{"command": command})
	var err error
	switch command {
	case daemonapi.CommandRenew:
		err = d.probeOnce(r.Context())
	case daemonapi.CommandReload:
	case daemonapi.CommandStop:
		go d.cancel()
	default:
		err = fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		result.Accepted = false
		result.Message = err.Error()
		d.publish(daemonapi.EventCommandRejected, daemonapi.SeverityWarning, "CommandRejected", err.Error(), map[string]string{"command": command})
		w.WriteHeader(http.StatusBadRequest)
		writeHTTPJSON(w, result)
		return
	}
	result.Accepted = true
	result.Message = "accepted"
	d.publish(daemonapi.EventCommandExecuted, daemonapi.SeverityInfo, "CommandExecuted", command, map[string]string{"command": command})
	writeHTTPJSON(w, result)
}

func (d *daemon) statusLocked() daemonapi.DaemonStatus {
	health := daemonapi.HealthOK
	if d.state.Phase == healthcheck.PhaseUnhealthy {
		health = daemonapi.HealthFailed
	}
	if d.state.Phase == healthcheck.PhaseFailing || d.state.Phase == healthcheck.PhaseUnknown {
		health = daemonapi.HealthDegraded
	}
	resourceStatus := daemonapi.ResourceStatus{
		Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: d.opts.resource},
		Phase:    d.state.Phase,
		Health:   health,
		Since:    d.state.LastTransitionAt,
		Conditions: []daemonapi.Condition{{
			Type:               "Healthy",
			Status:             conditionStatus(d.state.Phase == healthcheck.PhaseHealthy),
			Reason:             d.state.LastResult,
			Message:            d.state.LastMessage,
			LastTransitionTime: d.state.LastTransitionAt,
		}},
		Observed: map[string]string{
			"target":            d.spec.Target,
			"protocol":          d.spec.Protocol,
			"port":              strconv.Itoa(d.spec.Port),
			"lastResult":        d.state.LastResult,
			"lastCheckedAt":     d.state.LastCheckedAt.UTC().Format(time.RFC3339Nano),
			"lastTransitionAt":  d.state.LastTransitionAt.UTC().Format(time.RFC3339Nano),
			"consecutivePassed": strconv.Itoa(d.state.ConsecutivePassed),
			"consecutiveFailed": strconv.Itoa(d.state.ConsecutiveFailed),
			"via":               d.spec.Via,
			"sourceInterface":   d.spec.SourceInterface,
			"sourceAddress":     d.spec.SourceAddress,
		},
	}
	return daemonapi.DaemonStatus{
		TypeMeta:  daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindDaemonStatus},
		Daemon:    d.daemonRef(),
		Phase:     daemonapi.PhaseRunning,
		Health:    health,
		Since:     d.startedAt,
		Resources: []daemonapi.ResourceStatus{resourceStatus},
	}
}

func (d *daemon) eventsSinceLocked(since, topic string) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	cursor := since
	if cursor == "" && len(d.events) > 0 {
		cursor = d.events[len(d.events)-1].Cursor
	}
	sinceID, _ := strconv.ParseUint(since, 10, 64)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if id <= sinceID {
			continue
		}
		if topic != "" && !matchTopic(topic, event.Type) {
			continue
		}
		out = append(out, event)
		cursor = event.Cursor
	}
	return out, cursor
}

func (d *daemon) publish(topic, severity, reason, message string, attrs map[string]string) {
	event := daemonapi.NewEvent(d.daemonRef(), topic, severity)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: d.opts.resource}
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	d.publishEvent(event)
}

func (d *daemon) publishEvent(event daemonapi.DaemonEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextCursor++
	event.Cursor = strconv.FormatUint(d.nextCursor, 10)
	if event.Daemon.Name == "" {
		event.Daemon = d.daemonRef()
	}
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
	}
	d.appendEventFileLocked(event)
	d.cond.Broadcast()
	if d.telemetry != nil && d.telemetry.Enabled && d.telemetry.Logger != nil {
		d.telemetry.Logger.Info(event.Message, "event.type", event.Type, "resource", d.opts.resource, "reason", event.Reason)
	}
}

func (d *daemon) appendEventFileLocked(event daemonapi.DaemonEvent) {
	if err := os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755); err != nil {
		return
	}
	file, err := os.OpenFile(d.opts.eventFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(event)
}

func (d *daemon) daemonRef() daemonapi.DaemonRef {
	return daemonapi.DaemonRef{Name: daemonKind + "-" + d.opts.resource, Kind: daemonKind, Instance: d.opts.resource}
}

func selftestCommand(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	d := newDaemon(opts, &routerotel.Runtime{})
	if err := d.probeOnce(ctx); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return writeJSON(stdout, map[string]any{
		"resource": opts.resource,
		"state":    d.state,
	})
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func matchTopic(pattern, topic string) bool {
	if pattern == "" || pattern == topic {
		return true
	}
	p := strings.Split(pattern, ".")
	t := strings.Split(topic, ".")
	var walk func(int, int) bool
	walk = func(i, j int) bool {
		if i == len(p) {
			return j == len(t)
		}
		if p[i] == "**" {
			return walk(i+1, j) || (j < len(t) && walk(i, j+1))
		}
		if j == len(t) {
			return false
		}
		return (p[i] == "*" || p[i] == t[j]) && walk(i+1, j+1)
	}
	return walk(0, 0)
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func phaseValue(phase string) int64 {
	switch phase {
	case healthcheck.PhaseHealthy:
		return 2
	case healthcheck.PhasePassing:
		return 1
	case healthcheck.PhaseFailing:
		return -1
	case healthcheck.PhaseUnhealthy:
		return -2
	default:
		return 0
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-healthcheck [daemon|selftest] --target ADDRESS [flags]")
}
