// SPDX-License-Identifier: BSD-3-Clause

package state

import "testing"

func TestMobilityCaptureEpochBumpsOnHolderChange(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	rec := MobilityCaptureEpochRecord{
		CaptureKey:    "cloudedge|10.88.60.10/32|provider:azure-provider:placement:azure-edge",
		Pool:          "cloudedge",
		Address:       "10.88.60.10/32",
		CaptureDomain: "provider:azure-provider:placement:azure-edge",
		Holder:        "azure-router-a",
	}
	rows, err := store.ReconcileMobilityCaptureEpochs([]MobilityCaptureEpochRecord{rec})
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if len(rows) != 1 || rows[0].Epoch != 1 {
		t.Fatalf("first rows = %+v, want epoch 1", rows)
	}
	rows, err = store.ReconcileMobilityCaptureEpochs([]MobilityCaptureEpochRecord{rec})
	if err != nil {
		t.Fatalf("same holder reconcile: %v", err)
	}
	if rows[0].Epoch != 1 {
		t.Fatalf("same holder epoch = %d, want 1", rows[0].Epoch)
	}
	rec.Holder = "azure-router-b"
	rows, err = store.ReconcileMobilityCaptureEpochs([]MobilityCaptureEpochRecord{rec})
	if err != nil {
		t.Fatalf("holder change reconcile: %v", err)
	}
	if rows[0].Epoch != 2 || rows[0].Holder != "azure-router-b" {
		t.Fatalf("holder change rows = %+v, want epoch 2 holder b", rows)
	}
	got, ok, err := store.GetMobilityCaptureEpoch(rec.CaptureKey)
	if err != nil || !ok {
		t.Fatalf("GetMobilityCaptureEpoch: ok=%v err=%v", ok, err)
	}
	if got.Epoch != 2 || got.Holder != "azure-router-b" {
		t.Fatalf("stored epoch = %+v, want epoch 2 holder b", got)
	}
}
