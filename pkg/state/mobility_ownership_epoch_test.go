// SPDX-License-Identifier: BSD-3-Clause

package state

import "testing"

func TestMobilityOwnershipEpochBumpsOnOwnerChange(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	rec := MobilityOwnershipEpochRecord{
		Pool:      "cloudedge",
		Address:   "10.88.60.10/32",
		OwnerNode: "azure-router-a",
	}
	rows, err := store.ReconcileMobilityOwnershipEpochs([]MobilityOwnershipEpochRecord{rec})
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if len(rows) != 1 || rows[0].Epoch != 1 {
		t.Fatalf("first rows = %+v, want epoch 1", rows)
	}
	rows, err = store.ReconcileMobilityOwnershipEpochs([]MobilityOwnershipEpochRecord{rec})
	if err != nil {
		t.Fatalf("same owner reconcile: %v", err)
	}
	if rows[0].Epoch != 1 {
		t.Fatalf("same owner epoch = %d, want 1", rows[0].Epoch)
	}
	rec.OwnerNode = "azure-router-b"
	rows, err = store.ReconcileMobilityOwnershipEpochs([]MobilityOwnershipEpochRecord{rec})
	if err != nil {
		t.Fatalf("owner change reconcile: %v", err)
	}
	if rows[0].Epoch != 2 || rows[0].OwnerNode != "azure-router-b" {
		t.Fatalf("owner change rows = %+v, want epoch 2 owner b", rows)
	}
	got, ok, err := store.GetMobilityOwnershipEpoch(rec.Pool, rec.Address)
	if err != nil || !ok {
		t.Fatalf("GetMobilityOwnershipEpoch: ok=%v err=%v", ok, err)
	}
	if got.Epoch != 2 || got.OwnerNode != "azure-router-b" {
		t.Fatalf("stored epoch = %+v, want epoch 2 owner b", got)
	}
	listed, err := store.ListMobilityOwnershipEpochs("cloudedge")
	if err != nil {
		t.Fatalf("ListMobilityOwnershipEpochs: %v", err)
	}
	if len(listed) != 1 || listed[0].OwnerNode != "azure-router-b" {
		t.Fatalf("listed = %+v, want one owner b", listed)
	}
}
