// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"encoding/json"
	"errors"
	"fmt"
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

func TestReconcileErrorEntryJSONRoundTrip(t *testing.T) {
	original := ReconcileErrorEntry{
		StartedAt:    time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		CompletedAt:  time.Date(2026, 5, 27, 10, 0, 1, 0, time.UTC),
		Duration:     "1s",
		DurationMs:   1000,
		Trigger:      "periodic",
		ResourceKind: "DHCPv6Client",
		ResourceName: "wan",
		Error:        "boom",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded ReconcileErrorEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !decoded.StartedAt.Equal(original.StartedAt) || !decoded.CompletedAt.Equal(original.CompletedAt) {
		t.Fatalf("timestamps not preserved: %+v", decoded)
	}
	if decoded.Duration != original.Duration || decoded.DurationMs != original.DurationMs {
		t.Fatalf("duration not preserved: %+v", decoded)
	}
	if decoded.Trigger != original.Trigger || decoded.ResourceKind != original.ResourceKind || decoded.ResourceName != original.ResourceName {
		t.Fatalf("metadata not preserved: %+v", decoded)
	}
	if decoded.Error != original.Error {
		t.Fatalf("error not preserved: %+v", decoded)
	}
}

func TestControllerRuntimeStoreAppendsReconcileErrorHistory(t *testing.T) {
	store := NewControllerRuntimeStore([]ControllerStatus{{Name: "dns", Mode: "live"}})
	store.ControllerReconciledWithResource("dns", "event", ReconcileResource{Kind: "DNSResolver", Name: "lan"}, 30*time.Second, 12*time.Millisecond, errors.New("upstream timeout"))
	store.ControllerReconciledWithResource("dns", "periodic", ReconcileResource{}, 30*time.Second, 8*time.Millisecond, nil)
	store.ControllerReconciledWithResource("dns", "event", ReconcileResource{Kind: "DNSResolver", Name: "lan"}, 30*time.Second, 15*time.Millisecond, errors.New("nxdomain"))

	got := store.Snapshot()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	status := got[0]
	if len(status.ReconcileErrorHistory) != 2 {
		t.Fatalf("history len = %d, want 2", len(status.ReconcileErrorHistory))
	}
	if status.ReconcileErrorHistory[0].Error != "upstream timeout" {
		t.Fatalf("oldest error = %q", status.ReconcileErrorHistory[0].Error)
	}
	if status.ReconcileErrorHistory[1].Error != "nxdomain" || status.ReconcileErrorHistory[1].ResourceKind != "DNSResolver" || status.ReconcileErrorHistory[1].ResourceName != "lan" {
		t.Fatalf("newest entry = %+v", status.ReconcileErrorHistory[1])
	}
	if status.MaxDurationAt == nil || status.MaxDurationAt.IsZero() {
		t.Fatalf("MaxDurationAt not recorded: %+v", status)
	}
}

func TestControllerRuntimeStoreTrimsReconcileErrorHistory(t *testing.T) {
	store := NewControllerRuntimeStore([]ControllerStatus{{Name: "dns", Mode: "live"}})
	store.SetErrorHistoryLimit(3)
	for i := 0; i < 6; i++ {
		store.ControllerReconciled("dns", "periodic", 30*time.Second, time.Duration(i+1)*time.Millisecond, fmt.Errorf("err %d", i))
	}
	got := store.Snapshot()[0]
	if len(got.ReconcileErrorHistory) != 3 {
		t.Fatalf("history len = %d, want 3", len(got.ReconcileErrorHistory))
	}
	if got.ReconcileErrorHistory[0].Error != "err 3" {
		t.Fatalf("oldest retained = %q, want err 3", got.ReconcileErrorHistory[0].Error)
	}
	if got.ReconcileErrorHistory[2].Error != "err 5" {
		t.Fatalf("newest retained = %q, want err 5", got.ReconcileErrorHistory[2].Error)
	}
	if got.ReconcileErrorCount != 6 {
		t.Fatalf("ReconcileErrorCount = %d, want 6", got.ReconcileErrorCount)
	}
}

func TestControllerRuntimeStorePreservesHistoryAcrossSetBase(t *testing.T) {
	store := NewControllerRuntimeStore([]ControllerStatus{{Name: "dns", Mode: "live"}})
	store.ControllerReconciled("dns", "periodic", 30*time.Second, time.Millisecond, errors.New("oops"))
	store.SetBase([]ControllerStatus{{Name: "dns", Mode: "live", Message: "rebased"}})
	got := store.Snapshot()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Message != "rebased" {
		t.Fatalf("base message lost: %+v", got[0])
	}
	if len(got[0].ReconcileErrorHistory) != 1 {
		t.Fatalf("history lost across SetBase: %+v", got[0])
	}
}

func TestControllerRuntimeStoreSnapshotIsCopied(t *testing.T) {
	store := NewControllerRuntimeStore([]ControllerStatus{{Name: "dns", Mode: "live"}})
	store.ControllerReconciled("dns", "periodic", 30*time.Second, time.Millisecond, errors.New("oops"))
	first := store.Snapshot()
	first[0].ReconcileErrorHistory[0].Error = "tampered"
	second := store.Snapshot()
	if second[0].ReconcileErrorHistory[0].Error != "oops" {
		t.Fatalf("snapshot mutation leaked: %q", second[0].ReconcileErrorHistory[0].Error)
	}
}
