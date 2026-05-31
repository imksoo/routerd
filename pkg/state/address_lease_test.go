// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreAddressLeaseRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()
	store.now = func() time.Time { return now }

	rec := AddressLeaseRecord{
		Pool:                "cloudedge",
		Address:             "10.88.60.9/32",
		Status:              AddressLeaseStatusHolding,
		OwnerNode:           "onprem-router",
		OwnerSite:           "onprem",
		OwnerRole:           "onprem",
		Epoch:               2,
		ObservedAt:          now.Add(-time.Minute),
		ExpiresAt:           now.Add(4 * time.Minute),
		SourceEventID:       "evt-1",
		SourceGroup:         "cloudedge",
		SourceType:          "routerd.client.ipv4.observed",
		DedupeKey:           "client-9",
		CandidateOwnerNode:  "azure-router",
		CandidateOwnerSite:  "azure",
		CandidateOwnerRole:  "cloud",
		CandidateEventID:    "evt-2",
		CandidateGroup:      "cloudedge",
		CandidateType:       "routerd.client.ipv4.observed",
		CandidateDedupeKey:  "client-9",
		CandidateObservedAt: now.Add(-10 * time.Second),
		CandidateExpiresAt:  now.Add(5 * time.Minute),
		ConflictReason:      "held",
	}
	if err := store.UpsertAddressLease(rec); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
	got, found, err := store.GetAddressLease("cloudedge", "10.88.60.9/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if !found {
		t.Fatal("lease not found")
	}
	if got.Status != AddressLeaseStatusHolding || got.OwnerNode != "onprem-router" || got.CandidateOwnerNode != "azure-router" || got.Epoch != 2 {
		t.Fatalf("lease round trip mismatch: %+v", got)
	}
	listed, err := store.ListAddressLeases("cloudedge", true, now)
	if err != nil {
		t.Fatalf("ListAddressLeases: %v", err)
	}
	if len(listed) != 1 || listed[0].SourceEventID != "evt-1" {
		t.Fatalf("listed leases = %+v", listed)
	}
}
