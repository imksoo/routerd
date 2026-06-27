// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

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
