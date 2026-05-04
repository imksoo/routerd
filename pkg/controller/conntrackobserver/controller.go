package conntrackobserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
	"routerd/pkg/daemonapi"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Bus            *bus.Bus
	Store          Store
	Paths          conntrack.Paths
	Interval       time.Duration
	ThresholdRatio float64
	Logger         *slog.Logger
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
