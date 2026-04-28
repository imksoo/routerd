package state

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKAMEDHCP6CDUIDLLFromMAC(t *testing.T) {
	got, err := KAMEDHCP6CDUIDLLFromMAC("bc:24:11:e3:c2:38")
	if err != nil {
		t.Fatalf("build DUID: %v", err)
	}
	want := "0a0000030001bc2411e3c238"
	if hex.EncodeToString(got) != want {
		t.Fatalf("DUID = %s, want %s", hex.EncodeToString(got), want)
	}
}

func TestParseKAMEDHCP6CDUID(t *testing.T) {
	tests := []struct {
		name             string
		data             []byte
		wantLengthPrefix bool
		wantType         uint16
	}{
		{
			name:             "duid-llt little endian length",
			data:             []byte{0x0e, 0x00, 0x00, 0x01, 0x00, 0x01, 0x31, 0x82, 0x0f, 0x6f, 0xbc, 0x24, 0x11, 0xe3, 0xc2, 0x38},
			wantLengthPrefix: true,
			wantType:         DUIDTypeLinkLayerTime,
		},
		{
			name:             "duid-ll little endian length",
			data:             []byte{0x0a, 0x00, 0x00, 0x03, 0x00, 0x01, 0xbc, 0x24, 0x11, 0xe3, 0xc2, 0x38},
			wantLengthPrefix: true,
			wantType:         DUIDTypeLinkLayer,
		},
		{
			name:             "duid payload without length",
			data:             []byte{0x00, 0x03, 0x00, 0x01, 0xbc, 0x24, 0x11, 0xe3, 0xc2, 0x38},
			wantLengthPrefix: false,
			wantType:         DUIDTypeLinkLayer,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseKAMEDHCP6CDUID(tt.data)
			if got.HasLengthPrefix != tt.wantLengthPrefix || got.Type != tt.wantType {
				t.Fatalf("info = %+v, want length=%v type=%d", got, tt.wantLengthPrefix, tt.wantType)
			}
		})
	}
}

func TestEnsureKAMEDHCP6CDUIDLLBacksUpNonLL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dhcp6c_duid")
	old := []byte{0x0e, 0x00, 0x00, 0x01, 0x00, 0x01, 0x31, 0x82, 0x0f, 0x6f, 0xbc, 0x24, 0x11, 0xe3, 0xc2, 0x38}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatalf("write old DUID: %v", err)
	}
	changed, backup, err := EnsureKAMEDHCP6CDUIDLL(path, "bc:24:11:e3:c2:38", time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC))
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
	if got := ParseKAMEDHCP6CDUID(data); got.Type != DUIDTypeLinkLayer {
		t.Fatalf("new DUID info = %+v, want DUID-LL", got)
	}
}

func TestEnsureKAMEDHCP6CDUIDLLKeepsExistingLL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dhcp6c_duid")
	current, err := KAMEDHCP6CDUIDLLFromMAC("bc:24:11:e3:c2:38")
	if err != nil {
		t.Fatalf("build current DUID: %v", err)
	}
	if err := os.WriteFile(path, current, 0600); err != nil {
		t.Fatalf("write current DUID: %v", err)
	}
	changed, backup, err := EnsureKAMEDHCP6CDUIDLL(path, "bc:24:11:e3:c2:38", time.Now())
	if err != nil {
		t.Fatalf("ensure DUID: %v", err)
	}
	if changed || backup != "" {
		t.Fatalf("changed=%v backup=%q, want no change", changed, backup)
	}
}
