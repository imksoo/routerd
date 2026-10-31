// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type publicationWaitStore struct {
	*routerstate.SQLiteStore
	resumeAt  time.Time
	enteredAt *time.Time
}

func (s publicationWaitStore) PublishPluginResult(ctx context.Context, previous int64, part routerstate.DynamicConfigPartRecord) error {
	// Reproduce a persistence wait after the caller's lifetime check, then use
	// the real SQLite CAS boundary instead of implementing publication here.
	if s.enteredAt != nil {
		*s.enteredAt = time.Now()
	}
	resume := s.resumeAt
	if resume.IsZero() {
		resume = part.ExpiresAt.Add(10 * time.Millisecond)
	}
	timer := time.NewTimer(time.Until(resume))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.SQLiteStore.PublishPluginResult(ctx, previous, part)
}

func TestPublicationNotifiesRecoveryAfterPreviousExpiresDuringWait(t *testing.T) {
	c, store, _, _ := triggerFixture(t)
	c.Now = nil
	c.Runner = func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		return routerplugin.PluginResult{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "PluginResult"}, Status: routerplugin.PluginResultStatus{ObservedAt: time.Now().UTC(), TTL: "1s"}}, routerplugin.RunOutcome{}, nil
	}
	events, stop := c.Bus.Subscribe(t.Context(), bus.Subscription{Topics: []string{dynamicconfig.PartChangedEvent}}, 10)
	defer stop()
	if _, err := c.Periodic(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	default:
		t.Fatal("initial result did not notify")
	}
	before, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(before) != 1 {
		t.Fatalf("initial publication: %+v %v", before, err)
	}
	var entered time.Time
	c.Store = publicationWaitStore{SQLiteStore: store, resumeAt: before[0].ExpiresAt.Add(10 * time.Millisecond), enteredAt: &entered}
	c.Runner = func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		return routerplugin.PluginResult{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "PluginResult"}, Status: routerplugin.PluginResultStatus{ObservedAt: time.Now().UTC(), TTL: "1h"}}, routerplugin.RunOutcome{}, nil
	}
	if err := c.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: "device.changed"}); err != nil {
		t.Fatal(err)
	}
	if !entered.Before(before[0].ExpiresAt) {
		t.Fatal("fixture did not reach publication before the old expiry")
	}
	select {
	case <-events:
	default:
		t.Fatal("same content restored after expiry without notification")
	}
}

func TestPublicationExpiryDuringWaitPreservesPreviousGeneration(t *testing.T) {
	c, store, _, _ := triggerFixture(t)
	c.Now = nil
	if _, err := c.Periodic(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(before) != 1 {
		t.Fatalf("missing previous publication: %+v %v", before, err)
	}
	c.Store = publicationWaitStore{SQLiteStore: store}
	c.Runner = func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
		return routerplugin.PluginResult{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "PluginResult"}, Status: routerplugin.PluginResultStatus{ObservedAt: time.Now().UTC(), TTL: "20ms"}}, routerplugin.RunOutcome{}, nil
	}
	err = c.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: "device.changed"})
	after, readErr := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err == nil || readErr != nil || len(after) != 1 || after[0].Generation != before[0].Generation || !after[0].ExpiresAt.Equal(before[0].ExpiresAt) {
		t.Fatalf("expired publication advanced or replaced the previous generation: err=%v before=%+v after=%+v read=%v", err, before, after, readErr)
	}
}
