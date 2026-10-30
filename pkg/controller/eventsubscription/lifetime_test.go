// SPDX-License-Identifier: BSD-3-Clause

package eventsubscription

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func subscriptionLifetimeFixture(t *testing.T, now func() time.Time, runner PluginRunner, batchWindow string) (*Controller, *routerstate.SQLiteStore) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.RecordFederationEvent(routerstate.EventRecord{ID: "event", Group: "edge", Type: "route.changed", ObservedAt: now(), ExpiresAt: now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"}, Metadata: api.ObjectMeta{Name: "routes"}, Spec: api.EventSubscriptionSpec{GroupRef: "edge", Match: api.EventSubscriptionMatch{Types: []string{"route.changed"}}, Trigger: api.EventSubscriptionTrigger{PluginRef: "plugin", BatchWindow: batchWindow}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "plugin"}, Spec: api.PluginSpec{Executable: "/unused"}},
	}}}, Store: store, Now: now, PluginRunner: runner}
	return controller, store
}

func TestScheduledSubscriptionDropsExpiredFactsBeforePlugin(t *testing.T) {
	initial := time.Now().UTC()
	var clock atomic.Int64
	clock.Store(initial.UnixNano())
	var calls atomic.Int64
	controller, store := subscriptionLifetimeFixture(t, func() time.Time { return time.Unix(0, clock.Load()).UTC() }, func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		calls.Add(1)
		return eventSubscriptionTestResult(initial), routerplugin.RunOutcome{}, nil
	}, "15ms")
	// Hold the actual scheduled callback until the input clock has expired.
	// A wall-clock timer can otherwise fire before Reconcile returns.
	var fire func()
	controller.NewTimer = func(_ time.Duration, fn func()) triggerTimer {
		fire = fn
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		return timer
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.Start(ctx)
	defer controller.Stop()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	clock.Store(initial.Add(2 * time.Second).UnixNano())
	if fire == nil {
		t.Fatal("subscription did not schedule processing")
	}
	fire()
	parts, err := store.ListDynamicConfigParts()
	if err != nil || calls.Load() != 0 || len(parts) != 0 {
		t.Fatalf("expired facts invoked plugin or published config: calls=%d parts=%+v err=%v", calls.Load(), parts, err)
	}
}

func TestScheduledSubscriptionRunsAfterReconcileReturns(t *testing.T) {
	now := time.Now().UTC()
	var calls atomic.Int64
	controller, store := subscriptionLifetimeFixture(t, func() time.Time { return now }, func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		calls.Add(1)
		return eventSubscriptionTestResult(now), routerplugin.RunOutcome{}, nil
	}, "15ms")
	owner, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.Start(owner)
	defer controller.Stop()
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		parts, err := store.ListDynamicConfigParts()
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) == 1 {
			if calls.Load() != 1 {
				t.Fatalf("scheduled plugin invocations = %d", calls.Load())
			}
			return
		}
		select {
		case <-time.After(time.Millisecond):
		case <-deadline.C:
			t.Fatalf("trigger was lost after Reconcile returned: calls=%d parts=%+v", calls.Load(), parts)
		}
	}
}

func TestSubscriptionRechecksFactExpiryAfterPluginWait(t *testing.T) {
	now := time.Now().UTC()
	initial := now
	controller, store := subscriptionLifetimeFixture(t, func() time.Time { return now }, func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		now = now.Add(2 * time.Second)
		return eventSubscriptionTestResult(initial), routerplugin.RunOutcome{}, nil
	}, "")
	defer controller.Stop()
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("plugin result was accepted after its input expired")
	}
	parts, err := store.ListDynamicConfigParts()
	if err != nil || len(parts) != 0 {
		t.Fatalf("expired result became desired config: %+v %v", parts, err)
	}
}

func TestStandbyObservationDoesNotConsumeLivePluginRetries(t *testing.T) {
	now := time.Now().UTC()
	calls := 0
	controller, store := subscriptionLifetimeFixture(t, func() time.Time { return now }, func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		calls++
		if calls == 1 {
			return routerplugin.PluginResult{}, routerplugin.RunOutcome{}, errors.New("temporary plugin failure")
		}
		return eventSubscriptionTestResult(now), routerplugin.RunOutcome{}, nil
	}, "")
	defer controller.Stop()
	controller.DryRun = true
	for range 5 {
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, attempts, found, err := store.SubscriptionRunStatus("EventSubscription/routes", "event"); err != nil || found || attempts != 0 || calls != 0 {
		t.Fatalf("standby consumed invocation attempts=%d found=%t calls=%d err=%v", attempts, found, calls, err)
	}
	controller.DryRun = false
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("live plugin failure was hidden")
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first live failure exhausted retry budget: %v", err)
	}
	if status, attempts, found, err := store.SubscriptionRunStatus("EventSubscription/routes", "event"); err != nil || !found || status != "succeeded" || attempts != 2 || calls != 2 {
		t.Fatalf("live retry status=%q attempts=%d found=%t calls=%d err=%v", status, attempts, found, calls, err)
	}
}

func TestSubscriptionStopWaitsForPluginAndFencesCanceledResult(t *testing.T) {
	now := time.Now().UTC()
	started, release := make(chan struct{}), make(chan struct{})
	controller, store := subscriptionLifetimeFixture(t, func() time.Time { return now }, func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		close(started)
		<-release // A slow runner need not acknowledge cancellation promptly.
		return eventSubscriptionTestResult(now), routerplugin.RunOutcome{}, nil
	}, "")
	owner, cancel := context.WithCancel(context.Background())
	done := controller.Start(owner)
	result := make(chan error, 1)
	go func() { result <- controller.Reconcile(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("plugin did not start")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("owner acknowledged while plugin was still running")
	default:
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled operation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("plugin result was not fenced")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("owner did not acknowledge completed operations")
	}
	parts, err := store.ListDynamicConfigParts()
	if err != nil || len(parts) != 0 {
		t.Fatalf("canceled result became desired config: %+v %v", parts, err)
	}
}

func TestCanceledSubscriptionOwnerRejectsAdmissionBeforeStopCallback(t *testing.T) {
	owner, cancel := context.WithCancel(context.Background())
	controller := &Controller{ownerCtx: owner}
	// The owner is canceled, but its asynchronous Stop callback has not run.
	cancel()
	_, finish, err := controller.beginProcess(context.Background())
	if finish != nil {
		finish()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("admission after owner cancellation = %v", err)
	}
}

func TestSubscriptionResultChecksOwnerWithoutWaitingForCancellationCallback(t *testing.T) {
	now := time.Now().UTC()
	owner, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &eventSubscriptionTestStore{events: []routerstate.EventRecord{{ID: "event", Group: "edge", Type: "route.changed"}}}
	controller := &Controller{
		Router:   &api.Router{},
		Store:    store,
		Now:      func() time.Time { return now },
		ownerCtx: owner,
		PluginRunner: func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
			cancel()
			return eventSubscriptionTestResult(now), routerplugin.RunOutcome{}, nil
		},
	}
	defer controller.Stop()
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"}, Metadata: api.ObjectMeta{Name: "routes"}}
	spec := api.EventSubscriptionSpec{GroupRef: "edge", Match: api.EventSubscriptionMatch{Types: []string{"route.changed"}}, Trigger: api.EventSubscriptionTrigger{PluginRef: "plugin"}}
	if err := controller.processBatch(context.Background(), resource, spec, api.PluginSpec{}, store.events, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("result after owner cancellation = %v", err)
	}
	if len(store.parts) != 0 {
		t.Fatalf("canceled result became desired config: %+v", store.parts)
	}
}
