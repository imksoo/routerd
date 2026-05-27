// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// FailureKind classifies a probe failure into a stable, machine-readable
// category. The values are intentionally narrow so that operators can grep
// the State history for them.
const (
	FailureKindNone               = ""
	FailureKindTimeout            = "timeout"
	FailureKindConnectionRefused  = "connection_refused"
	FailureKindNetworkUnreachable = "network_unreachable"
	FailureKindHostUnreachable    = "host_unreachable"
	FailureKindNoRoute            = "no_route"
	FailureKindDNSError           = "dns_error"
	FailureKindTLSError           = "tls_error"
	FailureKindAddressInUse       = "address_in_use"
	FailureKindPermission         = "permission"
	FailureKindOther              = "other"
)

// SourceOrigin describes where the SourceAddress came from. The set is
// intentionally small and matches the user-facing wording in the issue.
const (
	SourceOriginUnknown = ""
	SourceOriginPD      = "pd"
	SourceOriginRA      = "ra"
	SourceOriginStatic  = "static"
	SourceOriginDynamic = "dynamic"
)

// ProbeEvidence captures the egress context that surrounded a probe attempt.
// It is embedded in ProbeResult so existing ProbeResult callers can keep the
// flat shape, and into ProbeRecord for history persistence.
type ProbeEvidence struct {
	FailureKind     string `json:"failureKind,omitempty"`
	EgressInterface string `json:"egressInterface,omitempty"`
	SourceAddress   string `json:"sourceAddress,omitempty"`
	SourceOrigin    string `json:"sourceOrigin,omitempty"`
	NextHop         string `json:"nextHop,omitempty"`
	OutInterface    string `json:"outInterface,omitempty"`
	RouteSource     string `json:"routeSource,omitempty"`
	TunnelLocal     string `json:"tunnelLocal,omitempty"`
	TunnelRemote    string `json:"tunnelRemote,omitempty"`
}

// ProbeRecord is one entry of probe history. We keep the latest N records on
// State.History so an operator can answer the "what egress did we use when
// this probe failed at 13:42?" question without trawling logs.
type ProbeRecord struct {
	Time     time.Time `json:"time"`
	OK       bool      `json:"ok"`
	Timeout  bool      `json:"timeout,omitempty"`
	Result   string    `json:"result,omitempty"`
	Message  string    `json:"message,omitempty"`
	Target   string    `json:"target,omitempty"`
	Protocol string    `json:"protocol,omitempty"`
	Port     int       `json:"port,omitempty"`

	ProbeEvidence
}

// HistoryEntries returns a defensive copy. State exposes the slice directly
// in JSON so this is mostly a helper for tests and callers that mutate.
func (s State) HistoryEntries() []ProbeRecord {
	if len(s.History) == 0 {
		return nil
	}
	out := make([]ProbeRecord, len(s.History))
	copy(out, s.History)
	return out
}

// classifyError maps an error returned from the probe into a stable
// FailureKind. We use errors.Is for the well-known sentinels and fall back
// to substring matching for anything more exotic. The empty string means
// "could not classify" — callers should still keep the raw message.
func classifyError(ctx context.Context, err error) string {
	if err == nil {
		return FailureKindNone
	}
	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return FailureKindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return FailureKindTimeout
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return FailureKindConnectionRefused
	case errors.Is(err, syscall.ENETUNREACH):
		return FailureKindNetworkUnreachable
	case errors.Is(err, syscall.EHOSTUNREACH):
		return FailureKindHostUnreachable
	case errors.Is(err, syscall.EADDRINUSE):
		return FailureKindAddressInUse
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return FailureKindPermission
	case errors.Is(err, os.ErrDeadlineExceeded):
		return FailureKindTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTimeout {
			return FailureKindTimeout
		}
		return FailureKindDNSError
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "tls"):
		return FailureKindTLSError
	case strings.Contains(msg, "no route to host"):
		return FailureKindHostUnreachable
	case strings.Contains(msg, "network is unreachable"):
		return FailureKindNetworkUnreachable
	case strings.Contains(msg, "connection refused"):
		return FailureKindConnectionRefused
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "dns"):
		return FailureKindDNSError
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"):
		return FailureKindTimeout
	case strings.Contains(msg, "permission denied"):
		return FailureKindPermission
	}
	return FailureKindOther
}

// RouteInfo carries the information returned from a routing lookup. All
// fields are optional — populated entries are added to ProbeEvidence.
type RouteInfo struct {
	NextHop      string
	OutInterface string
	Source       string
}

// RouteLookup resolves "ip route get TARGET" style information for a probe.
// Tests can override the default by replacing this var.
var RouteLookup = func(ctx context.Context, target, family string) (RouteInfo, error) {
	return lookupRoute(ctx, target, family)
}

// historyLimit returns the maximum number of probe history entries to keep.
// Defaults to 20 but can be overridden via ROUTERD_HEALTHCHECK_HISTORY.
func historyLimit() int {
	if value := strings.TrimSpace(os.Getenv("ROUTERD_HEALTHCHECK_HISTORY")); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			return n
		}
	}
	return 20
}

// appendHistory adds the record and trims the slice to the configured limit.
// It returns the new slice (the receiver may be nil).
func appendHistory(history []ProbeRecord, record ProbeRecord) []ProbeRecord {
	limit := historyLimit()
	history = append(history, record)
	if len(history) > limit {
		history = append([]ProbeRecord(nil), history[len(history)-limit:]...)
	}
	return history
}

// recordFromResult assembles a ProbeRecord from a probe spec, the result of
// that probe, and the timestamp the controller observed.
func recordFromResult(spec api.HealthCheckSpec, result ProbeResult, resultKind string, now time.Time) ProbeRecord {
	return ProbeRecord{
		Time:          now,
		OK:            result.OK,
		Timeout:       result.Timeout,
		Result:        resultKind,
		Message:       result.Message,
		Target:        spec.Target,
		Protocol:      effectiveProtocol(spec.Protocol, spec.Type),
		Port:          spec.Port,
		ProbeEvidence: result.ProbeEvidence,
	}
}

// effectiveProtocol returns the protocol string the probe actually used.
// The spec's protocol field can be empty (probes pick a default), so we keep
// the fallback logic in one place to share between probe code and event
// serialisation.
func effectiveProtocol(proto, typ string) string {
	if strings.TrimSpace(proto) != "" {
		return proto
	}
	if typ == "" || typ == "ping" {
		return ProtocolICMP
	}
	return ProtocolTCP
}
