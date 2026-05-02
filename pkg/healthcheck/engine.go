package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
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

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Probe  ProbeFunc
	Now    func() time.Time
	Logger *slog.Logger

	mu    sync.Mutex
	state map[string]*checkState
}

type checkState struct {
	phase             string
	lastResult        string
	lastMessage       string
	lastTransitionAt  time.Time
	lastCheckedAt     time.Time
	consecutivePassed int
	consecutiveFailed int
}

type ProbeDaemon interface {
	Run(ctx context.Context, resource api.Resource) error
	Status(ctx context.Context) (daemonapi.DaemonStatus, error)
}

func (c *Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "HealthCheck" {
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

	if state.phase == "" {
		state.phase = PhaseUnknown
		state.lastTransitionAt = now
	}
	nextResult := ResultFailed
	if result.Timeout {
		nextResult = ResultTimeout
	} else if result.OK {
		nextResult = ResultPassed
	}
	state.lastResult = nextResult
	state.lastMessage = result.Message
	state.lastCheckedAt = now
	if result.OK {
		state.consecutivePassed++
		state.consecutiveFailed = 0
		if state.consecutivePassed >= healthyThreshold(spec) {
			c.transition(state, PhaseHealthy, now)
		} else {
			c.transition(state, PhasePassing, now)
		}
	} else {
		state.consecutiveFailed++
		state.consecutivePassed = 0
		if state.consecutiveFailed >= unhealthyThreshold(spec) {
			c.transition(state, PhaseUnhealthy, now)
		} else {
			c.transition(state, PhaseFailing, now)
		}
	}
	status := map[string]any{
		"phase":             state.phase,
		"lastResult":        state.lastResult,
		"lastCheckedAt":     state.lastCheckedAt.UTC().Format(time.RFC3339Nano),
		"lastTransitionAt":  state.lastTransitionAt.UTC().Format(time.RFC3339Nano),
		"consecutivePassed": state.consecutivePassed,
		"consecutiveFailed": state.consecutiveFailed,
	}
	if state.lastMessage != "" {
		status["message"] = state.lastMessage
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", resource.Metadata.Name, status); err != nil {
		return err
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: DaemonKind + "-" + resource.Metadata.Name, Kind: DaemonKind, Instance: resource.Metadata.Name}, "routerd.healthcheck."+resource.Metadata.Name+"."+nextResult, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: resource.Metadata.Name}
	event.Reason = "HealthCheckProbe"
	event.Message = state.lastMessage
	event.Attributes = map[string]string{
		"phase":             state.phase,
		"result":            nextResult,
		"consecutivePassed": fmt.Sprint(state.consecutivePassed),
		"consecutiveFailed": fmt.Sprint(state.consecutiveFailed),
	}
	return c.Bus.Publish(ctx, event)
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
		return ProbeResult{Message: "icmp requires the external routerd-healthcheck daemon"}
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
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(target, fmt.Sprint(port)))
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
	dialer := net.Dialer{}
	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(target, fmt.Sprint(port)))
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return resultFromError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return ProbeResult{OK: true, Message: fmt.Sprintf("http status %d", resp.StatusCode)}
	}
	return ProbeResult{Message: fmt.Sprintf("http status %d", resp.StatusCode)}
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

func (c *Controller) transition(state *checkState, phase string, now time.Time) {
	if state.phase != phase {
		state.phase = phase
		state.lastTransitionAt = now
	}
}

func (c *Controller) resourceState(name string) *checkState {
	c.init()
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state[name]
	if state == nil {
		state = &checkState{phase: PhaseUnknown}
		c.state[name] = state
	}
	return state
}

func (c *Controller) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		c.state = map[string]*checkState{}
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
