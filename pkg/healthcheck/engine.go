// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/resourcequery"
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
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type ProbeResult struct {
	OK      bool
	Timeout bool
	Message string

	// ProbeEvidence carries the egress / source / route information that was
	// in effect for this probe attempt. It is populated by the Probe
	// implementations and by the controller after the probe returns (route
	// lookup runs in the controller path so it is hookable by tests).
	ProbeEvidence
}

type State struct {
	Phase             string    `json:"phase"`
	LastResult        string    `json:"lastResult,omitempty"`
	LastMessage       string    `json:"message,omitempty"`
	LastTransitionAt  time.Time `json:"lastTransitionAt,omitempty"`
	LastCheckedAt     time.Time `json:"lastCheckedAt,omitempty"`
	ConsecutivePassed int       `json:"consecutivePassed,omitempty"`
	ConsecutiveFailed int       `json:"consecutiveFailed,omitempty"`

	// Failure-history bookkeeping. FailureCount totals only consecutive
	// failures; reset on the next success so it matches "ongoing outage
	// length". LastSuccessTime/LastFailureTime are absolute timestamps;
	// FirstFailureTime is the start of the current failure streak.
	FirstFailureTime time.Time `json:"firstFailureTime,omitempty"`
	LastFailureTime  time.Time `json:"lastFailureTime,omitempty"`
	LastSuccessTime  time.Time `json:"lastSuccessTime,omitempty"`
	FailureCount     int       `json:"failureCount,omitempty"`

	// History is the rolling list of the most recent probe records. The
	// length is bounded by historyLimit (default 20, overridable via the
	// ROUTERD_HEALTHCHECK_HISTORY env var). Persisted in the state file so
	// it survives daemon restarts.
	History []ProbeRecord `json:"history,omitempty"`

	// LastEvidence is the egress / source / route snapshot from the most
	// recent probe. We keep it on the top-level state in addition to the
	// rolling history so consumers that only render the current state still
	// see the latest evidence without paging History.
	LastEvidence ProbeEvidence `json:"lastEvidence,omitempty"`
}

type Evaluation struct {
	State  State
	Result string
	Event  daemonapi.DaemonEvent
}

// EnrichEvidence fills route / tunnel information into the evidence
// returned by the probe. Probe implementations focus on socket-level data
// (source address selected by the dialer); EnrichEvidence asks the kernel
// what egress it would actually use and what the underlying tunnel local /
// remote addresses are. It is split out so non-controller callers (notably
// the standalone daemon) can call it too.
func EnrichEvidence(ctx context.Context, spec api.HealthCheckSpec, ev ProbeEvidence) ProbeEvidence {
	ev = mergeEvidence(spec, ev)
	if ev.NextHop == "" && ev.OutInterface == "" && ev.RouteSource == "" {
		if info, err := RouteLookup(ctx, spec); err == nil {
			if ev.NextHop == "" {
				ev.NextHop = info.NextHop
			}
			if ev.OutInterface == "" {
				ev.OutInterface = info.OutInterface
			}
			if ev.RouteSource == "" {
				ev.RouteSource = info.Source
			}
		}
	}
	return ev
}

// FwMarkRef describes a routing resource that can supply a socket mark for a
// HealthCheck probe.
type FwMarkRef struct {
	Resource string
	Name     string
	Mark     int
}

// ResolveSpec converts routerd resource names to the OS interface names used
// by probe runtime calls. Standalone daemon flags still accept raw OS names.
func ResolveSpec(router *api.Router, spec api.HealthCheckSpec) api.HealthCheckSpec {
	if strings.TrimSpace(spec.SourceInterface) != "" {
		spec.SourceInterface = resolveInterfaceName(router, spec.SourceInterface)
	}
	return spec
}

func ResolveSpecForResource(router *api.Router, resourceName string, spec api.HealthCheckSpec) api.HealthCheckSpec {
	if strings.TrimSpace(spec.SourceInterface) == "" {
		if ifname, ok := DerivedSourceInterface(router, resourceName); ok {
			spec.SourceInterface = ifname
		}
	}
	if spec.FwMark == 0 {
		if mark, ok := DerivedFwMark(router, resourceName); ok {
			spec.FwMark = mark
		}
	}
	return ResolveSpec(router, spec)
}

func DerivedSourceInterface(router *api.Router, healthCheckName string) (string, bool) {
	healthCheckName = strings.TrimSpace(healthCheckName)
	if router == nil || healthCheckName == "" {
		return "", false
	}
	var source string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := resource.EgressRoutePolicySpec()
		if err != nil {
			continue
		}
		for _, candidate := range spec.Candidates {
			if strings.TrimSpace(candidate.HealthCheck) == healthCheckName {
				if !sameDerivedSourceInterface(&source, candidate.EffectiveInterface()) {
					return "", false
				}
			}
			for _, target := range candidate.Targets {
				if strings.TrimSpace(target.HealthCheck) != healthCheckName {
					continue
				}
				if !sameDerivedSourceInterface(&source, target.EffectiveInterface()) {
					return "", false
				}
			}
		}
	}
	return source, source != ""
}

func sameDerivedSourceInterface(current *string, next string) bool {
	next = strings.TrimSpace(next)
	if next == "" {
		return true
	}
	if *current == "" {
		*current = next
		return true
	}
	return *current == next
}

func ResolveSpecWithStore(router *api.Router, store Store, spec api.HealthCheckSpec) api.HealthCheckSpec {
	spec = ResolveSpec(router, spec)
	if strings.TrimSpace(spec.SourceAddress) == "" && strings.TrimSpace(spec.SourceAddressFrom.Resource) != "" && store != nil {
		spec.SourceAddress = statusvalue.Address(resourcequery.Value(store, spec.SourceAddressFrom))
	}
	return spec
}

func ResolveSpecWithStoreForResource(router *api.Router, store Store, resourceName string, spec api.HealthCheckSpec) api.HealthCheckSpec {
	spec = ResolveSpecForResource(router, resourceName, spec)
	if strings.TrimSpace(spec.SourceAddress) == "" && strings.TrimSpace(spec.SourceAddressFrom.Resource) != "" && store != nil {
		spec.SourceAddress = statusvalue.Address(resourcequery.Value(store, spec.SourceAddressFrom))
	}
	return spec
}

func DerivedFwMark(router *api.Router, healthCheckName string) (int, bool) {
	refs := DerivedFwMarkRefs(router, healthCheckName)
	var mark int
	for _, ref := range refs {
		if ref.Mark == 0 {
			continue
		}
		if mark == 0 {
			mark = ref.Mark
			continue
		}
		if mark != ref.Mark {
			return 0, false
		}
	}
	return mark, mark != 0
}

func DerivedFwMarkRefs(router *api.Router, healthCheckName string) []FwMarkRef {
	healthCheckName = strings.TrimSpace(healthCheckName)
	if router == nil || healthCheckName == "" {
		return nil
	}
	var refs []FwMarkRef
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion {
			continue
		}
		switch resource.Kind {
		case "EgressRoutePolicy":
			spec, err := resource.EgressRoutePolicySpec()
			if err != nil {
				continue
			}
			for _, candidate := range spec.Candidates {
				if strings.TrimSpace(candidate.HealthCheck) == healthCheckName {
					refs = append(refs, FwMarkRef{
						Resource: resource.ID(),
						Name:     strings.TrimSpace(candidate.Name),
						Mark:     candidate.Mark,
					})
				}
				for _, target := range candidate.Targets {
					if strings.TrimSpace(target.HealthCheck) != healthCheckName {
						continue
					}
					refs = append(refs, FwMarkRef{
						Resource: resource.ID(),
						Name:     strings.TrimSpace(target.Name),
						Mark:     target.Mark,
					})
				}
			}
		}
	}
	return refs
}

func Probe(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	var result ProbeResult
	switch defaultString(spec.Protocol, protocolFromType(spec.Type)) {
	case ProtocolTCP:
		result = ProbeTCP(ctx, spec)
	case ProtocolDNS:
		result = ProbeDNS(ctx, spec)
	case ProtocolHTTP:
		result = ProbeHTTP(ctx, spec)
	case ProtocolICMP:
		result = ProbeICMP(ctx, spec)
	default:
		return ProbeResult{Message: "unsupported healthcheck protocol"}
	}
	result.ProbeEvidence = mergeEvidence(spec, result.ProbeEvidence)
	if !result.OK && result.FailureKind == "" {
		result.FailureKind = classifyResultMessage(ctx, result)
	}
	return result
}

// mergeEvidence overlays the spec-derived evidence (egress interface,
// configured source address, tunnel hints) on top of whatever the probe
// already filled in. The probe-specific data wins — it reflects the actual
// runtime — so we only fill empties from the spec.
func mergeEvidence(spec api.HealthCheckSpec, ev ProbeEvidence) ProbeEvidence {
	if ev.EgressInterface == "" {
		ev.EgressInterface = spec.SourceInterface
	}
	if ev.SourceAddress == "" {
		ev.SourceAddress = spec.SourceAddress
	}
	if ev.SourceOrigin == "" && spec.SourceAddress != "" && strings.TrimSpace(spec.SourceAddressFrom.Resource) != "" {
		// SourceAddress came from a status-driven binding (PD / RA / dynamic
		// DHCP). The exact origin can be derived from the source resource kind
		// in DerivedSourceOrigin; we leave it best-effort and let the caller
		// override.
	}
	return ev
}

// classifyResultMessage turns a free-form probe error message into a
// FailureKind. It is the fallback path for probes that did not classify the
// error themselves (e.g. ICMP "no IP found" branches).
func classifyResultMessage(ctx context.Context, result ProbeResult) string {
	if result.OK {
		return FailureKindNone
	}
	if result.Timeout {
		return FailureKindTimeout
	}
	if result.Message == "" {
		return FailureKindOther
	}
	// classifyError works off an error value; wrap message in a stub error so
	// we can reuse the substring rules.
	return classifyError(ctx, errors.New(result.Message))
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
		DisableKeepAlives: true,
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

func dialContext(ctx context.Context, spec api.HealthCheckSpec, network, address string) (net.Conn, error) {
	dialer, err := dialerForSpec(spec, network, address)
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, network, address)
}

func dialerForSpec(spec api.HealthCheckSpec, network, address string) (net.Dialer, error) {
	dialer := net.Dialer{}
	hasSourceAddress := false
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
		hasSourceAddress = true
	}
	if spec.SourceInterface != "" || spec.FwMark != 0 {
		if err := configureDialerSocket(&dialer, spec.SourceInterface, spec.FwMark, network, address, spec.AddressFamily, hasSourceAddress); err != nil {
			return net.Dialer{}, err
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
				return stringutil.FirstNonEmpty(spec.TunnelName, resource.Metadata.Name)
			}
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err == nil {
				return stringutil.FirstNonEmpty(spec.IfName, "ppp-"+resource.Metadata.Name)
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil {
				return stringutil.FirstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "VRF":
			spec, err := resource.VRFSpec()
			if err == nil {
				return stringutil.FirstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "VXLANTunnel":
			spec, err := resource.VXLANTunnelSpec()
			if err == nil {
				return stringutil.FirstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "WireGuardInterface":
			return resource.Metadata.Name
		}
	}
	return name
}

func resultFromError(ctx context.Context, err error) ProbeResult {
	kind := classifyError(ctx, err)
	if ctx.Err() == context.DeadlineExceeded {
		return ProbeResult{Timeout: true, Message: err.Error(), ProbeEvidence: ProbeEvidence{FailureKind: FailureKindTimeout}}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ProbeResult{Timeout: true, Message: err.Error(), ProbeEvidence: ProbeEvidence{FailureKind: FailureKindTimeout}}
	}
	return ProbeResult{Message: err.Error(), ProbeEvidence: ProbeEvidence{FailureKind: kind}}
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
	state.LastEvidence = result.ProbeEvidence
	if result.OK {
		state.ConsecutivePassed++
		state.ConsecutiveFailed = 0
		state.LastSuccessTime = now
		// A recovery clears the failure streak start. FailureCount is only the
		// length of the current outage so it goes to zero too.
		state.FirstFailureTime = time.Time{}
		state.FailureCount = 0
		if state.ConsecutivePassed >= healthyThreshold(spec) {
			transition(&state, PhaseHealthy, now)
		} else {
			transition(&state, PhasePassing, now)
		}
	} else {
		state.ConsecutiveFailed++
		state.ConsecutivePassed = 0
		state.LastFailureTime = now
		state.FailureCount = state.ConsecutiveFailed
		if state.FirstFailureTime.IsZero() {
			state.FirstFailureTime = now
		}
		if state.ConsecutiveFailed >= unhealthyThreshold(spec) {
			transition(&state, PhaseUnhealthy, now)
		} else if state.Phase == PhaseHealthy || state.Phase == PhaseFailing {
			transition(&state, PhaseFailing, now)
		} else {
			transition(&state, PhaseUnknown, now)
		}
	}
	state.History = appendHistory(state.History, recordFromResult(spec, result, nextResult, now))
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: DaemonKind + "-" + resource.Metadata.Name, Kind: DaemonKind, Instance: resource.Metadata.Name}, "routerd.healthcheck."+resource.Metadata.Name+"."+nextResult, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: resource.Metadata.Name}
	event.Reason = "HealthCheckProbe"
	event.Message = state.LastMessage
	event.Attributes = map[string]string{
		"phase":             state.Phase,
		"result":            nextResult,
		"consecutivePassed": fmt.Sprint(state.ConsecutivePassed),
		"consecutiveFailed": fmt.Sprint(state.ConsecutiveFailed),
		"failureCount":      fmt.Sprint(state.FailureCount),
		"network.address":   spec.Target,
		"network.protocol":  effectiveProtocol(spec.Protocol, spec.Type),
	}
	if spec.Port != 0 {
		event.Attributes["network.peer.port"] = fmt.Sprint(spec.Port)
	}
	for key, value := range map[string]string{
		"network.via":            spec.Via,
		"network.fwmark":         formatFwMark(spec.FwMark),
		"network.interface.name": spec.SourceInterface,
		"network.local.address":  spec.SourceAddress,
	} {
		if value != "" {
			event.Attributes[key] = value
		}
	}
	for key, value := range evidenceAttributes(result.ProbeEvidence) {
		event.Attributes[key] = value
	}
	addTimeAttribute(event.Attributes, "lastSuccessAt", state.LastSuccessTime)
	addTimeAttribute(event.Attributes, "lastFailureAt", state.LastFailureTime)
	addTimeAttribute(event.Attributes, "firstFailureAt", state.FirstFailureTime)
	return Evaluation{State: state, Result: nextResult, Event: event}
}

// evidenceAttributes returns a map keyed with OTel-style network.* names so
// the event payload is easy to filter on. Empty fields are skipped to keep
// the payload tidy.
func evidenceAttributes(ev ProbeEvidence) map[string]string {
	out := map[string]string{}
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		out[key] = value
	}
	add("routerd.healthcheck.failureKind", ev.FailureKind)
	add("network.egress.interface", ev.EgressInterface)
	add("network.source.address", ev.SourceAddress)
	add("network.source.origin", ev.SourceOrigin)
	add("network.nexthop.address", ev.NextHop)
	add("network.out.interface", ev.OutInterface)
	add("network.route.source", ev.RouteSource)
	add("network.tunnel.local", ev.TunnelLocal)
	add("network.tunnel.remote", ev.TunnelRemote)
	return out
}

func addTimeAttribute(attrs map[string]string, key string, t time.Time) {
	if t.IsZero() {
		return
	}
	attrs[key] = t.UTC().Format(time.RFC3339Nano)
}

func formatFwMark(mark int) string {
	if mark == 0 {
		return ""
	}
	return fmt.Sprintf("0x%x", mark)
}

func StatusMap(state State) map[string]any {
	status := map[string]any{
		"phase":             state.Phase,
		"lastResult":        state.LastResult,
		"lastCheckedAt":     state.LastCheckedAt.UTC().Format(time.RFC3339Nano),
		"lastTransitionAt":  state.LastTransitionAt.UTC().Format(time.RFC3339Nano),
		"consecutivePassed": state.ConsecutivePassed,
		"consecutiveFailed": state.ConsecutiveFailed,
		"failureCount":      state.FailureCount,
	}
	if state.LastMessage != "" {
		status["message"] = state.LastMessage
	}
	if !state.LastSuccessTime.IsZero() {
		status["lastSuccessTime"] = state.LastSuccessTime.UTC().Format(time.RFC3339Nano)
	}
	if !state.LastFailureTime.IsZero() {
		status["lastFailureTime"] = state.LastFailureTime.UTC().Format(time.RFC3339Nano)
	}
	if !state.FirstFailureTime.IsZero() {
		status["firstFailureTime"] = state.FirstFailureTime.UTC().Format(time.RFC3339Nano)
	}
	if ev := state.LastEvidence; ev != (ProbeEvidence{}) {
		status["lastEvidence"] = evidenceStatus(ev)
	}
	if len(state.History) > 0 {
		entries := make([]map[string]any, 0, len(state.History))
		for _, record := range state.History {
			entries = append(entries, historyStatus(record))
		}
		status["history"] = entries
	}
	return status
}

// evidenceStatus renders ProbeEvidence as a map[string]any suitable for
// status JSON. The keys mirror the JSON tags on ProbeEvidence so consumers
// see a stable shape.
func evidenceStatus(ev ProbeEvidence) map[string]any {
	out := map[string]any{}
	if ev.FailureKind != "" {
		out["failureKind"] = ev.FailureKind
	}
	if ev.EgressInterface != "" {
		out["egressInterface"] = ev.EgressInterface
	}
	if ev.SourceAddress != "" {
		out["sourceAddress"] = ev.SourceAddress
	}
	if ev.SourceOrigin != "" {
		out["sourceOrigin"] = ev.SourceOrigin
	}
	if ev.NextHop != "" {
		out["nextHop"] = ev.NextHop
	}
	if ev.OutInterface != "" {
		out["outInterface"] = ev.OutInterface
	}
	if ev.RouteSource != "" {
		out["routeSource"] = ev.RouteSource
	}
	if ev.TunnelLocal != "" {
		out["tunnelLocal"] = ev.TunnelLocal
	}
	if ev.TunnelRemote != "" {
		out["tunnelRemote"] = ev.TunnelRemote
	}
	return out
}

func historyStatus(record ProbeRecord) map[string]any {
	out := map[string]any{
		"time":   record.Time.UTC().Format(time.RFC3339Nano),
		"ok":     record.OK,
		"result": record.Result,
	}
	if record.Timeout {
		out["timeout"] = true
	}
	if record.Message != "" {
		out["message"] = record.Message
	}
	if record.Target != "" {
		out["target"] = record.Target
	}
	if record.Protocol != "" {
		out["protocol"] = record.Protocol
	}
	if record.Port != 0 {
		out["port"] = record.Port
	}
	if ev := record.ProbeEvidence; ev != (ProbeEvidence{}) {
		out["evidence"] = evidenceStatus(ev)
	}
	return out
}

func transition(state *State, phase string, now time.Time) {
	if state.Phase != phase {
		state.Phase = phase
		state.LastTransitionAt = now
	}
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
