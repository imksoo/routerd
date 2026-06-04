// SPDX-License-Identifier: BSD-3-Clause

package providerinventory

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func inventorySpec(bin string) api.PluginSpec {
	return api.PluginSpec{
		Executable:   bin,
		Timeout:      "10s",
		Capabilities: []string{CapabilityObserveProviderPrivateIPs},
	}
}

func TestRunInventoryRejectsMissingCapability(t *testing.T) {
	bin := writeInventoryPlugin(t, `{"apiVersion":"providerinventory.routerd.net/v1alpha1","kind":"ObservePrivateIPsResult","status":{"status":"succeeded"}}`)
	spec := inventorySpec(bin)
	spec.Capabilities = []string{"observe.cloud"}
	req := NewObservePrivateIPsRequest(ObservePrivateIPsRequestSpec{Provider: "aws", SelfNode: "aws-router", Pool: "cloudedge", Prefix: "10.88.60.0/24", SelfNICRef: "eni-router"})
	_, _, err := RunInventory(context.Background(), spec, req)
	if err == nil || !strings.Contains(err.Error(), "lacks capability") {
		t.Fatalf("want capability refusal, got %v", err)
	}
}

func TestRunInventoryRoundTrip(t *testing.T) {
	bin := writeInventoryPlugin(t, `{"apiVersion":"providerinventory.routerd.net/v1alpha1","kind":"ObservePrivateIPsResult","status":{"status":"succeeded","self":{"nicRef":"eni-router","subnetRef":"subnet-a","privateIPs":["10.88.60.21"],"forwardingEnabled":false},"ips":[{"address":"10.88.60.11","nicRef":"eni-client","subnetRef":"subnet-a","tags":{"cloudedge-mobility":"true"}}]}}`)
	req := NewObservePrivateIPsRequest(ObservePrivateIPsRequestSpec{Provider: "aws", ProviderRef: "aws-provider", SelfNode: "aws-router", Pool: "cloudedge", Prefix: "10.88.60.0/24", SelfNICRef: "eni-router"})
	res, outcome, err := RunInventory(context.Background(), inventorySpec(bin), req)
	if err != nil {
		t.Fatalf("RunInventory: %v stderr=%s", err, outcome.Stderr)
	}
	if res.Status.Status != ResultSucceeded || len(res.Status.IPs) != 1 || res.Status.IPs[0].Address != "10.88.60.11" {
		t.Fatalf("result = %#v", res)
	}
	if res.Status.Self == nil || res.Status.Self.NICRef != "eni-router" || res.Status.Self.SubnetRef != "subnet-a" || len(res.Status.Self.PrivateIPs) != 1 {
		t.Fatalf("self = %#v", res.Status.Self)
	}
	if res.Status.Self.ForwardingEnabled == nil || *res.Status.Self.ForwardingEnabled {
		t.Fatalf("self.forwardingEnabled = %#v, want false", res.Status.Self.ForwardingEnabled)
	}
}

func TestRunInventoryEnvNotInherited(t *testing.T) {
	bin := writeEnvCheckingInventoryPlugin(t)
	t.Setenv("ROUTERD_SECRET_TOKEN", "secret")
	res, _, err := RunInventory(context.Background(), inventorySpec(bin), NewObservePrivateIPsRequest(ObservePrivateIPsRequestSpec{Provider: "aws", SelfNode: "aws-router", Pool: "cloudedge", Prefix: "10.88.60.0/24", SelfNICRef: "eni-router"}))
	if err != nil {
		t.Fatalf("RunInventory: %v", err)
	}
	if res.Status.Status != ResultSucceeded {
		t.Fatalf("status = %q", res.Status.Status)
	}
}

func writeInventoryPlugin(t *testing.T, output string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	path := filepath.Join(t.TempDir(), "inventory")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + strings.ReplaceAll(output, "'", "'\\''") + "'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	return path
}

func writeEnvCheckingInventoryPlugin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	path := filepath.Join(t.TempDir(), "inventory-env")
	body := `#!/bin/sh
cat >/dev/null
if [ -n "$ROUTERD_SECRET_TOKEN" ]; then
  printf '%s' '{"apiVersion":"providerinventory.routerd.net/v1alpha1","kind":"ObservePrivateIPsResult","status":{"status":"failed","error":"secret leaked"}}'
  exit 0
fi
printf '%s' '{"apiVersion":"providerinventory.routerd.net/v1alpha1","kind":"ObservePrivateIPsResult","status":{"status":"succeeded"}}'
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	return path
}
