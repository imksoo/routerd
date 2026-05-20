// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/logstore"
	"routerd/pkg/platform"
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
		targetSpecs := logRetentionTargets(c.Router, spec)
		var total int64
		var targets []map[string]any
		for _, target := range targetSpecs {
			duration, err := logstore.ParseRetention(target.Retention)
			if err != nil {
				return fmt.Errorf("%s retention %q: %w", res.ID(), target.Retention, err)
			}
			result, err := logstore.ApplyRetention(ctx, logstore.RetentionTarget{File: target.File, Retention: duration}, spec.Vacuum)
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

func logRetentionTargets(router *api.Router, spec api.LogRetentionSpec) []api.LogRetentionTargetSpec {
	signals := spec.Signals
	if len(signals) == 0 {
		signals = []string{"events", "dnsQueries", "trafficFlows", "firewallEvents"}
	}
	var out []api.LogRetentionTargetSpec
	for _, signal := range signals {
		for _, file := range logRetentionSignalFiles(router, signal) {
			out = append(out, api.LogRetentionTargetSpec{File: file, Retention: spec.Retention})
		}
	}
	return out
}

func logRetentionSignalFiles(router *api.Router, signal string) []string {
	defaults, _ := platform.Current()
	switch signal {
	case "events":
		return []string{defaults.DBFile()}
	case "dnsQueries":
		if router != nil {
			var out []string
			for _, res := range router.Spec.Resources {
				if res.APIVersion != api.NetAPIVersion || res.Kind != "DNSResolver" {
					continue
				}
				spec, err := res.DNSResolverSpec()
				if err == nil && spec.QueryLog.Enabled && spec.QueryLog.Path != "" {
					out = append(out, spec.QueryLog.Path)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		return []string{defaults.StateDir + "/dns-queries.db"}
	case "trafficFlows":
		if router != nil {
			for _, res := range router.Spec.Resources {
				if res.APIVersion != api.NetAPIVersion || res.Kind != "TrafficFlowLog" {
					continue
				}
				spec, err := res.TrafficFlowLogSpec()
				if err == nil && spec.Enabled && spec.Path != "" {
					return []string{spec.Path}
				}
			}
		}
		return []string{defaults.StateDir + "/traffic-flows.db"}
	case "firewallEvents":
		if router != nil {
			for _, res := range router.Spec.Resources {
				if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallEventLog" {
					continue
				}
				spec, err := res.FirewallEventLogSpec()
				if err == nil && spec.Enabled && spec.Path != "" {
					return []string{spec.Path}
				}
			}
		}
		return []string{defaults.StateDir + "/firewall-logs.db"}
	default:
		return nil
	}
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
