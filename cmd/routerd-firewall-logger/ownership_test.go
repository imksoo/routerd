// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/nflog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPflogNativeHeaderUsesAlignedLengthAndActualRuleIdentity(t *testing.T) {
	// Current FreeBSD native header reports 69 bytes; the wire prefix is padded
	// to BPF_WORDALIGN(69). Its interface bytes are not the rule number.
	alignment := strconv.IntSize / 8
	offset := (69 + alignment - 1) &^ (alignment - 1)
	packet := make([]byte, offset+28)
	packet[0] = 69
	packet[2] = 1
	packet[60] = 2
	copy(packet[4:20], "em1")
	binary.BigEndian.PutUint32(packet[36:40], 70000)
	ip := packet[offset:]
	ip[0] = 0x45
	ip[9] = 17
	binary.BigEndian.PutUint16(ip[2:4], 28)
	copy(ip[12:20], []byte{192, 0, 2, 1, 198, 51, 100, 1})
	binary.BigEndian.PutUint16(ip[20:22], 12345)
	binary.BigEndian.PutUint16(ip[22:24], 53)
	binary.BigEndian.PutUint16(ip[24:26], 8)
	entry, payload, ok := firewallLogEntryAndPayloadFromPflogPacket(packet)
	if !ok || len(payload) != 28 || entry.RuleName != "pf-rule-70000" || entry.OutIface != "em1" || entry.DstPort != 53 || entry.Action != "drop" {
		t.Fatalf("native header decode: %+v payload=%d ok=%t", entry, len(payload), ok)
	}
	packet[2] = 12
	entry, _, ok = firewallLogEntryAndPayloadFromPflogPacket(packet)
	if !ok || isDenyAction(entry.Action) {
		t.Fatalf("unknown action fabricated deny: %+v", entry)
	}
}

func TestFirewallWriterRejectsRecordsAfterCloseAndCloseIsIdempotent(t *testing.T) {
	log, err := logstore.OpenFirewallLog(filepath.Join(t.TempDir(), "fw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	writer := newFirewallLogWriter(log, 128, time.Hour)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Record(context.Background(), logstore.FirewallLogEntry{}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed writer accepted: %v", err)
	}
}

func TestExpiredCorrelationReadFailureIsUnknown(t *testing.T) {
	log, err := logstore.OpenFirewallLog(filepath.Join(t.TempDir(), "fw.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	entry := correlateExpiredReturn(t.Context(), log, logstore.FirewallLogEntry{Action: "drop", SrcAddress: "192.0.2.1", DstAddress: "198.51.100.1", Protocol: "tcp"}, time.Now())
	if entry.Correlation != "unknown" {
		t.Fatalf("unavailable history fabricated absence: %+v", entry)
	}
}

func TestNFLogVerdictIsExplicitAndIndependentOfRuleNames(t *testing.T) {
	if action := actionFromFirewallPrefix("routerd firewall deny-listed-lan-to-wan accept "); action != "accept" {
		t.Fatalf("rule name overrode explicit action: %q", action)
	}
	entry := firewallLogEntryFromNFLogPacket(nflog.Packet{Prefix: "routerd firewall nas-https ", Protocol: "tcp", SrcAddress: "192.0.2.1", DstAddress: "198.51.100.1"})
	if entry.Action != "unknown" {
		t.Fatalf("missing verdict fabricated deny: %+v", entry)
	}
}
