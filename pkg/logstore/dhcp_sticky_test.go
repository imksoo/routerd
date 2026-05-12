// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDHCPStickyLogRecordsHeldLease(t *testing.T) {
	log, err := OpenDHCPStickyLog(filepath.Join(t.TempDir(), "dhcp-sticky.db"))
	if err != nil {
		t.Fatalf("open sticky log: %v", err)
	}
	defer log.Close()
	now := time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)
	if err := log.RecordLeaseEvent(context.Background(), "added", "AA:BB:CC:DD:EE:FF", "192.0.2.20", "phone", 3, now); err != nil {
		t.Fatalf("record add: %v", err)
	}
	if err := log.RecordLeaseEvent(context.Background(), "removed", "AA:BB:CC:DD:EE:FF", "192.0.2.20", "phone", 3, now.Add(time.Hour)); err != nil {
		t.Fatalf("record remove: %v", err)
	}
	rows, err := log.List(context.Background(), DHCPStickyFilter{HeldOnly: true, Now: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("held rows = %d, want 1", len(rows))
	}
	if rows[0].MAC != "aa:bb:cc:dd:ee:ff" || rows[0].IP != "192.0.2.20" || rows[0].Family != "ipv4" {
		t.Fatalf("row = %+v", rows[0])
	}
	if rows[0].StickyUntil.Sub(now.Add(time.Hour)) != 72*time.Hour {
		t.Fatalf("sticky until = %s", rows[0].StickyUntil)
	}
}

func TestDHCPStickyLogReacquireClearsHold(t *testing.T) {
	log, err := OpenDHCPStickyLog(filepath.Join(t.TempDir(), "dhcp-sticky.db"))
	if err != nil {
		t.Fatalf("open sticky log: %v", err)
	}
	defer log.Close()
	now := time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)
	if err := log.RecordLeaseEvent(context.Background(), "removed", "aa:bb:cc:dd:ee:ff", "2001:db8::20", "", 3, now); err != nil {
		t.Fatalf("record remove: %v", err)
	}
	if err := log.RecordLeaseEvent(context.Background(), "renewed", "aa:bb:cc:dd:ee:ff", "2001:db8::20", "", 3, now.Add(time.Hour)); err != nil {
		t.Fatalf("record renew: %v", err)
	}
	rows, err := log.List(context.Background(), DHCPStickyFilter{HeldOnly: true, Now: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("held rows = %d, want 0: %+v", len(rows), rows)
	}
}
