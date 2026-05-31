// SPDX-License-Identifier: BSD-3-Clause

package state

import "testing"

func TestMobilityDeprovisionMarkerRoundTrip(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	rec := MobilityDeprovisionMarkerRecord{
		Key:            "mobility:cloudedge:aws:eni-1:unassign-secondary-ip:10.88.60.9/32",
		Source:         "MobilityPool/cloudedge/node/aws-router-a",
		IdempotencyKey: "mobility:cloudedge:aws:eni-1:unassign-secondary-ip:10.88.60.9/32",
		Action:         "unassign-secondary-ip",
		ActionPlanJSON: `{"action":"unassign-secondary-ip","idempotencyKey":"mobility:cloudedge:aws:eni-1:unassign-secondary-ip:10.88.60.9/32"}`,
	}
	if err := store.UpsertMobilityDeprovisionMarker(rec); err != nil {
		t.Fatalf("UpsertMobilityDeprovisionMarker: %v", err)
	}
	rec.ActionPlanJSON = `{"action":"unassign-secondary-ip","riskLevel":"medium","idempotencyKey":"mobility:cloudedge:aws:eni-1:unassign-secondary-ip:10.88.60.9/32"}`
	if err := store.UpsertMobilityDeprovisionMarker(rec); err != nil {
		t.Fatalf("second UpsertMobilityDeprovisionMarker: %v", err)
	}
	rows, err := store.ListMobilityDeprovisionMarkers(rec.Source)
	if err != nil {
		t.Fatalf("ListMobilityDeprovisionMarkers: %v", err)
	}
	if len(rows) != 1 || rows[0].ActionPlanJSON != rec.ActionPlanJSON {
		t.Fatalf("markers = %+v, want one updated marker", rows)
	}
	if err := store.DeleteMobilityDeprovisionMarker(rec.Key); err != nil {
		t.Fatalf("DeleteMobilityDeprovisionMarker: %v", err)
	}
	rows, err = store.ListMobilityDeprovisionMarkers(rec.Source)
	if err != nil {
		t.Fatalf("ListMobilityDeprovisionMarkers after delete: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("markers after delete = %+v, want none", rows)
	}
}
