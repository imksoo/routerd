// SPDX-License-Identifier: BSD-3-Clause
package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDHCPStickyReplayDoesNotExtendRetentionOrOverwriteNewerState(t *testing.T) {
	l, err := OpenDHCPStickyLog(filepath.Join(t.TempDir(), "sticky.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	record := func(action string, days int, when time.Time) bool {
		t.Helper()
		current, err := l.RecordLeaseEventIfCurrent(ctx, action, "02:00:00:00:00:42", "192.0.2.42", "", days, when)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	if !record("removed", 3, at) || !record("removed", 30, at) {
		t.Fatal("equal replay must remain publishable")
	}
	rows, err := l.List(ctx, DHCPStickyFilter{})
	if err != nil || len(rows) != 1 || !rows[0].StickyUntil.Equal(at.Add(72*time.Hour)) {
		t.Fatal(rows, err)
	}
	if !record("renewed", 3, at.Add(time.Hour)) || record("removed", 30, at) {
		t.Fatal("stale delete accepted")
	}
	rows, err = l.List(ctx, DHCPStickyFilter{})
	if err != nil || len(rows) != 1 || !rows[0].StickyUntil.IsZero() || !rows[0].LastRequestAt.Equal(at.Add(time.Hour)) {
		t.Fatal(rows, err)
	}
	if !record("removed", 3, at.Add(2*time.Hour)) || record("renewed", 3, at.Add(time.Hour)) {
		t.Fatal("stale renewal accepted")
	}
	rows, err = l.List(ctx, DHCPStickyFilter{})
	if err != nil || len(rows) != 1 || !rows[0].StickyUntil.Equal(at.Add(74*time.Hour)) {
		t.Fatal(rows, err)
	}
}
