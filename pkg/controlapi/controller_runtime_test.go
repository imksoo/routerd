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
	if !status.CurrentError || status.ConsecutiveErrorCount != 1 || status.LastErrorTime == nil || status.LastErrorClearedAt != nil {
		t.Fatalf("current error fields = current=%t consecutive=%d last=%v cleared=%v", status.CurrentError, status.ConsecutiveErrorCount, status.LastErrorTime, status.LastErrorClearedAt)
	}
}

func TestControllerRuntimeStoreSeparatesHistoricAndCurrentErrors(t *testing.T) {
	store := NewControllerRuntimeStore([]ControllerStatus{{
		Name: "dhcpv6-information",
		Mode: "live",
	}})
	store.ControllerReconciled("dhcpv6-information", "bootstrap", 30*time.Second, 10*time.Millisecond, errors.New("socket missing"))
	first := store.Snapshot()[0]
	if !first.CurrentError || first.ReconcileErrorCount != 1 || first.ConsecutiveErrorCount != 1 || first.LastError == "" || first.LastErrorTime == nil {
		t.Fatalf("first failure status = %+v", first)
	}

	store.ControllerReconciled("dhcpv6-information", "periodic", 30*time.Second, 8*time.Millisecond, nil)
	recovered := store.Snapshot()[0]
	if recovered.ReconcileErrorCount != 1 {
		t.Fatalf("historic error count = %d, want 1", recovered.ReconcileErrorCount)
	}
	if recovered.CurrentError || recovered.ConsecutiveErrorCount != 0 || recovered.LastError != "" {
		t.Fatalf("current error not cleared: current=%t consecutive=%d last=%q", recovered.CurrentError, recovered.ConsecutiveErrorCount, recovered.LastError)
	}
	if recovered.LastErrorTime == nil || recovered.LastErrorClearedAt == nil || recovered.LastSuccessTime == nil {
		t.Fatalf("timestamps not populated: last=%v cleared=%v success=%v", recovered.LastErrorTime, recovered.LastErrorClearedAt, recovered.LastSuccessTime)
	}
	if !recovered.LastErrorClearedAt.After(*recovered.LastErrorTime) && !recovered.LastErrorClearedAt.Equal(*recovered.LastErrorTime) {
		t.Fatalf("clearedAt %v before lastErrorTime %v", recovered.LastErrorClearedAt, recovered.LastErrorTime)
	}
}
