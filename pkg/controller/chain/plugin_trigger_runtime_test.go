// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type pluginWorkerObserver struct{ started chan struct{} }

func (o pluginWorkerObserver) ControllerStarted(string, time.Duration) {
	select {
	case o.started <- struct{}{}:
	default:
	}
}
func (pluginWorkerObserver) ControllerReconciled(string, string, time.Duration, time.Duration, error) {
}

func TestDeclaredPluginNamedEventUsesFrameworkWorker(t *testing.T) {
	for _, topic := range []string{"inventory.refresh", "routerd.controller.bootstrap", "routerd.controller.periodic"} {
		t.Run(topic, func(t *testing.T) { declaredPluginNamedEventUsesFrameworkWorker(t, topic) })
	}
}

func declaredPluginNamedEventUsesFrameworkWorker(t *testing.T, topic string) {
	t.Helper()
	dir := t.TempDir()
	request := filepath.Join(dir, "request.json")
	executable := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nset -eu\ncat >\"$REQUEST\"\nprintf '%s\\n' '{\"apiVersion\":\"plugin.routerd.net/v1alpha1\",\"kind\":\"PluginResult\",\"status\":{\"ttl\":\"1h\"}}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "inventory"}, Spec: api.PluginSpec{Executable: executable, Env: map[string]string{"REQUEST": request}, Triggers: []api.PluginTrigger{{Type: "event", Topic: topic}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.DynamicConfigSourceSpec{PluginRef: "inventory", TTL: "2m", Triggers: []api.PluginTrigger{{Type: "event", Topic: topic}}}},
	}}}
	r := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"plugin"}}}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	controllers, _, _, err := r.frameworkControllers(ctx, logger, eventedStore{Store: store, Router: router}, false, ha.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	var declared framework.Controller
	for _, controller := range controllers {
		if controller.Name() == "plugin" {
			declared = controller
			break
		}
	}
	if declared == nil {
		t.Fatal("prepared Plugin owner is absent from the real chain")
	}
	observer := pluginWorkerObserver{started: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() {
		done <- (framework.Runner{Bus: r.Bus, Logger: logger, Observer: observer, Interval: time.Hour}).Run(ctx, declared)
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("framework stop: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Plugin framework worker did not stop")
		}
		stopped = true
	}
	t.Cleanup(stop)
	select {
	case <-observer.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := os.Stat(request); !os.IsNotExist(err) {
		t.Fatalf("event-only declaration ran at bootstrap: %v", err)
	}
	if err := r.Bus.Publish(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Name: "fixture", Kind: "test"}, topic, daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	for {
		parts, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 0 {
			if len(parts) != 1 || parts[0].Generation != 1 || parts[0].ExpiresAt.Sub(parts[0].ObservedAt) != 2*time.Minute {
				t.Fatalf("event union/source policy: %+v", parts)
			}
			break
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	data, err := os.ReadFile(request)
	if err != nil {
		t.Fatal(err)
	}
	var received routerplugin.PluginRequest
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatal(err)
	}
	if received.Spec.Trigger.Type != "event" || received.Spec.Trigger.Topic != topic {
		t.Fatalf("actual event request: %+v", received)
	}
	stop()
}

func TestDeclaredPluginReadOnlyModesNeverStartExecutable(t *testing.T) {
	for _, mode := range []string{"preview", "standby"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			marker, executable := filepath.Join(dir, "executed"), filepath.Join(dir, "plugin.sh")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\ncat >\"$MARKER\"\nprintf '%s\\n' '{\"apiVersion\":\"plugin.routerd.net/v1alpha1\",\"kind\":\"PluginResult\",\"status\":{\"ttl\":\"1m\"}}'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "inventory"}, Spec: api.PluginSpec{Executable: executable, Env: map[string]string{"MARKER": marker}, Triggers: []api.PluginTrigger{{Type: "interval", Every: "1s"}}}}}}}
			r := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"plugin"}, DryRunPlugin: mode == "preview"}}
			if mode == "standby" {
				r.HADecision = &ha.Decision{Enabled: true, Leader: false}
			}
			if err := r.ReconcileOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("read-only mode executed plugin: %v", err)
			}
			parts, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
			if err != nil || len(parts) != 0 {
				t.Fatalf("read-only mode published: %+v %v", parts, err)
			}
			if mode == "standby" && r.Opts.DryRunPlugin {
				t.Fatal("standby poisoned the next leader generation's options")
			}
		})
	}
}

func TestDeclaredPluginIntervalRepeatsWithinFrameworkWorker(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"apiVersion\":\"plugin.routerd.net/v1alpha1\",\"kind\":\"PluginResult\",\"status\":{\"ttl\":\"1h\"}}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
		Metadata: api.ObjectMeta{Name: "inventory"},
		Spec:     api.PluginSpec{Executable: executable, Triggers: []api.PluginTrigger{{Type: "interval", Every: "30ms"}}},
	}}}}
	r := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"plugin"}}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	controllers, _, _, err := r.frameworkControllers(ctx, logger, eventedStore{Store: store, Router: router}, false, ha.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	var declared framework.Controller
	for _, controller := range controllers {
		if controller.Name() == "plugin" {
			declared = controller
			break
		}
	}
	if declared == nil {
		t.Fatal("prepared Plugin owner is absent from the real chain")
	}
	done := make(chan error, 1)
	go func() {
		done <- (framework.Runner{Bus: r.Bus, Logger: logger, Interval: time.Hour}).Run(ctx, declared)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("framework stop: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Plugin interval worker did not stop")
		}
	})
	for {
		parts, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) >= 2 {
			first, second := false, false
			for _, part := range parts {
				first = first || part.Generation == 1
				second = second || part.Generation == 2
			}
			if !first || !second {
				t.Fatalf("actual interval publications: %+v", parts)
			}
			break
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("positive interval deadline did not start the next actual process")
		}
	}
}
