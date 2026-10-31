// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/platform"
)

// Status and journal data have different owners in a controller generation:
// the evented status adapter must not hide the actual SQLite source.
type logRetentionData interface {
	LogRetentionSource() (string, fs.FileInfo, error)
	PruneEventsOlderThan(time.Time) (int64, error)
}

type LogRetentionController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Data   logRetentionData
	DryRun bool
}

const eventRetentionWarning = "The event journal's independent write-time age and capacity limits take precedence; archives include only retained rows."

func (c LogRetentionController) Reconcile(ctx context.Context) error {
	// Dry-run must not open external log databases, archive records, vacuum,
	// or prune the journal. Even an isolated state snapshot can reference
	// live data files through resource settings and platform defaults.
	if c.DryRun || c.Router == nil || c.Store == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	explicitEvents := false
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.SystemAPIVersion || res.Kind != "LogRetention" {
			continue
		}
		spec, err := res.LogRetentionSpec()
		if err != nil {
			return err
		}
		eventPolicy := len(spec.Signals) == 0 || slices.Contains(spec.Signals, "events")
		explicitEvents = explicitEvents || eventPolicy
		duration, err := logstore.ParseRetention(spec.Retention)
		if err != nil {
			return fmt.Errorf("%s retention %q: %w", res.ID(), spec.Retention, err)
		}
		previous, err := c.Store.ReadObjectStatus(api.SystemAPIVersion, "LogRetention", res.Metadata.Name)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !retentionDue(previous, spec.Schedule) {
			if eventPolicy {
				status := maps.Clone(previous)
				status["warning"] = eventRetentionWarning
				status["archiveScope"] = "retainedRows"
				if duration > 24*time.Hour {
					status["phase"] = "Degraded"
					status["reason"] = "EventJournalSafetyBound"
				}
				if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "LogRetention", res.Metadata.Name, status); err != nil {
					return err
				}
			}
			continue
		}
		stateFile := ""
		var stateIdentity fs.FileInfo
		if len(spec.Signals) == 0 || slices.Contains(spec.Signals, "events") || slices.Contains(spec.Signals, "accessLogs") || slices.Contains(spec.Signals, "pluginRuns") {
			if c.Data == nil {
				return fmt.Errorf("%s state log database is unavailable", res.ID())
			}
			stateFile, stateIdentity, err = c.Data.LogRetentionSource()
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		targetSpecs := logRetentionTargets(c.Router, spec, stateFile)
		var archive *eventlog.Logger
		if len(spec.Sinks) > 0 {
			archive, err = eventlog.NewSelected(c.Router, spec.Sinks)
			if err != nil {
				return err
			}
			defer archive.Close()
		}
		var total int64
		var targets []map[string]any
		for _, target := range targetSpecs {
			retentionTarget := logstore.RetentionTarget{File: target.File, Retention: duration, Signals: target.Signals}
			if target.File == stateFile {
				retentionTarget.Identity = stateIdentity
			}
			if archive != nil {
				retentionTarget.BeforeDelete = func(record logstore.RetentionRecord) error {
					return archive.EmitRecord(record.Signal, record.Values)
				}
			}
			result, err := logstore.ApplyRetention(ctx, retentionTarget, spec.Vacuum)
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
		if eventPolicy {
			status["warning"] = eventRetentionWarning
			status["archiveScope"] = "retainedRows"
			if duration > 24*time.Hour {
				status["phase"] = "Degraded"
				status["reason"] = "EventJournalSafetyBound"
			}
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
	// The event journal is part of the router's safety envelope, not an
	// opt-in observability feature. Without an explicit events policy, prune
	// the idle journal here. A declared policy alone owns idle archive/expiry;
	// the state store's independent write-time safety bounds still apply.
	if c.Data != nil && !explicitEvents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := c.Data.PruneEventsOlderThan(time.Now().UTC().Add(-24 * time.Hour)); err != nil {
			return err
		}
	}
	return nil
}

func logRetentionTargets(router *api.Router, spec api.LogRetentionSpec, stateFile string) []api.LogRetentionTargetSpec {
	signals := spec.Signals
	if len(signals) == 0 {
		signals = []string{"events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"}
	}
	byFile := map[string]int{}
	var out []api.LogRetentionTargetSpec
	for _, signal := range signals {
		for _, file := range logRetentionSignalFiles(router, signal, stateFile) {
			if i, ok := byFile[file]; ok {
				out[i].Signals = append(out[i].Signals, signal)
				continue
			}
			byFile[file] = len(out)
			out = append(out, api.LogRetentionTargetSpec{File: file, Retention: spec.Retention, Signals: []string{signal}})
		}
	}
	return out
}

func logRetentionSignalFiles(router *api.Router, signal, stateFile string) []string {
	defaults, _ := platform.Current()
	switch signal {
	case "events", "accessLogs", "pluginRuns":
		return []string{stateFile}
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
		return []string{defaults.FirewallLogFile()}
	case "dhcpFingerprints":
		return []string{defaults.StateDir + "/dhcp-fingerprints.db"}
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
