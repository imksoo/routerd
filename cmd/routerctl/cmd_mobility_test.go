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

func TestMobilityEnrollmentJoinFetchesRRSetIntoDynamicState(t *testing.T) {
	rrSet := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "pve-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/pve-fou-leaves",
			MobilityPoolRefs:    []string{"MobilityPool/pve-mobility"},
			Members: []api.SAMRRSetMember{{
				NodeRef:       "pve-rr",
				Endpoint:      "10.30.0.10",
				TunnelAddress: "10.255.10.1/32",
				BGP:           api.SAMRRSetMemberBGPSpec{ASN: 64577, RouterID: "10.255.10.1"},
			}},
		},
	}
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	assertAuth := func(r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer rr-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
	}
	server := httptest.NewServer(controlapi.Handler{
		SubmitSAMEnrollmentClaim: func(r *http.Request, req controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
			assertAuth(r)
			if req.Claim.Kind != "SAMEnrollmentClaim" || req.Claim.Metadata.Name != "pve-leaf-b" {
				t.Fatalf("submitted claim = %#v", req.Claim)
			}
			result := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-b", "SAMEnrollmentClaim/pve-leaf-b", 1, now, now.Add(time.Hour))
			return &result, nil
		},
		GetSAMRRSet: func(r *http.Request, req controlapi.SAMRRSetGetRequest) (*controlapi.SAMRRSetGetResult, error) {
			assertAuth(r)
			if req.Name != "pve-rrs" || req.ClaimRef != "SAMEnrollmentClaim/pve-leaf-b" {
				t.Fatalf("rrset request = %#v", req)
			}
			result := controlapi.NewSAMRRSetGetResult("pve-rrs", rrSet)
			return &result, nil
		},
	})
	defer server.Close()
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	tokenPath := filepath.Join(t.TempDir(), "rr-token")
	if err := os.WriteFile(tokenPath, []byte("rr-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join("..", "..", "examples", "pve-minimal-leaf-b-fou.yaml")
	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"enrollment-join", "--config", configPath, "--claim", "pve-leaf-b", "--rr-url", server.URL, "--rr-token-file", tokenPath, "--state-file", statePath, "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility enrollment-join: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"rrSetRef": "SAMRRSet/pve-rrs"`) || !strings.Contains(stdout.String(), `"dynamicSource": "SAMRRSet/pve-rrs"`) {
		t.Fatalf("join output = %s", stdout.String())
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 || !strings.Contains(records[0].ResourcesJSON, `"pve-rrs"`) || !strings.Contains(records[0].ResourcesJSON, `"pve-rr"`) {
		t.Fatalf("records = %#v", records)
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
		"--mobility-pool", "pve-mobility",
		"--mobility-pool-prefix", "10.77.70.0/24",
		"--owned-address", "10.77.70.22/32",
		"--rr-set", "pve-rrs",
		"--policy", "pve-fou-leaves",
		"--join-token-file", secretPath,
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
	if claim.TunnelAddress != "10.255.10.22/32" || len(claim.Mobility.OwnedAddresses) != 1 || claim.Mobility.OwnedAddresses[0] != "10.77.70.22/32" {
		t.Fatalf("claim = %#v", claim)
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
}

func TestMobilityLeafConfigCommandRejectsMissingRequiredInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{"leaf-config", "--leaf-id", "leaf-a"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "mobility leaf-config requires --") {
		t.Fatalf("leaf-config error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
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
	for _, want := range []string{"cloudedge", "10.88.60.11/32", "Conflict", "oci-router", "aws-router-a", "remote-home-owner-overlaps-local-inventory"} {
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
		"phase": "Pending",
		"addresses": map[string]any{
			"10.88.60.11/32": map[string]any{
				"phase":                "Pending",
				"class":                "RemoteHomeOwned",
				"ownerNode":            "aws-router",
				"assignmentGeneration": "gen-42",
				"providerAction":       "assign-secondary-ip",
				"providerActionKey":    "assign-key",
				"blockingCondition":    "ProviderObserved",
				"conditions": map[string]any{
					"OwnershipResolved":     "True",
					"ProviderActionApplied": "True",
					"ProviderObserved":      "False",
				},
				"conditionReasons": map[string]any{
					"ProviderObserved": "provider inventory has not observed capture on self",
				},
			},
		},
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
	for _, want := range []string{"10.88.60.11/32", "Phase: Pending", "ProviderObserved", "gen-42", "provider inventory has not observed capture on self"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mobility explain output missing %q:\n%s", want, out)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if err := mobilityCommand([]string{"explain", "--state-file", path, "--pool", "cloudedge", "--address", "10.88.60.11/32", "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility explain json: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"phase": "Pending"`) || !strings.Contains(stdout.String(), `"blockingCondition": "ProviderObserved"`) {
		t.Fatalf("mobility explain json missing phase/blocker:\n%s", stdout.String())
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
		"addresses": map[string]any{
			"10.88.60.16/32": map[string]any{
				"phase":             "Pending",
				"class":             "StaleCapture",
				"blockingCondition": "OwnershipResolved",
				"conditions": map[string]any{
					"OwnershipResolved": "False",
					"ProviderObserved":  "True",
				},
				"conditionReasons": map[string]any{
					"OwnershipResolved": "stale capture evidence remains after ownership moved",
				},
			},
		},
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
