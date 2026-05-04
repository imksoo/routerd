package chain

import (
	"context"
	"fmt"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/logstore"
)

type LogRetentionController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
}

func (c LogRetentionController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.SystemAPIVersion || res.Kind != "LogRetention" {
			continue
		}
		spec, err := res.LogRetentionSpec()
		if err != nil {
			return err
		}
		if !retentionDue(c.Store.ObjectStatus(api.SystemAPIVersion, "LogRetention", res.Metadata.Name), spec.Schedule) {
			continue
		}
		var total int64
		var targets []map[string]any
		for _, target := range spec.Targets {
			duration, err := logstore.ParseRetention(target.Retention)
			if err != nil {
				return fmt.Errorf("%s retention %q: %w", res.ID(), target.Retention, err)
			}
			result, err := logstore.ApplyRetention(ctx, logstore.RetentionTarget{File: target.File, Retention: duration}, spec.IncrementalVacuum)
			if err != nil {
				return err
			}
			total += result.Deleted
			targets = append(targets, map[string]any{"file": result.File, "deleted": result.Deleted, "skipped": result.Skipped})
		}
		now := time.Now().UTC()
		status := map[string]any{
			"phase":     "Applied",
			"lastRunAt": now.Format(time.RFC3339Nano),
			"targets":   targets,
			"deleted":   total,
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "LogRetention", res.Metadata.Name, status); err != nil {
			return err
		}
		if total > 0 && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.log.retention.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "LogRetention", Name: res.Metadata.Name}
			event.Attributes = map[string]string{"deleted": fmt.Sprintf("%d", total)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func retentionDue(status map[string]any, schedule string) bool {
	last, _ := status["lastRunAt"].(string)
	if last == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, last)
	if err != nil {
		return true
	}
	switch schedule {
	case "", "daily":
		return time.Since(parsed) >= 23*time.Hour
	default:
		return true
	}
}
