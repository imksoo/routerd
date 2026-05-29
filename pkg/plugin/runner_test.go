// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestRunDecodesJSONStdoutWithTypedResourceSpec(t *testing.T) {
	requireShell(t)
	path := writePluginScript(t, `#!/bin/sh
cat <<'JSON'
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginResult",
  "metadata": { "name": "test-plugin" },
  "status": {
    "observedAt": "2026-05-29T12:00:00Z",
    "ttl": "5m",
    "resources": [
      {
        "apiVersion": "net.routerd.net/v1alpha1",
        "kind": "IPv4Route",
        "metadata": { "name": "cloud-route" },
        "spec": { "destination": "10.0.0.0/24", "gateway": "192.0.2.1" }
      }
    ]
  }
}
JSON
`)
	result, outcome, err := Run(context.Background(), api.PluginSpec{Executable: path}, "test-plugin", RunOptions{
		Now:     time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		Trigger: TriggerRef{Type: "manual"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.HasExitCode || outcome.ExitCode != 0 {
		t.Fatalf("outcome exit = %#v", outcome)
	}
	if len(result.Status.Resources) != 1 {
		t.Fatalf("resources = %#v", result.Status.Resources)
	}
	spec, ok := result.Status.Resources[0].Spec.(api.IPv4RouteSpec)
	if !ok {
		t.Fatalf("resource spec type = %T, want api.IPv4RouteSpec", result.Status.Resources[0].Spec)
	}
	if spec.Destination != "10.0.0.0/24" {
		t.Fatalf("destination = %q", spec.Destination)
	}
}

func TestRunTimeout(t *testing.T) {
	requireShell(t)
	path := writePluginScript(t, "#!/bin/sh\nsleep 1\n")
	_, outcome, err := Run(context.Background(), api.PluginSpec{Executable: path, Timeout: "20ms"}, "sleeper", RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if outcome.Error == "" {
		t.Fatalf("outcome error is empty")
	}
}

func TestRunRejectsMalformedStdout(t *testing.T) {
	requireShell(t)
	path := writePluginScript(t, "#!/bin/sh\nprintf '%s\n' '{'\n")
	_, outcome, err := Run(context.Background(), api.PluginSpec{Executable: path}, "bad", RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "decode plugin bad stdout") {
		t.Fatalf("error = %v, want decode error", err)
	}
	if outcome.StdoutDigest == "" {
		t.Fatalf("stdout digest is empty")
	}
}

func requireShell(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is unavailable")
	}
}

func writePluginScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.sh")
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
