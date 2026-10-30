// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// The opt-in entry point uses a separately built production executable, rather
// than recursively building another binary inside an ordinary package test.
func TestPluginDynamicConfigThroughRealBinary(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_PLUGIN_PROCESS") != "1" {
		t.Skip("set ROUTERD_RUN_PLUGIN_PROCESS=1 and ROUTERCTL_BINARY to run the production CLI")
	}
	binary := os.Getenv("ROUTERCTL_BINARY")
	if !filepath.IsAbs(binary) {
		t.Fatal("ROUTERCTL_BINARY must name a separately built absolute executable")
	}
	dir := t.TempDir()
	statePath, configPath := filepath.Join(dir, "state.db"), filepath.Join(dir, "router.yaml")
	mode, request, payload := filepath.Join(dir, "mode"), filepath.Join(dir, "request.json"), filepath.Join(dir, "result.json")
	pluginPath := filepath.Join(dir, "inventory.sh")
	write := func(path, text string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(pluginPath, "#!/bin/sh\nset -eu\ncat >\"$REQUEST\"\nif [ \"$(cat \"$MODE\")\" = fail ]; then echo 'independent failure' >&2; exit 7; fi\ncat \"$PAYLOAD\"\n", 0755)
	startup := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: plugin-process}
spec:
  resources:
  - apiVersion: plugin.routerd.net/v1alpha1
    kind: Plugin
    metadata: {name: inventory}
    spec:
      executable: ` + pluginPath + `
      timeout: 2s
      capabilities: [propose.dynamicConfig]
      env: {REQUEST: ` + request + `, MODE: ` + mode + `, PAYLOAD: ` + payload + `}
  - apiVersion: plugin.routerd.net/v1alpha1
    kind: DynamicConfigSource
    metadata: {name: declared-source}
    spec: {pluginRef: inventory, ttl: 1h, triggers: [{type: interval, every: 1ms}]}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4Route
    metadata: {name: fallback}
    spec: {destination: 198.51.100.0/24, gateway: 192.0.2.1}
`
	policy := `  - apiVersion: config.routerd.net/v1alpha1
    kind: DynamicOverridePolicy
    metadata: {name: mask-inventory}
    spec:
      allow:
      - source: Plugin/inventory
        operations: [mask]
        targets: [{apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: fallback}]
`
	write(configPath, startup+policy, 0600)
	write(mode, "success", 0600)
	write(payload, `{"apiVersion":"plugin.routerd.net/v1alpha1","kind":"PluginResult","status":{"ttl":"10s","resources":[{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","metadata":{"name":"learned"},"spec":{"destination":"198.51.100.0/24","gateway":"192.0.2.2"}}],"directives":[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","name":"fallback"}}]}}`, 0600)
	runCLI := func(success bool, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		if (err == nil) != success {
			t.Fatalf("routerctl %v: err=%v output=%s", args, err, out)
		}
		return out
	}
	runPlugin := func(success bool) []byte {
		return runCLI(success, "plugin", "run", "inventory", "--config", configPath, "--state-file", statePath, "-o", "json")
	}
	render := func(success bool) []byte {
		return runCLI(success, "dynamic", "render", "--config", configPath, "--state-file", statePath, "-o", "json")
	}
	var report pluginRunReport
	if err := json.Unmarshal(runPlugin(true), &report); err != nil || !report.Stored || report.Part.Spec.Generation != 1 {
		t.Fatalf("first publication: report=%+v err=%v", report, err)
	}
	var input routerplugin.PluginRequest
	data, err := os.ReadFile(request)
	if err != nil || json.Unmarshal(data, &input) != nil || input.Metadata.Name != "inventory" || input.Spec.Trigger.Type != "manual" || input.Spec.PreviousDynamicGeneration != 0 {
		t.Fatalf("actual stdin request=%s err=%v", data, err)
	}
	assertRoute := func(want, absent string) {
		t.Helper()
		var effective api.Router
		if err := json.Unmarshal(render(true), &effective); err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, resource := range effective.Spec.Resources {
			if resource.Kind == "IPv4Route" {
				names[resource.Metadata.Name] = true
			}
		}
		if !names[want] || names[absent] {
			t.Fatalf("effective routes=%v, want %s without %s", names, want, absent)
		}
	}
	assertRoute("learned", "fallback")
	write(configPath, startup, 0600)
	if out := render(false); !strings.Contains(string(out), "mask not allowed") {
		t.Fatalf("policy removal did not reject the mask: %s", out)
	}
	write(configPath, startup+policy, 0600)
	write(mode, "fail", 0600)
	if out := runPlugin(false); !strings.Contains(string(out), "exit status 7") {
		t.Fatalf("plugin failure was not preserved: %s", out)
	}
	store, err := routerstate.OpenSQLiteReadOnly(statePath)
	if err != nil {
		t.Fatal(err)
	}
	parts, readErr := store.GetDynamicConfigPartsBySource("Plugin/inventory")
	store.Close()
	if readErr != nil || len(parts) != 1 || parts[0].Generation != 1 || !parts[0].ExpiresAt.Equal(report.Part.Spec.ExpiresAt) {
		t.Fatalf("failed invocation replaced or extended prior output: %+v err=%v", parts, readErr)
	}
	assertRoute("learned", "fallback")
	// Each render is a new production process reopening the same SQLite database.
	// Expiry must restore startup intent without another successful invocation.
	if wait := time.Until(report.Part.Spec.ExpiresAt) + 20*time.Millisecond; wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
	assertRoute("fallback", "learned")
	write(mode, "success", 0600)
	if err := json.Unmarshal(runPlugin(true), &report); err != nil || report.Part.Spec.Generation != 2 {
		t.Fatalf("new process did not advance durable generation: %+v err=%v", report, err)
	}
	assertRoute("learned", "fallback")
}
