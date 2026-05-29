// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
        "provider": "test",
        "action": "AttachRoute",
        "target": { "route": "cloud-route" }
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
