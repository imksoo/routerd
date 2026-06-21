// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestMobilityDistributionCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"captureDistributionMode":          "distributed",
		"captureDistributionNodeCounts":    map[string]any{"aws-router-a": 9, "aws-router-b": 9},
		"captureDistributionReasonCounts":  map[string]any{"hash-assigned": 18},
		"captureDistributionTargetPerNode": 9,
		"captureDistributionTotalAssigned": 18,
		"captureRebalancePhase":            "Applied",
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"distribution", "--state-file", path}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility distribution: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"cloudedge", "aws-router-a", "aws-router-b", "hash-assigned=18", "Applied"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mobility distribution output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<nil>") {
		t.Fatalf("mobility distribution output leaked nil values:\n%s", out)
	}
}

func TestMobilityRebalanceCapturesCommandRecordsRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"captureDistributionMode": "distributed",
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"rebalance-captures", "--state-file", path, "--pool", "cloudedge", "--by", "tester", "--reason", "rejoin"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility rebalance-captures: %v stderr=%s", err, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "cloudedge") || !strings.Contains(out, "Pending") || !strings.Contains(out, "tester") {
		t.Fatalf("unexpected rebalance output:\n%s", out)
	}

	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite second: %v", err)
	}
	defer store.Close()
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["captureRebalancePhase"] != "Pending" || status["captureRebalanceRequestedBy"] != "tester" || status["captureRebalanceReason"] != "rejoin" {
		t.Fatalf("status = %#v, want pending rebalance request", status)
	}
	if strings.TrimSpace(fmt.Sprint(status["captureRebalanceRequestID"])) == "" {
		t.Fatalf("status = %#v, want request ID", status)
	}
}

func TestTopLevelUsageListsCurrentMobilityCommands(t *testing.T) {
	var stdout bytes.Buffer
	usage(&stdout)

	out := stdout.String()
	for _, want := range []string{
		"mobility owners",
		"mobility distribution",
		"mobility rebalance-captures",
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
