// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDHCPFingerprintLogUpsertAndList(t *testing.T) {
	log, err := OpenDHCPFingerprintLog(filepath.Join(t.TempDir(), "dhcp-fingerprints.db"))
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer log.Close()
	now := time.Now().UTC()
	if err := log.Upsert(context.Background(), DHCPFingerprint{
		MAC:              "AA:BB:CC:DD:EE:FF",
		Hostname:         "win-laptop",
		VendorClass:      "MSFT 5.0",
		RequestedOptions: []int{1, 15, 3, 6},
		OSFamily:         "Windows",
		DeviceClass:      "computer",
		Confidence:       90,
		Signal:           "dhcp-fingerprint/windows-vendor",
		ObservedAt:       now,
		Source:           "dnsmasq-log-dhcp",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rows, err := log.List(context.Background(), DHCPFingerprintFilter{Since: now.Add(-time.Minute), Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].MAC != "aa:bb:cc:dd:ee:ff" || rows[0].RequestedOptions[1] != 15 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}
