package main

import (
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
	"strings"
	"sync"
	"syscall"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/dohproxy"
)

type options struct {
	resource      string
	backend       string
	listenAddress string
	listenPort    int
	upstreams     []string
	command       string
	socketPath    string
	stateFile     string
	eventFile     string
	dryRun        bool
}

type daemon struct {
	opts      options
	spec      api.DoHProxySpec
	startedAt time.Time
	phase     string
	health    string
	process   *os.Process

	mu         sync.Mutex
	cond       *sync.Cond
	events     []daemonapi.DaemonEvent
	nextCursor uint64
	cancel     context.CancelFunc
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
			return selftest(args[1:], stdout)
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
	var upstreams string
	fs.StringVar(&opts.resource, "resource", "doh", "resource name")
	fs.StringVar(&opts.backend, "backend", dohproxy.BackendCloudflared, "backend: cloudflared or dnscrypt")
	fs.StringVar(&opts.listenAddress, "listen-address", "127.0.0.1", "stub resolver listen address")
	fs.IntVar(&opts.listenPort, "listen-port", 5053, "stub resolver listen port")
	fs.StringVar(&upstreams, "upstream", "", "comma-separated DoH upstream URLs")
	fs.StringVar(&opts.command, "command", "", "backend command path")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.stateFile, "state-file", "", "state JSON path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "do not start backend process")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	for _, upstream := range strings.Split(upstreams, ",") {
		if strings.TrimSpace(upstream) != "" {
			opts.upstreams = append(opts.upstreams, strings.TrimSpace(upstream))
		}
	}
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join("/run/routerd/doh-proxy", opts.resource+".sock")
	}
	if opts.stateFile == "" {
		opts.stateFile = filepath.Join("/var/lib/routerd/doh-proxy", opts.resource, "state.json")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join("/var/lib/routerd/doh-proxy", opts.resource, "events.jsonl")
	}
	return opts, nil
}

func selftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	spec := specFromOptions(opts)
	command, commandArgs, err := dohproxy.Command(spec)
	if err != nil {
		return err
	}
	out := map[string]any{"backend": spec.Backend, "listenAddress": spec.ListenAddress, "listenPort": spec.ListenPort, "command": command, "args": commandArgs}
	return json.NewEncoder(stdout).Encode(out)
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newDaemon(opts)
	d.cancel = cancel
	return d.Run(ctx)
}

func newDaemon(opts options) *daemon {
	spec := specFromOptions(opts)
	d := &daemon{opts: opts, spec: spec, startedAt: time.Now().UTC(), phase: daemonapi.PhaseStarting, health: daemonapi.HealthUnknown}
	d.cond = sync.NewCond(&d.mu)
	return d
}

func specFromOptions(opts options) api.DoHProxySpec {
	return dohproxy.NormalizeSpec(api.DoHProxySpec{
		Backend:       opts.backend,
		ListenAddress: opts.listenAddress,
		ListenPort:    opts.listenPort,
		Upstreams:     opts.upstreams,
		Command:       opts.command,
		StateFile:     opts.stateFile,
		EventFile:     opts.eventFile,
		SocketPath:    opts.socketPath,
	})
}

func (d *daemon) Run(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(d.opts.socketPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.stateFile), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.eventFile), 0o755); err != nil {
		return err
	}
	_ = os.Remove(d.opts.socketPath)
	listener, err := net.Listen("unix", d.opts.socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: d.routes()}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DoH proxy daemon started", nil)
	if err := d.startBackend(ctx); err != nil {
		d.setState(daemonapi.PhaseBlocked, daemonapi.HealthFailed)
		d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "BackendStartFailed", err.Error(), nil)
		return err
	}
	d.setState(daemonapi.PhaseRunning, daemonapi.HealthOK)
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "DoH proxy daemon is ready", nil)
	<-ctx.Done()
	d.stopBackend()
	d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "DoH proxy daemon stopped", nil)
	return ctx.Err()
}

func (d *daemon) startBackend(ctx context.Context) error {
	if d.opts.dryRun {
		return nil
	}
	command, args, err := dohproxy.Command(d.spec)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	d.mu.Lock()
	d.process = cmd.Process
	d.mu.Unlock()
	go func() {
		if err := cmd.Wait(); err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			d.setState(daemonapi.PhaseStopped, daemonapi.HealthFailed)
			d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "BackendExited", err.Error(), nil)
		}
	}()
	return nil
}

func (d *daemon) stopBackend() {
	d.mu.Lock()
	proc := d.process
	d.process = nil
	d.mu.Unlock()
	if proc != nil {
		_ = proc.Signal(syscall.SIGTERM)
	}
}

func (d *daemon) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		if d.currentHealth() != daemonapi.HealthOK {
			http.Error(w, "not healthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(d.status()) })
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(d.eventsSince(r.URL.Query().Get("since")))
	})
	mux.HandleFunc("/v1/commands/", d.commandHandler)
	return mux
}

func (d *daemon) commandHandler(w http.ResponseWriter, r *http.Request) {
	command := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
	result := daemonapi.CommandResult{TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult}, Command: command, Accepted: true}
	switch command {
	case daemonapi.CommandReload:
		d.publish(daemonapi.EventCommandExecuted, daemonapi.SeverityInfo, "Reload", "reload accepted", nil)
	case daemonapi.CommandStop:
		if d.cancel != nil {
			d.cancel()
		}
	case daemonapi.CommandRenew:
		d.publish(daemonapi.EventCommandExecuted, daemonapi.SeverityInfo, "Probe", "renew command accepted as probe trigger", nil)
	default:
		result.Accepted = false
		result.Message = "unsupported command"
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (d *daemon) status() daemonapi.DaemonStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: d.opts.resource, Kind: dohproxy.DaemonKind, Instance: d.opts.resource})
	status.Phase = d.phase
	status.Health = d.health
	status.Since = d.startedAt
	status.Resources = []daemonapi.ResourceStatus{{
		Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DoHProxy", Name: d.opts.resource},
		Phase:    d.phase,
		Health:   d.health,
		Since:    d.startedAt,
		Conditions: []daemonapi.Condition{{Type: "Ready", Status: conditionStatus(d.health == daemonapi.HealthOK), Reason: d.phase,
			LastTransitionTime: d.startedAt}},
		Observed: map[string]string{"backend": d.spec.Backend, "listenAddress": d.spec.ListenAddress, "listenPort": fmt.Sprintf("%d", d.spec.ListenPort)},
	}}
	return status
}

func (d *daemon) eventsSince(since string) eventsResponse {
	d.mu.Lock()
	defer d.mu.Unlock()
	var cursor uint64
	_, _ = fmt.Sscanf(since, "%d", &cursor)
	var events []daemonapi.DaemonEvent
	for _, event := range d.events {
		var eventCursor uint64
		_, _ = fmt.Sscanf(event.Cursor, "%d", &eventCursor)
		if eventCursor > cursor {
			events = append(events, event)
		}
	}
	if len(d.events) > 0 {
		return eventsResponse{Cursor: d.events[len(d.events)-1].Cursor, Events: events}
	}
	return eventsResponse{Events: events}
}

func (d *daemon) publish(eventType, severity, reason, message string, attrs map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextCursor++
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: d.opts.resource, Kind: dohproxy.DaemonKind, Instance: d.opts.resource}, eventType, severity)
	event.Cursor = fmt.Sprintf("%d", d.nextCursor)
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DoHProxy", Name: d.opts.resource}
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = d.events[len(d.events)-1000:]
	}
	d.cond.Broadcast()
	_ = appendJSONLine(d.opts.eventFile, event)
}

func (d *daemon) setState(phase, health string) {
	d.mu.Lock()
	d.phase = phase
	d.health = health
	state := map[string]string{"phase": d.phase, "health": d.health, "backend": d.spec.Backend, "listenAddress": d.spec.ListenAddress, "listenPort": fmt.Sprintf("%d", d.spec.ListenPort)}
	d.mu.Unlock()
	data, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(d.opts.stateFile, append(data, '\n'), 0o644)
}

func (d *daemon) currentHealth() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.health
}

func appendJSONLine(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(value); err != nil {
		return err
	}
	return nil
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-doh-proxy [daemon] --resource NAME --upstream https://...")
}
