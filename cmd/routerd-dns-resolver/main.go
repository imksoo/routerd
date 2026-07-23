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
	"syscall"
	"time"

	"github.com/miekg/dns"

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

	stateMu      sync.RWMutex
	reloadMu     sync.Mutex
	zones        *zoneTable
	sources      []runtimeSource
	listeners    map[string]*boundListener
	sourceCancel context.CancelFunc
	cache        map[string]cacheEntry
	queryLogMu   sync.Mutex
	queryLog     *logstore.DNSQueryLog
}

type runtimeSource struct {
	Spec api.DNSResolverSourceSpec
	Pool *upstreamPool
}

type boundListener struct {
	Addr string
	UDP  *dns.Server
	TCP  *dns.Server
}

type cacheEntry struct {
	Message []byte
	Expires time.Time
}

type reloadSummary struct {
	Reloaded  bool `json:"reloaded"`
	Listeners int  `json:"listeners"`
	Sources   int  `json:"sources"`
}

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
	return d.Run(ctx)
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
	if config.Resource == "" {
		config.Resource = opts.resource
	}
	config.Spec = dnsresolver.NormalizeSpec(config.Spec)
	return config, dnsresolver.Validate(config.Spec)
}

func newDaemon(opts options, config dnsresolver.RuntimeConfig) (*daemon, error) {
	d := &daemon{
		opts:      opts,
		config:    config,
		startedAt: time.Now().UTC(),
		phase:     daemonapi.PhaseStarting,
		health:    daemonapi.HealthUnknown,
		cache:     map[string]cacheEntry{},
		listeners: map[string]*boundListener{},
	}
	d.cond = sync.NewCond(&d.mu)
	d.zones = newZoneTable(config.Zones)
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
			timeout, _ := time.ParseDuration(firstNonEmpty(source.Healthcheck.Timeout, "3s"))
			interval, _ := time.ParseDuration(firstNonEmpty(source.Healthcheck.Interval, "15s"))
			pool, err := newUpstreamPool(source.Upstreams, upstreamPoolConfig{
				ProbeInterval:     interval,
				ProbeTimeout:      timeout,
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
	go func() { _ = apiServer.Serve(listener) }()
	defer apiServer.Close()

	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DNS resolver daemon started", nil)
	if !d.opts.dryRun {
		d.stateMu.RLock()
		queryLogSpec := d.config.Spec.QueryLog
		d.stateMu.RUnlock()
		if queryLogSpec.Enabled {
			queryLog, err := logstore.OpenDNSQueryLog(queryLogSpec.Path)
			if err != nil {
				d.setState(daemonapi.PhaseBlocked, daemonapi.HealthFailed)
				d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "QueryLogOpenFailed", err.Error(), nil)
				return err
			}
			d.queryLogMu.Lock()
			d.queryLog = queryLog
			d.queryLogMu.Unlock()
			defer d.closeQueryLog()
		}
		sourceCtx, sourceCancel := context.WithCancel(ctx)
		d.stateMu.Lock()
		d.sourceCancel = sourceCancel
		sources := append([]runtimeSource(nil), d.sources...)
		d.stateMu.Unlock()
		for i := range sources {
			if sources[i].Pool != nil {
				sources[i].Pool.Start(sourceCtx, d.publish)
			}
		}
		if err := d.startDNS(ctx); err != nil {
			sourceCancel()
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
	<-ctx.Done()
	d.shutdownDNSServers()
	d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "DNS resolver daemon stopped", nil)
	return ctx.Err()
}

func (d *daemon) startDNS(ctx context.Context) error {
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
		d.shutdownDNSServers()
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

func (d *daemon) openBoundListeners(ctx context.Context, addrs map[string]struct{}) (map[string]*boundListener, error) {
	handler := dns.HandlerFunc(d.handleDNS)
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
			Addr: addr,
			UDP:  &dns.Server{PacketConn: packetConn, Net: "udp", Handler: handler},
			TCP:  &dns.Server{Listener: listener, Net: "tcp", Handler: handler},
		}
		listeners[addr] = bound
		go d.serveDNS(bound.UDP, addr, "udp")
		go d.serveDNS(bound.TCP, addr, "tcp")
	}
	return listeners, nil
}

func (d *daemon) serveDNS(server *dns.Server, addr, network string) {
	if err := server.ActivateAndServe(); err != nil && err != http.ErrServerClosed {
		d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "ServeFailed", err.Error(), map[string]string{"listen": addr, "network": network})
	}
}

func (d *daemon) shutdownDNSServers() {
	d.stateMu.Lock()
	listeners := d.listeners
	d.listeners = map[string]*boundListener{}
	d.stateMu.Unlock()
	shutdownBoundListeners(listeners)
}

func shutdownBoundListeners(listeners map[string]*boundListener) {
	for _, listener := range listeners {
		_ = listener.UDP.Shutdown()
		_ = listener.TCP.Shutdown()
	}
}

func (d *daemon) handleDNS(w dns.ResponseWriter, req *dns.Msg) {
	started := time.Now()
	result, err := d.resolve(w.LocalAddr().String(), req)
	resp := result.Response
	if err != nil {
		resp = new(dns.Msg)
		resp.SetRcode(req, dns.RcodeServerFailure)
		result.Response = resp
		result.ResponseCode = dns.RcodeToString[resp.Rcode]
		d.publish("routerd.dns.resolver.query.failed", daemonapi.SeverityWarning, "QueryFailed", err.Error(), nil)
	}
	_ = w.WriteMsg(resp)
	d.recordQuery(w.RemoteAddr().String(), req, result, time.Since(started))
}

func (d *daemon) resolve(localAddr string, req *dns.Msg) (resolveResult, error) {
	config, sources, zones := d.runtimeSnapshot()
	if len(req.Question) == 0 {
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeFormatError)
		return resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
	}
	cacheKey := dnsCacheKey(localAddr, req)
	if config.Spec.Cache.Enabled {
		if cached, ok := d.cacheGet(cacheKey); ok {
			var msg dns.Msg
			if err := msg.Unpack(cached); err == nil {
				msg.Id = req.Id
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
				d.publish("routerd.dns.zone.answered", daemonapi.SeverityInfo, "Answered", question.Name, map[string]string{"qname": question.Name})
				return resolveResult{Response: resp, Upstream: sourceName(source.Spec), ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
			}
		case "forward", "upstream":
			if source.Pool == nil {
				continue
			}
			upstreamReq := req.Copy()
			if source.Spec.DNSSECValidate {
				upstreamReq.SetEdns0(1232, true)
			}
			wire, err := upstreamReq.Pack()
			if err != nil {
				return resolveResult{}, err
			}
			out, err := source.Pool.Exchange(context.Background(), wire, d.publish)
			if err != nil {
				continue
			}
			var resp dns.Msg
			if err := resp.Unpack(out); err != nil {
				return resolveResult{}, err
			}
			if source.Spec.DNSSECValidate && !resp.AuthenticatedData {
				d.publish("routerd.dns.resolver.dnssec.failed", daemonapi.SeverityWarning, "DNSSECValidationFailed", question.Name, map[string]string{"source": source.Spec.Name, "qname": question.Name})
				continue
			}
			if config.Spec.Cache.Enabled {
				d.cacheSet(cacheKey, out, dnsMessageTTL(&resp, config.Spec.Cache), config.Spec.Cache.MaxEntries)
			}
			return resolveResult{Response: &resp, Upstream: sourceName(source.Spec), ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
		}
	}
	resp := new(dns.Msg)
	resp.SetRcode(req, dns.RcodeNameError)
	return resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
}

func (d *daemon) runtimeSnapshot() (dnsresolver.RuntimeConfig, []runtimeSource, *zoneTable) {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.config, append([]runtimeSource(nil), d.sources...), d.zones
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
	d.queryLogMu.Lock()
	defer d.queryLogMu.Unlock()
	if d.queryLog == nil {
		return
	}
	if err := d.queryLog.Record(context.Background(), entry); err == nil {
		return
	}

	// A resolver must continue serving DNS even if its query log becomes
	// unavailable. Reopen the log after a failed write so a transient or stale
	// SQLite connection does not leave observability disabled until a restart.
	path := d.queryLog.Stats().Path
	_ = d.queryLog.Close()
	queryLog, err := logstore.OpenDNSQueryLog(path)
	if err != nil {
		return
	}
	d.queryLog = queryLog
	_ = d.queryLog.Record(context.Background(), entry)
}

func (d *daemon) closeQueryLog() {
	d.queryLogMu.Lock()
	defer d.queryLogMu.Unlock()
	if d.queryLog == nil {
		return
	}
	_ = d.queryLog.Close()
	d.queryLog = nil
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

func (d *daemon) cacheGet(key string) ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.cache[key]
	if !ok || time.Now().After(entry.Expires) {
		delete(d.cache, key)
		return nil, false
	}
	return append([]byte(nil), entry.Message...), true
}

func (d *daemon) cacheSet(key string, msg []byte, ttl time.Duration, maxEntries int) {
	if ttl <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if maxEntries > 0 && len(d.cache) >= maxEntries {
		for k := range d.cache {
			delete(d.cache, k)
			break
		}
	}
	d.cache[key] = cacheEntry{Message: append([]byte(nil), msg...), Expires: time.Now().Add(ttl)}
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
	summary, err := d.reload(ctx)
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&lease); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.stateMu.RLock()
	zones := d.zones
	d.stateMu.RUnlock()
	zones.ApplyLease(lease)
	d.publish("routerd.dhcp.lease."+lease.Action, daemonapi.SeverityInfo, "LeaseUpdated", lease.Hostname, map[string]string{"mac": lease.MAC, "ip": lease.IP, "hostname": lease.Hostname})
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
}

func (d *daemon) reload(ctx context.Context) (reloadSummary, error) {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()

	config, err := loadConfig(d.opts)
	if err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}
	sources, err := buildRuntimeSources(config)
	if err != nil {
		return reloadSummary{}, &reloadError{status: http.StatusBadRequest, err: err}
	}
	zones := newZoneTable(config.Zones)

	d.stateMu.RLock()
	oldZones := d.zones
	oldListeners := d.listeners
	oldSourceCancel := d.sourceCancel
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

	newListeners := map[string]*boundListener{}
	if !d.opts.dryRun {
		newListeners, err = d.openBoundListeners(ctx, additions)
		if err != nil {
			return reloadSummary{}, &reloadError{status: http.StatusInternalServerError, err: err}
		}
	}

	var sourceCancel context.CancelFunc
	if !d.opts.dryRun {
		var sourceCtx context.Context
		sourceCtx, sourceCancel = context.WithCancel(ctx)
		for i := range sources {
			if sources[i].Pool != nil {
				sources[i].Pool.Start(sourceCtx, d.publish)
			}
		}
	}

	d.stateMu.Lock()
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
	d.sources = sources
	d.zones = zones
	d.listeners = mergedListeners
	d.sourceCancel = sourceCancel
	d.stateMu.Unlock()

	shutdownBoundListeners(removals)
	if oldSourceCancel != nil {
		oldSourceCancel()
	}
	if !d.opts.dryRun {
		if len(mergedListeners) > 0 {
			d.setState(daemonapi.PhaseRunning, daemonapi.HealthOK)
		} else {
			d.setState(daemonapi.PhaseStarting, daemonapi.HealthUnknown)
		}
	}
	summary := reloadSummary{Reloaded: true, Listeners: len(mergedListeners), Sources: len(sources)}
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
	d.queryLogMu.Lock()
	queryLog := d.queryLog
	d.queryLogMu.Unlock()
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
