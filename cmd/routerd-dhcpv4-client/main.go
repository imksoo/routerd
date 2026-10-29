// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"github.com/imksoo/routerd/pkg/eventfile"
	routerotel "github.com/imksoo/routerd/pkg/otel"
	"github.com/imksoo/routerd/pkg/platform"
)

const daemonKind = "routerd-dhcpv4-client"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	resource         string
	ifname           string
	hostname         string
	requestedAddress string
	classID          string
	clientID         string
	timeout          time.Duration
	socketPath       string
	leaseFile        string
	eventFile        string
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "selftest", "once", "run":
			return onceCommand(args[1:], stdout)
		case "daemon":
			return daemonCommand(args[1:])
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	if hasFlag(args, "once") {
		return onceCommand(args, stdout)
	}
	return daemonCommand(args)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts options
	fs.StringVar(&opts.resource, "resource", "wan-dhcpv4", "resource name")
	_ = fs.String("supervisor-owner", "", "internal routerd supervisor ownership token")
	fs.StringVar(&opts.ifname, "interface", "", "uplink interface name")
	fs.StringVar(&opts.hostname, "hostname", "", "DHCP option 12 hostname")
	fs.StringVar(&opts.requestedAddress, "requested-address", "", "DHCP option 50 requested IPv4 address")
	fs.StringVar(&opts.classID, "class-id", "", "DHCP option 60 vendor class identifier")
	fs.StringVar(&opts.clientID, "client-id", "", "DHCP option 61 client identifier")
	fs.DurationVar(&opts.timeout, "timeout", 30*time.Second, "overall acquisition timeout for selftest/once")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.leaseFile, "lease-file", "", "lease snapshot path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	_ = fs.Bool("once", false, "run once and exit")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.timeout <= 0 {
		return options{}, errors.New("--timeout must be positive")
	}
	if opts.ifname == "" {
		return options{}, errors.New("--interface is required")
	}
	if opts.socketPath == "" {
		defaults, _ := platform.Current()
		opts.socketPath = filepath.Join(defaults.RuntimeDir, "dhcpv4-client", opts.resource+".sock")
	}
	if opts.leaseFile == "" {
		defaults, _ := platform.Current()
		opts.leaseFile = filepath.Join(defaults.StateDir, "dhcpv4-client", opts.resource, "lease.json")
	}
	if opts.eventFile == "" {
		defaults, _ := platform.Current()
		opts.eventFile = filepath.Join(defaults.StateDir, "dhcpv4-client", opts.resource, "events.jsonl")
	}
	return opts, nil
}

type dhcpv4PacketConn interface {
	ReadFromUDP([]byte) (int, *net.UDPAddr, error)
	Send(context.Context, dhcpv4client.Packet) error
	SetReadDeadline(time.Time) error
	Close() error
}

func onceCommand(args []string, stdout io.Writer) error {
	opts, err := parseOptions("once", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	d, err := newDaemon(opts)
	if err != nil {
		return err
	}
	defer d.conn.Close()
	if err := d.loop(ctx, true); err != nil {
		return err
	}
	d.mu.Lock()
	snapshot := d.snapshotLocked()
	d.mu.Unlock()
	return writeJSON(stdout, snapshot)
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	telemetry, err := routerotel.Setup(context.Background(), daemonKind, attribute.String("routerd.resource.name", opts.resource))
	if err != nil {
		return err
	}
	defer telemetry.ShutdownGracefully()
	d, err := newDaemon(opts)
	if err != nil {
		return err
	}
	d.telemetry = telemetry
	d.leaseGauge, _ = telemetry.Meter.Int64Gauge("routerd.dhcpv4.client.lease.state")
	d.renewCounter, _ = telemetry.Meter.Int64Counter("routerd.dhcpv4.client.renew")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err = d.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type dhcpv4Daemon struct {
	cancel     context.CancelFunc
	opts       options
	ifi        *net.Interface
	conn       dhcpv4PacketConn
	startedAt  time.Time
	mu         sync.Mutex
	cond       *sync.Cond
	client     *dhcpv4client.Client
	lastError  string
	dirty      bool
	events     []daemonapi.DaemonEvent
	nextCursor uint64

	telemetry    *routerotel.Runtime
	leaseGauge   metric.Int64Gauge
	renewCounter metric.Int64Counter
}

type eventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
}

func newDaemon(opts options) (*dhcpv4Daemon, error) {
	ifi, err := net.InterfaceByName(opts.ifname)
	if err != nil {
		return nil, err
	}
	conn, err := listenDHCPv4(opts.ifname)
	if err != nil {
		return nil, err
	}
	d := &dhcpv4Daemon{opts: opts, ifi: ifi, conn: conn, startedAt: time.Now().UTC()}
	d.client, err = dhcpv4client.New(d.config(), conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	d.cond = sync.NewCond(&d.mu)
	return d, nil
}

func (d *dhcpv4Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancel = cancel
	d.mu.Unlock()
	defer cancel()
	defer d.conn.Close()
	if err := d.prepareFilesystem(); err != nil {
		return err
	}
	if err := d.restoreLease(); err != nil {
		return fmt.Errorf("restore DHCPv4 lease: %w", err)
	}
	if err := d.restoreEvents(); err != nil {
		return fmt.Errorf("restore DHCPv4 events: %w", err)
	}
	server, listener, err := d.httpServer()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	serveErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			cancel()
		}
	}()
	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DHCPv4 client daemon started", nil)
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "DHCPv4 client daemon is ready", nil)
	err = d.loop(ctx, false)
	select {
	case err = <-serveErr:
	default:
	}
	return err
}

// loop is the only receive/timer owner for both once and daemon execution.
// Commands enter the same client/effect owner through advance.
func (d *dhcpv4Daemon) loop(ctx context.Context, once bool) error {
	if err := d.advance(ctx, d.client.Start); err != nil {
		if once {
			return err
		}
		d.publish("routerd.dhcpv4.client.exchange.failed", daemonapi.SeverityWarning, "SendFailed", err.Error(), nil)
	}
	buf := make([]byte, 65535)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.advance(ctx, d.client.Tick); err != nil {
			d.publish("routerd.dhcpv4.client.exchange.failed", daemonapi.SeverityWarning, "ExchangeFailed", err.Error(), nil)
		}
		d.mu.Lock()
		bound := d.snapshotLocked().State == dhcpv4client.StateBound
		next := d.client.NextDeadline()
		d.mu.Unlock()
		if once && bound {
			return nil
		}
		deadline := time.Now().Add(500 * time.Millisecond)
		if !next.IsZero() && next.Before(deadline) {
			deadline = next
		}
		if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
			deadline = end
		}
		if err := d.conn.SetReadDeadline(deadline); err != nil {
			return err
		}
		n, _, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if timeoutError(err) {
				continue
			}
			return err
		}
		if err := d.advance(ctx, func(ctx context.Context) error { return d.client.Receive(ctx, buf[:n]) }); err != nil {
			d.publish("routerd.dhcpv4.client.exchange.failed", daemonapi.SeverityWarning, "ReplyFailed", err.Error(), nil)
		}
	}
}

// advance serializes protocol decisions, sends, lease persistence and result
// publication. HTTP handlers cannot interleave an exchange with a timer or ACK.
func (d *dhcpv4Daemon) advance(ctx context.Context, operation func(context.Context) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	before := d.snapshotLocked()
	err := operation(ctx)
	after := d.snapshotLocked()
	changed := before.State != after.State || before.CurrentAddress != after.CurrentAddress || !before.AcquiredAt.Equal(after.AcquiredAt) || before.ServerID != after.ServerID
	if changed && err == nil {
		d.lastError = ""
	}
	if err != nil {
		d.lastError = err.Error()
	}
	if changed || before.LastError != d.lastError {
		d.dirty = true
	}
	if d.dirty {
		if saveErr := d.saveLeaseLocked(); saveErr != nil {
			d.lastError = saveErr.Error()
			err = errors.Join(err, saveErr)
		} else {
			d.dirty = false
		}
	}
	if changed {
		if after.State == dhcpv4client.StateRenewing && d.renewCounter != nil {
			d.renewCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource)))
		}
		d.publishLocked("routerd.dhcpv4.client.lease.changed", daemonapi.SeverityInfo, string(after.State), "DHCPv4 lease state changed", map[string]string{"state": string(after.State), "address": after.CurrentAddress})
	}
	return err
}

func (d *dhcpv4Daemon) config() dhcpv4client.Config {
	return dhcpv4client.Config{Resource: d.opts.resource, Interface: d.opts.ifname, HardwareAddr: d.ifi.HardwareAddr, Hostname: d.opts.hostname, RequestedAddress: d.opts.requestedAddress, ClassID: d.opts.classID, ClientID: d.opts.clientID}
}

func (d *dhcpv4Daemon) snapshotLocked() dhcpv4client.Snapshot {
	s := d.client.Snapshot()
	s.LastError = d.lastError
	return s
}

func (d *dhcpv4Daemon) restoreLease() error {
	data, err := os.ReadFile(d.opts.leaseFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var snapshot dhcpv4client.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.client.Restore(snapshot)
}

func (d *dhcpv4Daemon) saveLeaseLocked() error {
	snapshot := d.snapshotLocked()
	if err := os.MkdirAll(filepath.Dir(d.opts.leaseFile), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.opts.leaseFile + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, d.opts.leaseFile)
}

func (d *dhcpv4Daemon) prepareFilesystem() error {
	if err := os.MkdirAll(filepath.Dir(d.opts.socketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755); err != nil {
		return err
	}
	_ = os.Remove(d.opts.socketPath)
	return nil
}

func (d *dhcpv4Daemon) httpServer() (*http.Server, net.Listener, error) {
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
	return &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}, listener, nil
}

func (d *dhcpv4Daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	snapshot := d.snapshotLocked()
	if d.leaseGauge != nil {
		d.leaseGauge.Record(context.Background(), stateValue(snapshot.State), metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource), attribute.String("routerd.dhcpv4.state", string(snapshot.State))))
	}
	d.mu.Unlock()
	status := daemonapi.NewStatus(d.daemonRef())
	status.Phase = daemonapi.PhaseRunning
	status.Health = daemonapi.HealthOK
	if snapshot.LastError != "" {
		status.Health = daemonapi.HealthDegraded
	}
	status.Since = d.startedAt
	status.Resources = []daemonapi.ResourceStatus{{
		Resource: daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv4Client", Name: d.opts.resource},
		Phase:    resourcePhase(snapshot.State),
		Health:   status.Health,
		Since:    snapshot.UpdatedAt,
		Conditions: []daemonapi.Condition{{
			Type:               "LeaseReady",
			Status:             conditionStatus(snapshot.CurrentAddress != ""),
			Reason:             string(snapshot.State),
			LastTransitionTime: snapshot.UpdatedAt,
		}},
		Observed: map[string]string{
			"interface":      snapshot.Interface,
			"leaseState":     string(snapshot.State),
			"lastError":      snapshot.LastError,
			"currentAddress": snapshot.CurrentAddress,
			"prefixLength":   strconv.Itoa(snapshot.PrefixLength),
			"defaultGateway": snapshot.DefaultGateway,
			"dnsServers":     jsonStringList(snapshot.DNSServers),
			"domain":         snapshot.Domain,
			"leaseTime":      strconv.FormatInt(snapshot.LeaseTimeSeconds, 10),
			"renewAt":        snapshot.RenewAt.Format(time.RFC3339Nano),
			"rebindAt":       snapshot.RebindAt.Format(time.RFC3339Nano),
			"expiresAt":      snapshot.ExpiresAt.Format(time.RFC3339Nano),
		},
	}}
	writeHTTPJSON(w, status)
}

func (d *dhcpv4Daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	since := r.URL.Query().Get("since")
	wait := eventWait(r)
	deadline := time.Now().Add(wait)
	stop := context.AfterFunc(r.Context(), func() {
		d.mu.Lock()
		d.cond.Broadcast()
		d.mu.Unlock()
	})
	defer stop()
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
		if len(events) > 0 || wait == 0 || time.Now().After(deadline) || r.Context().Err() != nil {
			d.mu.Unlock()
			writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
			return
		}
		d.cond.Wait()
		d.mu.Unlock()
	}
}

func eventWait(r *http.Request) time.Duration {
	const maxWait = 30 * time.Second
	wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
	if wait < 0 {
		return 0
	}
	if wait > maxWait {
		return maxWait
	}
	return wait
}

func (d *dhcpv4Daemon) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	command := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
	result := daemonapi.CommandResult{TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult}, Command: command}
	var err error
	switch command {
	case daemonapi.CommandRenew:
		err = d.advance(r.Context(), d.client.Renew)
	case daemonapi.CommandRelease:
		err = d.advance(r.Context(), d.client.Release)
	case daemonapi.CommandStart:
		err = d.advance(r.Context(), d.client.Start)
	case daemonapi.CommandReload:
		err = errors.New("worker configuration is supplied by its supervisor; update the Router configuration to restart it")
	case daemonapi.CommandStop:
		d.mu.Lock()
		cancel := d.cancel
		d.mu.Unlock()
		if cancel == nil {
			err = errors.New("daemon is not running")
		} else {
			defer cancel()
		}
	default:
		err = fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		result.Accepted = false
		result.Message = err.Error()
		w.WriteHeader(http.StatusBadRequest)
		writeHTTPJSON(w, result)
		return
	}
	result.Accepted = true
	result.Message = "accepted"
	writeHTTPJSON(w, result)
}

func (d *dhcpv4Daemon) publish(eventType, severity, reason, message string, attrs map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.publishLocked(eventType, severity, reason, message, attrs)
}

func (d *dhcpv4Daemon) publishLocked(eventType, severity, reason, message string, attrs map[string]string) {
	event := daemonapi.NewEvent(d.daemonRef(), eventType, severity)
	event.Resource = &daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv4Client", Name: d.opts.resource}
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	d.nextCursor++
	event.Cursor = strconv.FormatUint(d.nextCursor, 10)
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
	}
	d.appendEventFileLocked(event)
	d.cond.Broadcast()
}

func (d *dhcpv4Daemon) appendEventFileLocked(event daemonapi.DaemonEvent) {
	if err := eventfile.AppendJSONLine(d.opts.eventFile, event); err != nil {
		slog.Warn("persist DHCPv4 client event", "resource", d.opts.resource, "error", err)
	}
}

func (d *dhcpv4Daemon) restoreEvents() error {
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
			d.events = d.events[len(d.events)-1000:]
		}
		if id, err := strconv.ParseUint(event.Cursor, 10, 64); err == nil && id > d.nextCursor {
			d.nextCursor = id
		}
	}
	return scanner.Err()
}

func (d *dhcpv4Daemon) eventsSinceLocked(since, topic string) ([]daemonapi.DaemonEvent, string) {
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

func (d *dhcpv4Daemon) daemonRef() daemonapi.DaemonRef {
	return daemonapi.DaemonRef{Name: daemonKind + "-" + d.opts.resource, Kind: daemonKind, Instance: d.opts.resource}
}

func resourcePhase(state dhcpv4client.State) string {
	switch state {
	case dhcpv4client.StateBound, dhcpv4client.StateRenewing, dhcpv4client.StateRebinding:
		return daemonapi.ResourcePhaseBound
	case dhcpv4client.StateDiscovering, dhcpv4client.StateRequesting:
		return daemonapi.ResourcePhaseAcquiring
	case dhcpv4client.StateExpired:
		return daemonapi.ResourcePhaseExpired
	case dhcpv4client.StateReleased:
		return daemonapi.ResourcePhaseReleased
	default:
		return daemonapi.ResourcePhaseIdle
	}
}

func stateValue(state dhcpv4client.State) int64 {
	switch state {
	case dhcpv4client.StateBound:
		return 4
	case dhcpv4client.StateRenewing:
		return 5
	case dhcpv4client.StateRebinding:
		return 6
	case dhcpv4client.StateExpired:
		return 7
	default:
		return 0
	}
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func jsonStringList(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func timeoutError(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "deadline exceeded") || strings.Contains(text, "i/o timeout") || strings.Contains(text, "operation timed out")
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || arg == "-"+name {
			return true
		}
	}
	return false
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "routerd-dhcpv4-client [daemon|selftest|once] --interface IFNAME [--resource NAME]")
}
