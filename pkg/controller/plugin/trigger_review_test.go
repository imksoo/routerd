//go:build linux || freebsd

// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func actualTriggerReview(t *testing.T, declarations map[string][]api.PluginTrigger) (*Controller, *routerstate.SQLiteStore) {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "plugin.sh")
	script := `#!/bin/sh
set -eu
cat >/dev/null
echo '{"apiVersion":"plugin.routerd.net/v1alpha1","kind":"PluginResult","status":{"ttl":"24h"}}'
`
	if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "trigger-review"}}
	for name, triggers := range declarations {
		router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.PluginSpec{Executable: executable, Triggers: triggers}})
	}
	if err := config.ValidateForOS(router, platform.CurrentOS()); err != nil {
		t.Fatal(err)
	}
	controller, err := New(router, store, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return controller, store
}

func TestReviewNamedEventDoesNotConsumeUnrelatedIntervalDue(t *testing.T) {
	controller, store := actualTriggerReview(t, map[string][]api.PluginTrigger{
		"named":    {{Type: "event", Topic: "inventory.changed"}},
		"interval": {{Type: "interval", Every: "1h"}},
	})
	now := time.Now().UTC()
	controller.Now = func() time.Time { return now }
	if worked, err := controller.Periodic(t.Context()); err != nil || !worked {
		t.Fatalf("initial interval: worked=%v err=%v", worked, err)
	}
	now = now.Add(time.Hour)
	if err := controller.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: "inventory.changed"}); err != nil {
		t.Fatal(err)
	}
	named, err := store.GetDynamicConfigPartsBySource("Plugin/named")
	if err != nil || len(named) != 1 {
		t.Fatalf("named event did not execute actual plugin: parts=%+v err=%v", named, err)
	}
	unrelated, err := store.GetDynamicConfigPartsBySource("Plugin/interval")
	if err != nil || len(unrelated) != 1 {
		t.Errorf("named event executed an unrelated due plugin: parts=%+v err=%v", unrelated, err)
	}
	if worked, err := controller.Periodic(t.Context()); err != nil || !worked {
		t.Errorf("named event consumed the other plugin interval: worked=%v err=%v", worked, err)
	}
}

func TestReviewExplicitControllerEventTopicIsNotSilenced(t *testing.T) {
	for _, topic := range []string{"routerd.controller.bootstrap", "routerd.controller.periodic"} {
		t.Run(topic, func(t *testing.T) {
			controller, store := actualTriggerReview(t, map[string][]api.PluginTrigger{"named": {{Type: "event", Topic: topic}}})
			if worked, err := controller.Periodic(t.Context()); err != nil || worked {
				t.Fatalf("periodic call executed event-only plugin: worked=%v err=%v", worked, err)
			}
			if err := controller.HandleEvent(t.Context(), daemonapi.DaemonEvent{Type: topic}); err != nil {
				t.Fatal(err)
			}
			parts, err := store.GetDynamicConfigPartsBySource("Plugin/named")
			if err != nil || len(parts) != 1 {
				t.Fatalf("explicit event topic was silently ignored: topic=%s parts=%+v err=%v", topic, parts, err)
			}
		})
	}
}
