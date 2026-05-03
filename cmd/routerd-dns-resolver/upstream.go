package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"

	"routerd/pkg/daemonapi"
	resolvercfg "routerd/pkg/dnsresolver"
)

const (
	upstreamHealthy = "Healthy"
	upstreamFailing = "Failing"
	upstreamDown    = "Down"
	upstreamProbing = "Probing"
)

type upstreamPoolConfig struct {
	ProbeInterval     time.Duration
	ProbeTimeout      time.Duration
	FailThreshold     int
	PassThreshold     int
	ViaInterface      string
	BootstrapResolver []string
}

type upstreamPool struct {
	mu        sync.Mutex
	upstreams []*dnsUpstream
	config    upstreamPoolConfig
	client    *http.Client
}

type dnsUpstream struct {
	Index       int       `json:"index"`
	URL         string    `json:"url"`
	Scheme      string    `json:"scheme"`
	Address     string    `json:"address"`
	ServerName  string    `json:"serverName,omitempty"`
	Phase       string    `json:"phase"`
	LastError   string    `json:"lastError,omitempty"`
	LastSuccess time.Time `json:"lastSuccess,omitempty"`
	LastFailure time.Time `json:"lastFailure,omitempty"`
	Successes   int       `json:"successes,omitempty"`
	Failures    int       `json:"failures,omitempty"`
}

func newUpstreamPool(raw []string, cfg upstreamPoolConfig) (*upstreamPool, error) {
	if cfg.ProbeInterval <= 0 {
		cfg.ProbeInterval = 15 * time.Second
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 3 * time.Second
	}
	if cfg.FailThreshold <= 0 {
		cfg.FailThreshold = 3
	}
	if cfg.PassThreshold <= 0 {
		cfg.PassThreshold = 2
	}
	var upstreams []*dnsUpstream
	for i, value := range raw {
		upstream, err := parseDNSUpstream(i, value)
		if err != nil {
			return nil, err
		}
		upstreams = append(upstreams, upstream)
	}
	if len(upstreams) == 0 {
		return nil, fmt.Errorf("no DNS upstream configured")
	}
	return &upstreamPool{
		upstreams: upstreams,
		config:    cfg,
		client:    &http.Client{Timeout: cfg.ProbeTimeout, Transport: &http.Transport{DialContext: (&net.Dialer{Resolver: resolverForBootstrap(cfg.BootstrapResolver), Control: interfaceControl(cfg.ViaInterface)}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}},
	}, nil
}

func parseDNSUpstream(index int, raw string) (*dnsUpstream, error) {
	trimmed := resolvercfg.NormalizeUpstream(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse DNS upstream %q: %w", raw, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https", "tls", "quic", "udp":
	default:
		return nil, fmt.Errorf("unsupported DNS upstream scheme %q", scheme)
	}
	address := ""
	serverName := parsed.Hostname()
	switch scheme {
	case "https":
		if parsed.Host == "" {
			return nil, fmt.Errorf("DoH upstream %q has no host", raw)
		}
	case "tls", "quic":
		port := parsed.Port()
		if port == "" {
			port = "853"
		}
		address = net.JoinHostPort(parsed.Hostname(), port)
	case "udp":
		port := parsed.Port()
		if port == "" {
			port = "53"
		}
		address = net.JoinHostPort(parsed.Hostname(), port)
		serverName = ""
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("DNS upstream %q has no host", raw)
	}
	if ip, err := netip.ParseAddr(parsed.Hostname()); err == nil && ip.IsValid() {
		serverName = ""
	}
	return &dnsUpstream{Index: index, URL: trimmed, Scheme: scheme, Address: address, ServerName: serverName, Phase: upstreamProbing}, nil
}

func (p *upstreamPool) Start(ctx context.Context, publish func(string, string, string, string, map[string]string)) {
	for _, upstream := range p.snapshotPointers() {
		go p.probeLoop(ctx, upstream, publish)
	}
}

func (p *upstreamPool) probeLoop(ctx context.Context, upstream *dnsUpstream, publish func(string, string, string, string, map[string]string)) {
	p.probe(ctx, upstream, publish)
	ticker := time.NewTicker(p.config.ProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probe(ctx, upstream, publish)
		}
	}
}

func (p *upstreamPool) probe(ctx context.Context, upstream *dnsUpstream, publish func(string, string, string, string, map[string]string)) {
	queryCtx, cancel := context.WithTimeout(ctx, p.config.ProbeTimeout)
	defer cancel()
	_, err := p.exchangeOne(queryCtx, upstream, dnsProbeQuery())
	if err != nil {
		phase := p.markFailure(upstream, err)
		if publish != nil && phase == upstreamDown {
			publish("routerd.dns.upstream.down", daemonapi.SeverityWarning, "UpstreamDown", err.Error(), map[string]string{"upstream": upstream.URL, "scheme": upstream.Scheme})
		}
		return
	}
	phase := p.markSuccess(upstream)
	if publish != nil && phase == upstreamHealthy {
		publish("routerd.dns.upstream.healthy", daemonapi.SeverityInfo, "UpstreamHealthy", "DNS upstream is healthy", map[string]string{"upstream": upstream.URL, "scheme": upstream.Scheme})
	}
}

func (p *upstreamPool) Exchange(ctx context.Context, query []byte, publish func(string, string, string, string, map[string]string)) ([]byte, error) {
	var errs []string
	for _, upstream := range p.attemptOrder() {
		queryCtx, cancel := context.WithTimeout(ctx, p.config.ProbeTimeout)
		resp, err := p.exchangeOne(queryCtx, upstream, query)
		cancel()
		if err == nil {
			p.markSuccess(upstream)
			if publish != nil {
				publish("routerd.dns.upstream.query.succeeded", daemonapi.SeverityInfo, "QuerySucceeded", "DNS query succeeded", map[string]string{"upstream": upstream.URL, "scheme": upstream.Scheme})
			}
			return resp, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", upstream.URL, err))
		p.markFailure(upstream, err)
		if publish != nil {
			publish("routerd.dns.upstream.query.failed", daemonapi.SeverityWarning, "QueryFailed", err.Error(), map[string]string{"upstream": upstream.URL, "scheme": upstream.Scheme})
		}
	}
	return nil, fmt.Errorf("all DNS upstreams failed: %s", strings.Join(errs, "; "))
}

func (p *upstreamPool) exchangeOne(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	switch upstream.Scheme {
	case "https":
		return p.exchangeDoH(ctx, upstream, query)
	case "tls":
		return p.exchangeDoT(ctx, upstream, query)
	case "quic":
		return p.exchangeDoQ(ctx, upstream, query)
	case "udp":
		return p.exchangeUDP(ctx, upstream, query)
	default:
		return nil, fmt.Errorf("unsupported DNS upstream scheme %q", upstream.Scheme)
	}
}

func (p *upstreamPool) exchangeDoH(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("DoH upstream returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (p *upstreamPool) exchangeDoT(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	dialer := tls.Dialer{NetDialer: &net.Dialer{Resolver: resolverForBootstrap(p.config.BootstrapResolver), Control: interfaceControl(p.config.ViaInterface)}, Config: &tls.Config{ServerName: upstream.ServerName, MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", upstream.Address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return exchangeLengthPrefixed(ctx, conn, query)
}

func (p *upstreamPool) exchangeDoQ(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	resolver := resolverForBootstrap(p.config.BootstrapResolver)
	host, port, err := net.SplitHostPort(upstream.Address)
	if err != nil {
		return nil, err
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(addrs[0].IP.String(), port))
	if err != nil {
		return nil, err
	}
	listenConfig := net.ListenConfig{Control: interfaceControl(p.config.ViaInterface)}
	packetConn, err := listenConfig.ListenPacket(ctx, udpNetworkForIP(addrs[0].IP), "")
	if err != nil {
		return nil, err
	}
	defer packetConn.Close()
	conn, err := quic.Dial(ctx, packetConn, udpAddr, &tls.Config{ServerName: upstream.ServerName, NextProtos: []string{"doq"}, MinVersion: tls.VersionTLS13}, &quic.Config{})
	if err != nil {
		return nil, err
	}
	defer conn.CloseWithError(0, "")
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return exchangeLengthPrefixed(ctx, stream, query)
}

func (p *upstreamPool) exchangeUDP(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	dialer := net.Dialer{Resolver: resolverForBootstrap(p.config.BootstrapResolver), Control: interfaceControl(p.config.ViaInterface)}
	conn, err := dialer.DialContext(ctx, "udp", upstream.Address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

func resolverForBootstrap(servers []string) *net.Resolver {
	cleaned := make([]string, 0, len(servers))
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(server); err != nil {
			server = net.JoinHostPort(server, "53")
		}
		cleaned = append(cleaned, server)
	}
	if len(cleaned) == 0 {
		return net.DefaultResolver
	}
	var next int
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			server := cleaned[next%len(cleaned)]
			next++
			dialer := net.Dialer{Control: interfaceControl("")}
			return dialer.DialContext(ctx, "udp", server)
		},
	}
}

func udpNetworkForIP(ip net.IP) string {
	if ip.To4() != nil {
		return "udp4"
	}
	return "udp6"
}

func interfaceControl(name string) func(network, address string, c syscall.RawConn) error {
	name = strings.TrimSpace(name)
	if name == "" || runtime.GOOS != "linux" {
		return nil
	}
	return bindToDeviceControl(name)
}

type deadlineConn interface {
	io.Reader
	io.Writer
	SetDeadline(time.Time) error
}

func exchangeLengthPrefixed(ctx context.Context, conn deadlineConn, query []byte) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if len(query) > 65535 {
		return nil, fmt.Errorf("DNS query too large")
	}
	if err := binary.Write(conn, binary.BigEndian, uint16(len(query))); err != nil {
		return nil, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	var length uint16
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	resp := make([]byte, int(length))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *upstreamPool) attemptOrder() []*dnsUpstream {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*dnsUpstream, 0, len(p.upstreams))
	out = append(out, p.upstreams...)
	sort.SliceStable(out, func(i, j int) bool {
		return upstreamRank(out[i].Phase) < upstreamRank(out[j].Phase)
	})
	return out
}

func upstreamRank(phase string) int {
	switch phase {
	case upstreamHealthy:
		return 0
	case upstreamProbing, upstreamFailing:
		return 1
	case upstreamDown:
		return 2
	default:
		return 1
	}
}

func (p *upstreamPool) snapshotPointers() []*dnsUpstream {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*dnsUpstream, len(p.upstreams))
	copy(out, p.upstreams)
	return out
}

func (p *upstreamPool) markSuccess(upstream *dnsUpstream) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	upstream.Successes++
	upstream.Failures = 0
	upstream.LastError = ""
	upstream.LastSuccess = time.Now().UTC()
	if upstream.Phase == upstreamHealthy || upstream.Successes >= p.config.PassThreshold {
		upstream.Phase = upstreamHealthy
	} else {
		upstream.Phase = upstreamProbing
	}
	return upstream.Phase
}

func (p *upstreamPool) markFailure(upstream *dnsUpstream, err error) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	upstream.Failures++
	upstream.Successes = 0
	upstream.LastError = err.Error()
	upstream.LastFailure = time.Now().UTC()
	if upstream.Failures >= p.config.FailThreshold {
		upstream.Phase = upstreamDown
	} else {
		upstream.Phase = upstreamFailing
	}
	return upstream.Phase
}

func (p *upstreamPool) Snapshot() []dnsUpstream {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]dnsUpstream, 0, len(p.upstreams))
	for _, upstream := range p.upstreams {
		out = append(out, *upstream)
	}
	return out
}

func (p *upstreamPool) Summary() string {
	var parts []string
	for _, upstream := range p.Snapshot() {
		parts = append(parts, fmt.Sprintf("%d:%s:%s", upstream.Index, upstream.Scheme, upstream.Phase))
	}
	return strings.Join(parts, ",")
}

func dnsProbeQuery() []byte {
	return []byte{
		0x72, 0x64, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x03, 'd', 'n', 's',
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x00,
		0x00, 0x01, 0x00, 0x01,
	}
}
