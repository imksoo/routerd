// SPDX-License-Identifier: BSD-3-Clause

package conntrackobserver

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
	"routerd/pkg/daemonapi"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Bus            *bus.Bus
	Router         *api.Router
	Store          Store
	Paths          conntrack.Paths
	Interval       time.Duration
	ThresholdRatio float64
	Logger         *slog.Logger
	Connections    func(limit int) (*observe.ConnectionTable, error)
	lastCount      int
	aboveThreshold bool
	seen           bool
	lastFlowBytes  map[string]flowByteTotals
}

func (c *Controller) Start(ctx context.Context) {
	if c.Bus == nil || c.Store == nil {
		return
	}
	interval := c.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		c.reconcileLogged(ctx)
		for {
			select {
			case <-ticker.C:
				c.reconcileLogged(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Controller) reconcileLogged(ctx context.Context) {
	if err := c.Reconcile(ctx); err != nil && c.Logger != nil {
		c.Logger.Warn("conntrack observer failed", "error", err)
	}
}

func (c *Controller) Reconcile(ctx context.Context) error {
	snapshot, err := conntrack.ReadSnapshot(c.Paths)
	if err != nil {
		return err
	}
	var created int64
	if c.seen && snapshot.Count > c.lastCount {
		created = int64(snapshot.Count - c.lastCount)
	}
	c.seen = true
	c.lastCount = snapshot.Count
	conntrack.RecordMetrics(ctx, snapshot, created)
	ratio := 0.0
	if snapshot.Max > 0 {
		ratio = float64(snapshot.Count) / float64(snapshot.Max)
	}
	status := map[string]any{
		"phase":       "Observed",
		"count":       snapshot.Count,
		"max":         snapshot.Max,
		"usageRatio":  ratio,
		"observedAt":  time.Now().UTC().Format(time.RFC3339Nano),
		"createdHint": created,
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default", status); err != nil {
		return err
	}
	if err := c.recordTrafficFlows(ctx, snapshot.Count); err != nil && c.Logger != nil {
		c.Logger.Warn("traffic flow log failed", "error", err)
	}
	threshold := c.ThresholdRatio
	if threshold == 0 {
		threshold = 0.8
	}
	overThreshold := snapshot.Max > 0 && ratio >= threshold
	if overThreshold && !c.aboveThreshold {
		c.aboveThreshold = true
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.conntrack.threshold.exceeded", daemonapi.SeverityWarning)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "ConntrackObserver", Name: "default"}
		event.Attributes = map[string]string{"count": fmt.Sprintf("%d", snapshot.Count), "max": fmt.Sprintf("%d", snapshot.Max), "threshold": fmt.Sprintf("%.2f", threshold)}
		return c.Bus.Publish(ctx, event)
	}
	if !overThreshold {
		c.aboveThreshold = false
	}
	return nil
}

func (c *Controller) recordTrafficFlows(ctx context.Context, count int) error {
	resource, spec, ok := trafficFlowLogSpec(c.Router)
	if !ok || !spec.Enabled {
		return nil
	}
	path := strings.TrimSpace(spec.Path)
	if path == "" {
		path = "/var/lib/routerd/traffic-flows.db"
	}
	connections := c.Connections
	if connections == nil {
		connections = observe.Connections
	}
	table, err := connections(10000)
	if err != nil {
		return err
	}
	log, err := logstore.OpenTrafficFlowLog(path)
	if err != nil {
		return err
	}
	defer log.Close()
	var dpiStore *logstore.FirewallLog
	if dpiPath := firewallLogPath(c.Router); dpiPath != "" {
		if store, err := logstore.OpenFirewallLogReadOnly(dpiPath); err == nil {
			dpiStore = store
			defer dpiStore.Close()
		} else if c.Logger != nil {
			c.Logger.Debug("traffic flow DPI cache unavailable", "error", err)
		}
	}
	now := time.Now().UTC()
	var active []string
	for _, entry := range table.Entries {
		flow := trafficFlowFromConnection(entry, now)
		if flow.FlowKey == "" {
			continue
		}
		if dpiStore != nil {
			enrichTrafficFlowFromDPIStore(ctx, dpiStore, &flow, now, time.Hour)
		}
		active = append(active, flow.FlowKey)
		if err := log.UpsertActive(ctx, flow); err != nil {
			return err
		}
	}
	c.recordTrafficMetrics(ctx, table.Entries)
	if err := log.EndMissing(ctx, active, now); err != nil {
		return err
	}
	status := map[string]any{
		"phase":       "Observed",
		"path":        path,
		"source":      firstNonEmpty(spec.Source, "conntrack"),
		"activeFlows": len(active),
		"count":       count,
		"observedAt":  now.Format(time.RFC3339Nano),
	}
	return c.Store.SaveObjectStatus(resource.APIVersion, "TrafficFlowLog", resource.Metadata.Name, status)
}

func trafficFlowLogSpec(router *api.Router) (api.Resource, api.TrafficFlowLogSpec, bool) {
	if router == nil {
		return api.Resource{}, api.TrafficFlowLogSpec{}, false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "TrafficFlowLog" {
			continue
		}
		spec, err := resource.TrafficFlowLogSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(spec.Source) == "" {
			spec.Source = "conntrack"
		}
		if strings.TrimSpace(spec.Path) == "" {
			spec.Path = "/var/lib/routerd/traffic-flows.db"
		}
		return resource, spec, true
	}
	return api.Resource{}, api.TrafficFlowLogSpec{}, false
}

func firewallLogPath(router *api.Router) string {
	if router == nil {
		return ""
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "FirewallLog" {
			continue
		}
		spec, err := resource.FirewallLogSpec()
		if err != nil || !spec.Enabled {
			continue
		}
		if strings.TrimSpace(spec.Path) != "" {
			return spec.Path
		}
		return "/var/lib/routerd/firewall-logs.db"
	}
	return ""
}

func enrichTrafficFlowFromDPIStore(ctx context.Context, store *logstore.FirewallLog, flow *logstore.TrafficFlow, now time.Time, ttl time.Duration) {
	if store == nil || flow == nil {
		return
	}
	dpiFlow, ok, err := store.FindDPIFlowForFirewallEntry(ctx, logstore.FirewallLogEntry{
		Protocol:   flow.Protocol,
		SrcAddress: flow.ClientAddress,
		SrcPort:    flow.ClientPort,
		DstAddress: flow.PeerAddress,
		DstPort:    flow.PeerPort,
	}, now, ttl)
	if err != nil || !ok {
		return
	}
	applyDPIFlow(flow, dpiFlow)
}

func applyDPIFlow(flow *logstore.TrafficFlow, dpiFlow logstore.DPIFlowEntry) {
	if flow == nil {
		return
	}
	if flow.AppName == "" {
		flow.AppName = dpiFlow.AppName
	}
	if flow.AppCategory == "" {
		flow.AppCategory = dpiFlow.AppCategory
	}
	if flow.AppConfidence == 0 {
		flow.AppConfidence = dpiFlow.AppConfidence
	}
	if flow.DetectedProtocol == "" {
		flow.DetectedProtocol = dpiFlow.DetectedProtocol
	}
	if flow.MasterProtocol == "" {
		flow.MasterProtocol = dpiFlow.MasterProtocol
	}
	if flow.ApplicationProtocol == "" {
		flow.ApplicationProtocol = dpiFlow.ApplicationProtocol
	}
	if flow.Category == "" {
		flow.Category = dpiFlow.Category
	}
	if len(flow.Risk) == 0 {
		flow.Risk = append([]string(nil), dpiFlow.Risk...)
	}
	if flow.Confidence == 0 {
		flow.Confidence = dpiFlow.Confidence
	}
	if len(flow.Metadata) == 0 && len(dpiFlow.Metadata) > 0 {
		flow.Metadata = map[string]string{}
		for key, value := range dpiFlow.Metadata {
			flow.Metadata[key] = value
		}
	}
	if flow.Engine == "" {
		flow.Engine = dpiFlow.Engine
	}
	if flow.Source == "" {
		flow.Source = dpiFlow.Source
	}
	if flow.TLSSNI == "" {
		flow.TLSSNI = dpiFlow.TLSSNI
	}
	if flow.HTTPHost == "" {
		flow.HTTPHost = dpiFlow.HTTPHost
	}
	if flow.DNSQuery == "" {
		flow.DNSQuery = dpiFlow.DNSQuery
	}
	if flow.ResolvedHostname == "" {
		flow.ResolvedHostname = firstNonEmpty(dpiFlow.TLSSNI, dpiFlow.HTTPHost, dpiFlow.DNSQuery)
	}
}

func trafficFlowFromConnection(entry observe.ConnectionEntry, now time.Time) logstore.TrafficFlow {
	client := entry.Original.Source
	peer := entry.Original.Destination
	natAddress := ""
	if entry.Reply.Destination != "" && entry.Reply.Destination != entry.Original.Source {
		natAddress = entry.Reply.Destination
	}
	clientPort := atoi(entry.Original.SourcePort)
	peerPort := atoi(entry.Original.DestinationPort)
	flow := logstore.TrafficFlow{
		StartedAt:            now,
		ClientAddress:        client,
		ClientPort:           clientPort,
		PeerAddress:          peer,
		PeerPort:             peerPort,
		Protocol:             strings.ToLower(entry.Protocol),
		NATTranslatedAddress: natAddress,
		Accounting:           entry.Original.Accounting || entry.Reply.Accounting,
		BytesOut:             entry.Original.Bytes,
		BytesIn:              entry.Reply.Bytes,
		PacketsOut:           entry.Original.Packets,
		PacketsIn:            entry.Reply.Packets,
		AppName:              strings.ToLower(strings.TrimSpace(entry.AppName)),
		AppCategory:          entry.AppCategory,
		AppConfidence:        entry.AppConfidence,
		TLSSNI:               entry.TLSSNI,
		HTTPHost:             entry.HTTPHost,
		DNSQuery:             entry.DNSQuery,
		ResolvedHostname:     firstNonEmpty(entry.TLSSNI, entry.HTTPHost, entry.DNSQuery),
	}
	flow.FlowKey = logstore.FlowKey(flow.Protocol, flow.ClientAddress, flow.ClientPort, flow.PeerAddress, flow.PeerPort)
	return flow
}

func (c *Controller) recordTrafficMetrics(ctx context.Context, entries []observe.ConnectionEntry) {
	if len(entries) == 0 {
		c.lastFlowBytes = nil
		return
	}
	meter := otel.Meter("routerd.conntrackobserver")
	flowGauge, _ := meter.Int64Gauge("routerd.connection.flow.protocol")
	bytesCounter, _ := meter.Int64Counter("routerd.connection.bytes")
	clientCounter, _ := meter.Int64Counter("routerd.client.activity.protocol")
	if c.lastFlowBytes == nil {
		c.lastFlowBytes = map[string]flowByteTotals{}
	}
	counts := map[string]int64{}
	next := map[string]flowByteTotals{}
	for _, entry := range entries {
		flow := trafficFlowFromConnection(entry, time.Time{})
		if flow.FlowKey == "" {
			continue
		}
		protocol := trafficMetricProtocol(flow)
		counts[protocol]++
		current := flowByteTotals{Out: flow.BytesOut, In: flow.BytesIn}
		previous := c.lastFlowBytes[flow.FlowKey]
		outDelta := positiveDelta(current.Out, previous.Out)
		inDelta := positiveDelta(current.In, previous.In)
		attrs := []attribute.KeyValue{
			attribute.String("network.protocol.name", protocol),
			attribute.String("network.transport", flow.Protocol),
		}
		if outDelta > 0 {
			bytesCounter.Add(ctx, outDelta, metric.WithAttributes(append(attrs, attribute.String("network.direction", "out"))...))
		}
		if inDelta > 0 {
			bytesCounter.Add(ctx, inDelta, metric.WithAttributes(append(attrs, attribute.String("network.direction", "in"))...))
		}
		totalDelta := outDelta + inDelta
		if totalDelta > 0 && flow.ClientAddress != "" {
			clientCounter.Add(ctx, totalDelta, metric.WithAttributes(
				attribute.String("network.protocol.name", protocol),
				attribute.String("network.transport", flow.Protocol),
				attribute.String("routerd.client.address", flow.ClientAddress),
			))
		}
		next[flow.FlowKey] = current
	}
	for protocol, count := range counts {
		flowGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.protocol.name", protocol)))
	}
	c.lastFlowBytes = next
}

type flowByteTotals struct {
	Out int64
	In  int64
}

func positiveDelta(current, previous int64) int64 {
	if current <= 0 {
		return 0
	}
	if previous < 0 || current < previous {
		return current
	}
	return current - previous
}

func trafficMetricProtocol(flow logstore.TrafficFlow) string {
	if app := strings.ToLower(strings.TrimSpace(flow.AppName)); app != "" && app != "unknown" {
		if protocol := providerTrafficProtocol(app, flow); protocol != "" {
			return protocol
		}
		return app
	}
	if strings.TrimSpace(flow.TLSSNI) != "" {
		return "tls"
	}
	switch flow.PeerPort {
	case 53:
		return "dns"
	case 80:
		return "http"
	case 3478, 5349:
		return "stun"
	case 41641:
		return "tailscale"
	case 51820:
		return "wireguard"
	case 443:
		if strings.EqualFold(flow.Protocol, "udp") {
			return "quic"
		}
		return "tls"
	}
	if protocol := strings.ToLower(strings.TrimSpace(flow.Protocol)); protocol != "" {
		return protocol
	}
	return "unidentified"
}

func providerTrafficProtocol(app string, flow logstore.TrafficFlow) string {
	switch strings.ToLower(strings.TrimSpace(app)) {
	case "google", "googleservices", "amazonaws", "microsoft", "microsoft365", "azure", "apple", "appleicloud", "cloudflare", "nintendo":
		if strings.EqualFold(flow.Protocol, "udp") && flow.PeerPort == 443 {
			return "quic"
		}
		return "tls"
	default:
		return ""
	}
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
