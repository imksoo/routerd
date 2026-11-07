// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package eventsubscription

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRealPluginStopFailureAndDurableRestart(t *testing.T) {
	dir := t.TempDir()
	mode, pidPath, calls := filepath.Join(dir, "mode"), filepath.Join(dir, "pid"), filepath.Join(dir, "calls")
	pluginPath := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(pluginPath, []byte(`#!/bin/sh
set -eu
echo invoked >>"$PLUGIN_CALLS"
echo $$ >"$PLUGIN_PID"
case "$(cat "$PLUGIN_MODE")" in
block) exec sleep 30 ;;
fail) echo 'independent plugin failure' >&2; exit 7 ;;
success) printf '%s\n' '{"apiVersion":"plugin.routerd.net/v1alpha1","kind":"PluginResult","status":{"ttl":"1m","resources":[{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","metadata":{"name":"from-real-plugin"},"spec":{"destination":"198.51.100.0/24","gateway":"192.0.2.1"}}]}}' ;;
esac
`), 0755); err != nil {
		t.Fatal(err)
	}
	setMode := func(value string) {
		t.Helper()
		if err := os.WriteFile(mode, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	statePath := filepath.Join(dir, "state.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.RecordFederationEvent(routerstate.EventRecord{ID: "event", Group: "edge", Type: "route.changed", ObservedAt: now, ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"}, Metadata: api.ObjectMeta{Name: "routes"}, Spec: api.EventSubscriptionSpec{GroupRef: "edge", Match: api.EventSubscriptionMatch{Types: []string{"route.changed"}}, Trigger: api.EventSubscriptionTrigger{PluginRef: "plugin"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "plugin"}, Spec: api.PluginSpec{Executable: pluginPath, Timeout: "10s", Env: map[string]string{"PLUGIN_MODE": mode, "PLUGIN_PID": pidPath, "PLUGIN_CALLS": calls}}},
	}}}
	controller := &Controller{Router: router, Store: store}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	controller.Start(ctx)
	t.Cleanup(controller.Stop)
	setMode("block")
	finished := make(chan error, 1)
	go func() { finished <- controller.Reconcile(ctx) }()
	var pid int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, readErr := os.ReadFile(pidPath); readErr == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("real plugin did not start")
	}
	stopped := make(chan struct{})
	go func() { controller.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not acknowledge the real plugin process")
	}
	if err := <-finished; err == nil {
		t.Fatal("stopped real invocation reported success")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("stopped plugin still exists: pid=%d err=%v", pid, err)
	}
	assertParts := func(want int) []routerstate.DynamicConfigPartRecord {
		t.Helper()
		parts, err := store.ListDynamicConfigParts()
		if err != nil || len(parts) != want {
			t.Fatalf("parts=%+v err=%v, want %d", parts, err, want)
		}
		return parts
	}
	assertParts(0)

	// A new owner retries durable unfinished work using the production runner.
	controller = &Controller{Router: router, Store: store}
	t.Cleanup(controller.Stop)
	setMode("fail")
	if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("real plugin failure was lost: %v", err)
	}
	assertParts(0)
	setMode("success")
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	parts := assertParts(1)
	part, err := codec.Decode(parts[0])
	if err != nil || len(part.Spec.Resources) != 1 || part.Spec.Resources[0].Metadata.Name != "from-real-plugin" {
		t.Fatalf("real stdout was not published: part=%+v err=%v", part, err)
	}
	controller.Stop()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	controller = &Controller{Router: router, Store: store}
	t.Cleanup(controller.Stop)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertParts(1)
	data, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(data), "invoked\n") != 3 {
		t.Fatalf("restart replayed acknowledged work: calls=%q err=%v", data, err)
	}
}
