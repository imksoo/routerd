package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/dnsresolver"
	"routerd/pkg/logstore"
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

	zones    *zoneTable
	sources  []runtimeSource
	servers  []*dns.Server
	cache    map[string]cacheEntry
	queryLog *logstore.DNSQueryLog
}

type runtimeSource struct {
	Spec api.DNSResolverSourceSpec
	Pool *upstreamPool
}

type cacheEntry struct {
	Message []byte
	Expires time.Time
}

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
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d, err := newDaemon(opts, config)
	if err != nil {
		return err
	}
	d.cancel = cancel
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
	}
	d.cond = sync.NewCond(&d.mu)
	d.zones = newZoneTable(config.Zones)
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
		d.sources = append(d.sources, runtime)
	}
	return d, nil
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
	apiServer := &http.Server{Handler: d.routes()}
	go func() { _ = apiServer.Serve(listener) }()
	defer apiServer.Close()

	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DNS resolver daemon started", nil)
	if !d.opts.dryRun {
		if d.config.Spec.QueryLog.Enabled {
			queryLog, err := logstore.OpenDNSQueryLog(d.config.Spec.QueryLog.Path)
			if err != nil {
				d.setState(daemonapi.PhaseBlocked, daemonapi.HealthFailed)
				d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "QueryLogOpenFailed", err.Error(), nil)
				return err
			}
			d.queryLog = queryLog
			defer queryLog.Close()
		}
		for i := range d.sources {
			if d.sources[i].Pool != nil {
				d.sources[i].Pool.Start(ctx, d.publish)
			}
		}
		if err := d.startDNS(ctx); err != nil {
			d.setState(daemonapi.PhaseBlocked, daemonapi.HealthFailed)
			d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "ListenFailed", err.Error(), nil)
			return err
		}
	}
	d.setState(daemonapi.PhaseRunning, daemonapi.HealthOK)
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "DNS resolver daemon is ready", nil)
	<-ctx.Done()
	for _, server := range d.servers {
		_ = server.Shutdown()
	}
	d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "DNS resolver daemon stopped", nil)
	return ctx.Err()
}

func (d *daemon) startDNS(ctx context.Context) error {
	handler := dns.HandlerFunc(d.handleDNS)
	for _, listen := range d.config.Spec.Listen {
		for _, address := range listen.Addresses {
			addr := net.JoinHostPort(address, fmt.Sprintf("%d", listen.Port))
			packetConn, err := net.ListenPacket("udp", addr)
			if err != nil {
				d.shutdownDNSServers()
				return fmt.Errorf("listen udp %s: %w", addr, err)
			}
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				_ = packetConn.Close()
				d.shutdownDNSServers()
				return fmt.Errorf("listen tcp %s: %w", addr, err)
			}
			udp := &dns.Server{PacketConn: packetConn, Net: "udp", Handler: handler}
			tcp := &dns.Server{Listener: listener, Net: "tcp", Handler: handler}
			d.servers = append(d.servers, udp, tcp)
			go d.serveDNS(udp, addr, "udp")
			go d.serveDNS(tcp, addr, "tcp")
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

func (d *daemon) serveDNS(server *dns.Server, addr, network string) {
	if err := server.ActivateAndServe(); err != nil && err != http.ErrServerClosed {
		d.publish(daemonapi.EventDaemonCrashed, daemonapi.SeverityError, "ServeFailed", err.Error(), map[string]string{"listen": addr, "network": network})
	}
}

func (d *daemon) shutdownDNSServers() {
	for _, server := range d.servers {
		_ = server.Shutdown()
	}
	d.servers = nil
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
	if len(req.Question) == 0 {
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeFormatError)
		return resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
	}
	cacheKey := dnsCacheKey(localAddr, req)
	if d.config.Spec.Cache.Enabled {
		if cached, ok := d.cacheGet(cacheKey); ok {
			var msg dns.Msg
			if err := msg.Unpack(cached); err == nil {
				msg.Id = req.Id
				return resolveResult{Response: &msg, CacheHit: true, Upstream: "cache", ResponseCode: dns.RcodeToString[msg.Rcode]}, nil
			}
		}
	}
	question := req.Question[0]
	for _, source := range d.sourcesForListen(localAddr) {
		if !sourceMatches(source.Spec.Match, question.Name) {
			continue
		}
		switch source.Spec.Kind {
		case "zone":
			if resp, ok := d.zones.Answer(req, source.Spec.ZoneRef); ok {
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
			if d.config.Spec.Cache.Enabled {
				d.cacheSet(cacheKey, out, dnsMessageTTL(&resp, d.config.Spec.Cache))
			}
			return resolveResult{Response: &resp, Upstream: sourceName(source.Spec), ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
		}
	}
	resp := new(dns.Msg)
	resp.SetRcode(req, dns.RcodeNameError)
	return resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, nil
}

func (d *daemon) recordQuery(remoteAddr string, req *dns.Msg, result resolveResult, duration time.Duration) {
	if d.queryLog == nil || len(req.Question) == 0 {
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
	_ = d.queryLog.Record(context.Background(), entry)
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

func (d *daemon) sourcesForListen(localAddr string) []runtimeSource {
	host, port, err := net.SplitHostPort(localAddr)
	if err != nil {
		return d.sources
	}
	var selected []string
	for _, listen := range d.config.Spec.Listen {
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
		return d.sources
	}
	allowed := map[string]bool{}
	for _, name := range selected {
		allowed[name] = true
	}
	var out []runtimeSource
	for _, source := range d.sources {
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

func (d *daemon) cacheSet(key string, msg []byte, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if max := d.config.Spec.Cache.MaxEntries; max > 0 && len(d.cache) >= max {
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
	return mux
}

func (d *daemon) leaseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var lease dhcpLeaseEvent
	if err := json.NewDecoder(r.Body).Decode(&lease); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.zones.ApplyLease(lease)
	d.publish("routerd.dhcp.lease."+lease.Action, daemonapi.SeverityInfo, "LeaseUpdated", lease.Hostname, map[string]string{"mac": lease.MAC, "ip": lease.IP, "hostname": lease.Hostname})
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
}

func (d *daemon) status() daemonapi.DaemonStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	status := daemonapi.NewStatus(daemonapi.DaemonRef{Name: d.opts.resource, Kind: dnsresolver.DaemonKind, Instance: d.opts.resource})
	status.Phase = d.phase
	status.Health = d.health
	status.Since = d.startedAt
	status.Resources = []daemonapi.ResourceStatus{{
		Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: d.opts.resource},
		Phase:    d.phase,
		Health:   d.health,
		Since:    d.startedAt,
		Conditions: []daemonapi.Condition{{Type: "Ready", Status: conditionStatus(d.health == daemonapi.HealthOK), Reason: d.phase,
			LastTransitionTime: d.startedAt}},
		Observed: d.observedStatus(),
	}}
	return status
}

func (d *daemon) observedStatus() map[string]string {
	observed := map[string]string{"listeners": fmt.Sprintf("%d", len(d.servers)/2), "zones": fmt.Sprintf("%d", d.zones.ZoneCount())}
	var parts []string
	for _, source := range d.sources {
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
	_ = appendJSONLine(d.opts.eventFile, event)
}

func (d *daemon) setState(phase, health string) {
	d.mu.Lock()
	d.phase = phase
	d.health = health
	state := map[string]any{"phase": d.phase, "health": d.health, "listeners": len(d.servers) / 2, "zones": d.zones.ZoneCount()}
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
	return json.NewEncoder(file).Encode(value)
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
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
