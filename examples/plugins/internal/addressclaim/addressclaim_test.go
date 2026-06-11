// SPDX-License-Identifier: BSD-3-Clause

package addressclaim_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/imksoo/routerd/examples/plugins/internal/addressclaim"
	"github.com/imksoo/routerd/pkg/plugin"
)

// profiles mirrors the per-provider ProviderProfile each plugin main supplies,
// kept here so the shared test matrix exercises all three identically.
var profiles = map[string]addressclaim.ProviderProfile{
	"aws": {
		Provider:             "aws",
		ForwardingParamKey:   "sourceDestCheck",
		ForwardingParamValue: "false",
		TargetKeys: []addressclaim.TargetKey{
			{TargetKey: "region", PayloadKey: "region"},
			{TargetKey: "account", PayloadKey: "account"},
			{TargetKey: "subnetRef", PayloadKey: "subnetRef"},
		},
	},
	"azure": {
		Provider:             "azure",
		ForwardingParamKey:   "ipForwarding",
		ForwardingParamValue: "true",
		TargetKeys: []addressclaim.TargetKey{
			{TargetKey: "subscriptionId", PayloadKey: "subscriptionId"},
			{TargetKey: "resourceGroup", PayloadKey: "resourceGroup"},
			{TargetKey: "region", PayloadKey: "region"},
			{TargetKey: "ipConfigName", PayloadKey: "ipConfigName"},
		},
	},
	"oci": {
		Provider:             "oci",
		ForwardingParamKey:   "skipSourceDestCheck",
		ForwardingParamValue: "true",
		TargetKeys: []addressclaim.TargetKey{
			{TargetKey: "compartmentId", PayloadKey: "compartmentId"},
			{TargetKey: "region", PayloadKey: "region"},
		},
	},
}

func canonicalRequest(extraPayload map[string]string) addressclaim.PluginRequest {
	payload := map[string]string{
		"domain":    "cloudedge-same-subnet",
		"ownerSide": "onprem",
		"nicRef":    "nic-xyz",
		"region":    "ap-northeast-1",
	}
	for k, v := range extraPayload {
		payload[k] = v
	}
	return addressclaim.PluginRequest{
		Spec: addressclaim.PluginRequestSpec{
			Events: []addressclaim.MatchedEvent{{
				ID:      "e1",
				Type:    addressclaim.ObservedEventType,
				Subject: "10.88.60.9/32",
				Payload: payload,
			}},
			Context: addressclaim.PluginContext{
				Resources: []addressclaim.ContextResource{
					{APIVersion: addressclaim.ClaimAPIVersion, Kind: addressclaim.KindCloudProviderProfile, Name: "cloud-tokyo", Spec: map[string]any{"provider": "aws"}},
					{APIVersion: addressclaim.ClaimAPIVersion, Kind: addressclaim.KindAddressMobilityDomain, Name: "cloudedge-same-subnet", Spec: map[string]any{"prefix": "10.88.60.0/24"}},
					{APIVersion: addressclaim.ClaimAPIVersion, Kind: addressclaim.KindOverlayPeer, Name: "onprem-main", Spec: map[string]any{"role": "onprem"}},
				},
			},
		},
	}
}

func findPlan(plans []addressclaim.ActionPlan, action string) (addressclaim.ActionPlan, bool) {
	for _, p := range plans {
		if p.Action == action {
			return p, true
		}
	}
	return addressclaim.ActionPlan{}, false
}

// TestBuildValidClaimAndPlans is the per-provider fixture test: a canned request
// must yield a valid RemoteAddressClaim plus the two canonical, undo-bearing,
// dry-run action plans, with the provider-specific forwarding parameter.
func TestBuildValidClaimAndPlans(t *testing.T) {
	for name, profile := range profiles {
		profile := profile
		t.Run(name, func(t *testing.T) {
			res, err := addressclaim.Build(name+"-address-claim", profile, canonicalRequest(nil))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			// --- RemoteAddressClaim ---
			if len(res.Status.Resources) != 1 {
				t.Fatalf("want 1 resource, got %d", len(res.Status.Resources))
			}
			claim := res.Status.Resources[0]
			if claim.Kind != addressclaim.ClaimKind {
				t.Errorf("claim kind = %q, want %q", claim.Kind, addressclaim.ClaimKind)
			}
			if claim.Metadata.Name != "onprem-10-88-60-9" {
				t.Errorf("claim name = %q, want onprem-10-88-60-9", claim.Metadata.Name)
			}
			if claim.Spec.Address != "10.88.60.9/32" {
				t.Errorf("claim address = %q", claim.Spec.Address)
			}
			if claim.Spec.DomainRef != "cloudedge-same-subnet" {
				t.Errorf("claim domainRef = %q", claim.Spec.DomainRef)
			}
			if claim.Spec.OwnerSide != "onprem" {
				t.Errorf("claim ownerSide = %q", claim.Spec.OwnerSide)
			}
			if claim.Spec.Capture.ProviderRef != "cloud-tokyo" {
				t.Errorf("capture.providerRef = %q, want cloud-tokyo", claim.Spec.Capture.ProviderRef)
			}
			if claim.Spec.Capture.NICRef != "nic-xyz" {
				t.Errorf("capture.nicRef = %q, want nic-xyz", claim.Spec.Capture.NICRef)
			}
			if claim.Spec.Capture.ConfigureOSAddress {
				t.Errorf("capture.configureOSAddress must be false (dry-run intent)")
			}
			if claim.Spec.Delivery.PeerRef != "onprem-main" {
				t.Errorf("delivery.peerRef = %q, want onprem-main", claim.Spec.Delivery.PeerRef)
			}

			// --- assign-secondary-ip plan ---
			assign, ok := findPlan(res.Status.ActionPlans, addressclaim.ActionAssignSecondaryIP)
			if !ok {
				t.Fatalf("no assign-secondary-ip plan")
			}
			if assign.Provider != name {
				t.Errorf("assign provider = %q, want %q", assign.Provider, name)
			}
			if assign.Mode != addressclaim.ActionModeDryRun {
				t.Errorf("assign mode = %q, want dry-run", assign.Mode)
			}
			if assign.Target["address"] != "10.88.60.9/32" {
				t.Errorf("assign target.address = %q", assign.Target["address"])
			}
			if assign.Target["nicRef"] != "nic-xyz" {
				t.Errorf("assign target.nicRef = %q", assign.Target["nicRef"])
			}
			if assign.IdempotencyKey != name+":nic-xyz:assign-secondary-ip:10.88.60.9/32" {
				t.Errorf("assign idempotencyKey = %q", assign.IdempotencyKey)
			}
			if assign.Undo == nil || assign.Undo.Action != addressclaim.ActionUnassignSecondaryIP {
				t.Errorf("assign undo must be unassign-secondary-ip, got %+v", assign.Undo)
			}

			// --- ensure-forwarding-enabled plan ---
			fwd, ok := findPlan(res.Status.ActionPlans, addressclaim.ActionEnsureForwardingEnabled)
			if !ok {
				t.Fatalf("no ensure-forwarding-enabled plan")
			}
			if fwd.Mode != addressclaim.ActionModeDryRun {
				t.Errorf("forwarding mode = %q, want dry-run", fwd.Mode)
			}
			if got := fwd.Parameters[profile.ForwardingParamKey]; got != profile.ForwardingParamValue {
				t.Errorf("forwarding param %s = %q, want %q", profile.ForwardingParamKey, got, profile.ForwardingParamValue)
			}
			if fwd.Undo == nil || fwd.Undo.Action != addressclaim.ActionEnsureForwardingDisabled {
				t.Errorf("forwarding undo must be ensure-forwarding-disabled, got %+v", fwd.Undo)
			}
		})
	}
}

// TestBuildPassesValidateActionPlan is the ValidateActionPlan-conformance check:
// every emitted plan (and its undo, projected to a plan) must pass the real
// pkg/plugin.ValidateActionPlan, and none may use mode=execute.
func TestBuildPassesValidateActionPlan(t *testing.T) {
	for name, profile := range profiles {
		profile := profile
		t.Run(name, func(t *testing.T) {
			res, err := addressclaim.Build(name+"-address-claim", profile, canonicalRequest(nil))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			for _, p := range res.Status.ActionPlans {
				if p.Mode == "execute" {
					t.Fatalf("plan %q must not use mode=execute", p.Name)
				}
				// The wire ActionPlan is structurally identical to the pkg type;
				// re-assemble the fields ValidateActionPlan inspects.
				vp := plugin.ActionPlan{
					Name:           p.Name,
					Provider:       p.Provider,
					Action:         p.Action,
					Target:         p.Target,
					ProviderRef:    p.ProviderRef,
					Mode:           p.Mode,
					RiskLevel:      p.RiskLevel,
					IdempotencyKey: p.IdempotencyKey,
					Parameters:     p.Parameters,
				}
				if p.Undo != nil {
					vp.Undo = &plugin.ActionUndo{Action: p.Undo.Action, Parameters: p.Undo.Parameters}
				}
				if err := plugin.ValidateActionPlan(vp); err != nil {
					t.Errorf("plan %q failed ValidateActionPlan: %v", p.Name, err)
				}
			}
		})
	}
}

// TestBuildMissingContextResource is the negative test for required context.
func TestBuildMissingContextResource(t *testing.T) {
	cases := []struct {
		drop string
		want string
	}{
		{addressclaim.KindCloudProviderProfile, "CloudProviderProfile"},
		{addressclaim.KindAddressMobilityDomain, "AddressMobilityDomain"},
		{addressclaim.KindOverlayPeer, "OverlayPeer"},
	}
	for _, tc := range cases {
		t.Run(tc.drop, func(t *testing.T) {
			req := canonicalRequest(nil)
			kept := req.Spec.Context.Resources[:0]
			for _, r := range req.Spec.Context.Resources {
				if r.Kind != tc.drop {
					kept = append(kept, r)
				}
			}
			req.Spec.Context.Resources = kept
			_, err := addressclaim.Build("aws-address-claim", profiles["aws"], req)
			if err == nil {
				t.Fatalf("expected error when %s is missing", tc.drop)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

// TestBuildMissingAddress is the negative test for an event with no address.
func TestBuildMissingAddress(t *testing.T) {
	req := canonicalRequest(nil)
	req.Spec.Events[0].Subject = ""
	req.Spec.Events[0].Payload["address"] = ""
	_, err := addressclaim.Build("aws-address-claim", profiles["aws"], req)
	if err == nil || !strings.Contains(err.Error(), "no address") {
		t.Fatalf("expected 'no address' error, got %v", err)
	}
}

// TestBuildMissingDomain is the negative test for a missing domain: payload.domain
// empty AND no AddressMobilityDomain name to fall back to.
func TestBuildMissingDomain(t *testing.T) {
	req := canonicalRequest(nil)
	req.Spec.Events[0].Payload["domain"] = ""
	for i := range req.Spec.Context.Resources {
		if req.Spec.Context.Resources[i].Kind == addressclaim.KindAddressMobilityDomain {
			req.Spec.Context.Resources[i].Name = ""
		}
	}
	_, err := addressclaim.Build("aws-address-claim", profiles["aws"], req)
	if err == nil || !strings.Contains(err.Error(), "no domain") {
		t.Fatalf("expected 'no domain' error, got %v", err)
	}
}

// TestBuildMissingNICRef is the negative test for a missing nicRef: the plugin
// must refuse rather than invent a cloud NIC id.
func TestBuildMissingNICRef(t *testing.T) {
	req := canonicalRequest(nil)
	req.Spec.Events[0].Payload["nicRef"] = ""
	_, err := addressclaim.Build("aws-address-claim", profiles["aws"], req)
	if err == nil || !strings.Contains(err.Error(), "nicRef") {
		t.Fatalf("expected 'nicRef' error, got %v", err)
	}
}

// TestNoExecImports is the no-execution invariant: the address-claim PLANNER
// plugins and the FAKE provider executor (and the shared core) must import
// neither os/exec nor any provider SDK, and make no network call. We assert it
// statically by parsing every .go file under examples/plugins and rejecting
// forbidden import paths.
//
// SCOPING (Phase 5.1): the REAL provider executors (aws-provider-executor,
// oci-provider-executor, azure-provider-executor) LEGITIMATELY use os/exec to run
// their provider command and link no cloud SDK. The OCI SDK is intentionally
// isolated in aws-routerd-helper / oci-routerd-helper, the shipped cloud
// control-plane commands used by the provider executors. These directories are
// therefore EXCLUDED from this planner/fake-executor invariant. Planners + the
// fake executor remain bound by the invariant here.
func TestNoExecImports(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	// .../examples/plugins/internal/addressclaim/addressclaim_test.go -> examples/plugins
	pluginsDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	// The real provider executors legitimately exec their provider command; the
	// AWS/OCI helpers are the allowed SDK boundaries for cloud API calls.
	exemptDirs := map[string]bool{
		filepath.Join(pluginsDir, "aws-provider-executor"):   true,
		filepath.Join(pluginsDir, "oci-provider-executor"):   true,
		filepath.Join(pluginsDir, "azure-provider-executor"): true,
		filepath.Join(pluginsDir, "aws-routerd-helper"):      true,
		filepath.Join(pluginsDir, "oci-routerd-helper"):      true,
	}

	forbidden := []string{
		"os/exec",
		"net/http",
		"net/rpc",
		"github.com/aws/",
		"github.com/Azure/",
		"github.com/oracle/oci-go-sdk",
		"github.com/imksoo/routerd/pkg/plugin", // plugins must not import pkg/plugin
		"github.com/imksoo/routerd/pkg/api",    // ...nor pkg/api
	}

	fset := token.NewFileSet()
	walkErr := filepath.Walk(pluginsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if exemptDirs[path] {
				// Skip the whole real-executor dir: it may exec its CLI.
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// The conformance test (this file) legitimately imports pkg/plugin; skip
		// *_test.go files — the invariant is about shipped plugin/core code.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad || strings.HasPrefix(p, bad) {
					t.Errorf("%s imports forbidden package %q (no-exec/no-SDK invariant)", path, p)
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
}
