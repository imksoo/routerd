// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPluginRunDryRunPrintsCandidateAndDoesNotWriteState(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is unavailable")
	}
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "cloud-plugin.sh")
	if err := os.WriteFile(pluginPath, []byte(`#!/bin/sh
cat <<'JSON'
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginResult",
  "metadata": { "name": "cloud" },
  "status": {
    "observedAt": "2026-05-29T12:00:00Z",
    "ttl": "5m",
    "resources": [
      {
        "apiVersion": "net.routerd.net/v1alpha1",
        "kind": "IPv4Route",
        "metadata": { "name": "cloud-route" },
        "spec": { "destination": "10.10.0.0/24", "gateway": "192.0.2.1" }
      }
    ],
    "directives": [
      {
        "op": "mask",
        "target": {
          "apiVersion": "net.routerd.net/v1alpha1",
          "kind": "IPv4Route",
          "name": "static-cloud-route"
        },
        "reason": "cloud route observed"
      }
    ],
    "actionPlans": [
      {
        "name": "attach-route",
        "provider": "oci",
        "action": "ensure-forwarding-enabled",
        "mode": "dry-run",
        "riskLevel": "low"
      }
    ]
  }
}
JSON
`), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata:
        name: cloud
      spec:
        executable: `+pluginPath+`
        timeout: 2s
        capabilities: [observe.cloud, propose.dynamicConfig]
`), 0644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "routerd.db")

	var out bytes.Buffer
	if err := run([]string{"plugin", "run", "cloud", "--dry-run", "--config", configPath, "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("plugin run --dry-run: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Plugin:", "cloud", "cloud-route", "static-cloud-route", "Action Plans:", "display-only; not executed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file exists after dry-run: stat err=%v", err)
	}
}

func TestPluginRunStoresBestEffortEvents(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is unavailable")
	}
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "cloud-plugin.sh")
	if err := os.WriteFile(pluginPath, []byte(`#!/bin/sh
cat <<'JSON'
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginResult",
  "metadata": { "name": "cloud" },
  "status": {
    "observedAt": "2026-05-29T12:00:00Z",
    "ttl": "5m"
  }
}
JSON
`), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata:
        name: cloud
      spec:
        executable: `+pluginPath+`
        timeout: 2s
        capabilities: [observe.cloud, propose.dynamicConfig]
`), 0644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "routerd.db")

	var out bytes.Buffer
	if err := run([]string{"plugin", "run", "cloud", "--config", configPath, "--state-file", statePath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("plugin run: %v\n%s", err, out.String())
	}
	store, err := routerstate.OpenSQLiteReadOnly(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer store.Close()
	events, err := store.ListEvents(routerstate.EventQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	byTopic := map[string]routerstate.StoredEvent{}
	for _, event := range events {
		byTopic[event.Topic] = event
	}
	for _, topic := range []string{"routerd.plugin.run.started", "routerd.plugin.run.succeeded", "routerd.dynamic.part.accepted"} {
		if _, ok := byTopic[topic]; !ok {
			t.Fatalf("missing event topic %q in %+v", topic, events)
		}
	}
	if got := byTopic["routerd.plugin.run.succeeded"].Attributes["plugin.name"]; got != "cloud" {
		t.Fatalf("succeeded plugin.name attr = %v", got)
	}
	if got := byTopic["routerd.dynamic.part.accepted"].Attributes["dynamic.source"]; got != "Plugin/cloud" {
		t.Fatalf("accepted dynamic.source attr = %v", got)
	}
	if got := byTopic["routerd.dynamic.part.accepted"].Attributes["dynamic.generation"]; got != "1" {
		t.Fatalf("accepted dynamic.generation attr = %v", got)
	}
}
