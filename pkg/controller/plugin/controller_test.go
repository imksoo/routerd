// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func triggerFixture(t *testing.T) (*Controller, *routerstate.SQLiteStore, *time.Time, *[]routerplugin.RunOptions) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "inventory"}, Spec: api.PluginSpec{Executable: "/not-invoked", Triggers: []api.PluginTrigger{{Type: "interval", Every: "10s"}, {Type: "event", Topic: "device.*"}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.DynamicConfigSourceSpec{PluginRef: "inventory", TTL: "30m", Triggers: []api.PluginTrigger{{Type: "interval", Every: "10s"}, {Type: "interval", Every: "20s"}, {Type: "event", Topic: "device.changed"}}}},
	}}}
	c, err := New(router, store, bus.New(), false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var calls []routerplugin.RunOptions
	c.Now = func() time.Time { return now }
	c.Runner = func(_ context.Context, _ api.PluginSpec, _ string, opts routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		calls = append(calls, opts)
		return routerplugin.PluginResult{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "PluginResult"}, Status: routerplugin.PluginResultStatus{ObservedAt: now, TTL: "1h"}}, routerplugin.RunOutcome{}, nil
	}
	return c, store, &now, &calls
}

func TestPreparedTriggerUnionUsesOneInvocationAndIndividualDeadlines(t *testing.T) {
	c, store, now, calls := triggerFixture(t)
	for _, elapsed := range []time.Duration{0, 10 * time.Second, 10 * time.Second} {
		*now = now.Add(elapsed)
		if worked, err := c.Periodic(t.Context()); err != nil || !worked {
			t.Fatalf("due reconcile: worked=%t err=%v", worked, err)
		}
	}
	if len(*calls) != 3 || c.NextAfter() != 10*time.Second {
		t.Fatalf("simultaneous/duplicate triggers caused extra runs: calls=%d next=%v", len(*calls), c.NextAfter())
	}
	*now = now.Add(time.Second)
	if err := c.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: "device.changed"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 4 || (*calls)[3].Trigger.Type != "event" || (*calls)[3].Trigger.Topic != "device.changed" || c.NextAfter() != 9*time.Second {
		t.Fatalf("event union consumed future interval deadlines: %+v next=%v", *calls, c.NextAfter())
	}
	parts, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(parts) != 4 || parts[0].ExpiresAt.Sub(parts[0].ObservedAt) != 30*time.Minute {
		t.Fatalf("source lifetime/publication: %+v %v", parts, err)
	}
}

func TestReadOnlyDoesNotRunOrConsumeDueWork(t *testing.T) {
	c, _, now, calls := triggerFixture(t)
	c.DryRun = true
	if worked, err := c.Periodic(t.Context()); err != nil || worked || c.NextAfter() != 0 {
		t.Fatalf("preview scheduled live work: %t %v %v", worked, err, c.NextAfter())
	}
	*now = now.Add(time.Hour)
	if err := c.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: "device.changed"}); err != nil || len(*calls) != 0 {
		t.Fatalf("preview invoked plugin: %d %v", len(*calls), err)
	}
	c.DryRun = false
	if worked, err := c.Periodic(t.Context()); err != nil || !worked || len(*calls) != 1 {
		t.Fatalf("preview consumed activation work: %t calls=%d %v", worked, len(*calls), err)
	}
}

func TestFailureKeepsPublicationAndAdvancesPositiveDeadline(t *testing.T) {
	c, store, now, calls := triggerFixture(t)
	if _, err := c.Periodic(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Second)
	c.Runner = func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		*now = now.Add(30 * time.Second)
		return routerplugin.PluginResult{}, routerplugin.RunOutcome{}, errors.New("failed observation")
	}
	if _, err := c.Periodic(t.Context()); err == nil {
		t.Fatal("failure was hidden")
	}
	after, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(after) != len(before) || after[0].Generation != before[0].Generation || !after[0].ExpiresAt.Equal(before[0].ExpiresAt) {
		t.Fatalf("failure changed previous publication: before=%+v after=%+v err=%v", before, after, err)
	}
	if c.NextAfter() <= 0 {
		t.Fatalf("failure left an overdue deadline: %v", c.NextAfter())
	}
	if worked, err := c.Periodic(t.Context()); err != nil || worked || len(*calls) != 1 {
		t.Fatalf("failed interval spun: worked=%t err=%v", worked, err)
	}
}

func TestSameEffectiveContentDoesNotRepublishChangeNotifications(t *testing.T) {
	c, _, now, _ := triggerFixture(t)
	events, stop := c.Bus.Subscribe(t.Context(), bus.Subscription{Topics: []string{dynamicconfig.PartChangedEvent}}, 10)
	defer stop()
	if _, err := c.Periodic(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	default:
		t.Fatal("initial effective output did not notify")
	}
	*now = now.Add(10 * time.Second)
	if _, err := c.Periodic(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		t.Fatalf("TTL/generation-only refresh notified: %+v", event)
	default:
	}
	*now = now.Add(31 * time.Minute)
	if _, err := c.Periodic(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	default:
		t.Fatal("expired-to-active recovery did not notify")
	}
}

func TestCancellationCannotPublishSuccessfulProcessResult(t *testing.T) {
	c, store, now, _ := triggerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	c.Runner = func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		cancel()
		return routerplugin.PluginResult{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "PluginResult"}, Status: routerplugin.PluginResultStatus{ObservedAt: *now, TTL: "1h"}}, routerplugin.RunOutcome{}, nil
	}
	if _, err := c.Periodic(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was ignored: %v", err)
	}
	parts, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(parts) != 0 {
		t.Fatalf("canceled process result was published: %+v %v", parts, err)
	}
}

func TestEmptyEventTopicDoesNotExpandToNamedNotifications(t *testing.T) {
	c, store, _, _ := triggerFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "empty"}, Spec: api.PluginSpec{Executable: "/not-invoked", Triggers: []api.PluginTrigger{{Type: "event"}}}}}}}
	empty, err := New(router, store, c.Bus, false)
	if err != nil {
		t.Fatal(err)
	}
	empty.Runner = func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		t.Fatal("blank topic invoked a plugin")
		return routerplugin.PluginResult{}, routerplugin.RunOutcome{}, nil
	}
	for _, topic := range []string{"routerd.controller.bootstrap", "routerd.controller.periodic", dynamicconfig.PartChangedEvent, "device.changed"} {
		if err := empty.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: topic}); err != nil {
			t.Fatal(err)
		}
	}
}
