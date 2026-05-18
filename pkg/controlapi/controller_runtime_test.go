// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"errors"
	"testing"
	"time"
)

func TestControllerRuntimeStoreRecordsReconcileStats(t *testing.T) {
	store := NewControllerRuntimeStore([]ControllerStatus{{
		Name: "route",
		Mode: "live",
	}})
	store.ControllerStarted("route", 30*time.Second)
	store.ControllerReconciled("route", "bootstrap", 30*time.Second, 12*time.Millisecond, nil)
	store.ControllerReconciled("route", "periodic", 30*time.Second, 30*time.Millisecond, errors.New("route failed"))

	got := store.Snapshot()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	status := got[0]
	if status.Name != "route" || status.Mode != "live" {
		t.Fatalf("identity = %+v", status)
	}
	if status.Interval != "30s" || status.LastTrigger != "periodic" {
		t.Fatalf("interval/trigger = %q/%q", status.Interval, status.LastTrigger)
	}
	if status.ReconcileCount != 2 || status.ReconcileErrorCount != 1 {
		t.Fatalf("counts = %d/%d", status.ReconcileCount, status.ReconcileErrorCount)
	}
	if status.LastDurationMillis != 30 || status.MaxDurationMillis != 30 || status.AverageDurationMillis != 21 {
		t.Fatalf("durations = last %v max %v avg %v", status.LastDurationMillis, status.MaxDurationMillis, status.AverageDurationMillis)
	}
	if status.LastError != "route failed" {
		t.Fatalf("last error = %q", status.LastError)
	}
}
