// SPDX-License-Identifier: BSD-3-Clause

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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/eventfile"
	"github.com/imksoo/routerd/pkg/logstore"
)

type options struct {
	resource   string
	configFile string
	socketPath string
	stateFile  string
	eventFile  string
	dryRun     bool
}

type daemon struct {
	opts      options
	config    dnsresolver.RuntimeConfig
	startedAt time.Time
	phase     string
	health    string

	mu         sync.Mutex
	cond       *sync.Cond
	events     []daemonapi.DaemonEvent
	nextCursor uint64
	cancel     context.CancelFunc
	runCtx     context.Context

	stateMu        sync.RWMutex
	reloadMu       sync.Mutex
	queryMu        sync.RWMutex
	querySlots     chan struct{}
	queryRejected  atomic.Uint64
	requestTimeout time.Duration
	zones          *zoneTable
	sources        []runtimeSource
	listeners      map[string]*boundListener
	sourceCancel   context.CancelFunc
	sourceCtx      context.Context
	generation     uint64
	configDigest   string
	cache          map[string]cacheEntry
	queryLogMu     sync.Mutex
	queryLog       *logstore.DNSQueryLog
	queryHistory   queryHistory
}

type runtimeSource struct {
	Spec api.DNSResolverSourceSpec
	Pool *upstreamPool
}

type boundListener struct {
	Addr    string
	UDP     *dns.Server
	TCP     *dns.Server
	UDPDone chan struct{}
	TCPDone chan struct{}
}

type cacheEntry struct {
	Message    []byte
	Stored     time.Time
	Expires    time.Time
	Generation uint64
}

type reloadSummary = dnsresolver.ReloadResult

type reloadError struct {
	status int
	err    error
}

func (e *reloadError) Error() string { return e.err.Error() }
func (e *reloadError) Unwrap() error { return e.err }

type resolveResult struct {
	Response     *dns.Msg
	Upstream     string
	CacheHit     bool
	ResponseCode string
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
	fs.StringVar(&opts.resource, "resource", "resolver", "resource name")
	_ = fs.String("supervisor-owner", "", "internal routerd supervisor ownership token")
	fs.StringVar(&opts.configFile, "config-file", "", "resolver runtime config JSON")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.stateFile, "state-file", "", "state JSON path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "validate config but do not listen")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join("/run/routerd/dns-resolver", opts.resource+".sock")
	}
	if opts.stateFile == "" {
		opts.stateFile = filepath.Join("/var/lib/routerd/dns-resolver", opts.resource, "state.json")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join("/var/lib/routerd/dns-resolver", opts.resource, "events.jsonl")
	}
	return opts, nil
}

func selftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	config, err := loadConfig(opts)
	if err != nil {
		return err
	}
	config.Spec = dnsresolver.NormalizeSpec(config.Spec)
	if err := dnsresolver.Validate(config.Spec); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(config)
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	config, err := loadConfig(opts)
	if err != nil {
		// Start as a long-lived service even without a usable config file, the
		// same way routerd-bgp idles until it is configured. routerd writes the
		// config file and triggers a reload at runtime; coming up empty avoids a
		// crash-loop when the service starts before the config exists (e.g. a
		// fresh live ISO before the first apply).
		fmt.Fprintf(os.Stderr, "routerd-dns-resolver: starting without config (%v); awaiting reload\n", err)
		config = dnsresolver.RuntimeConfig{Resource: opts.resource}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d, err := newDaemon(opts, config)
	if err != nil {
		return err
	}
	d.cancel = cancel
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		for {
			select {
			case sig := <-signals:
				switch sig {
				case syscall.SIGHUP:
					if _, err := d.reload(ctx); err != nil {
						d.publish("routerd.dns.resolver.reload.failed", daemonapi.SeverityWarning, "ReloadFailed", err.Error(), nil)
					}
				default:
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return normalizeDaemonExit(ctx, d.Run(ctx))
}

func normalizeDaemonExit(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
}

func loadConfig(opts options) (dnsresolver.RuntimeConfig, error) {
	if opts.configFile == "" {
		return dnsresolver.RuntimeConfig{}, fmt.Errorf("--config-file is required")
	}
	data, err := os.ReadFile(opts.configFile)
	if err != nil {
		return dnsresolver.RuntimeConfig{}, err
	}
	var config dnsresolver.RuntimeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return dnsresolver.RuntimeConfig{}, err
	}
	if config.Resource == "" || config.Resource != opts.resource {
		return config, fmt.Errorf("resolver config resource %q does not match worker %q", config.Resource, opts.resource)
	}
	if err := validateLocalZones(config); err != nil {
		return config, err
	}
	config.Spec = dnsresolver.NormalizeSpec(config.Spec)
	return config, dnsresolver.Validate(config.Spec)
}

func newDaemon(opts options, config dnsresolver.RuntimeConfig) (*daemon, error) {
	if config.Resource == "" || config.Resource != opts.resource {
		return nil, fmt.Errorf("resolver config resource %q does not match worker %q", config.Resource, opts.resource)
	}
	if err := validateLocalZones(config); err != nil {
		return nil, err
	}
	requestTimeout, err := dnsresolver.RequestTimeout(config.Spec)
	if err != nil {
		return nil, err
	}
	d := &daemon{
		opts:           opts,
		config:         config,
		startedAt:      time.Now().UTC(),
		phase:          daemonapi.PhaseStarting,
		health:         daemonapi.HealthUnknown,
		cache:          map[string]cacheEntry{},
		listeners:      map[string]*boundListener{},
		querySlots:     make(chan struct{}, maxDNSQueries),
		requestTimeout: requestTimeout,
	}
	d.sourceCtx, d.sourceCancel = context.WithCancel(context.Background())
	d.cond = sync.NewCond(&d.mu)
	zones, err := currentLeaseZones(context.Background(), config)
	if err != nil {
		return nil, err
	}
	d.zones = newZoneTable(zones)
	if err := d.zones.LoadError(); err != nil {
		return nil, err
	}
	sources, err := buildRuntimeSources(config)
	if err != nil {
		return nil, err
	}
	d.sources = sources
	return d, nil
}

func buildRuntimeSources(config dnsresolver.RuntimeConfig) ([]runtimeSource, error) {
	var sources []runtimeSource
	for _, source := range config.Spec.Sources {
		runtime := runtimeSource{Spec: source}
		if source.Kind == "forward" || source.Kind == "upstream" {
			timeout, _ := time.ParseDuration(stringutil.FirstNonEmpty(source.Healthcheck.Timeout, "3s"))
			queryTimeout, _ := time.ParseDuration(stringutil.FirstNonEmpty(source.QueryTimeout, source.Healthcheck.Timeout, "3s"))
			interval, _ := time.ParseDuration(stringutil.FirstNonEmpty(source.Healthcheck.Interval, "15s"))
			pool, err := newUpstreamPool(source.Upstreams, upstreamPoolConfig{
				ProbeInterval:     interval,
				ProbeTimeout:      timeout,
				QueryTimeout:      queryTimeout,
				FailThreshold:     source.Healthcheck.FailThreshold,
				PassThreshold:     source.Healthcheck.PassThreshold,
				ViaInterface:      source.ViaInterface,
				BootstrapResolver: source.BootstrapResolver,
			})
			if err != nil {
				return nil, err
			}
			runtime.Pool = pool
		}
		sources = append(sources, runtime)
	}
	return sources, nil
}

func (d *daemon) Run(ctx context.Context) error {
	defer func() {
		d.reloadMu.Lock()
		defer d.reloadMu.Unlock()
		d.closeQueryLog()
	}()
	d.runCtx = ctx
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
	apiServer := &http.Server{Handler: d.routes(), ReadHeaderTimeout: 5 * time.Second}
	defer apiServer.Close()

	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DNS resolver daemon started", nil)
	if !d.opts.dryRun {
		d.stateMu.RLock()
		queryLogSpec := d.config.Spec.QueryLog
		d.stateMu.RUnlock()
		if queryLogSpec.Enabled {
			openCtx, cancelOpen := context.WithTimeout(ctx, queryHistoryOpenTimeout)
			queryLog, err := logstore.OpenDNSQueryLogContext(openCtx, queryLogSpec.Path)
			cancelOpen()
			if err != nil {
				// Query history must not prevent serving the configured DNS
				// zones and upstreams. Reload will retry opening the log and
				// will not acknowledge the complete config until that succeeds.
				d.publish("routerd.dns.query_log.failed", daemonapi.SeverityWarning, "QueryLogOpenFailed", err.Error(), nil)
			} else {
				d.queryLogMu.Lock()
				d.queryLog = queryLog
				d.queryHistory.generation++
				d.queryLogMu.Unlock()
			}
		}
		d.startQueryHistory(ctx)
		sourceCtx, sourceCancel := context.WithCancel(ctx)
		d.stateMu.Lock()
		d.sourceCancel()
		d.sourceCtx, d.sourceCancel = sourceCtx, sourceCancel
		sources := append([]runtimeSource(nil), d.sources...)
		d.stateMu.Unlock()
		for i := range sources {
			if sources[i].Pool != nil {
				sources[i].Pool.Start(sourceCtx, d.publish)
			}
		}
		if err := d.startDNS(ctx); err != nil {
			sourceCancel()
			stopRuntimeSources(sources)
			d.setState(daemonapi.PhaseBlocked, daemonapi.HealthFailed)
			d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "ListenFailed", err.Error(), nil)
			return err
		}
	}
	if !d.opts.dryRun && d.listenerCount() == 0 {
		d.setState(daemonapi.PhaseStarting, daemonapi.HealthUnknown)
		d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "AwaitingConfig", "DNS resolver started without usable config; awaiting reload", nil)
	} else {
		d.setState(daemonapi.PhaseRunning, daemonapi.HealthOK)
		d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "DNS resolver daemon is ready", nil)
	}
	// Accept reloads only after startup owns its listeners and query log.
	go func() { _ = apiServer.Serve(listener) }()
	<-ctx.Done()
	d.shutdownDNSServers()
	d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "DNS resolver daemon stopped", nil)
	return ctx.Err()
}

func (d *daemon) startDNS(ctx context.Context) error {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	d.stateMu.RLock()
	desired := listenAddressSet(d.config.Spec.Listen)
	d.stateMu.RUnlock()
	listeners, err := d.openBoundListeners(ctx, desired)
	if err != nil {
		shutdownBoundListeners(listeners)
		return err
	}
	d.stateMu.Lock()
	d.listeners = listeners
	d.stateMu.Unlock()
	select {
	case <-ctx.Done():
		d.shutdownDNSLocked()
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

func (d *daemon) openBoundListeners(ctx context.Context, addrs map[string]struct{}) (map[string]*boundListener, error) {
	listenConfig := dnsListenConfig()
	listeners := map[string]*boundListener{}
	for addr := range addrs {
		packetConn, err := listenConfig.ListenPacket(ctx, "udp", addr)
		if err != nil {
			shutdownBoundListeners(listeners)
			return nil, fmt.Errorf("listen udp %s: %w", addr, err)
		}
		listener, err := listenConfig.Listen(ctx, "tcp", addr)
		if err != nil {
			_ = packetConn.Close()
			shutdownBoundListeners(listeners)
			return nil, fmt.Errorf("listen tcp %s: %w", addr, err)
		}
		bound := &boundListener{
			Addr:    addr,
			UDP:     &dns.Server{PacketConn: packetConn, Net: "udp", UDPSize: maxDNSMessageSize},
			TCP:     &dns.Server{Listener: dnsDeadlineListener{Listener: listener}, Net: "tcp"},
			UDPDone: make(chan struct{}),
			TCPDone: make(chan struct{}),
		}
		// Binding is staging, not publication. New sockets may already receive
		// packets while retirement waits for old queries/history. Admit only
		// the exact listener selected together with the current configuration.
		// Rejection is immediate, so staged sockets create no waiting handlers.
		handler := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) { d.handleDNSOnListener(w, req, bound) })
		bound.UDP.Handler, bound.TCP.Handler = handler, handler
		udpWrites := new(sync.Mutex)
		bound.UDP.DecorateWriter = func(writer dns.Writer) dns.Writer {
			return dnsPacketWriter{Writer: writer, packet: packetConn, mu: udpWrites}
		}
		udpReady, tcpReady := make(chan struct{}), make(chan struct{})
		bound.UDP.NotifyStartedFunc = func() { close(udpReady) }
		bound.TCP.NotifyStartedFunc = func() { close(tcpReady) }
		listeners[addr] = bound
		go func() { defer close(bound.UDPDone); d.serveDNS(bound.UDP, addr, "udp") }()
		go func() { defer close(bound.TCPDone); d.serveDNS(bound.TCP, addr, "tcp") }()
		for i, ready := range []chan struct{}{udpReady, tcpReady} {
			done := bound.UDPDone
			if i == 1 {
				done = bound.TCPDone
			}
			select {
			case <-ready:
			case <-done:
				packetConn.Close()
				listener.Close()
				shutdownBoundListeners(listeners)
				return nil, fmt.Errorf("DNS server %s exited before listener readiness", addr)
			case <-ctx.Done():
				packetConn.Close()
				listener.Close()
				shutdownBoundListeners(listeners)
				return nil, ctx.Err()
			}
		}
	}
	return listeners, nil
}

func (d *daemon) serveDNS(server *dns.Server, addr, network string) {
	if err := server.ActivateAndServe(); err != nil && err != http.ErrServerClosed {
		d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "ServeFailed", err.Error(), map[string]string{"listen": addr, "network": network})
	}
}

func (d *daemon) shutdownDNSServers() {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	d.shutdownDNSLocked()
}

func (d *daemon) shutdownDNSLocked() {
	d.stateMu.Lock()
	listeners := d.listeners
	d.listeners = map[string]*boundListener{}
	cancel, sources := d.sourceCancel, append([]runtimeSource(nil), d.sources...)
	d.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	stopRuntimeSources(sources)
	shutdownBoundListeners(listeners)
	// Stop admission before reporting exit; retained direct handlers also join.
	d.queryMu.Lock()
	d.queryMu.Unlock()
}

func shutdownBoundListeners(listeners map[string]*boundListener) {
	for _, listener := range listeners {
		_ = listener.UDP.Shutdown()
		_ = listener.TCP.Shutdown()
		if listener.UDPDone != nil {
			<-listener.UDPDone
		}
		if listener.TCPDone != nil {
			<-listener.TCPDone
		}
	}
}

func (d *daemon) handleDNS(w dns.ResponseWriter, req *dns.Msg) {
	d.handleDNSOnListener(w, req, nil)
}

func (d *daemon) handleDNSOnListener(w dns.ResponseWriter, req *dns.Msg, listener *boundListener) {
	if listener != nil {
		d.stateMu.RLock()
		selected := d.listeners[listener.Addr] == listener
		d.stateMu.RUnlock()
		if !selected {
			_ = w.Close()
			return
		}
	}
	select {
	case d.querySlots <- struct{}{}:
	default:
		// No waiting query queue: UDP drops and TCP closes on saturation.
		d.queryRejected.Add(1)
		_ = w.Close()
		return
	}
	d.queryMu.RLock()
	defer func() {
		<-d.querySlots
		d.queryMu.RUnlock()
	}()
	d.stateMu.RLock()
	// A listener may have retired while this request waited for the query lock.
	// The writer lock keeps this admission and source selection in one generation.
	if listener != nil && d.listeners[listener.Addr] != listener {
		d.stateMu.RUnlock()
		_ = w.Close()
		return
	}
	ctx := d.sourceCtx
	requestTimeout, generation := d.requestTimeout, d.generation
	localAddr := w.LocalAddr().String()
	if listener != nil {
		localAddr = listener.Addr
	}
	d.stateMu.RUnlock()
	if ctx.Err() != nil {
		_ = w.Close()
		return
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	started := time.Now()
	result, err := d.resolveContext(ctx, localAddr, req)
	d.stateMu.RLock()
	currentGeneration := d.generation
	owner := d.sourceCtx
	d.stateMu.RUnlock()
	if owner.Err() != nil || generation != currentGeneration {
		_ = w.Close()
		return
	}
	resp := result.Response
	if err != nil {
		resp = new(dns.Msg)
		resp.SetRcode(req, dns.RcodeServerFailure)
		result.Response = resp
		result.ResponseCode = dns.RcodeToString[resp.Rcode]
	}
	if _, udp := w.RemoteAddr().(*net.UDPAddr); udp {
		// EDNS is negotiated on this hop. Without it the UDP limit is 512;
		// smaller advertisements also mean 512 (RFC 6891, section 6.2.3).
		const maxUDPPayload = 65507 // IPv4's largest usable UDP payload.
		limit := dns.MinMsgSize
		if opt := req.IsEdns0(); opt != nil {
			if int(opt.UDPSize()) > limit {
				limit = int(opt.UDPSize())
			}
			if resp.IsEdns0() == nil {
				resp.SetEdns0(maxUDPPayload, false)
			}
		}
		if limit > maxUDPPayload {
			limit = maxUDPPayload
		}
		resp.Truncate(limit)
	}
	writeErr := w.WriteMsg(resp)
	if ctx.Err() != nil {
		return
	}
	if writeErr != nil {
		d.publish("routerd.dns.reply.failed", daemonapi.SeverityWarning, "DNSReplyFailed", writeErr.Error(), nil)
		result.ResponseCode = "WRITE_FAILED"
		d.recordQuery(w.RemoteAddr().String(), req, result, time.Since(started))
		return
	}
	if err != nil && len(req.Question) > 0 {
		client := w.RemoteAddr().String()
		if host, _, splitErr := net.SplitHostPort(client); splitErr == nil {
			client = host
		}
		d.publish("routerd.dns.query.failed", daemonapi.SeverityWarning, "DNSResolutionFailed", err.Error(), map[string]string{
			"source": result.Upstream, "qname": req.Question[0].Name,
			"qtype": dns.TypeToString[req.Question[0].Qtype], "clientAddress": client,
		})
	}
	d.recordQuery(w.RemoteAddr().String(), req, result, time.Since(started))
}

func (d *daemon) resolve(localAddr string, req *dns.Msg) (resolveResult, error) {
	return d.resolveContext(context.Background(), localAddr, req)
}

func (d *daemon) resolveContext(ctx context.Context, localAddr string, req *dns.Msg) (resolveResult, error) {
	config, sources, zones, generation := d.runtimeSnapshot()
	if len(req.Question) == 0 {
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeFormatError)
		return resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
	}
	cacheKey := dnsCacheKey(localAddr, req)
	if config.Spec.Cache.Enabled {
		if cached, ok := d.cacheGet(cacheKey, generation); ok {
			var msg dns.Msg
			if err := msg.Unpack(cached); err == nil {
				msg.Id = req.Id
				msg.Question = append([]dns.Question(nil), req.Question...)
				msg.RecursionDesired = req.RecursionDesired
				return resolveResult{Response: &msg, CacheHit: true, Upstream: "cache", ResponseCode: dns.RcodeToString[msg.Rcode]}, nil
			}
		}
	}
	question := req.Question[0]
	for _, source := range sourcesForListen(config, sources, localAddr) {
		if !sourceMatches(source.Spec.Match, question.Name) {
			continue
		}
		switch source.Spec.Kind {
		case "zone":
			if resp, ok := zones.Answer(req, source.Spec.ZoneRef); ok {
				return resolveResult{Response: resp, Upstream: sourceName(source.Spec), ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
			}
		case "forward", "upstream":
			result := resolveResult{Upstream: sourceName(source.Spec)}
			// RFC 1034 section 4.3.1 limits nonrecursive queries to local
			// information. Keep source precedence: refusing this source must
			// not expose a later authoritative view or export the question.
			if !req.RecursionDesired {
				resp := new(dns.Msg)
				resp.SetRcode(req, dns.RcodeRefused)
				result.Response, result.ResponseCode = resp, dns.RcodeToString[resp.Rcode]
				return result, nil
			}
			if source.Pool == nil {
				return result, fmt.Errorf("DNS source %q has no upstream pool", result.Upstream)
			}
			upstreamReq := req.Copy()
			if source.Spec.DNSSECValidate {
				if opt := upstreamReq.IsEdns0(); opt != nil {
					opt.SetDo()
				} else {
					upstreamReq.SetEdns0(1232, true)
				}
			}
			wire, err := upstreamReq.Pack()
			if err != nil {
				return result, err
			}
			out, err := source.Pool.Exchange(ctx, wire, d.publish)
			if err != nil {
				// Failover is limited to this source's explicitly configured
				// upstreams. Falling through could leak a private query to a
				// less-specific public source, or invent an NXDOMAIN response.
				return result, fmt.Errorf("DNS source %q: %w", result.Upstream, err)
			}
			var resp dns.Msg
			if err := resp.Unpack(out); err != nil {
				return result, err
			}
			if source.Spec.DNSSECValidate && !resp.AuthenticatedData {
				d.publish("routerd.dns.resolver.dnssec.failed", daemonapi.SeverityWarning, "DNSSECValidationFailed", question.Name, map[string]string{"source": source.Spec.Name, "qname": question.Name})
				return result, fmt.Errorf("DNS source %q: DNSSEC validation failed", result.Upstream)
			}
			if config.Spec.Cache.Enabled {
				d.cacheSet(cacheKey, out, dnsMessageTTL(&resp, config.Spec.Cache), config.Spec.Cache.MaxEntries, generation)
			}
			return resolveResult{Response: &resp, Upstream: sourceName(source.Spec), ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
		}
	}
	resp := new(dns.Msg)
	resp.SetRcode(req, dns.RcodeNameError)
	return resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
}

func (d *daemon) runtimeSnapshot() (dnsresolver.RuntimeConfig, []runtimeSource, *zoneTable, uint64) {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.config, append([]runtimeSource(nil), d.sources...), d.zones, d.generation
}

func (d *daemon) listenerCount() int {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return len(d.listeners)
}

func (d *daemon) recordQuery(remoteAddr string, req *dns.Msg, result resolveResult, duration time.Duration) {
	if len(req.Question) == 0 {
		return
	}
	client := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		client = host
	}
	question := req.Question[0]
	entry := logstore.DNSQuery{
		Timestamp:     time.Now().UTC(),
		ClientAddress: client,
		QuestionName:  question.Name,
		QuestionType:  dns.TypeToString[question.Qtype],
		ResponseCode:  result.ResponseCode,
		Answers:       dnsAnswerStrings(result.Response),
		Upstream:      result.Upstream,
		CacheHit:      result.CacheHit,
		Duration:      duration,
	}
	if entry.ResponseCode == "" && result.Response != nil {
		entry.ResponseCode = dns.RcodeToString[result.Response.Rcode]
	}
	d.enqueueQueryHistory(entry)
}

func sourceName(source api.DNSResolverSourceSpec) string {
	if strings.TrimSpace(source.Name) != "" {
		return source.Name
	}
	return source.Kind
}

func dnsAnswerStrings(msg *dns.Msg) []string {
	if msg == nil {
		return nil
	}
	var out []string
	for _, rr := range msg.Answer {
		switch record := rr.(type) {
		case *dns.A:
			out = append(out, record.A.String())
		case *dns.AAAA:
			out = append(out, record.AAAA.String())
		}
	}
	return out
}

func sourcesForListen(config dnsresolver.RuntimeConfig, sources []runtimeSource, localAddr string) []runtimeSource {
	host, port, err := net.SplitHostPort(localAddr)
	if err != nil {
		return sources
	}
	var selected []string
	for _, listen := range config.Spec.Listen {
		if fmt.Sprintf("%d", listen.Port) != port {
			continue
		}
		for _, address := range listen.Addresses {
			if address == host {
				selected = listen.Sources
			}
		}
	}
	if len(selected) == 0 {
		return sources
	}
	allowed := map[string]bool{}
	for _, name := range selected {
		allowed[name] = true
	}
	var out []runtimeSource
	for _, source := range sources {
		if allowed[source.Spec.Name] {
			out = append(out, source)
		}
	}
	return out
}

func (d *daemon) cacheGet(key string, generation uint64) ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.cache[key]
	now := time.Now()
	if !ok || entry.Generation != generation || !now.Before(entry.Expires) {
		delete(d.cache, key)
		return nil, false
	}
	var msg dns.Msg
	if err := msg.Unpack(entry.Message); err != nil {
		delete(d.cache, key)
		return nil, false
	}
	age := uint32(now.Sub(entry.Stored) / time.Second)
	remaining := uint32(entry.Expires.Sub(now) / time.Second)
	for _, section := range [][]dns.RR{msg.Answer, msg.Ns, msg.Extra} {
		for _, rr := range section {
			if rr.Header().Rrtype == dns.TypeOPT {
				continue // OPT's TTL field contains EDNS flags, not a lifetime.
			}
			ttl := rr.Header().Ttl
			if ttl < age {
				ttl = 0
			} else {
				ttl -= age
			}
			rr.Header().Ttl = min(ttl, remaining)
		}
	}
	wire, err := msg.Pack()
	return wire, err == nil
}

func (d *daemon) cacheSet(key string, msg []byte, ttl time.Duration, maxEntries int, generation uint64) {
	if key == "" || ttl <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stateMu.RLock()
	currentGeneration := d.generation
	d.stateMu.RUnlock()
	if generation != currentGeneration {
		return
	}
	if maxEntries > 0 && len(d.cache) >= maxEntries {
		for k := range d.cache {
			delete(d.cache, k)
			break
		}
	}
	now := time.Now()
	d.cache[key] = cacheEntry{Message: append([]byte(nil), msg...), Stored: now, Expires: now.Add(ttl), Generation: generation}
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
	mux.HandleFunc("/v1/leases", d.leaseHandler)
	mux.HandleFunc("/v1/reload", d.reloadHandler)
	return mux
}

func (d *daemon) reloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	ctx := d.runCtx
	if ctx == nil {
		ctx = r.Context()
	}
	summary, err := d.reloadConfig(ctx, r.Header.Get(dnsresolver.ConfigDigestHeader))
	if err != nil {
		status := http.StatusInternalServerError
		var reloadErr *reloadError
		if errors.As(err, &reloadErr) {
			status = reloadErr.status
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = json.NewEncoder(w).Encode(summary)
}

func (d *daemon) leaseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var lease dhcpLeaseEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lease); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		http.Error(w, "DHCP lease request must contain exactly one JSON value", http.StatusBadRequest)
		return
	}
	topic, ok := daemonapi.DHCPLeaseEventType(lease.Action)
	if !ok {
		http.Error(w, "unsupported DHCP lease action", http.StatusBadRequest)
		return
	}
	// Reload snapshots and replaces the zone table under the same owner. A
	// callback arriving during staging must update the table that is actually
	// selected afterward, rather than acknowledge a write into the retired one.
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	if err := r.Context().Err(); err != nil {
		http.Error(w, err.Error(), http.StatusRequestTimeout)
		return
	}
	d.stateMu.RLock()
	zones, owner := d.zones, d.sourceCtx
	d.stateMu.RUnlock()
	if owner.Err() != nil {
		http.Error(w, "DNS resolver owner is stopped", http.StatusServiceUnavailable)
		return
	}
	if err := zones.ApplyLease(lease); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.stateMu.Lock()
	d.generation++
	d.stateMu.Unlock()
	d.mu.Lock()
	clear(d.cache)
	d.mu.Unlock()
	d.publish(topic, daemonapi.SeverityInfo, "LeaseUpdated", lease.Hostname, map[string]string{"mac": lease.MAC, "ip": lease.IP, "hostname": lease.Hostname})
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
}

func (d *daemon) reload(ctx context.Context) (reloadSummary, error) {
	return d.reloadConfig(ctx, "")
}

func (d *daemon) reloadConfig(ctx context.Context, expectedDigest string) (reloadSummary, error) {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	if err := ctx.Err(); err != nil {
		return reloadSummary{}, err
	}

	config, err := loadConfig(d.opts)
	if err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}
	digest, err := config.Digest()
	if err != nil {
		return reloadSummary{}, err
	}
	if expectedDigest != "" && digest != expectedDigest {
		return reloadSummary{}, &reloadError{status: http.StatusConflict, err: fmt.Errorf("desired configuration changed before reload")}
	}
	requestTimeout, err := dnsresolver.RequestTimeout(config.Spec)
	if err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}
	d.stateMu.RLock()
	unchanged := expectedDigest != "" && d.configDigest == digest
	summary := reloadSummary{ConfigDigest: d.configDigest, Listeners: len(d.listeners), Sources: len(d.sources)}
	d.stateMu.RUnlock()
	if unchanged {
		return summary, nil
	}
	sources, err := buildRuntimeSources(config)
	if err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}
	currentZones, err := currentLeaseZones(ctx, config)
	if err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}
	zones := newZoneTable(currentZones)
	if err := zones.LoadError(); err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}

	d.stateMu.RLock()
	oldZones := d.zones
	oldListeners := d.listeners
	oldSourceCancel := d.sourceCancel
	oldSources := append([]runtimeSource(nil), d.sources...)
	d.stateMu.RUnlock()
	zones.CopyDynamicFrom(oldZones)

	desired := listenAddressSet(config.Spec.Listen)
	var additions = map[string]struct{}{}
	var removals = map[string]*boundListener{}
	for addr := range desired {
		if _, ok := oldListeners[addr]; !ok {
			additions[addr] = struct{}{}
		}
	}
	for addr, listener := range oldListeners {
		if _, ok := desired[addr]; !ok {
			removals[addr] = listener
		}
	}

	var stagedQueryLog *logstore.DNSQueryLog
	replaceQueryLog := false
	if !d.opts.dryRun {
		d.queryLogMu.Lock()
		currentQueryLog := d.queryLog
		currentPath := ""
		if currentQueryLog != nil {
			currentPath = currentQueryLog.Stats().Path
		}
		d.queryLogMu.Unlock()
		if config.Spec.QueryLog.Enabled {
			if currentQueryLog == nil || currentPath != config.Spec.QueryLog.Path {
				openCtx, cancelOpen := context.WithTimeout(ctx, queryHistoryOpenTimeout)
				stagedQueryLog, err = logstore.OpenDNSQueryLogContext(openCtx, config.Spec.QueryLog.Path)
				cancelOpen()
				if err != nil {
					return reloadSummary{}, &reloadError{status: http.StatusInternalServerError, err: fmt.Errorf("open DNS query log: %w", err)}
				}
				replaceQueryLog = true
			}
		} else {
			replaceQueryLog = true
		}
	}
	defer func() {
		if stagedQueryLog != nil {
			_ = stagedQueryLog.Close()
		}
	}()

	newListeners := map[string]*boundListener{}
	if !d.opts.dryRun {
		newListeners, err = d.openBoundListeners(ctx, additions)
		if err != nil {
			return reloadSummary{}, &reloadError{status: http.StatusInternalServerError, err: err}
		}
	}
	if err := ctx.Err(); err != nil {
		shutdownBoundListeners(newListeners)
		return reloadSummary{}, err
	}

	// Finish staging before retiring the current owner. History enqueue never
	// takes this operation lock, so DNS work can always finish while we join it.
	d.queryHistory.operation.Lock()
	defer d.queryHistory.operation.Unlock()
	if err := ctx.Err(); err != nil {
		shutdownBoundListeners(newListeners)
		return reloadSummary{}, err
	}
	if oldSourceCancel != nil {
		oldSourceCancel()
	}
	stopRuntimeSources(oldSources)
	// Stop removed listeners before taking the query writer lock: Shutdown
	// joins their handlers, including ones waiting for query admission.
	shutdownBoundListeners(removals)
	d.queryMu.Lock()
	if err := ctx.Err(); err != nil {
		d.queryMu.Unlock()
		shutdownBoundListeners(newListeners)
		d.stateMu.Lock()
		d.configDigest = ""
		for addr := range removals {
			delete(d.listeners, addr)
		}
		d.stateMu.Unlock()
		d.setState(daemonapi.PhaseBlocked, daemonapi.HealthFailed)
		return reloadSummary{}, err
	}
	defer d.queryMu.Unlock()
	sourceCtx, sourceCancel := context.WithCancel(ctx)
	d.stateMu.Lock()
	d.queryLogMu.Lock()
	var retiredQueryLog *logstore.DNSQueryLog
	if replaceQueryLog {
		retiredQueryLog = d.queryLog
		d.queryLog = stagedQueryLog
		d.queryHistory.generation++
		stagedQueryLog = nil
	}
	mergedListeners := map[string]*boundListener{}
	for addr, listener := range oldListeners {
		if _, remove := removals[addr]; !remove {
			mergedListeners[addr] = listener
		}
	}
	for addr, listener := range newListeners {
		mergedListeners[addr] = listener
	}
	d.config = config
	d.requestTimeout = requestTimeout
	d.configDigest = digest
	d.sources = sources
	d.zones = zones
	d.listeners = mergedListeners
	d.sourceCtx, d.sourceCancel = sourceCtx, sourceCancel
	d.generation++
	d.queryLogMu.Unlock()
	d.stateMu.Unlock()
	if retiredQueryLog != nil {
		_ = retiredQueryLog.Close()
	}
	for _, source := range sources {
		if source.Pool != nil && !d.opts.dryRun {
			source.Pool.Start(sourceCtx, d.publish)
		}
	}
	d.mu.Lock()
	clear(d.cache)
	d.mu.Unlock()

	if !d.opts.dryRun {
		if len(mergedListeners) > 0 {
			d.setState(daemonapi.PhaseRunning, daemonapi.HealthOK)
		} else {
			d.setState(daemonapi.PhaseStarting, daemonapi.HealthUnknown)
		}
	}
	summary = reloadSummary{Reloaded: true, ConfigDigest: digest, Listeners: len(mergedListeners), Sources: len(sources)}
	d.publish("routerd.dns.resolver.reloaded", daemonapi.SeverityInfo, "Reloaded", "DNS resolver config reloaded", map[string]string{
		"listeners": fmt.Sprintf("%d", summary.Listeners),
		"sources":   fmt.Sprintf("%d", summary.Sources),
	})
	return summary, nil
}

func (d *daemon) status() daemonapi.DaemonStatus {
	d.mu.Lock()
	phase := d.phase
	health := d.health
	startedAt := d.startedAt
	d.mu.Unlock()
	status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: d.opts.resource, Kind: dnsresolver.DaemonKind, Instance: d.opts.resource})
	status.Phase = phase
	status.Health = health
	status.Since = startedAt
	status.Resources = []daemonapi.ResourceStatus{{
		Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: d.opts.resource},
		Phase:    phase,
		Health:   health,
		Since:    startedAt,
		Conditions: []daemonapi.Condition{{Type: "Ready", Status: conditionStatus(health == daemonapi.HealthOK), Reason: phase,
			LastTransitionTime: startedAt}},
		Observed: d.observedStatus(),
	}}
	return status
}

func (d *daemon) observedStatus() map[string]string {
	d.stateMu.RLock()
	listenerCount := len(d.listeners)
	zones := d.zones
	sources := append([]runtimeSource(nil), d.sources...)
	d.stateMu.RUnlock()
	observed := map[string]string{"listeners": fmt.Sprintf("%d", listenerCount), "zones": fmt.Sprintf("%d", zones.ZoneCount())}
	observed["queryRejected"] = fmt.Sprintf("%d", d.queryRejected.Load())
	d.queryLogMu.Lock()
	queryLog := d.queryLog
	queue := d.queryHistory.queue
	d.queryLogMu.Unlock()
	observed["queryLogDropped"] = fmt.Sprintf("%d", d.queryHistory.dropped.Load())
	observed["queryLogWriteErrors"] = fmt.Sprintf("%d", d.queryHistory.writeErrors.Load())
	observed["queryLogQueueDepth"] = fmt.Sprintf("%d", len(queue))
	if queryLog != nil {
		stats := queryLog.Stats()
		observed["queryLogEnabled"] = "true"
		observed["queryLogPath"] = stats.Path
		observed["queryLogRecords"] = fmt.Sprintf("%d", stats.Records)
		observed["queryLogRecordErrors"] = fmt.Sprintf("%d", stats.RecordErrors)
		observed["queryLogDBBytes"] = fmt.Sprintf("%d", stats.DBBytes)
		observed["queryLogWALBytes"] = fmt.Sprintf("%d", stats.WALBytes)
		if !stats.LastRecordTime.IsZero() {
			observed["queryLogLastRecordAt"] = stats.LastRecordTime.Format(time.RFC3339Nano)
		}
	} else {
		observed["queryLogEnabled"] = "false"
	}
	var parts []string
	for _, source := range sources {
		if source.Pool != nil {
			parts = append(parts, source.Spec.Name+"="+source.Pool.Summary())
		}
	}
	if len(parts) > 0 {
		observed["upstreams"] = strings.Join(parts, ";")
	}
	return observed
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
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: d.opts.resource, Kind: dnsresolver.DaemonKind, Instance: d.opts.resource}, eventType, severity)
	event.Cursor = fmt.Sprintf("%d", d.nextCursor)
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: d.opts.resource}
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = d.events[len(d.events)-1000:]
	}
	d.cond.Broadcast()
	_ = eventfile.AppendJSONLine(d.opts.eventFile, event)
}

func (d *daemon) setState(phase, health string) {
	d.stateMu.RLock()
	listenerCount := len(d.listeners)
	zoneCount := d.zones.ZoneCount()
	d.stateMu.RUnlock()
	d.mu.Lock()
	d.phase = phase
	d.health = health
	state := map[string]any{"phase": d.phase, "health": d.health, "listeners": listenerCount, "zones": zoneCount}
	d.mu.Unlock()
	data, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(d.opts.stateFile, append(data, '\n'), 0o644)
}

func (d *daemon) currentHealth() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.health
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func listenAddressSet(listen []api.DNSResolverListenSpec) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range listen {
		for _, address := range item.Addresses {
			out[net.JoinHostPort(address, fmt.Sprintf("%d", item.Port))] = struct{}{}
		}
	}
	return out
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-dns-resolver daemon --resource NAME --config-file /path/config.json")
}
