// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/eventsubscription"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// TestEventToRemoteClaimEndToEnd exercises the full CloudEdge Event Federation
// Phase 3 receiver path with the SHIPPED example plugin and the REAL
// `routerctl dynamic render` command:
//
//	observed event (already federated, Phase 2) -> EventSubscription match ->
//	example plugin -> DynamicConfigPart -> effective config.
//
// It builds examples/plugins/event-to-remote-claim into a temp binary, records a
// routerd.client.ipv4.observed event, reconciles the EventSubscription
// controller against a temp state DB, and then asserts the operator-visible
// `routerctl dynamic render --state-file <db>` output contains the
// RemoteAddressClaim with its provenance annotations.
func TestEventToRemoteClaimEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	pluginBin := buildExamplePlugin(t, tmp)

	statePath := filepath.Join(tmp, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Close before invoking the read-only render command so there is no writer
	// holding the DB; render reopens it read-only.
	closeOnce := func() {
		if store != nil {
			_ = store.Close()
			store = nil
		}
	}
	defer closeOnce()

	const (
		group   = "cloudedge"
		subject = "10.88.60.9/32"
		eventID = "evt-e2e-1"
	)

	if err := store.RecordFederationEvent(routerstate.EventRecord{
		ID:         eventID,
		Group:      group,
		SourceNode: "onprem",
		Type:       "routerd.client.ipv4.observed",
		Subject:    subject,
		Payload: map[string]string{
			"address":     subject,
			"domain":      "cloudedge-same-subnet",
			"ownerSide":   "onprem",
			"peerRef":     "onprem-main",
			"providerRef": "example-provider",
			"nicRef":      "example-nic-ref",
		},
		ObservedAt: time.Date(2026, 5, 30, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record federation event: %v", err)
	}

	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "cloudedge-receiver"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
				Metadata: api.ObjectMeta{Name: "event-to-remote-claim"},
				Spec:     api.PluginSpec{Executable: pluginBin, Timeout: "10s"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"},
				Metadata: api.ObjectMeta{Name: "cloud-claims"},
				Spec: api.EventSubscriptionSpec{
					GroupRef: group,
					Match:    api.EventSubscriptionMatch{Types: []string{"routerd.client.ipv4.observed"}},
					Trigger:  api.EventSubscriptionTrigger{PluginRef: "event-to-remote-claim"},
				},
			},
		}},
	}

	ctrl := eventsubscription.Controller{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC) },
	}
	if err := ctrl.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Assert a DynamicConfigPart was persisted by the controller.
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("list dynamic config parts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("want 1 dynamic config part, got %d: %+v", len(parts), parts)
	}

	// Write a minimal startup config so `dynamic render` can merge it with the
	// active parts. The receiver-side EventGroup/EventSubscription/Plugin would
	// normally live here; render only needs a valid Router.
	configPath := filepath.Join(tmp, "router.yaml")
	if err := os.WriteFile(configPath, []byte(startupConfigYAML), 0o644); err != nil {
		t.Fatalf("write startup config: %v", err)
	}

	// Release the writer handle before the read-only render reopens the DB.
	closeOnce()

	// KEY ASSERTION: go through the actual `routerctl dynamic render` command so
	// we prove the operator-visible Phase 3 acceptance ("routerctl dynamic render
	// shows RemoteAddressClaim").
	var out bytes.Buffer
	if err := run([]string{
		"dynamic", "render",
		"--config", configPath,
		"--state-file", statePath,
		"-o", "yaml",
	}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("dynamic render: %v\n%s", err, out.String())
	}

	rendered := out.String()
	for _, want := range []string{
		"kind: RemoteAddressClaim",
		"address: " + subject,
		"name: onprem-10-88-60-9",
		"ownerSide: onprem",
		"domainRef: cloudedge-same-subnet",
		"type: provider-secondary-ip",
		"tunnelInterface: wg-hybrid",
		// provenance annotations stamped by the controller.
		"routerd.net/event-id: " + eventID,
		"routerd.net/event-group: " + group,
		"routerd.net/dynamic-source: EventSubscription/cloud-claims",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("dynamic render output missing %q\n---\n%s", want, rendered)
		}
	}
}

// buildExamplePlugin compiles the shipped, provider-agnostic example plugin into
// a temp binary so the test exercises the REAL plugin, not a throwaway fake.
func buildExamplePlugin(t *testing.T, dir string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	pkgDir := filepath.Join(repoRoot, "examples", "plugins", "event-to-remote-claim")

	bin := filepath.Join(dir, "event-to-remote-claim")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = pkgDir
	if outB, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example plugin: %v\n%s", err, outB)
	}
	return bin
}

// startupConfigYAML is the receiver-side context the dynamic RemoteAddressClaim
// resolves against: the WireGuard underlay, the OverlayPeer delivery target, the
// AddressMobilityDomain that scopes the claim address, and the
// CloudProviderProfile the provider-secondary-ip capture references. The claim
// itself is NOT here — it arrives as a DynamicConfigPart from the plugin.
const startupConfigYAML = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: cloudedge-receiver
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg-hybrid
      spec:
        privateKeyFile: /usr/local/etc/routerd/secrets/wg-hybrid.key
        listenPort: 51820
        mtu: 1420
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata:
        name: onprem-main
      spec:
        interface: wg-hybrid
        publicKey: ONPREM_PEER_PUBLIC_KEY_REPLACE_ME
        endpoint: 203.0.113.10:51820
        allowedIPs:
          - 169.254.110.2/32
          - 10.88.60.0/24
        persistentKeepalive: 25
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: onprem-main
      spec:
        role: onprem
        nodeID: onprem-router
        underlay:
          type: wireguard
          interface: wg-hybrid
          address: 169.254.110.1
        remote:
          nodeID: cloud-router
          address: 169.254.110.2
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: AddressMobilityDomain
      metadata:
        name: cloudedge-same-subnet
      spec:
        prefix: 10.88.60.0/24
        mode: selective-address
        peerRef: onprem-main
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata:
        name: example-provider
      spec:
        provider: oci
        capabilities:
          - assign-secondary-ip
        auth:
          mode: external-command
          command: /usr/local/libexec/routerd/example-provider-auth
`
