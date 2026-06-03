// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
