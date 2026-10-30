// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/miekg/dns"
	"github.com/quic-go/quic-go"

	"github.com/imksoo/routerd/pkg/daemonapi"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
)

const (
	upstreamHealthy = "Healthy"
	upstreamFailing = "Failing"
	upstreamDown    = "Down"
	upstreamProbing = "Probing"

	maxDNSMessageSize = 65535
)

var errTruncatedUpstreamResponse = errors.New("DNS upstream response is truncated")

type upstreamPoolConfig struct {
	ProbeInterval     time.Duration
	ProbeTimeout      time.Duration
	QueryTimeout      time.Duration
	FailThreshold     int
	PassThreshold     int
	ViaInterface      string
	BootstrapResolver []string
}

type upstreamPool struct {
	mu        sync.Mutex
	upstreams []*dnsUpstream
	config    upstreamPoolConfig
	owner     context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	retired   bool
	queries   sync.WaitGroup
}

type dnsUpstream struct {
	client      *http.Client
	requestURL  string
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
	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = cfg.ProbeTimeout
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
		if upstream.Scheme == "https" {
			// Each HTTPS endpoint owns its TLS identity and idle connections.
			// An IP dial address may intentionally use a DNS certificate name.
			upstream.client = &http.Client{Transport: &http.Transport{
				IdleConnTimeout: 30 * time.Second,
				DialContext:     (&net.Dialer{Resolver: resolverForBootstrap(cfg.BootstrapResolver), Control: interfaceControl(cfg.ViaInterface)}).DialContext,
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: upstream.ServerName},
			}}
		}
		upstreams = append(upstreams, upstream)
	}
	if len(upstreams) == 0 {
		return nil, fmt.Errorf("no DNS upstream configured")
	}
	owner, cancel := context.WithCancel(context.Background())
	return &upstreamPool{upstreams: upstreams, config: cfg, owner: owner, cancel: cancel}, nil
}

func parseDNSUpstream(index int, raw string) (*dnsUpstream, error) {
	trimmed := resolvercfg.NormalizeUpstream(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse DNS upstream %q: %w", raw, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https", "tls", "quic", "udp", "tcp":
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
	case "udp", "tcp":
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
	if override := strings.TrimSpace(parsed.Query().Get("serverName")); override != "" {
		serverName = override
	}
	// serverName is local TLS configuration, not a DoH HTTP query parameter.
	query := parsed.Query()
	query.Del("serverName")
	parsed.RawQuery = query.Encode()
	return &dnsUpstream{Index: index, URL: trimmed, requestURL: parsed.String(), Scheme: scheme, Address: address, ServerName: serverName, Phase: upstreamProbing}, nil
}

func (p *upstreamPool) Start(ctx context.Context, publish func(string, string, string, string, map[string]string)) {
	p.mu.Lock()
	if p.done != nil || p.retired {
		p.mu.Unlock()
		return
	}
	p.cancel()
	ctx, p.cancel = context.WithCancel(ctx)
	p.owner = ctx
	p.done = make(chan struct{})
	done := p.done
	upstreams := append([]*dnsUpstream(nil), p.upstreams...)
	p.mu.Unlock()
	go func() {
		defer close(done)
		var probes sync.WaitGroup
		for _, upstream := range upstreams {
			probes.Add(1)
			go func() {
				defer probes.Done()
				p.probeLoop(ctx, upstream, publish)
			}()
		}
		probes.Wait()
		p.closeIdleConnections()
	}()
}

// Stop first closes admission, then cancels and joins both real query and probe
// work. No retired probe callback or exchange may publish after it returns.
func (p *upstreamPool) Stop() {
	p.mu.Lock()
	p.retired = true
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	cancel()
	if done != nil {
		<-done
	}
	p.queries.Wait()
	p.closeIdleConnections()
}

func (p *upstreamPool) closeIdleConnections() {
	for _, upstream := range p.snapshotPointers() {
		if upstream.client != nil {
			upstream.client.CloseIdleConnections()
		}
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
	if ctx.Err() != nil {
		return
	}
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
	p.mu.Lock()
	if p.retired {
		p.mu.Unlock()
		return nil, context.Canceled
	}
	p.queries.Add(1)
	owner := p.owner
	p.mu.Unlock()
	defer p.queries.Done()
	if owner != nil {
		if err := owner.Err(); err != nil {
			return nil, err
		}
		queryCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(owner, cancel)
		defer stop()
		defer cancel()
		ctx = queryCtx
	}
	var errs []string
	for _, upstream := range p.attemptOrder() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		queryCtx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
		resp, err := p.exchangeOne(queryCtx, upstream, query)
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err == nil {
			p.markSuccess(upstream)
			return resp, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", upstream.URL, err))
		p.markFailure(upstream, err)
	}
	return nil, fmt.Errorf("all DNS upstreams failed: %s", strings.Join(errs, "; "))
}

func (p *upstreamPool) exchangeOne(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	var resp []byte
	var err error
	switch upstream.Scheme {
	case "https":
		resp, err = p.exchangeDoH(ctx, upstream, query)
	case "tls":
		resp, err = p.exchangeDoT(ctx, upstream, query)
	case "quic":
		resp, err = p.exchangeDoQ(ctx, upstream, query)
	case "udp":
		resp, err = p.exchangeUDP(ctx, upstream, query)
	case "tcp":
		resp, err = p.exchangeTCP(ctx, upstream, query)
	default:
		return nil, fmt.Errorf("unsupported DNS upstream scheme %q", upstream.Scheme)
	}
	if err != nil {
		return nil, err
	}
	if err := validateUpstreamResponse(query, resp); err != nil {
		if upstream.Scheme != "udp" || !errors.Is(err, errTruncatedUpstreamResponse) {
			return nil, err
		}
		// Retry a valid truncated UDP response over TCP to the same server,
		// within the original attempt deadline, before trying another provider.
		resp, err = p.exchangeTCP(ctx, upstream, query)
		if err != nil {
			return nil, fmt.Errorf("retry truncated DNS response over TCP: %w", err)
		}
		if err := validateUpstreamResponse(query, resp); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func validateUpstreamResponse(query, response []byte) error {
	var req, resp dns.Msg
	if err := req.Unpack(query); err != nil {
		return fmt.Errorf("invalid DNS query: %w", err)
	}
	if err := resp.Unpack(response); err != nil {
		return fmt.Errorf("invalid DNS upstream response: %w", err)
	}
	if !resp.Response || resp.Id != req.Id || resp.Opcode != req.Opcode {
		return fmt.Errorf("DNS upstream response header does not match query")
	}
	if len(req.Question) == 0 || len(resp.Question) != len(req.Question) {
		return fmt.Errorf("DNS upstream response question does not match query")
	}
	for i, question := range req.Question {
		answer := resp.Question[i]
		if !strings.EqualFold(question.Name, answer.Name) || question.Qtype != answer.Qtype || question.Qclass != answer.Qclass {
			return fmt.Errorf("DNS upstream response question does not match query")
		}
	}
	if resp.Truncated {
		return errTruncatedUpstreamResponse
	}
	// NXDOMAIN and NOERROR (including NODATA and filtered answers) are final
	// DNS answers, not evidence that another provider should be queried.
	if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
		return fmt.Errorf("DNS upstream returned failure rcode %s (%d)", dns.RcodeToString[resp.Rcode], resp.Rcode)
	}
	return nil
}

func (p *upstreamPool) exchangeDoH(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.requestURL, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := upstream.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("DoH upstream returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDNSMessageSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDNSMessageSize {
		return nil, fmt.Errorf("DoH upstream response too large")
	}
	return body, nil
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
	serverName := upstream.ServerName
	if serverName == "" {
		serverName = host // TLS also verifies certificate IP address identities.
	}
	conn, err := quic.Dial(ctx, packetConn, udpAddr, &tls.Config{ServerName: serverName, NextProtos: []string{"doq"}, MinVersion: tls.VersionTLS13}, &quic.Config{})
	if err != nil {
		return nil, err
	}
	defer conn.CloseWithError(0, "")
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return exchangeDoQMessage(ctx, stream, query)
}

// RFC9250 section4.2: the query has ID0 and a sending-direction FIN before
// waiting for the response. A peer may wait for that FIN before answering.
func exchangeDoQMessage(ctx context.Context, stream *quic.Stream, query []byte) ([]byte, error) {
	if len(query) < 12 {
		return nil, fmt.Errorf("DoQ query has no DNS header")
	}
	stop := setExchangeDeadline(ctx, stream)
	defer stop()
	wire := append([]byte(nil), query...)
	wire[0], wire[1] = 0, 0
	if err := writeDNSMessage(stream, wire); err != nil {
		return nil, err
	}
	if err := stream.Close(); err != nil {
		return nil, err
	}
	response, err := readDNSMessage(stream)
	if err != nil {
		return nil, err
	}
	if len(response) < 12 || response[0] != 0 || response[1] != 0 {
		return nil, fmt.Errorf("DoQ response must have DNS message ID0")
	}
	var extra [1]byte
	if n, err := stream.Read(extra[:]); n != 0 || err != io.EOF {
		if err != nil && err != io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("DoQ peer sent more than one DNS response")
	}
	response[0], response[1] = query[0], query[1]
	return response, nil
}

func (p *upstreamPool) exchangeUDP(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	dialer := net.Dialer{Resolver: resolverForBootstrap(p.config.BootstrapResolver), Control: interfaceControl(p.config.ViaInterface)}
	conn, err := dialer.DialContext(ctx, "udp", upstream.Address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := setExchangeDeadline(ctx, conn)
	defer stop()
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, maxDNSMessageSize)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

func (p *upstreamPool) exchangeTCP(ctx context.Context, upstream *dnsUpstream, query []byte) ([]byte, error) {
	dialer := net.Dialer{Resolver: resolverForBootstrap(p.config.BootstrapResolver), Control: interfaceControl(p.config.ViaInterface)}
	conn, err := dialer.DialContext(ctx, "tcp", upstream.Address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return exchangeLengthPrefixed(ctx, conn, query)
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
	var next atomic.Uint64
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			server := cleaned[(next.Add(1)-1)%uint64(len(cleaned))]
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
	if name == "" {
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
	stop := setExchangeDeadline(ctx, conn)
	defer stop()
	if err := writeDNSMessage(conn, query); err != nil {
		return nil, err
	}
	return readDNSMessage(conn)
}

func writeDNSMessage(writer io.Writer, message []byte) error {
	if len(message) > maxDNSMessageSize {
		return fmt.Errorf("DNS query too large")
	}
	frame := make([]byte, 2+len(message))
	binary.BigEndian.PutUint16(frame, uint16(len(message)))
	copy(frame[2:], message)
	_, err := bytes.NewReader(frame).WriteTo(writer)
	return err
}

func readDNSMessage(reader io.Reader) ([]byte, error) {
	var length uint16
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	resp := make([]byte, int(length))
	if _, err := io.ReadFull(reader, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func setExchangeDeadline(ctx context.Context, conn deadlineConn) func() bool {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	// A parent may be canceled before its deadline, including one with no
	// deadline at all. Interrupt an in-flight socket read/write in that case.
	return context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
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
