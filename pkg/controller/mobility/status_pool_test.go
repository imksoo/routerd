// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestMobilityPoolStatusSerializeClearsReasonAfterPartialMerge(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	const poolName = "status-reason"

	if err := controller.savePlannerStatus(poolName, map[string]any{"reason": "provider actions are pending"}); err != nil {
		t.Fatalf("seed planner status: %v", err)
	}
	next := MobilityPoolStatus{Phase: "Ready"}.Serialize()
	if reason, ok := next["reason"]; !ok || reason != "" {
		t.Fatalf("serialized reason = %#v (present=%t), want explicit empty string", reason, ok)
	}
	if err := controller.savePlannerStatus(poolName, next); err != nil {
		t.Fatalf("save recovered planner status: %v", err)
	}
	if got := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)["reason"]; got != "" {
		t.Fatalf("merged reason = %#v, want cleared empty string", got)
	}
}

func TestMobilityPoolStatusSerializeClearsTransitionMarkersAfterPartialMerge(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	const poolName = "status-transitions"

	if err := controller.savePlannerStatus(poolName, map[string]any{
		bgpCaptureTransitionCompletedKey: map[string]map[string]string{
			bgpCaptureTransitionCompletedField: {"192.0.2.10/32": "generation-1"},
		},
	}); err != nil {
		t.Fatalf("seed planner transition status: %v", err)
	}
	next := MobilityPoolStatus{Phase: "Ready"}.Serialize()
	if completed, ok := next[bgpCaptureTransitionCompletedKey]; !ok {
		t.Fatalf("serialized transition completion key missing: %#v", next)
	} else if got := decodeBGPCaptureTransitionState(map[string]any{bgpCaptureTransitionCompletedKey: completed}); len(got.SeizeComplete) != 0 || len(got.CaptureConfirmed) != 0 {
		t.Fatalf("serialized transition state = %#v, want empty", got)
	}
	if err := controller.savePlannerStatus(poolName, next); err != nil {
		t.Fatalf("save cleared planner transition status: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	transitions := decodeBGPCaptureTransitionState(status)
	if len(transitions.SeizeComplete) != 0 || len(transitions.CaptureConfirmed) != 0 {
		t.Fatalf("merged transition state = %#v, want old completion markers cleared", transitions)
	}
}
