// SPDX-License-Identifier: BSD-3-Clause

package conntracktuning

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
	routerotel "github.com/imksoo/routerd/pkg/otel"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Summary struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Window      string       `json:"window"`
	ApplyMode   string       `json:"applyMode"`
	AutoApply   bool         `json:"autoApply"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

type Suggestion struct {
	Application          string  `json:"application"`
	Protocol             string  `json:"protocol"`
	SysctlKey            string  `json:"sysctlKey"`
	RecommendedSeconds   int     `json:"recommendedSeconds"`
	BaselineSeconds      int     `json:"baselineSeconds"`
	ObservedFlows        int     `json:"observedFlows"`
	ExpiredFlows         int     `json:"expiredFlows"`
	OrphanReturns        int     `json:"orphanReturns"`
	DenyEvents           int     `json:"denyEvents"`
	AverageDurationSecs  int     `json:"averageDurationSeconds,omitempty"`
	OrphanRate           float64 `json:"orphanRate"`
	Rationale            string  `json:"rationale"`
	ProductionApplyGuard string  `json:"productionApplyGuard,omitempty"`
}

type Inputs struct {
	DPIFlows     []logstore.DPIFlowEntry
	FirewallLogs []logstore.FirewallLogEntry
	ExpiredFlows []logstore.ExpiredFlowEntry
	Now          time.Time
	Window       time.Duration
	AutoApply    bool
}

type groupStats struct {
	app           string
	protocol      string
	flows         int
	expired       int
	orphans       int
	denies        int
	durationTotal time.Duration
}

func Analyze(inputs Inputs) Summary {
	now := inputs.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	window := inputs.Window
	if window <= 0 {
		window = 24 * time.Hour
	}
	groups := map[string]*groupStats{}
	group := func(app, protocol string) *groupStats {
		app = normalizeApp(app, protocol)
		protocol = normalizeProtocol(protocol)
		key := protocol + "/" + app
		stat := groups[key]
		if stat == nil {
			stat = &groupStats{app: app, protocol: protocol}
			groups[key] = stat
		}
		return stat
	}
	for _, flow := range inputs.DPIFlows {
		stat := group(flow.AppName, flow.Protocol)
		stat.flows++
		if !flow.FirstSeen.IsZero() && !flow.LastSeen.IsZero() && flow.LastSeen.After(flow.FirstSeen) {
			stat.durationTotal += flow.LastSeen.Sub(flow.FirstSeen)
		}
	}
	for _, entry := range inputs.FirewallLogs {
		if !isDeny(entry.Action) {
			continue
		}
		stat := group(firewallApp(entry), entry.Protocol)
		stat.denies++
		if strings.EqualFold(strings.TrimSpace(entry.Correlation), "orphan_return") {
			stat.orphans++
		}
	}
	dpiByTuple := dpiFlowIndex(inputs.DPIFlows)
	for _, flow := range inputs.ExpiredFlows {
		stat := group(expiredFlowApp(flow, dpiByTuple), flow.Protocol)
		stat.expired++
	}
	suggestions := make([]Suggestion, 0, len(groups))
	for _, stat := range groups {
		if stat.flows == 0 && stat.orphans == 0 && stat.expired == 0 {
			continue
		}
		suggestions = append(suggestions, suggestion(stat))
	}
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].OrphanReturns != suggestions[j].OrphanReturns {
			return suggestions[i].OrphanReturns > suggestions[j].OrphanReturns
		}
		if suggestions[i].ObservedFlows != suggestions[j].ObservedFlows {
			return suggestions[i].ObservedFlows > suggestions[j].ObservedFlows
		}
		return suggestions[i].Protocol+"/"+suggestions[i].Application < suggestions[j].Protocol+"/"+suggestions[j].Application
	})
	if len(suggestions) > 8 {
		suggestions = suggestions[:8]
	}
	mode := "manual"
	if inputs.AutoApply {
		mode = "auto"
	}
	return Summary{
		GeneratedAt: now,
		Window:      window.String(),
		ApplyMode:   mode,
		AutoApply:   inputs.AutoApply,
		Suggestions: suggestions,
	}
}

func suggestion(stat *groupStats) Suggestion {
	baseline := baselineSeconds(stat.protocol)
	avg := 0
	if stat.flows > 0 && stat.durationTotal > 0 {
		avg = int(math.Round(stat.durationTotal.Seconds() / float64(stat.flows)))
	}
	orphanRate := 0.0
	if stat.denies > 0 {
		orphanRate = float64(stat.orphans) / float64(stat.denies)
	}
	recommended := recommendedSeconds(stat.app, stat.protocol, avg, orphanRate)
	rationale := "read-only suggestion from DPI flow duration and expired-return observations"
	if stat.orphans > 0 {
		rationale = "orphan returns observed; extend timeout if this traffic is expected"
	} else if recommended < baseline {
		rationale = "observed flows are short; shorter timeout can reduce conntrack table residency"
	}
	return Suggestion{
		Application:          stat.app,
		Protocol:             stat.protocol,
		SysctlKey:            sysctlKey(stat.protocol, stat.app),
		RecommendedSeconds:   recommended,
		BaselineSeconds:      baseline,
		ObservedFlows:        stat.flows,
		ExpiredFlows:         stat.expired,
		OrphanReturns:        stat.orphans,
		DenyEvents:           stat.denies,
		AverageDurationSecs:  avg,
		OrphanRate:           math.Round(orphanRate*1000) / 1000,
		Rationale:            rationale,
		ProductionApplyGuard: "manual approval required before sysctl apply",
	}
}

func recommendedSeconds(app, protocol string, avg int, orphanRate float64) int {
	protocol = normalizeProtocol(protocol)
	app = normalizeApp(app, protocol)
	if protocol == "udp" {
		base := 60
		switch app {
		case "dns":
			base = 30
		case "mdns", "ssdp", "netbios", "llmnr":
			base = 120
		case "wireguard", "tailscale", "ipsec", "stun", "sip", "rtp":
			base = 300
		}
		if orphanRate >= 0.20 {
			base *= 2
		}
		return clamp(base, 15, 900)
	}
	base := 300
	switch app {
	case "http":
		base = 120
	case "tls", "https":
		base = 300
	case "ssh", "rdp", "smb":
		base = 7200
	}
	if avg > 0 {
		base = maxInt(base, avg*2+30)
	}
	if orphanRate >= 0.20 {
		base *= 2
	}
	return clamp(base, 30, 86400)
}

func RecordMetrics(ctx context.Context, telemetry *routerotel.Runtime, summary Summary) {
	if telemetry == nil {
		return
	}
	timeoutGauge := telemetry.Gauge("routerd.conntrack.timeout.application")
	orphanGauge := telemetry.Float64Gauge("routerd.conntrack.expire.orphan_rate")
	for _, row := range summary.Suggestions {
		attrs := metric.WithAttributes(
			attribute.String("network.protocol.name", row.Application),
			attribute.String("network.transport", row.Protocol),
			attribute.String("routerd.conntrack.sysctl", row.SysctlKey),
		)
		if timeoutGauge != nil {
			timeoutGauge.Record(ctx, int64(row.RecommendedSeconds), attrs)
		}
		if orphanGauge != nil {
			orphanGauge.Record(ctx, row.OrphanRate, attrs)
		}
	}
}

func RecommendationForEvent(entry logstore.FirewallLogEntry) Suggestion {
	return suggestion(&groupStats{
		app:      normalizeApp(firewallApp(entry), entry.Protocol),
		protocol: normalizeProtocol(entry.Protocol),
		denies:   1,
		orphans:  boolInt(strings.EqualFold(strings.TrimSpace(entry.Correlation), "orphan_return")),
	})
}

func normalizeApp(app, protocol string) string {
	app = strings.ToLower(strings.TrimSpace(app))
	switch app {
	case "", "unknown", "unidentified":
		protocol = normalizeProtocol(protocol)
		if protocol != "" {
			return protocol
		}
		return "unknown"
	case "ssl", "https":
		return "tls"
	default:
		return app
	}
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func firewallApp(entry logstore.FirewallLogEntry) string {
	if entry.DPIApp != "" {
		return entry.DPIApp
	}
	switch {
	case entry.DPITLSSNI != "":
		return "tls"
	case entry.DPIHTTPHost != "":
		return "http"
	case entry.DPIDNSQuery != "":
		return "dns"
	default:
		return ""
	}
}

func expiredFlowApp(flow logstore.ExpiredFlowEntry, dpiByTuple map[string]string) string {
	for _, key := range []string{
		flowTupleKey(flow.Protocol, flow.OrigSrc, flow.OrigSrcPort, flow.OrigDst, flow.OrigDstPort),
		flowTupleKey(flow.Protocol, flow.OrigDst, flow.OrigDstPort, flow.OrigSrc, flow.OrigSrcPort),
		flowTupleKey(flow.Protocol, flow.ReplySrc, flow.ReplySrcPort, flow.ReplyDst, flow.ReplyDstPort),
		flowTupleKey(flow.Protocol, flow.ReplyDst, flow.ReplyDstPort, flow.ReplySrc, flow.ReplySrcPort),
	} {
		if app := dpiByTuple[key]; app != "" {
			return app
		}
	}
	return ""
}

func dpiFlowIndex(flows []logstore.DPIFlowEntry) map[string]string {
	out := map[string]string{}
	for _, flow := range flows {
		app := normalizeApp(flow.AppName, flow.Protocol)
		if app == "" || app == "unknown" || app == normalizeProtocol(flow.Protocol) {
			continue
		}
		for _, key := range []string{
			flowTupleKey(flow.Protocol, flow.SrcAddress, flow.SrcPort, flow.DstAddress, flow.DstPort),
			flowTupleKey(flow.Protocol, flow.DstAddress, flow.DstPort, flow.SrcAddress, flow.SrcPort),
		} {
			if key != "" {
				out[key] = app
			}
		}
	}
	return out
}

func flowTupleKey(protocol, src string, srcPort int, dst string, dstPort int) string {
	protocol = normalizeProtocol(protocol)
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if protocol == "" || src == "" || dst == "" {
		return ""
	}
	return protocol + "|" + src + "|" + intString(srcPort) + "|" + dst + "|" + intString(dstPort)
}

func sysctlKey(protocol, app string) string {
	protocol = normalizeProtocol(protocol)
	app = normalizeApp(app, protocol)
	if protocol == "udp" {
		switch app {
		case "wireguard", "tailscale", "ipsec", "stun", "sip", "rtp":
			return "net.netfilter.nf_conntrack_udp_timeout_stream"
		default:
			return "net.netfilter.nf_conntrack_udp_timeout"
		}
	}
	return "net.netfilter.nf_conntrack_tcp_timeout_established"
}

func baselineSeconds(protocol string) int {
	if normalizeProtocol(protocol) == "udp" {
		return 30
	}
	return 86400
}

func isDeny(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "drop", "deny", "reject", "block":
		return true
	default:
		return false
	}
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intString(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}
