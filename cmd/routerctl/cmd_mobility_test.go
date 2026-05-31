// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestMobilityLeasesCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusActive,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"leases", "--state-file", path, "--pool", "cloudedge"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility leases: %v stderr=%s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "10.88.60.9/32") || !strings.Contains(out, "onprem-router") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
