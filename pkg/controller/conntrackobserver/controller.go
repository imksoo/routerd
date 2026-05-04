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
	now := time.Now().UTC()
	var active []string
	for _, entry := range table.Entries {
		flow := trafficFlowFromConnection(entry, now)
		if flow.FlowKey == "" {
			continue
		}
		active = append(active, flow.FlowKey)
		if err := log.UpsertActive(ctx, flow); err != nil {
			return err
		}
	}
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
	}
	flow.FlowKey = logstore.FlowKey(flow.Protocol, flow.ClientAddress, flow.ClientPort, flow.PeerAddress, flow.PeerPort)
	return flow
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
