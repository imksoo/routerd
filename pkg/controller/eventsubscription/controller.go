// SPDX-License-Identifier: BSD-3-Clause

// Package eventsubscription holds the EventSubscriptionController (ADR 0006,
// Phase 3 Chunk 2). It polls persisted federation events, matches them against
// EventSubscription resources, invokes the referenced Plugin for the matched
// events, and turns the PluginResult into a DynamicConfigPart.
//
// Observation model is poll + dedup (user-decided), not an event bus: each tick
// lists non-expired events for the group and uses the event_subscription_runs
// table to skip already-succeeded events and to bound retries (at-least-once +
// idempotent). With no EventSubscription resource it is a no-op (additive,
// zero-regression).
//
// Plugins receive only the matched events as observed facts on stdin; routerd
// never passes config or secrets to a plugin, preserving that guarantee.
package eventsubscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	defaultMaxAttempts = 3
	// triggerType is recorded on plugin_runs rows so federation-triggered runs
	// are distinguishable from interval/manual/event-bus runs.
	triggerType = "federation-subscription"
)

// DataStore is the SQLite-backed data surface the controller needs (federation
// events, subscription runs, dynamic config parts, plugin runs). It is
// satisfied by *state.SQLiteStore.
type DataStore interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	SubscriptionRunStatus(subscription, eventID string) (status string, attempts int, found bool, err error)
	UpsertSubscriptionRunStart(subscription, eventID, eventGroup, plugin string) error
	MarkSubscriptionRunResult(subscription, eventID, status, dynamicSource string, dynamicGeneration int64, errMsg string) error
	UpsertDynamicConfigPart(part routerstate.DynamicConfigPartRecord) error
	RecordPluginRun(run routerstate.PluginRunRecord) (int64, error)
	CompletePluginRun(id int64, completedAt time.Time, exitCode *int, status, stdoutDigest, stderrText, runError string) error
}

// Store is the full persistence surface the controller needs: object status
// writes plus the DataStore methods. It is satisfied directly by
// *state.SQLiteStore, and in the chain by a composite that routes status writes
// through the evented store for ownership + bus parity.
type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
	DataStore
}

// PluginRunner runs a Plugin. It abstracts plugin.Run so tests can inject a
// fake without an executable; the production wiring uses plugin.Run.
type PluginRunner func(ctx context.Context, spec api.PluginSpec, name string, opts routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error)

// Controller reconciles EventSubscription resources. See package doc.
type Controller struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	RuntimeDir   string
	StateDir     string
	PluginRunner PluginRunner
	Now          func() time.Time
	MaxAttempts  int
}

// HandleEvent reconciles in response to a bus event (bridge for FuncController).
func (c Controller) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c Controller) maxAttempts() int {
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return defaultMaxAttempts
}

func (c Controller) runner() PluginRunner {
	if c.PluginRunner != nil {
		return c.PluginRunner
	}
	return routerplugin.Run
}

// Reconcile processes every EventSubscription once. With no EventSubscription it
// returns nil. It never aborts the whole tick on a single subscription error so
// that one misconfigured subscription does not stall the rest; per-subscription
// problems are recorded in object status and event_subscription_runs.
func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "EventSubscription" {
			continue
		}
		if err := c.reconcileSubscription(ctx, resource); err != nil {
			// Record and continue; do not fail the whole reconcile.
			c.saveStatus(resource.Metadata.Name, "Degraded", err.Error(), 0)
		}
	}
	return nil
}

func (c Controller) reconcileSubscription(ctx context.Context, resource api.Resource) error {
	subName := resource.Metadata.Name
	// subKey is the qualified subscription key used for run rows and the
	// DynamicConfigPart source so they match the dynamic-source annotation.
	subKey := "EventSubscription/" + subName
	spec, err := resource.EventSubscriptionSpec()
	if err != nil {
		return fmt.Errorf("parse EventSubscription %q: %w", subName, err)
	}

	pluginName := strings.TrimSpace(spec.Trigger.PluginRef)
	pluginSpec, found := c.findPlugin(pluginName)
	if !found {
		c.saveStatus(subName, "Pending", fmt.Sprintf("Plugin %q not found", pluginName), 0)
		return nil
	}

	now := c.now()
	events, err := c.Store.ListFederationEvents(spec.GroupRef, false, now.Unix())
	if err != nil {
		return fmt.Errorf("list federation events for %q: %w", subName, err)
	}

	var batch []routerstate.EventRecord
	for _, ev := range events {
		if !matchEvent(spec.Match, ev) {
			continue
		}
		eligible, err := c.eligible(subKey, ev.ID)
		if err != nil {
			return err
		}
		if eligible {
			batch = append(batch, ev)
		}
	}
	if len(batch) == 0 {
		c.saveStatus(subName, "Idle", "", 0)
		return nil
	}

	// Record start (pending/attempts++) for each event in the batch.
	for _, ev := range batch {
		if err := c.Store.UpsertSubscriptionRunStart(subKey, ev.ID, ev.Group, pluginName); err != nil {
			return fmt.Errorf("record run start for %q/%s: %w", subName, ev.ID, err)
		}
	}

	if c.DryRun {
		// Dry-run: do not invoke the plugin and do not write a part. The pending
		// rows above record that the batch was observed.
		c.saveStatus(subName, "Pending", fmt.Sprintf("DryRun: %d event(s) matched", len(batch)), len(batch))
		return nil
	}

	runID, _ := c.Store.RecordPluginRun(routerstate.PluginRunRecord{
		Plugin:       pluginName,
		TriggerType:  triggerType,
		TriggerTopic: subName,
		StartedAt:    now,
		Status:       "running",
	})

	matched := make([]routerplugin.PluginMatchedEvent, 0, len(batch))
	for _, ev := range batch {
		matched = append(matched, routerplugin.PluginMatchedEvent{
			ID:         ev.ID,
			Group:      ev.Group,
			SourceNode: ev.SourceNode,
			Type:       ev.Type,
			Subject:    ev.Subject,
			DedupeKey:  ev.DedupeKey,
			Payload:    ev.Payload,
			ObservedAt: ev.ObservedAt,
			ExpiresAt:  ev.ExpiresAt,
		})
	}

	// Build the least-privilege plugin context (Phase 4.0): only the resources
	// the Plugin allowlisted via spec.context.resources, with secrets always
	// redacted. No allowlist -> empty context (default-deny, no config passed).
	pluginContext, err := routerplugin.BuildPluginContext(pluginSpec.Context.Resources, c.Router.Spec.Resources)
	if err != nil {
		c.failBatch(batch, subKey, err.Error())
		c.completeRun(runID, routerplugin.RunOutcome{}, "failed", err.Error())
		c.saveStatus(subName, "Degraded", err.Error(), len(batch))
		return nil
	}

	runOpts := routerplugin.RunOptions{
		Now:     now,
		Trigger: routerplugin.TriggerRef{Type: triggerType, Topic: subscriptionTopic(subName)},
		Events:  matched,
		Context: pluginContext,
	}

	result, outcome, runErr := c.runner()(ctx, pluginSpec, pluginName, runOpts)
	if runErr != nil {
		c.failBatch(batch, subKey, runErr.Error())
		c.completeRun(runID, outcome, "failed", runErr.Error())
		c.saveStatus(subName, "Degraded", runErr.Error(), len(batch))
		return nil
	}

	source := subscriptionSource(subName, batch)
	part, err := routerplugin.DynamicConfigPartFromResult(source, 1, result, now)
	if err != nil {
		c.failBatch(batch, subKey, err.Error())
		c.completeRun(runID, outcome, "failed", err.Error())
		c.saveStatus(subName, "Degraded", err.Error(), len(batch))
		return nil
	}

	annotateDynamicPart(&part, subName, batch)

	record, err := dynamicPartRecord(part)
	if err != nil {
		c.failBatch(batch, subKey, err.Error())
		c.completeRun(runID, outcome, "failed", err.Error())
		c.saveStatus(subName, "Degraded", err.Error(), len(batch))
		return nil
	}
	if err := c.Store.UpsertDynamicConfigPart(record); err != nil {
		c.failBatch(batch, subKey, err.Error())
		c.completeRun(runID, outcome, "failed", err.Error())
		c.saveStatus(subName, "Degraded", err.Error(), len(batch))
		return nil
	}

	for _, ev := range batch {
		if err := c.Store.MarkSubscriptionRunResult(subKey, ev.ID, "succeeded", source, part.Spec.Generation, ""); err != nil {
			return fmt.Errorf("mark succeeded for %q/%s: %w", subName, ev.ID, err)
		}
	}
	c.completeRun(runID, outcome, "succeeded", "")
	c.saveStatus(subName, "Applied", "", len(batch))

	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.event.subscription.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription", Name: subName}
		event.Attributes = map[string]string{
			"dynamic.source": source,
			"events":         fmt.Sprint(len(batch)),
		}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

// eligible reports whether an event should be processed this tick: new events,
// pending events, and failed events with retries remaining are eligible;
// succeeded events and failed events that have exhausted MaxAttempts are not.
func (c Controller) eligible(subscription, eventID string) (bool, error) {
	status, attempts, found, err := c.Store.SubscriptionRunStatus(subscription, eventID)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	switch status {
	case "succeeded":
		return false, nil
	case "failed":
		// Give up once attempts have reached the cap.
		return attempts < c.maxAttempts(), nil
	default:
		// pending: a prior tick recorded a start but never marked a result
		// (e.g. crash). Re-process.
		return true, nil
	}
}

func (c Controller) failBatch(batch []routerstate.EventRecord, subName, msg string) {
	for _, ev := range batch {
		_ = c.Store.MarkSubscriptionRunResult(subName, ev.ID, "failed", "", 0, msg)
	}
}

func (c Controller) completeRun(runID int64, outcome routerplugin.RunOutcome, status, runError string) {
	if runID == 0 {
		return
	}
	var exitCode *int
	if outcome.HasExitCode {
		exitCode = &outcome.ExitCode
	}
	if runError == "" {
		runError = outcome.Error
	}
	_ = c.Store.CompletePluginRun(runID, c.now(), exitCode, status, outcome.StdoutDigest, outcome.Stderr, runError)
}

func (c Controller) findPlugin(name string) (api.PluginSpec, bool) {
	if name == "" {
		return api.PluginSpec{}, false
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind == "Plugin" && res.Metadata.Name == name {
			spec, err := res.PluginSpec()
			if err != nil {
				return api.PluginSpec{}, false
			}
			return spec, true
		}
	}
	return api.PluginSpec{}, false
}

func (c Controller) saveStatus(subName, phase, message string, matched int) {
	if c.Store == nil {
		return
	}
	status := map[string]any{
		"phase":     phase,
		"matched":   matched,
		"updatedAt": c.now().Format(time.RFC3339Nano),
	}
	if message != "" {
		status["message"] = message
		status["reason"] = message
	}
	_ = c.Store.SaveObjectStatus(api.FederationAPIVersion, "EventSubscription", subName, status)
}

// matchEvent applies the EventSubscriptionMatch predicate. Types is required and
// must contain the event type; the other fields narrow further, with empty
// optional fields meaning "any".
func matchEvent(match api.EventSubscriptionMatch, ev routerstate.EventRecord) bool {
	if !containsString(match.Types, ev.Type) {
		return false
	}
	if len(match.SubjectPrefixes) > 0 && !hasAnyPrefix(ev.Subject, match.SubjectPrefixes) {
		return false
	}
	for key, want := range match.Payload {
		if ev.Payload[key] != want {
			return false
		}
	}
	if len(match.SourceNodes) > 0 && !containsString(match.SourceNodes, ev.SourceNode) {
		return false
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// subscriptionTopic is the trigger topic delivered to the plugin.
func subscriptionTopic(subName string) string {
	return "routerd.federation.subscription/" + subName
}

// subscriptionSource derives the DynamicConfigPart source for a batch.
//
// Part-keying rationale: dynamicconfig.BuildEffectiveConfig UNIONS the resources
// of every active (non-expired) part; it does not collapse to the latest
// generation per source. UpsertDynamicConfigPart, however, keys rows by
// UNIQUE(source, generation), so reusing one (source, generation) would
// overwrite an earlier still-valid claim. To let distinct batches of events
// each contribute their RemoteAddressClaim WITHOUT clobbering earlier active
// parts, we make the source batch-distinct ("EventSubscription/<sub>/<digest>"
// of the sorted event ids) and keep generation fixed at 1. Distinct batches ->
// distinct sources -> independent rows -> all coexist in the effective config.
// Re-processing the identical batch yields the same source+digest, so the
// upsert is an idempotent overwrite of the same data.
func subscriptionSource(subName string, batch []routerstate.EventRecord) string {
	ids := batchEventIDs(batch)
	h := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return fmt.Sprintf("EventSubscription/%s/%s", subName, hex.EncodeToString(h[:])[:16])
}

func batchEventIDs(batch []routerstate.EventRecord) []string {
	ids := make([]string, 0, len(batch))
	for _, ev := range batch {
		ids = append(ids, ev.ID)
	}
	sort.Strings(ids)
	return ids
}

// annotateDynamicPart stamps provenance annotations onto every resource the
// plugin produced so operators can trace a dynamic resource back to the
// subscription, event group, and the events that triggered it.
func annotateDynamicPart(part *dynamicconfig.DynamicConfigPart, subName string, batch []routerstate.EventRecord) {
	source := "EventSubscription/" + subName
	ids := batchEventIDs(batch)
	group := ""
	subjects := make([]string, 0, len(batch))
	for _, ev := range batch {
		if group == "" {
			group = ev.Group
		}
		if ev.Subject != "" {
			subjects = append(subjects, ev.Subject)
		}
	}
	for i := range part.Spec.Resources {
		res := &part.Spec.Resources[i]
		if res.Metadata.Annotations == nil {
			res.Metadata.Annotations = map[string]string{}
		}
		res.Metadata.Annotations["routerd.net/dynamic-source"] = source
		res.Metadata.Annotations["routerd.net/event-group"] = group
		res.Metadata.Annotations["routerd.net/event-id"] = strings.Join(ids, ",")
		if len(subjects) > 0 {
			res.Metadata.Annotations["routerd.net/event-subject"] = strings.Join(subjects, ",")
		}
	}
}

func dynamicPartRecord(part dynamicconfig.DynamicConfigPart) (routerstate.DynamicConfigPartRecord, error) {
	resources, err := json.Marshal(part.Spec.Resources)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	directives, err := json.Marshal(part.Spec.Directives)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	var actionPlansJSON string
	if len(part.Spec.ActionPlans) > 0 {
		// Preserve the plugin's display-only ActionPlans so federation-triggered
		// runs stay reviewable. routerd never executes them.
		data, err := json.Marshal(part.Spec.ActionPlans)
		if err != nil {
			return routerstate.DynamicConfigPartRecord{}, err
		}
		actionPlansJSON = string(data)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:          part.Spec.Source,
		Generation:      part.Spec.Generation,
		ObservedAt:      part.Spec.ObservedAt,
		ExpiresAt:       part.Spec.ExpiresAt,
		Digest:          part.Spec.Digest,
		ResourcesJSON:   string(resources),
		DirectivesJSON:  string(directives),
		ActionPlansJSON: actionPlansJSON,
		Status:          "active",
	}, nil
}
