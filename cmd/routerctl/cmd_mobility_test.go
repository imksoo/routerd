// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestMobilityEnrollmentHMACCommand(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretPath, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	configPath := filepath.Join("..", "..", "examples", "cloudedge-dynamic-leaf-pve.yaml")
	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"enrollment-hmac", "--config", configPath, "--claim", "leaf-pve", "--secret-file", secretPath}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility enrollment-hmac: %v stderr=%s", err, stderr.String())
	}
	hmacValue := strings.TrimSpace(stdout.String())
	if hmacValue == "" || strings.Contains(hmacValue, "EXAMPLE") {
		t.Fatalf("unexpected hmac output %q", hmacValue)
	}

	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load example: %v", err)
	}
	for i := range router.Spec.Resources {
		resource := &router.Spec.Resources[i]
		switch resource.Kind {
		case "SAMEnrollmentPolicy":
			spec, err := resource.SAMEnrollmentPolicySpec()
			if err != nil {
				t.Fatalf("%s spec: %v", resource.ID(), err)
			}
			spec.JoinTokenFrom.File = secretPath
			resource.Spec = spec
		case "SAMEnrollmentClaim":
			spec, err := resource.SAMEnrollmentClaimSpec()
			if err != nil {
				t.Fatalf("%s spec: %v", resource.ID(), err)
			}
			stdout.Reset()
			stderr.Reset()
			if err := mobilityCommand([]string{"enrollment-hmac", "--config", configPath, "--claim", resource.Metadata.Name, "--secret-file", secretPath}, &stdout, &stderr); err != nil {
				t.Fatalf("mobility enrollment-hmac %s: %v stderr=%s", resource.Metadata.Name, err, stderr.String())
			}
			spec.JoinHMAC = strings.TrimSpace(stdout.String())
			resource.Spec = spec
		}
	}
	rendered, err := yaml.Marshal(router)
	if err != nil {
		t.Fatalf("Marshal candidate: %v", err)
	}
	candidate := filepath.Join(t.TempDir(), "rr-a.yaml")
	if err := os.WriteFile(candidate, rendered, 0o600); err != nil {
		t.Fatalf("WriteFile candidate: %v", err)
	}
	router, err = config.Load(candidate)
	if err != nil {
		t.Fatalf("Load candidate: %v", err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("Validate candidate with generated HMAC: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := mobilityCommand([]string{"enrollment-hmac", "--config", configPath, "--claim", "leaf-pve", "--secret-file", secretPath, "--show-payload"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility enrollment-hmac --show-payload: %v stderr=%s", err, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "leafID=leaf-pve") || !strings.Contains(out, hmacValue) {
		t.Fatalf("show-payload output missing payload or hmac:\n%s", out)
	}
}

func TestMobilityEnrollmentRevokeCommand(t *testing.T) {
	now := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	assertAuth := func(r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer rr-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
	}
	server := httptest.NewServer(controlapi.Handler{
		RevokeSAMEnrollmentClaim: func(r *http.Request, req controlapi.SAMEnrollmentClaimRevokeRequest) (*controlapi.SAMEnrollmentClaimRevokeResult, error) {
			assertAuth(r)
			if req.Name != "pve-leaf-b" || req.Reason != "rotate" {
				t.Fatalf("revoke request = %#v", req)
			}
			result := controlapi.NewSAMEnrollmentClaimRevokeResult("SAMEnrollmentClaim/pve-leaf-b", "SAMEnrollmentClaim/pve-leaf-b", 1, now, now, req.Reason)
			return &result, nil
		},
	})
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "rr-token")
	if err := os.WriteFile(tokenPath, []byte("rr-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"enrollment-revoke", "--claim", "pve-leaf-b", "--reason", "rotate", "--rr-url", server.URL, "--rr-token-file", tokenPath, "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility enrollment-revoke: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"revoked": true`) || !strings.Contains(stdout.String(), `"claimRef": "SAMEnrollmentClaim/pve-leaf-b"`) {
		t.Fatalf("revoke output = %s", stdout.String())
	}
}

func TestMobilityLeafConfigCommandGeneratesValidConfig(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretPath, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{
		"leaf-config",
		"--leaf-id", "pve-leaf-b",
		"--underlay-ifname", "vmbr0",
		"--underlay-address", "10.30.0.22/24",
		"--local-endpoint", "10.30.0.22",
		"--endpoint-prefix", "10.30.0.0/24",
		"--inner-prefix", "10.255.10.0/24",
		"--tunnel-address", "10.255.10.22/32",
		"--mobility-prefix", "10.77.70.0/24",
		"--owned-address", "10.77.70.22/32",
		"--rr-set", "pve-rrs",
		"--direct-peer-group", "pve-fou-direct-leaves",
		"--rr-local-preference", "110",
		"--direct-local-preference", "240",
		"--policy", "pve-fou-leaves",
		"--join-audience", "pve-private-underlay",
		"--join-nonce", "pve-leaf-b-0001",
		"--join-timestamp", "2026-06-28T00:00:00Z",
		"--bootstrap-endpoint", "https://10.30.0.10:65432",
		"--bootstrap-endpoint", "https://10.30.0.11:65432",
		"--control-api-token-file", "/usr/local/etc/routerd/secrets/control-api-token",
		"--control-api-ca-file", "/usr/local/etc/routerd/secrets/rr-ca.pem",
		"--control-api-client-cert-file", "/usr/local/etc/routerd/secrets/leaf.crt",
		"--control-api-client-key-file", "/usr/local/etc/routerd/secrets/leaf.key",
		"--secret-file", secretPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("mobility leaf-config: %v stderr=%s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "EXAMPLE_HMAC") {
		t.Fatalf("leaf-config should compute joinHMAC when --secret-file is supplied:\n%s", stdout.String())
	}
	router, err := config.LoadBytes(stdout.Bytes(), "generated-leaf.yaml")
	if err != nil {
		t.Fatalf("LoadBytes generated config: %v\n%s", err, stdout.String())
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("Validate generated config: %v\n%s", err, stdout.String())
	}
	claim, err := mobilityEnrollmentClaim(router, "pve-leaf-b")
	if err != nil {
		t.Fatalf("generated claim: %v", err)
	}
	if claim.JoinHMAC == "" || claim.JoinHMAC == "EXAMPLE_HMAC_SHA256_HEX" {
		t.Fatalf("claim.JoinHMAC = %q", claim.JoinHMAC)
	}
	if claim.TunnelAddress != "10.255.10.22/32" || !claim.DirectMesh || len(claim.Mobility.OwnedAddresses) != 1 || claim.Mobility.OwnedAddresses[0] != "10.77.70.22/32" {
		t.Fatalf("claim = %#v", claim)
	}
	var transport api.SAMTransportProfileSpec
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" || resource.Metadata.Name != "pve-leaf-b" {
			continue
		}
		var err error
		transport, err = resource.SAMTransportProfileSpec()
		if err != nil {
			t.Fatalf("SAMTransportProfile spec: %v", err)
		}
	}
	if len(transport.PeersFrom) != 2 || transport.PeersFrom[0].Resource != "SAMRRSet/pve-rrs" || transport.PeersFrom[1].Resource != "SAMPeerGroup/pve-fou-direct-leaves" || !transport.PeersFrom[1].Direct {
		t.Fatalf("generated direct transport peersFrom = %#v", transport.PeersFrom)
	}
	if transport.BGP.ImportPolicy.LocalPreference != 110 || transport.BGP.DirectLocalPreference != 240 {
		t.Fatalf("generated direct transport preferences = %#v", transport.BGP)
	}
	if got, want := transport.BGP.ImportPolicy.NextHopRewrite, "peer-address"; got != want {
		t.Fatalf("generated RR import next-hop rewrite = %q, want %q", got, want)
	}
	var bgpRouter api.BGPRouterSpec
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" || resource.Metadata.Name != "mobility-bgp" {
			continue
		}
		var err error
		bgpRouter, err = resource.BGPRouterSpec()
		if err != nil {
			t.Fatalf("BGPRouter spec: %v", err)
		}
	}
	if expected := bgpstate.MobilityNodeIdentityCommunity("pve-leaf-b"); !bgpstate.HasCommunity(bgpRouter.Communities.Set.Out, expected) {
		t.Fatalf("generated BGPRouter outbound communities = %#v, want direct leaf identity %q", bgpRouter.Communities.Set.Out, expected)
	}
	var foundClient bool
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClient" || resource.Metadata.Name != "pve-leaf-b" {
			continue
		}
		foundClient = true
		spec, err := resource.SAMEnrollmentClientSpec()
		if err != nil {
			t.Fatalf("SAMEnrollmentClientSpec: %v", err)
		}
		if len(spec.BootstrapEndpoints) != 2 || spec.ControlAPITokenFrom.File == "" || spec.ControlAPITLS.CAFile == "" || spec.ControlAPITLS.CertFile == "" || spec.ControlAPITLS.KeyFile == "" {
			t.Fatalf("SAMEnrollmentClient spec = %#v", spec)
		}
	}
	if !foundClient {
		t.Fatal("generated config missing SAMEnrollmentClient/pve-leaf-b")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion {
			switch resource.Kind {
			case "SAMEnrollmentPolicy", "SAMNodeSet", "MobilityPool":
				t.Fatalf("generated leaf must not contain %s: %s", resource.Kind, resource.ID())
			}
		}
		if resource.APIVersion == api.FederationAPIVersion && resource.Kind == "EventGroup" {
			t.Fatalf("generated leaf must not contain EventGroup: %s", resource.ID())
		}
	}
}

// A direct-mesh leaf joins before it owns a mobility address in the normal PVE
// rollout. The generator must preserve that valid empty-ownership claim while
// emitting neither a local service address nor any BGP export candidate.
func TestMobilityLeafConfigCommandAllowsDirectLeafWithoutOwnedAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{
		"leaf-config",
		"--leaf-id", "pve-rt01",
		"--underlay-ifname", "vmbr0",
		"--underlay-address", "10.20.0.21/24",
		"--local-endpoint", "10.20.0.21",
		"--endpoint-prefix", "10.20.0.0/24",
		"--inner-prefix", "10.255.10.0/24",
		"--tunnel-address", "10.255.10.21/32",
		"--mobility-prefix", "10.77.60.0/24",
		"--rr-set", "svnet1-rrs",
		"--direct-peer-group", "svnet1-direct-leaves",
		"--policy", "svnet1-leaves",
		"--join-audience", "svnet1-underlay",
		"--join-nonce", "pve-rt01-0001",
		"--join-timestamp", "2026-08-22T16:00:00Z",
		"--bootstrap-endpoint", "https://10.20.0.2:65432",
		"--bootstrap-endpoint", "https://10.20.0.3:65432",
		"--mode", "ipip",
		"--secret", "test-join-token",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("mobility leaf-config without owned address: %v stderr=%s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "EXAMPLE_HMAC") {
		t.Fatalf("leaf-config should compute joinHMAC when --secret is supplied:\n%s", stdout.String())
	}
	router, err := config.LoadBytes(stdout.Bytes(), "generated-empty-owner-leaf.yaml")
	if err != nil {
		t.Fatalf("LoadBytes generated config: %v\n%s", err, stdout.String())
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("Validate generated config: %v\n%s", err, stdout.String())
	}
	claim, err := mobilityEnrollmentClaim(router, "pve-rt01")
	if err != nil {
		t.Fatalf("generated claim: %v", err)
	}
	if !claim.DirectMesh || len(claim.Mobility.OwnedAddresses) != 0 {
		t.Fatalf("empty-owner direct claim = %#v", claim)
	}

	var foundRouter, foundTransport bool
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv4StaticAddress" && resource.Metadata.Name == "owned-service-ip" {
			t.Fatalf("empty-owner leaf must not generate owned-service-ip: %#v", resource)
		}
		switch {
		case resource.APIVersion == api.NetAPIVersion && resource.Kind == "BGPRouter" && resource.Metadata.Name == "mobility-bgp":
			foundRouter = true
			spec, err := resource.BGPRouterSpec()
			if err != nil {
				t.Fatalf("BGPRouter spec: %v", err)
			}
			if len(spec.ExportPolicy.AllowedPrefixes) != 0 || len(spec.Redistribute.Connected.AllowedPrefixes) != 0 {
				t.Fatalf("empty-owner BGP export = %#v, want no route advertisement", spec)
			}
		case resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMTransportProfile" && resource.Metadata.Name == "pve-rt01":
			foundTransport = true
			spec, err := resource.SAMTransportProfileSpec()
			if err != nil {
				t.Fatalf("SAMTransportProfile spec: %v", err)
			}
			if len(spec.BGP.ExportPolicy.AllowedPrefixes) != 0 {
				t.Fatalf("empty-owner transport export = %#v, want no route advertisement", spec.BGP.ExportPolicy)
			}
			if len(spec.PeersFrom) != 2 || spec.PeersFrom[0].Resource != "SAMRRSet/svnet1-rrs" || spec.PeersFrom[1].Resource != "SAMPeerGroup/svnet1-direct-leaves" || !spec.PeersFrom[1].Direct {
				t.Fatalf("empty-owner direct transport peersFrom = %#v", spec.PeersFrom)
			}
		}
	}
	if !foundRouter || !foundTransport {
		t.Fatalf("generated empty-owner leaf missing BGP router or transport: %#v", router.Spec.Resources)
	}
}

func TestMobilityLeafConfigCommandRejectsMissingRequiredInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{"leaf-config", "--leaf-id", "leaf-a"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "mobility leaf-config requires --") {
		t.Fatalf("leaf-config error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestMobilityGeneratedLeafConfigRejectsNonPreferredDirectPath(t *testing.T) {
	_, err := mobilityGeneratedLeafConfig(mobilityLeafConfigOptions{
		LeafID:                "leaf-a",
		UnderlayIfName:        "eth0",
		UnderlayAddress:       "10.30.0.2/24",
		LocalEndpoint:         "10.30.0.2",
		EndpointPrefix:        "10.30.0.0/24",
		InnerPrefix:           "10.255.10.0/24",
		TunnelAddress:         "10.255.10.2/32",
		MobilityPrefix:        "10.77.70.0/24",
		OwnedAddress:          "10.77.70.2/32",
		RRSet:                 "rrs",
		DirectPeerGroup:       "direct-leaves",
		Policy:                "leaves",
		JoinAudience:          "underlay",
		BootstrapEndpoints:    []string{"https://10.30.0.10:65432"},
		BGPASN:                64577,
		Mode:                  "ipip",
		RRLocalPreference:     200,
		DirectLocalPreference: 200,
	})
	if err == nil || !strings.Contains(err.Error(), "--direct-local-preference greater than --rr-local-preference") {
		t.Fatalf("mobilityGeneratedLeafConfig error = %v", err)
	}
}

func TestMobilityLeafConfigCommandRejectsRemovedPoolFlags(t *testing.T) {
	for _, flagName := range []string{"--mobility-pool", "--mobility-pool-prefix", "--site", "--role"} {
		t.Run(flagName, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := mobilityCommand([]string{"leaf-config", flagName, "removed"}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
				t.Fatalf("leaf-config %s error = %v stdout=%s stderr=%s", flagName, err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestMobilityPathsCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "BGPRouter", "fabric", map[string]any{
		"installedNextHops": map[string]any{
			"10.88.60.10/32": []any{"10.99.0.10"},
		},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"paths", "--state-file", path}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility paths: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "10.88.60.10/32") || !strings.Contains(out, "10.99.0.10") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestMobilityTrapsCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	plans := []dynamicconfig.ActionPlan{{
		Name:           "assign-10-88-60-10",
		Provider:       "aws",
		ProviderRef:    "aws-main",
		Action:         "assign-secondary-ip",
		IdempotencyKey: "assign-key",
		Target: map[string]string{
			"address": "10.88.60.10/32",
			"nicRef":  "eni-123",
		},
	}}
	raw, err := json.Marshal(plans)
	if err != nil {
		t.Fatalf("MarshalActionPlans: %v", err)
	}
	now := time.Now().UTC()
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          "MobilityPool/cloudedge/node/aws-router-a",
		Generation:      1,
		ActionPlansJSON: string(raw),
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"traps", "--state-file", path}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility traps: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "10.88.60.10/32") || !strings.Contains(out, "assign-secondary-ip") || !strings.Contains(out, "eni-123") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestMobilityOwnersCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"ownershipResolverControlPlaneOwnerTable": []map[string]any{{
			"address":                  "10.88.60.11/32",
			"state":                    "Conflict",
			"class":                    "RemoteHomeOwned",
			"ownerNode":                "oci-router",
			"ownerProviderRef":         "oci-provider",
			"ownerNICRef":              "oci-client",
			"localEvidenceNode":        "aws-router-a",
			"localEvidenceSource":      "local-inventory",
			"localEvidenceNICRef":      "eni-client",
			"localEvidenceResourceRef": "i-aws-client",
			"captureHolderNode":        "aws-router-a",
			"captureDisposition":       "protect-existing",
			"captureReason":            "provider capture observed",
			"conflictReason":           "remote-home-owner-overlaps-local-inventory",
		}},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"owners", "--state-file", path}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility owners: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"cloudedge", "10.88.60.11/32", "Conflict", "oci-router", "aws-router-a", "protect-existing", "provider capture observed", "remote-home-owner-overlaps-local-inventory"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mobility owners output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<nil>") {
		t.Fatalf("mobility owners output leaked nil values:\n%s", out)
	}
	stdout.Reset()
	stderr.Reset()
	if err := mobilityCommand([]string{"owners", "--state-file", path, "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility owners json: %v stderr=%s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "<nil>") {
		t.Fatalf("mobility owners json leaked nil values:\n%s", stdout.String())
	}
}

func TestMobilityExplainCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                               "Pending",
		"providerActionPhase":                 "Failed",
		"providerActionError":                 "a different address failed",
		"providerActionFailedAddresses":       []string{"10.88.60.12/32"},
		"providerObservationPendingAddresses": []string{"10.88.60.11/32"},
		"ownershipResolverControlPlaneOwnerTable": []map[string]any{{
			"address":            "10.88.60.11/32",
			"state":              "OK",
			"class":              "RemoteHomeOwned",
			"ownerNode":          "aws-router",
			"captureDisposition": "desired",
			"captureReason":      "installed BGP path",
		}},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"explain", "--state-file", path, "--pool", "cloudedge", "--address", "10.88.60.11/32"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility explain: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"10.88.60.11/32", "Phase: Pending", "Capture disposition: desired", "installed BGP path", "ProviderObserved", "provider observation pending", "ProviderActionApplied", "address-specific provider action status unavailable"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mobility explain output missing %q:\n%s", want, out)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if err := mobilityCommand([]string{"explain", "--state-file", path, "--pool", "cloudedge", "--address", "10.88.60.11/32", "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility explain json: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"phase": "Pending"`) || !strings.Contains(stdout.String(), `"blockingCondition": "ProviderObserved"`) || !strings.Contains(stdout.String(), `"captureDisposition": "desired"`) {
		t.Fatalf("mobility explain json missing phase/blocker:\n%s", stdout.String())
	}
}

func TestMobilityExplainScopesPoolProviderStatusByAddress(t *testing.T) {
	statuses := []routerstate.ObjectStatus{{
		APIVersion: api.MobilityAPIVersion,
		Kind:       "MobilityPool",
		Name:       "cloudedge",
		Status: map[string]any{
			"phase":                               "Pending",
			"providerActionPhase":                 "Failed",
			"providerActionError":                 "assign failed",
			"providerActionFailedAddresses":       []string{"10.88.60.12/32"},
			"providerObservationPendingAddresses": []string{"10.88.60.13/32"},
			"ownershipResolverControlPlaneOwnerTable": []map[string]any{
				{"address": "10.88.60.11/32", "state": "OK", "class": "RemoteHomeOwned"},
				{"address": "10.88.60.12/32", "state": "OK", "class": "RemoteHomeOwned"},
				{"address": "10.88.60.13/32", "state": "OK", "class": "RemoteHomeOwned"},
			},
		},
	}}

	for _, tc := range []struct {
		address          string
		providerAction   string
		providerObserved string
		blocking         string
	}{
		{address: "10.88.60.11/32", providerAction: "Unknown", providerObserved: "Unknown"},
		{address: "10.88.60.12/32", providerAction: "False", providerObserved: "Unknown", blocking: "ProviderActionApplied"},
		{address: "10.88.60.13/32", providerAction: "Unknown", providerObserved: "False", blocking: "ProviderObserved"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			report, err := mobilityExplainReportFor(statuses, "cloudedge", tc.address)
			if err != nil {
				t.Fatalf("mobilityExplainReportFor: %v", err)
			}
			if report.Conditions["ProviderActionApplied"] != tc.providerAction || report.Conditions["ProviderObserved"] != tc.providerObserved || report.BlockingCondition != tc.blocking {
				t.Fatalf("report = %#v, want provider action=%s observation=%s blocker=%s", report, tc.providerAction, tc.providerObserved, tc.blocking)
			}
		})
	}
}

func TestMobilityExplainClassifiesStaleCaptureAsDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase": "Ready",
		"ownershipResolverControlPlaneOwnerTable": []map[string]any{{
			"address": "10.88.60.16/32",
			"state":   "Stale",
			"class":   "StaleCapture",
		}},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"explain", "--state-file", path, "--pool", "cloudedge", "--address", "10.88.60.16/32"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility explain: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Severity: warning", "Diagnostic:", "stale capture evidence"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mobility explain diagnostic output missing %q:\n%s", want, out)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if err := mobilityCommand([]string{"explain", "--state-file", path, "--pool", "cloudedge", "--address", "10.88.60.16/32", "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility explain json: %v stderr=%s", err, stderr.String())
	}
	jsonOut := stdout.String()
	for _, want := range []string{`"severity": "warning"`, `"diagnostic": true`, `"diagnosticReason": "stale capture evidence`} {
		if !strings.Contains(jsonOut, want) {
			t.Fatalf("mobility explain json missing %q:\n%s", want, jsonOut)
		}
	}
}

func TestTopLevelUsageListsCurrentMobilityCommands(t *testing.T) {
	var stdout bytes.Buffer
	usage(&stdout)

	out := stdout.String()
	for _, want := range []string{
		"mobility owners",
		"mobility explain",
		"mobility paths",
		"mobility traps",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage is missing %q:\n%s", want, out)
		}
	}
	for _, old := range []string{
		"mobility leases",
		"mobility ownership",
		"mobility show",
	} {
		if strings.Contains(out, old) {
			t.Fatalf("usage still lists removed command %q:\n%s", old, out)
		}
	}
}
