// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestManualPluginCannotExtendDeclaredSourceLifetime(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "inventory.sh")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"apiVersion\":\"plugin.routerd.net/v1alpha1\",\"kind\":\"PluginResult\",\"status\":{\"ttl\":\"1h\"}}'\n"
	if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: "routerd.net/v1alpha1", Kind: "Router"}, Metadata: api.ObjectMeta{Name: "source-policy"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "inventory"}, Spec: api.PluginSpec{Executable: executable, Timeout: "2s"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.DynamicConfigSourceSpec{PluginRef: "inventory", TTL: "1s"}},
	}}}
	data, err := json.Marshal(router)
	if err != nil {
		t.Fatal(err)
	}
	configPath, statePath := filepath.Join(dir, "router.json"), filepath.Join(dir, "state.db")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := pluginRunCommand([]string{"inventory", "--config", configPath, "--state-file", statePath, "-o", "json"}, &output); err != nil {
		t.Fatal(err)
	}
	var report pluginRunReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Stored || report.Part.Spec.ExpiresAt.Sub(report.Part.Spec.ObservedAt) != time.Second {
		t.Fatalf("manual publication bypassed source lifetime: %+v", report)
	}
	store, err := routerstate.OpenSQLiteReadOnly(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, err := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	if err != nil || len(records) != 1 || !records[0].ExpiresAt.Equal(report.Part.Spec.ExpiresAt) {
		t.Fatalf("durable source lifetime: records=%+v err=%v", records, err)
	}
}
