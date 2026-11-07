// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDeclaredPluginIntervalUsesRealProcessAndSourceLifetime(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.json")
	executable := filepath.Join(dir, "inventory.sh")
	script := `#!/bin/sh
set -eu
cat >"$REQUEST"
printf '%s\n' '{"apiVersion":"plugin.routerd.net/v1alpha1","kind":"PluginResult","status":{"ttl":"1h","resources":[{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","metadata":{"name":"learned"},"spec":{"destination":"198.51.100.0/24","gateway":"192.0.2.1"}}]}}'
`
	if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "inventory"}, Spec: api.PluginSpec{Executable: executable, Timeout: "2s", Env: map[string]string{"REQUEST": requestPath}, Triggers: []api.PluginTrigger{{Type: "interval", Every: "1h"}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"}, Metadata: api.ObjectMeta{Name: "inventory-policy"}, Spec: api.DynamicConfigSourceSpec{PluginRef: "inventory", TTL: "1s"}},
	}}}
	runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"plugin"}}}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	parts, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(parts) != 1 {
		t.Fatalf("declared interval did not publish through the actual runtime: parts=%+v err=%v", parts, err)
	}
	if parts[0].Generation != 1 || parts[0].ExpiresAt.Sub(parts[0].ObservedAt) != time.Second {
		t.Fatalf("source lifetime did not bound the plugin result: %+v", parts[0])
	}
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request routerplugin.PluginRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if request.Metadata.Name != "inventory" || request.Spec.Trigger.Type != "interval" || request.Spec.PreviousDynamicGeneration != 0 {
		t.Fatalf("actual interval request: %+v", request)
	}
}
