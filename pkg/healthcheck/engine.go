package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/resourcequery"
)

const (
	DaemonKind = "routerd-healthcheck"

	ProtocolICMP = "icmp"
	ProtocolTCP  = "tcp"
	ProtocolDNS  = "dns"
	ProtocolHTTP = "http"

	ResultPassed  = "passed"
	ResultFailed  = "failed"
	ResultTimeout = "timeout"

	PhaseUnknown   = "Unknown"
	PhasePassing   = "Passing"
	PhaseHealthy   = "Healthy"
	PhaseFailing   = "Failing"
	PhaseUnhealthy = "Unhealthy"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type ProbeResult struct {
	OK      bool
	Timeout bool
	Message string
}

type ProbeFunc func(ctx context.Context, spec api.HealthCheckSpec) ProbeResult

type State struct {
	Phase             string    `json:"phase"`
	LastResult        string    `json:"lastResult,omitempty"`
	LastMessage       string    `json:"message,omitempty"`
	LastTransitionAt  time.Time `json:"lastTransitionAt,omitempty"`
	LastCheckedAt     time.Time `json:"lastCheckedAt,omitempty"`
	ConsecutivePassed int       `json:"consecutivePassed,omitempty"`
	ConsecutiveFailed int       `json:"consecutiveFailed,omitempty"`
}

type Evaluation struct {
	State  State
	Result string
	Event  daemonapi.DaemonEvent
	Status map[string]any
}

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Probe  ProbeFunc
	Now    func() time.Time
	Logger *slog.Logger

	mu    sync.Mutex
	state map[string]*State
}

type ProbeDaemon interface {
	Run(ctx context.Context, resource api.Resource) error
	Status(ctx context.Context) (daemonapi.DaemonStatus, error)
}

func (c *Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.healthcheck.*.*"}}, 64)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "HealthCheck" {
				continue
			}
			c.saveEventStatus(event)
		}
	}()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "HealthCheck" {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err == nil && (spec.Daemon == DaemonKind || spec.SocketSource != "") {
			continue
		}
		resource := resource
		go c.runResource(ctx, resource)
	}
}

func (c *Controller) runResource(ctx context.Context, resource api.Resource) {
	spec, err := resource.HealthCheckSpec()
	if err != nil {
		if c.Logger != nil {
			c.Logger.Warn("healthcheck spec decode failed", "resource", resource.Metadata.Name, "error", err)
		}
		return
	}
	interval := durationOr(spec.Interval, 30*time.Second)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if err := c.ProbeOnce(ctx, resource, spec); err != nil && c.Logger != nil {
		c.Logger.Warn("healthcheck probe failed", "resource", resource.Metadata.Name, "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.ProbeOnce(ctx, resource, spec); err != nil && c.Logger != nil {
				c.Logger.Warn("healthcheck probe failed", "resource", resource.Metadata.Name, "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *Controller) ProbeOnce(ctx context.Context, resource api.Resource, spec api.HealthCheckSpec) error {
	spec = ResolveSpecWithStore(c.Router, c.Store, spec)
	timeout := durationOr(spec.Timeout, 3*time.Second)
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	probe := c.Probe
	if probe == nil {
		probe = Probe
	}
	result := probe(probeCtx, spec)
	if probeCtx.Err() == context.DeadlineExceeded && !result.OK {
		result.Timeout = true
	}
	return c.applyResult(ctx, resource, spec, result)
}

func (c *Controller) applyResult(ctx context.Context, resource api.Resource, spec api.HealthCheckSpec, result ProbeResult) error {
	state := c.resourceState(resource.Metadata.Name)
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := *state
	evaluation := ApplyResult(resource, spec, previous, result, now)
	*state = evaluation.State
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", resource.Metadata.Name, evaluation.Status); err != nil {
		return err
	}
	if previous.Phase == evaluation.State.Phase && previous.LastResult == evaluation.State.LastResult {
		return nil
	}
	return c.Bus.Publish(ctx, evaluation.Event)
}

func (c *Controller) saveEventStatus(event daemonapi.DaemonEvent) {
	var current map[string]any
	if c.Store != nil {
		current = c.Store.ObjectStatus(event.Resource.APIVersion, event.Resource.Kind, event.Resource.Name)
		if existing, ok := parseStatusTime(current["lastCheckedAt"]); ok && existing.After(event.Time) {
			return
		}
	}
	status := map[string]any{}
	for key, value := range event.Attributes {
		status[key] = value
	}
	if phase := event.Attributes["phase"]; phase != "" {
		status["phase"] = phase
	}
	if result := event.Attributes["result"]; result != "" {
		status["lastResult"] = result
	}
	status["lastCheckedAt"] = event.Time.UTC().Format(time.RFC3339Nano)
	if current != nil && fmt.Sprint(current["phase"]) == fmt.Sprint(status["phase"]) && fmt.Sprint(current["lastTransitionAt"]) != "" {
		status["lastTransitionAt"] = current["lastTransitionAt"]
	} else if status["lastTransitionAt"] == nil {
		status["lastTransitionAt"] = event.Time.UTC().Format(time.RFC3339Nano)
	}
	if event.Message != "" {
		status["message"] = event.Message
	}
	_ = c.Store.SaveObjectStatus(event.Resource.APIVersion, event.Resource.Kind, event.Resource.Name, status)
}

func parseStatusTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// ResolveSpec converts routerd resource names to the OS interface names used
// by probe runtime calls. Standalone daemon flags still accept raw OS names.
func ResolveSpec(router *api.Router, spec api.HealthCheckSpec) api.HealthCheckSpec {
	if strings.TrimSpace(spec.SourceInterface) != "" {
		spec.SourceInterface = resolveInterfaceName(router, spec.SourceInterface)
	}
	return spec
}

func ResolveSpecWithStore(router *api.Router, store Store, spec api.HealthCheckSpec) api.HealthCheckSpec {
	spec = ResolveSpec(router, spec)
	if strings.TrimSpace(spec.SourceAddress) == "" && strings.TrimSpace(spec.SourceAddressFrom.Resource) != "" && store != nil {
		spec.SourceAddress = statusAddressValue(resourcequery.Value(store, spec.SourceAddressFrom))
	}
	return spec
}

func statusAddressValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}

func Probe(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	switch defaultString(spec.Protocol, protocolFromType(spec.Type)) {
	case ProtocolTCP:
		return ProbeTCP(ctx, spec)
	case ProtocolDNS:
		return ProbeDNS(ctx, spec)
	case ProtocolHTTP:
		return ProbeHTTP(ctx, spec)
	case ProtocolICMP:
		return ProbeICMP(ctx, spec)
	default:
		return ProbeResult{Message: "unsupported healthcheck protocol"}
	}
}

func ProbeTCP(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	target := strings.TrimSpace(spec.Target)
	if target == "" {
		return ProbeResult{Message: "target is required"}
	}
	port := spec.Port
	if port == 0 {
		port = 443
	}
	conn, err := dialContext(ctx, spec, "tcp", net.JoinHostPort(target, fmt.Sprint(port)))
	if err != nil {
		return resultFromError(ctx, err)
	}
	_ = conn.Close()
	return ProbeResult{OK: true, Message: "tcp connect succeeded"}
}

func ProbeDNS(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	target := strings.TrimSpace(spec.Target)
	if target == "" {
		return ProbeResult{Message: "target is required"}
	}
	port := spec.Port
	if port == 0 {
		port = 53
	}
	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialContext(ctx, spec, network, net.JoinHostPort(target, fmt.Sprint(port)))
		},
	}
	if _, err := resolver.LookupIP(ctx, "ip4", "example.com"); err != nil {
		return resultFromError(ctx, err)
	}
	return ProbeResult{OK: true, Message: "dns lookup succeeded"}
}

func ProbeHTTP(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	target := strings.TrimSpace(spec.Target)
	if target == "" {
		return ProbeResult{Message: "target is required"}
	}
	port := spec.Port
	if port == 0 {
		port = 80
	}
	url := "http://" + net.JoinHostPort(target, fmt.Sprint(port)) + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeResult{Message: err.Error()}
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialContext(ctx, spec, network, address)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return resultFromError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return ProbeResult{OK: true, Message: fmt.Sprintf("http status %d", resp.StatusCode)}
	}
	return ProbeResult{Message: fmt.Sprintf("http status %d", resp.StatusCode)}
}

func ProbeICMP(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	target := strings.TrimSpace(spec.Target)
	if target == "" {
		return ProbeResult{Message: "target is required"}
	}
	ip, network, err := resolveICMPTarget(ctx, target, spec.AddressFamily)
	if err != nil {
		return resultFromError(ctx, err)
	}
	var conn *icmp.PacketConn
	var messageType icmp.Type
	var replyType icmp.Type
	var protocol int
	if network == "ip4:icmp" {
		conn, err = icmp.ListenPacket(network, listenAddress(spec, "0.0.0.0"))
		messageType = ipv4.ICMPTypeEcho
		replyType = ipv4.ICMPTypeEchoReply
		protocol = 1
	} else {
		conn, err = icmp.ListenPacket(network, listenAddress(spec, "::"))
		messageType = ipv6.ICMPTypeEchoRequest
		replyType = ipv6.ICMPTypeEchoReply
		protocol = 58
	}
	if err != nil {
		return resultFromError(ctx, err)
	}
	defer conn.Close()
	identifier := os.Getpid() & 0xffff
	msg := icmp.Message{
		Type: messageType,
		Code: 0,
		Body: &icmp.Echo{ID: identifier, Seq: 1, Data: []byte("routerd-healthcheck")},
	}
	payload, err := msg.Marshal(nil)
	if err != nil {
		return ProbeResult{Message: err.Error()}
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.WriteTo(payload, &net.IPAddr{IP: ip}); err != nil {
		return resultFromError(ctx, err)
	}
	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return resultFromError(ctx, err)
		}
		parsed, err := icmp.ParseMessage(protocol, buf[:n])
		if err != nil {
			continue
		}
		echo, ok := parsed.Body.(*icmp.Echo)
		if parsed.Type == replyType && ok && echo.ID == identifier {
			return ProbeResult{OK: true, Message: "icmp echo succeeded from " + peer.String()}
		}
	}
}

func dialContext(ctx context.Context, spec api.HealthCheckSpec, network, address string) (net.Conn, error) {
	dialer, err := dialerForSpec(spec, network)
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, network, address)
}

func dialerForSpec(spec api.HealthCheckSpec, network string) (net.Dialer, error) {
	dialer := net.Dialer{}
	if spec.SourceAddress != "" {
		addr := net.ParseIP(spec.SourceAddress)
		if addr == nil {
			return net.Dialer{}, fmt.Errorf("sourceAddress %q is not an IP address", spec.SourceAddress)
		}
		switch {
		case strings.HasPrefix(network, "tcp"):
			dialer.LocalAddr = &net.TCPAddr{IP: addr}
		case strings.HasPrefix(network, "udp"):
			dialer.LocalAddr = &net.UDPAddr{IP: addr}
		default:
			dialer.LocalAddr = &net.IPAddr{IP: addr}
		}
	}
	if spec.SourceInterface != "" {
		ifname := spec.SourceInterface
		dialer.Control = func(network, address string, conn syscall.RawConn) error {
			return bindToDevice(conn, ifname)
		}
	}
	return dialer, nil
}

func resolveInterfaceName(router *api.Router, name string) string {
	name = strings.TrimSpace(name)
	if router == nil || name == "" {
		return name
	}
	for _, resource := range router.Spec.Resources {
		if resource.Metadata.Name != name {
			continue
		}
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil && strings.TrimSpace(spec.IfName) != "" {
				return spec.IfName
			}
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			if err == nil {
				return firstNonEmpty(spec.TunnelName, resource.Metadata.Name)
			}
		case "PPPoEInterface":
			spec, err := resource.PPPoEInterfaceSpec()
			if err == nil {
				return firstNonEmpty(spec.IfName, "ppp-"+resource.Metadata.Name)
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil {
				return firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "VRF":
			spec, err := resource.VRFSpec()
			if err == nil {
				return firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "VXLANTunnel":
			spec, err := resource.VXLANTunnelSpec()
			if err == nil {
				return firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "WireGuardInterface":
			return resource.Metadata.Name
		}
	}
	return name
}

func listenAddress(spec api.HealthCheckSpec, fallback string) string {
	if spec.SourceAddress == "" {
		return fallback
	}
	return spec.SourceAddress
}

func resultFromError(ctx context.Context, err error) ProbeResult {
	if ctx.Err() == context.DeadlineExceeded {
		return ProbeResult{Timeout: true, Message: err.Error()}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ProbeResult{Timeout: true, Message: err.Error()}
	}
	return ProbeResult{Message: err.Error()}
}

func ApplyResult(resource api.Resource, spec api.HealthCheckSpec, state State, result ProbeResult, now time.Time) Evaluation {
	if state.Phase == "" {
		state.Phase = PhaseUnknown
		state.LastTransitionAt = now
	}
	nextResult := ResultFailed
	if result.Timeout {
		nextResult = ResultTimeout
	} else if result.OK {
		nextResult = ResultPassed
	}
	state.LastResult = nextResult
	state.LastMessage = result.Message
	state.LastCheckedAt = now
	if result.OK {
		state.ConsecutivePassed++
		state.ConsecutiveFailed = 0
		if state.ConsecutivePassed >= healthyThreshold(spec) {
			transition(&state, PhaseHealthy, now)
		} else {
			transition(&state, PhasePassing, now)
		}
	} else {
		state.ConsecutiveFailed++
		state.ConsecutivePassed = 0
		if state.ConsecutiveFailed >= unhealthyThreshold(spec) {
			transition(&state, PhaseUnhealthy, now)
		} else {
			transition(&state, PhaseFailing, now)
		}
	}
	status := StatusMap(state)
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: DaemonKind + "-" + resource.Metadata.Name, Kind: DaemonKind, Instance: resource.Metadata.Name}, "routerd.healthcheck."+resource.Metadata.Name+"."+nextResult, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: resource.Metadata.Name}
	event.Reason = "HealthCheckProbe"
	event.Message = state.LastMessage
	event.Attributes = map[string]string{
		"phase":             state.Phase,
		"result":            nextResult,
		"consecutivePassed": fmt.Sprint(state.ConsecutivePassed),
		"consecutiveFailed": fmt.Sprint(state.ConsecutiveFailed),
		"network.address":   spec.Target,
		"network.protocol":  defaultString(spec.Protocol, protocolFromType(spec.Type)),
	}
	for key, value := range map[string]string{
		"network.via":            spec.Via,
		"network.interface.name": spec.SourceInterface,
		"network.local.address":  spec.SourceAddress,
	} {
		if value != "" {
			event.Attributes[key] = value
			status[key] = value
		}
	}
	return Evaluation{State: state, Result: nextResult, Event: event, Status: status}
}

func StatusMap(state State) map[string]any {
	status := map[string]any{
		"phase":             state.Phase,
		"lastResult":        state.LastResult,
		"lastCheckedAt":     state.LastCheckedAt.UTC().Format(time.RFC3339Nano),
		"lastTransitionAt":  state.LastTransitionAt.UTC().Format(time.RFC3339Nano),
		"consecutivePassed": state.ConsecutivePassed,
		"consecutiveFailed": state.ConsecutiveFailed,
	}
	if state.LastMessage != "" {
		status["message"] = state.LastMessage
	}
	return status
}

func transition(state *State, phase string, now time.Time) {
	if state.Phase != phase {
		state.Phase = phase
		state.LastTransitionAt = now
	}
}

func (c *Controller) resourceState(name string) *State {
	c.init()
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state[name]
	if state == nil {
		state = &State{Phase: PhaseUnknown}
		c.state[name] = state
	}
	return state
}

func (c *Controller) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		c.state = map[string]*State{}
	}
}

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func durationOr(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func healthyThreshold(spec api.HealthCheckSpec) int {
	if spec.HealthyThreshold > 0 {
		return spec.HealthyThreshold
	}
	return 1
}

func unhealthyThreshold(spec api.HealthCheckSpec) int {
	if spec.UnhealthyThreshold > 0 {
		return spec.UnhealthyThreshold
	}
	return 3
}

func protocolFromType(value string) string {
	if value == "" || value == "ping" {
		return ProtocolICMP
	}
	return ProtocolTCP
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveICMPTarget(ctx context.Context, target, family string) (net.IP, string, error) {
	if ip := net.ParseIP(target); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4, "ip4:icmp", nil
		}
		return ip.To16(), "ip6:ipv6-icmp", nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, target)
	if err != nil {
		return nil, "", err
	}
	for _, addr := range addrs {
		if family == "ipv6" {
			if addr.IP.To4() == nil {
				return addr.IP, "ip6:ipv6-icmp", nil
			}
			continue
		}
		if addr.IP.To4() != nil {
			return addr.IP.To4(), "ip4:icmp", nil
		}
		if family != "ipv4" {
			return addr.IP, "ip6:ipv6-icmp", nil
		}
	}
	return nil, "", fmt.Errorf("no %s address found for %s", defaultString(family, "IP"), target)
}
