// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/eventsubscription"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// TestEventSubscriptionProviderActionPlanE2E is the Phase 4.1-E fixture e2e: it
// proves, for ALL THREE cloud providers (AWS, Azure, OCI), the full receiver
// path
//
//	observed event (already federated, Phase 2) -> EventSubscription match ->
//	the SHIPPED provider <provider>-address-claim plugin -> DynamicConfigPart
//	(RemoteAddressClaim + display-only ActionPlans) -> the operator-visible
//	`routerctl dynamic render` / `dynamic describe` output.
//
// It is FIXTURE-ONLY: it spins up NO cloud VM, imports NO cloud CLI/SDK, makes
// NO provider call, and executes NO ActionPlan. Each provider plugin is built
// from examples/plugins/<provider>-address-claim into t.TempDir() (no binaries
// left in the tree). Every emitted ActionPlan is asserted to pass
// pkg/plugin.ValidateActionPlan and to be mode!=execute.
//
// The static no-exec / no-SDK invariant for aws/azure/oci-address-claim is
// already covered by examples/plugins/internal/addressclaim.TestNoExecImports
// (it walks all of examples/plugins); it is intentionally NOT duplicated here.
func TestEventSubscriptionProviderActionPlanE2E(t *testing.T) {
	// A secret value + a secret-file path planted in the receiver Router. The
	// controller builds the plugin context with secrets redacted, so neither
	// must ever surface in the stored DynamicConfigPart (resources + actionPlans)
	// nor in the rendered config. We assert their literal absence below.
	const (
		secretToken    = "SUPER-SECRET-PROVIDER-TOKEN-DO-NOT-LEAK"
		secretKeyPath  = "/usr/local/etc/routerd/secrets/provider-auth.keyfile"
		wgPrivKeyPath  = "/usr/local/etc/routerd/secrets/wg-hybrid.key"
		domainName     = "cloudedge-same-subnet"
		overlayPeer    = "onprem-main"
		subscriptionNm = "cloud-claims"
		groupName      = "cloudedge"
		nicRef         = "nic-eni-0abc123"
	)

	cases := []struct {
		provider    string
		address     string // observed LAN client address (CIDR /32)
		fwdKey      string // provider-specific forwarding parameter key
		fwdValue    string // provider-specific forwarding parameter value
		profileSpec map[string]any
		// extraPayload is the provider-specific event payload fields each plugin
		// needs (region / compartmentId / etc.).
		extraPayload map[string]string
	}{
		{
			provider: "aws",
			address:  "10.88.60.9/32",
			fwdKey:   "sourceDestCheck",
			fwdValue: "false",
			extraPayload: map[string]string{
				"region":    "ap-northeast-1",
				"account":   "123456789012",
				"subnetRef": "subnet-0aaa111",
			},
		},
		{
			provider: "azure",
			address:  "10.77.60.9/32",
			fwdKey:   "ipForwarding",
			fwdValue: "true",
			// subscriptionID / resourceGroup ride on the CloudProviderProfile spec;
			// the azure plugin backfills them onto the event payload from the
			// (redacted, secret-free) profile.
			profileSpec: map[string]any{
				"subscriptionID": "00000000-1111-2222-3333-444444444444",
				"resourceGroup":  "rg-cloudedge",
			},
			extraPayload: map[string]string{
				"region":       "japaneast",
				"ipConfigName": "ipconfig1",
			},
		},
		{
			provider: "oci",
			address:  "10.99.60.9/32",
			fwdKey:   "skipSourceDestCheck",
			fwdValue: "true",
			extraPayload: map[string]string{
				"compartmentId": "ocid1.compartment.oc1..aaaa",
				"region":        "ap-tokyo-1",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(strings.ToUpper(tc.provider), func(t *testing.T) {
			tmp := t.TempDir()
			pluginBin := buildProviderPlugin(t, tmp, tc.provider+"-address-claim")
			pluginName := tc.provider + "-address-claim"

			statePath := filepath.Join(tmp, "routerd.db")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			closeOnce := func() {
				if store != nil {
					_ = store.Close()
					store = nil
				}
			}
			defer closeOnce()

			eventID := "evt-" + tc.provider + "-1"
			payload := map[string]string{
				"address":     tc.address,
				"domain":      domainName,
				"ownerSide":   "onprem",
				"peerRef":     overlayPeer,
				"providerRef": "cloud-provider",
				"nicRef":      nicRef,
			}
			for k, v := range tc.extraPayload {
				payload[k] = v
			}

			// No ExpiresAt on the event (far future implied): it must remain
			// listable by the controller's non-expired query.
			if err := store.RecordFederationEvent(routerstate.EventRecord{
				ID:         eventID,
				Group:      groupName,
				SourceNode: "onprem-router",
				Type:       "routerd.client.ipv4.observed",
				Subject:    tc.address,
				Payload:    payload,
				ObservedAt: time.Now().UTC().Add(-time.Minute),
			}); err != nil {
				t.Fatalf("record federation event: %v", err)
			}

			router := buildReceiverRouter(receiverParams{
				provider:      tc.provider,
				pluginName:    pluginName,
				pluginBin:     pluginBin,
				group:         groupName,
				subscription:  subscriptionNm,
				domain:        domainName,
				overlayPeer:   overlayPeer,
				address:       tc.address,
				profileSpec:   tc.profileSpec,
				secretToken:   secretToken,
				secretKeyPath: secretKeyPath,
				wgPrivKeyFile: wgPrivKeyPath,
			})

			// Anchor Now to the real wall clock: the controller stamps the part's
			// ExpiresAt = now + plugin TTL (30m), but `dynamic render`/`describe`
			// below filter expired parts by the REAL time.Now(). A hardcoded
			// historical now made the part expire before render ran (flaky once
			// wall-clock passed now+TTL). Anchoring to time.Now keeps it active.
			now := time.Now().UTC()
			ctrl := eventsubscription.Controller{
				Router: router,
				Store:  store,
				Now:    func() time.Time { return now },
			}
			if err := ctrl.Reconcile(context.Background()); err != nil {
				t.Fatalf("reconcile: %v", err)
			}

			parts, err := store.ListDynamicConfigParts()
			if err != nil {
				t.Fatalf("list dynamic config parts: %v", err)
			}
			if len(parts) != 1 {
				t.Fatalf("want 1 dynamic config part, got %d: %+v", len(parts), parts)
			}
			part := parts[0]

			sourcePrefix := "EventSubscription/" + subscriptionNm + "/"
			if !strings.HasPrefix(part.Source, sourcePrefix) {
				t.Fatalf("part source %q does not start with %q", part.Source, sourcePrefix)
			}

			// --- assert the RemoteAddressClaim in the part resources ---
			var claims []api.Resource
			if err := json.Unmarshal([]byte(part.ResourcesJSON), &claims); err != nil {
				t.Fatalf("decode part resources: %v", err)
			}
			var claim *api.Resource
			for i := range claims {
				if claims[i].Kind == "RemoteAddressClaim" {
					claim = &claims[i]
					break
				}
			}
			if claim == nil {
				t.Fatalf("part resources have no RemoteAddressClaim: %s", part.ResourcesJSON)
			}
			assertClaimField(t, claim, "address", tc.address)
			assertClaimField(t, claim, "domainRef", domainName)
			assertClaimField(t, claim, "ownerSide", "onprem")
			assertClaimNested(t, claim, "capture", "providerRef", "cloud-provider")
			assertClaimNested(t, claim, "capture", "nicRef", nicRef)

			// --- assert the ActionPlans ---
			var plans []dynamicconfig.ActionPlan
			if err := json.Unmarshal([]byte(part.ActionPlansJSON), &plans); err != nil {
				t.Fatalf("decode part actionPlans: %v", err)
			}
			if len(plans) < 2 {
				t.Fatalf("want >= 2 actionPlans, got %d: %s", len(plans), part.ActionPlansJSON)
			}

			var assign, forwarding *dynamicconfig.ActionPlan
			for i := range plans {
				switch plans[i].Action {
				case routerplugin.ActionAssignSecondaryIP:
					assign = &plans[i]
				case routerplugin.ActionEnsureForwardingEnabled:
					forwarding = &plans[i]
				}
			}
			if assign == nil {
				t.Fatalf("no assign-secondary-ip actionPlan: %s", part.ActionPlansJSON)
			}
			if forwarding == nil {
				t.Fatalf("no ensure-forwarding-enabled actionPlan: %s", part.ActionPlansJSON)
			}

			// assign-secondary-ip: dry-run, riskLevel + idempotencyKey set,
			// target.address + target.nicRef set, undo=unassign-secondary-ip.
			if assign.Mode != "dry-run" {
				t.Errorf("assign mode = %q, want dry-run", assign.Mode)
			}
			if assign.RiskLevel == "" {
				t.Errorf("assign riskLevel is empty")
			}
			if assign.IdempotencyKey == "" {
				t.Errorf("assign idempotencyKey is empty")
			}
			if assign.Target["address"] != tc.address {
				t.Errorf("assign target.address = %q, want %q", assign.Target["address"], tc.address)
			}
			if assign.Target["nicRef"] != nicRef {
				t.Errorf("assign target.nicRef = %q, want %q", assign.Target["nicRef"], nicRef)
			}
			if assign.Undo == nil || assign.Undo.Action != routerplugin.ActionUnassignSecondaryIP {
				t.Errorf("assign undo = %+v, want action unassign-secondary-ip", assign.Undo)
			}

			// ensure-forwarding-enabled: provider-specific forwarding param.
			if got := forwarding.Parameters[tc.fwdKey]; got != tc.fwdValue {
				t.Errorf("forwarding parameters[%q] = %q, want %q (full: %+v)",
					tc.fwdKey, got, tc.fwdValue, forwarding.Parameters)
			}

			// Every emitted plan: passes ValidateActionPlan AND mode != execute.
			for _, p := range plans {
				if err := routerplugin.ValidateActionPlan(p); err != nil {
					t.Errorf("actionPlan %q failed ValidateActionPlan: %v", p.Name, err)
				}
				if p.Mode == "execute" {
					t.Errorf("actionPlan %q has mode=execute (forbidden)", p.Name)
				}
			}

			// --- SECRET-LEAK NEGATIVE ASSERTION ---
			// The stored part (resources + actionPlans) must contain NEITHER the
			// secret value NOR the secret file path NOR the WG private key path.
			for _, blob := range []struct{ name, data string }{
				{"resourcesJSON", part.ResourcesJSON},
				{"actionPlansJSON", part.ActionPlansJSON},
			} {
				for _, leak := range []string{secretToken, secretKeyPath, wgPrivKeyPath} {
					if strings.Contains(blob.data, leak) {
						t.Errorf("SECRET LEAK: %s contains %q\n%s", blob.name, leak, blob.data)
					}
				}
			}

			// --- KEY ASSERTION 1: routerctl dynamic render shows the claim ---
			configPath := filepath.Join(tmp, "router.yaml")
			startup := receiverStartupYAML(tc.provider, tc.profileSpec, secretToken, secretKeyPath, wgPrivKeyPath)
			if err := os.WriteFile(configPath, []byte(startup), 0o644); err != nil {
				t.Fatalf("write startup config: %v", err)
			}

			// Release the writer handle before the read-only render reopens the DB.
			closeOnce()

			var renderOut bytes.Buffer
			if err := run([]string{
				"dynamic", "render",
				"--config", configPath,
				"--state-file", statePath,
				"-o", "yaml",
			}, &renderOut, &bytes.Buffer{}); err != nil {
				t.Fatalf("dynamic render: %v\n%s", err, renderOut.String())
			}
			rendered := renderOut.String()
			for _, want := range []string{
				"kind: RemoteAddressClaim",
				"address: " + tc.address,
				"domainRef: " + domainName,
				"ownerSide: onprem",
			} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("dynamic render missing %q\n---\n%s", want, rendered)
				}
			}
			// Render output must also be secret-free.
			for _, leak := range []string{secretToken, secretKeyPath} {
				if strings.Contains(rendered, leak) {
					t.Errorf("SECRET LEAK in dynamic render: %q\n%s", leak, rendered)
				}
			}

			// --- KEY ASSERTION 2: routerctl dynamic describe shows the plans ---
			var describeOut bytes.Buffer
			if err := run([]string{
				"dynamic", "describe", part.Source,
				"--state-file", statePath,
			}, &describeOut, &bytes.Buffer{}); err != nil {
				t.Fatalf("dynamic describe: %v\n%s", err, describeOut.String())
			}
			describe := describeOut.String()
			for _, want := range []string{
				"action=" + routerplugin.ActionAssignSecondaryIP,
				"action=" + routerplugin.ActionEnsureForwardingEnabled,
				"mode=dry-run",
				"target.address:",
				"target.nicRef:",
			} {
				if !strings.Contains(describe, want) {
					t.Fatalf("dynamic describe missing %q\n---\n%s", want, describe)
				}
			}

			// --- OPTIONAL: dynamic list shows actionPlan count >= 2 ---
			var listOut bytes.Buffer
			if err := run([]string{
				"dynamic", "list",
				"--state-file", statePath,
				"-o", "json",
			}, &listOut, &bytes.Buffer{}); err != nil {
				t.Fatalf("dynamic list: %v\n%s", err, listOut.String())
			}
			var rows []struct {
				Source      string `json:"source"`
				ActionPlans int    `json:"actionPlans"`
			}
			if err := json.Unmarshal(listOut.Bytes(), &rows); err != nil {
				t.Fatalf("decode dynamic list json: %v\n%s", err, listOut.String())
			}
			found := false
			for _, r := range rows {
				if r.Source == part.Source {
					found = true
					if r.ActionPlans < 2 {
						t.Errorf("dynamic list actionPlans = %d for %q, want >= 2", r.ActionPlans, r.Source)
					}
				}
			}
			if !found {
				t.Errorf("dynamic list did not include source %q: %s", part.Source, listOut.String())
			}
		})
	}
}

// buildProviderPlugin compiles examples/plugins/<dir> into a temp binary so the
// test exercises the REAL shipped provider plugin, not a fake. The binary lives
// in t.TempDir() so nothing is left in the working tree.
func buildProviderPlugin(t *testing.T, dir, pkg string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	pkgDir := filepath.Join(repoRoot, "examples", "plugins", pkg)

	bin := filepath.Join(dir, pkg)
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = pkgDir
	if outB, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s plugin: %v\n%s", pkg, err, outB)
	}
	return bin
}

type receiverParams struct {
	provider      string
	pluginName    string
	pluginBin     string
	group         string
	subscription  string
	domain        string
	overlayPeer   string
	address       string
	profileSpec   map[string]any
	secretToken   string
	secretKeyPath string
	wgPrivKeyFile string
}

// buildReceiverRouter assembles the in-memory receiver Router the controller
// reconciles against: the Plugin (with a context.resources allowlist for
// CloudProviderProfile + AddressMobilityDomain + OverlayPeer), the
// EventSubscription, the CloudProviderProfile (carrying a secret value + secret
// file path the redactor must strip), the AddressMobilityDomain, the OverlayPeer
// and the WireGuard underlay.
func buildReceiverRouter(p receiverParams) *api.Router {
	prefix := prefixFromAddress(p.address)

	auth := map[string]any{
		"mode":    "external-command",
		"command": "/usr/local/libexec/routerd/cloud-provider-auth",
		// secret VALUE (key contains "token") -> blanked by the redactor.
		"apiToken": p.secretToken,
		// secret FILE PATH (key ends with "keyfile") -> omitted by the redactor.
		"credentialKeyfile": p.secretKeyPath,
	}
	profileSpec := map[string]any{
		"provider":     p.provider,
		"capabilities": []any{"assign-secondary-ip"},
		"auth":         auth,
	}
	for k, v := range p.profileSpec {
		profileSpec[k] = v
	}

	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "cloudedge-receiver"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
				Metadata: api.ObjectMeta{Name: p.pluginName},
				Spec: api.PluginSpec{
					Executable:   p.pluginBin,
					Timeout:      "10s",
					Capabilities: []string{"observe.cloud", "propose.dynamicConfig", "propose.providerAction"},
					Context: api.PluginContextSpec{
						Resources: []api.PluginContextResourceRef{
							{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile", Name: "cloud-provider"},
							{APIVersion: api.HybridAPIVersion, Kind: "AddressMobilityDomain", Name: p.domain},
							{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer", Name: p.overlayPeer},
						},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: p.group},
				Spec:     api.EventGroupSpec{NodeName: "cloud-router"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"},
				Metadata: api.ObjectMeta{Name: p.subscription},
				Spec: api.EventSubscriptionSpec{
					GroupRef: p.group,
					Match: api.EventSubscriptionMatch{
						Types:       []string{"routerd.client.ipv4.observed"},
						SourceNodes: []string{"onprem-router"},
					},
					Trigger: api.EventSubscriptionTrigger{PluginRef: p.pluginName},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
				Metadata: api.ObjectMeta{Name: "wg-hybrid"},
				Spec: map[string]any{
					"privateKeyFile": p.wgPrivKeyFile,
					"listenPort":     51820,
					"mtu":            1420,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
				Metadata: api.ObjectMeta{Name: p.overlayPeer},
				Spec: map[string]any{
					"interface":  "wg-hybrid",
					"publicKey":  "ONPREM_PEER_PUBLIC_KEY_REPLACE_ME",
					"endpoint":   "203.0.113.10:51820",
					"allowedIPs": []any{"169.254.110.1/32", prefix},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
				Metadata: api.ObjectMeta{Name: p.overlayPeer},
				Spec: map[string]any{
					"role":   "onprem",
					"nodeID": "onprem-router",
					"underlay": map[string]any{
						"type":      "wireguard",
						"interface": "wg-hybrid",
						"address":   "169.254.110.2",
					},
					"remote": map[string]any{
						"nodeID":  "cloud-router",
						"address": "169.254.110.1",
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "AddressMobilityDomain"},
				Metadata: api.ObjectMeta{Name: p.domain},
				Spec: map[string]any{
					"prefix":  prefix,
					"mode":    "selective-address",
					"peerRef": p.overlayPeer,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: "cloud-provider"},
				Spec:     profileSpec,
			},
		}},
	}
}

func assertClaimField(t *testing.T, res *api.Resource, key, want string) {
	t.Helper()
	spec, ok := res.Spec.(map[string]any)
	if !ok {
		t.Fatalf("claim spec is not a map: %T", res.Spec)
	}
	got, _ := spec[key].(string)
	if got != want {
		t.Errorf("claim spec.%s = %q, want %q", key, got, want)
	}
}

func assertClaimNested(t *testing.T, res *api.Resource, parent, key, want string) {
	t.Helper()
	spec, ok := res.Spec.(map[string]any)
	if !ok {
		t.Fatalf("claim spec is not a map: %T", res.Spec)
	}
	sub, ok := spec[parent].(map[string]any)
	if !ok {
		t.Fatalf("claim spec.%s is not a map: %T", parent, spec[parent])
	}
	got, _ := sub[key].(string)
	if got != want {
		t.Errorf("claim spec.%s.%s = %q, want %q", parent, key, got, want)
	}
}

func prefixFromAddress(addr string) string {
	host := addr
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		host = addr[:i]
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return "10.0.0.0/24"
	}
	return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
}

// receiverStartupYAML builds the receiver-side startup config the dynamic
// RemoteAddressClaim resolves against (mirrors examples/event-federation/
// receiver-cloud.yaml's hybrid context so `dynamic render` validation passes).
// It deliberately carries the same secret value + secret file path as the
// in-memory Router so the render output can be checked for leaks too.
func receiverStartupYAML(provider string, profileSpec map[string]any, secretToken, secretKeyPath, wgPrivKeyPath string) string {
	var extraProfile strings.Builder
	for k, v := range profileSpec {
		if s, ok := v.(string); ok {
			fmt.Fprintf(&extraProfile, "        %s: %q\n", k, s)
		}
	}
	return fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
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
        privateKeyFile: %s
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
          - 10.0.0.0/8
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
          address: 169.254.110.2
        remote:
          nodeID: cloud-router
          address: 169.254.110.1
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: AddressMobilityDomain
      metadata:
        name: cloudedge-same-subnet
      spec:
        prefix: 10.0.0.0/8
        mode: selective-address
        peerRef: onprem-main
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata:
        name: cloud-provider
      spec:
        provider: %s
        capabilities:
          - assign-secondary-ip
%s        auth:
          mode: external-command
          command: /usr/local/libexec/routerd/cloud-provider-auth
          apiToken: %q
          credentialKeyfile: %s
`, wgPrivKeyPath, provider, extraProfile.String(), secretToken, secretKeyPath)
}
