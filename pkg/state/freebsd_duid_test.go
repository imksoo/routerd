package state

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKAMEDHCPv6CDUIDLLFromMAC(t *testing.T) {
	got, err := KAMEDHCPv6CDUIDLLFromMAC("02:00:00:00:01:01")
	if err != nil {
		t.Fatalf("build DUID: %v", err)
	}
	want := "0a0000030001020000000101"
	if hex.EncodeToString(got) != want {
		t.Fatalf("DUID = %s, want %s", hex.EncodeToString(got), want)
	}
}

func TestKAMEDHCPv6CDUIDLLFromRawData(t *testing.T) {
	got, err := KAMEDHCPv6CDUIDLLFromRawData("00:01:02:00:00:01:00:03")
	if err != nil {
		t.Fatalf("build raw DUID: %v", err)
	}
	want := "0a0000030001020000010003"
	if hex.EncodeToString(got) != want {
		t.Fatalf("DUID = %s, want %s", hex.EncodeToString(got), want)
	}
}

func TestParseKAMEDHCPv6CDUID(t *testing.T) {
	tests := []struct {
		name             string
		data             []byte
		wantLengthPrefix bool
		wantType         uint16
	}{
		{
			name:             "duid-llt little endian length",
			data:             []byte{0x0e, 0x00, 0x00, 0x01, 0x00, 0x01, 0x31, 0x82, 0x0f, 0x6f, 0x02, 0x00, 0x00, 0x00, 0x01, 0x01},
			wantLengthPrefix: true,
			wantType:         DUIDTypeLinkLayerTime,
		},
		{
			name:             "duid-ll little endian length",
			data:             []byte{0x0a, 0x00, 0x00, 0x03, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0x01, 0x01},
			wantLengthPrefix: true,
			wantType:         DUIDTypeLinkLayer,
		},
		{
			name:             "duid payload without length",
			data:             []byte{0x00, 0x03, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0x01, 0x01},
			wantLengthPrefix: false,
			wantType:         DUIDTypeLinkLayer,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseKAMEDHCPv6CDUID(tt.data)
			if got.HasLengthPrefix != tt.wantLengthPrefix || got.Type != tt.wantType {
				t.Fatalf("info = %+v, want length=%v type=%d", got, tt.wantLengthPrefix, tt.wantType)
			}
		})
	}
}

func TestEnsureKAMEDHCPv6CDUIDLLBacksUpNonLL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dhcp6c_duid")
	old := []byte{0x0e, 0x00, 0x00, 0x01, 0x00, 0x01, 0x31, 0x82, 0x0f, 0x6f, 0x02, 0x00, 0x00, 0x00, 0x01, 0x01}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatalf("write old DUID: %v", err)
	}
	changed, backup, err := EnsureKAMEDHCPv6CDUIDLL(path, "02:00:00:00:01:01", time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ensure DUID: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if backup != path+".bak.20260428T093000Z" {
		t.Fatalf("backup = %q", backup)
	}
	backupData, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if hex.EncodeToString(backupData) != hex.EncodeToString(old) {
		t.Fatalf("backup data = %x, want %x", backupData, old)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read DUID: %v", err)
	}
	if got := ParseKAMEDHCPv6CDUID(data); got.Type != DUIDTypeLinkLayer {
		t.Fatalf("new DUID info = %+v, want DUID-LL", got)
	}
}

func TestEnsureKAMEDHCPv6CDUIDLLKeepsMatchingLL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dhcp6c_duid")
	current, err := KAMEDHCPv6CDUIDLLFromMAC("02:00:00:00:01:01")
	if err != nil {
		t.Fatalf("build current DUID: %v", err)
	}
	if err := os.WriteFile(path, current, 0600); err != nil {
		t.Fatalf("write current DUID: %v", err)
	}
	changed, backup, err := EnsureKAMEDHCPv6CDUIDLL(path, "02:00:00:00:01:01", time.Now())
	if err != nil {
		t.Fatalf("ensure DUID: %v", err)
	}
	if changed || backup != "" {
		t.Fatalf("changed=%v backup=%q, want no change", changed, backup)
	}
}

func TestEnsureKAMEDHCPv6CDUIDLLReplacesDifferentLL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dhcp6c_duid")
	current, err := KAMEDHCPv6CDUIDLLFromMAC("02:00:00:01:00:01")
	if err != nil {
		t.Fatalf("build current DUID: %v", err)
	}
	if err := os.WriteFile(path, current, 0600); err != nil {
		t.Fatalf("write current DUID: %v", err)
	}
	changed, backup, err := EnsureKAMEDHCPv6CDUIDLL(path, "02:00:00:00:01:01", time.Date(2026, 4, 29, 7, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ensure DUID: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if backup != path+".bak.20260429T073000Z" {
		t.Fatalf("backup = %q", backup)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read DUID: %v", err)
	}
	want, err := KAMEDHCPv6CDUIDLLFromMAC("02:00:00:00:01:01")
	if err != nil {
		t.Fatalf("build wanted DUID: %v", err)
	}
	if hex.EncodeToString(data) != hex.EncodeToString(want) {
		t.Fatalf("DUID = %x, want %x", data, want)
	}
}

func TestEnsureKAMEDHCPv6CDUIDLLRawWritesOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dhcp6c_duid")
	changed, backup, err := EnsureKAMEDHCPv6CDUIDLLRaw(path, "00:01:02:00:00:01:00:03", time.Now())
	if err != nil {
		t.Fatalf("ensure raw DUID: %v", err)
	}
	if !changed || backup != "" {
		t.Fatalf("changed=%v backup=%q, want create without backup", changed, backup)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read DUID: %v", err)
	}
	want := "0a0000030001020000010003"
	if hex.EncodeToString(data) != want {
		t.Fatalf("DUID = %s, want %s", hex.EncodeToString(data), want)
	}
}
