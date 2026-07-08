// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dpi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestParseOptionsDefaultPathUsesPlatformStateDir(t *testing.T) {
	opts, err := parseOptions("daemon", []string{"daemon"})
	if err != nil {
		t.Fatal(err)
	}
	defaults, _ := platform.Current()
	if got, want := opts.path, defaults.FirewallLogFile(); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestSelftestCreatesDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	var out bytes.Buffer
	if err := run([]string{"selftest", "--path", path}, &out, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"ok":true`) && !strings.Contains(out.String(), `"ok": true`) {
		t.Fatalf("output = %s", out.String())
	}
}

func TestSelftestUsesDPIClassifierSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "dpi.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(dpi.ClassifyResult{AppName: "tls", AppCategory: "web", AppConfidence: 90, TLSSNI: "routerd-firewall-selftest.example"})
	})}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	if err := run([]string{"selftest", "--path", path, "--dpi-socket", socket}, &bytes.Buffer{}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	log, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	rows, err := log.List(context.Background(), logstore.FirewallLogFilter{Action: "drop", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var foundDPI, foundOrphan, foundSYN bool
	for _, row := range rows {
		if row.DPIApp == "tls" && row.DPITLSSNI == "routerd-firewall-selftest.example" {
			foundDPI = true
		}
		if row.Correlation == "orphan_return" && row.RuleName == "selftest-orphan-return" {
			foundOrphan = true
		}
		if row.RuleName == "selftest" && row.TCPFlags == "SYN" {
			foundSYN = true
		}
	}
	if !foundDPI || !foundOrphan || !foundSYN {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestDaemonReadsKeyValueLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	input := strings.NewReader(`action=drop src=172.18.0.10 dst=198.51.100.10 proto=tcp rule_name=test
`)
	if err := run([]string{"daemon", "--path", path}, &bytes.Buffer{}, input); err != nil {
		t.Fatal(err)
	}
	log, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	rows, err := log.List(context.Background(), logstore.FirewallLogFilter{Action: "drop", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RuleName != "test" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestDaemonReadsPflogTCPDumpLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	input := strings.NewReader(`2026-05-04 12:00:00.000000 rule 12/0(match): block in on em0: 172.18.0.101.53168 > 198.51.100.10.443: Flags [S], length 0
`)
	if err := run([]string{"daemon", "--path", path, "--input-format", "pflog-tcpdump"}, &bytes.Buffer{}, input); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonReadsNFLogTCPDumpLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	input := strings.NewReader(`2026-05-05 12:00:00.000000 routerd firewall forward deny IP 172.18.0.101.53168 > 198.51.100.10.443: Flags [S], length 0
`)
	if err := run([]string{"daemon", "--path", path, "--input-format", "nflog-tcpdump"}, &bytes.Buffer{}, input); err != nil {
		t.Fatal(err)
	}
}

func TestParsePflogTCPDumpLine(t *testing.T) {
	line := `2026-05-04 12:00:00.000000 rule 12/0(match): block in on em0: 172.18.0.101.53168 > 198.51.100.10.443: Flags [S], length 0`
	entry, ok := parseFirewallLogLine(line, "pflog-tcpdump")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Action != "drop" || entry.Protocol != "tcp" || entry.L3Proto != "ipv4" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.InIface != "em0" || entry.SrcAddress != "172.18.0.101" || entry.SrcPort != 53168 {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.DstAddress != "198.51.100.10" || entry.DstPort != 443 || entry.RuleName != "rule 12/0(match)" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.TCPFlags != "SYN" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestParseNFLogTCPDumpLine(t *testing.T) {
	line := `2026-05-05 12:00:00.000000 routerd firewall forward deny IP 172.18.0.101.53168 > 198.51.100.10.443: Flags [S], seq 1, length 0`
	entry, ok := parseFirewallLogLine(line, "nflog-tcpdump")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Action != "drop" || entry.Protocol != "tcp" || entry.L3Proto != "ipv4" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.SrcAddress != "172.18.0.101" || entry.SrcPort != 53168 {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.DstAddress != "198.51.100.10" || entry.DstPort != 443 || entry.RuleName != "routerd firewall forward deny" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.TCPFlags != "SYN" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestParseNFLogTCPDumpIPv6UDPLine(t *testing.T) {
	line := `routerd firewall input deny IP6 2409:10:3d60:1271::100.5353 > ff02::fb.5353: UDP, length 32`
	entry, ok := parseFirewallLogLine(line, "auto")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Action != "drop" || entry.Protocol != "udp" || entry.L3Proto != "ipv6" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.SrcAddress != "2409:10:3d60:1271::100" || entry.DstAddress != "ff02::fb" || entry.PacketBytes != 32 {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestFirewallLogEntryFromIPv4Packet(t *testing.T) {
	packet := []byte{
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 64, 6, 0, 0,
		172, 18, 0, 101,
		198, 51, 100, 10,
		0xcf, 0xb0, 0x01, 0xbb,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0x50, 0x02, 0, 0,
		0, 0, 0, 0,
	}
	entry, ok := firewallLogEntryFromIPPacket(time.Unix(1, 0).UTC(), packet, "test")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.L3Proto != "ipv4" || entry.Protocol != "tcp" || entry.SrcAddress != "172.18.0.101" || entry.DstPort != 443 {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.TCPFlags != "SYN" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestParseKeyValueTCPFlags(t *testing.T) {
	entry, ok := parseFirewallLogLine(`action=drop src=172.18.0.10 dst=198.51.100.10 proto=tcp flags=SYN,ACK`, "kv")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.TCPFlags != "SYN,ACK" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestAppendDPIHintUsesClassifierSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "dpi.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(dpi.ClassifyResult{AppName: "tls", AppCategory: "web", AppConfidence: 90, TLSSNI: "routerd.example"})
	})}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	entry := enrichEntryWithDPI(context.Background(), options{dpiSocket: socket, dpiTimeout: time.Second}, logstoreEntry("nflog-netlink"), []byte{0x45})
	if entry.DPIApp != "tls" || entry.DPITLSSNI != "routerd.example" || entry.DPIConfidence != 90 {
		t.Fatalf("entry = %+v", entry)
	}
	if !strings.Contains(entry.Hint, "dpi.app=tls") || !strings.Contains(entry.Hint, "dpi.tls_sni=routerd.example") {
		t.Fatalf("hint = %q", entry.Hint)
	}
}

func TestParseConntrackDestroyLine(t *testing.T) {
	line := `[DESTROY] tcp      6 src=172.18.0.10 dst=198.51.100.10 sport=53168 dport=443 packets=10 bytes=1234 src=198.51.100.10 dst=192.0.0.2 sport=443 dport=53168 packets=8 bytes=4321`
	flow, ok := parseConntrackDestroyLine(line, time.Unix(10, 0).UTC())
	if !ok {
		t.Fatal("parse failed")
	}
	if flow.Protocol != "tcp" || flow.OrigSrc != "172.18.0.10" || flow.OrigDstPort != 443 || flow.ReplyDst != "192.0.0.2" || flow.Bytes != 5555 {
		t.Fatalf("flow = %+v", flow)
	}
}

func TestCorrelateExpiredReturn(t *testing.T) {
	log, err := logstore.OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	if err := log.RecordExpiredFlow(context.Background(), logstore.ExpiredFlowEntry{
		Timestamp:    now.Add(-2 * time.Minute),
		Protocol:     "tcp",
		L3Proto:      "ipv4",
		OrigSrc:      "172.18.0.10",
		OrigSrcPort:  53168,
		OrigDst:      "198.51.100.10",
		OrigDstPort:  443,
		ReplySrc:     "198.51.100.10",
		ReplySrcPort: 443,
		ReplyDst:     "192.0.0.2",
		ReplyDstPort: 53168,
		Bytes:        4096,
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	entry := correlateExpiredReturn(context.Background(), log, logstore.FirewallLogEntry{
		Action:     "drop",
		SrcAddress: "198.51.100.10",
		SrcPort:    443,
		DstAddress: "192.0.0.2",
		DstPort:    53168,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}, now)
	if entry.Correlation != "orphan_return" || entry.ExpiredBytes != 4096 || !strings.Contains(entry.CorrelationDetail, "likely orphan return") {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestAcceptDPIFlowEnrichesLaterDeny(t *testing.T) {
	log, err := logstore.OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	opts := options{dpiFlowTTL: time.Hour, dpiFlowLimit: 100000}
	now := time.Now().UTC()
	accept := logstore.FirewallLogEntry{
		Timestamp:     now.Add(-30 * time.Second),
		Action:        "accept",
		SrcAddress:    "172.18.0.10",
		SrcPort:       53168,
		DstAddress:    "198.51.100.10",
		DstPort:       443,
		Protocol:      "tcp",
		L3Proto:       "ipv4",
		DPIApp:        "tls",
		DPICategory:   "web",
		DPIConfidence: 90,
		DPITLSSNI:     "cached.example",
	}
	if err := recordFirewallEntry(context.Background(), log, accept, nil, opts); err != nil {
		t.Fatal(err)
	}
	deny := logstore.FirewallLogEntry{
		Action:     "drop",
		SrcAddress: "198.51.100.10",
		SrcPort:    443,
		DstAddress: "172.18.0.10",
		DstPort:    53168,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}
	deny = enrichEntryWithDPIFlow(context.Background(), log, deny, opts, now)
	if deny.DPIApp != "tls" || deny.DPITLSSNI != "cached.example" || !strings.Contains(deny.Hint, "prior TLS-SNI=cached.example flow") {
		t.Fatalf("deny = %+v", deny)
	}
}

func TestShouldClassifyForwardDPISkipsKnownFlow(t *testing.T) {
	log, err := logstore.OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	opts := options{dpiSocket: "/tmp/dpi.sock", dpiFlowTTL: time.Hour, dpiFlowLimit: 100000, dpiFlowFirstPackets: 10}
	entry := logstore.FirewallLogEntry{
		Action:     "accept",
		SrcAddress: "172.18.0.10",
		SrcPort:    53168,
		DstAddress: "198.51.100.10",
		DstPort:    443,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}
	if !shouldClassifyForwardDPI(context.Background(), log, entry, opts, time.Now().UTC()) {
		t.Fatal("empty cache should classify")
	}
	entry.DPIApp = "tls"
	entry.DPITLSSNI = "cached.example"
	if err := recordDPIFlowFromEntry(context.Background(), log, entry, opts); err != nil {
		t.Fatal(err)
	}
	entry.DPIApp = ""
	entry.DPITLSSNI = ""
	if shouldClassifyForwardDPI(context.Background(), log, entry, opts, time.Now().UTC()) {
		t.Fatal("known cached flow should not be classified again")
	}
}

func TestShouldClassifyForwardDPIStopsAfterUnknownPacketBudget(t *testing.T) {
	log, err := logstore.OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	opts := options{dpiSocket: "/tmp/dpi.sock", dpiFlowTTL: time.Hour, dpiFlowLimit: 100000, dpiFlowFirstPackets: 2}
	entry := logstore.FirewallLogEntry{
		Timestamp:  time.Now().UTC(),
		Action:     "accept",
		SrcAddress: "172.18.0.10",
		SrcPort:    53168,
		DstAddress: "198.51.100.10",
		DstPort:    443,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
		Hint: appendDPIHintFields("", dpi.ClassifyResult{
			AppName: "unknown",
			Engine:  "builtin",
			Source:  "builtin",
			Reason:  "no_application_signal",
		}),
	}
	if err := recordFirewallEntry(context.Background(), log, entry, nil, opts); err != nil {
		t.Fatal(err)
	}
	if !shouldClassifyForwardDPI(context.Background(), log, entry, opts, time.Now().UTC()) {
		t.Fatal("unknown flow below packet budget should still classify")
	}
	if err := recordFirewallEntry(context.Background(), log, entry, nil, opts); err != nil {
		t.Fatal(err)
	}
	if shouldClassifyForwardDPI(context.Background(), log, entry, opts, time.Now().UTC()) {
		t.Fatal("unknown flow at packet budget should stop classification")
	}
	flow, ok, err := log.FindDPIFlowForFirewallEntry(context.Background(), entry, time.Now().UTC(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || flow.AppName != "unknown" || flow.PacketCount != 2 || flow.Metadata["reason"] != "no_application_signal" {
		t.Fatalf("flow ok=%v flow=%+v", ok, flow)
	}
}

func logstoreEntry(hint string) logstore.FirewallLogEntry {
	return logstore.FirewallLogEntry{Action: "drop", Protocol: "tcp", L3Proto: "ipv4", Hint: hint}
}

func TestFirewallLogEntryFromIPv6Packet(t *testing.T) {
	packet := append([]byte{
		0x60, 0, 0, 0, 0, 8, 17, 64,
		0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
		0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2,
	}, []byte{0x14, 0xe9, 0x00, 0x35}...)
	entry, ok := firewallLogEntryFromIPPacket(time.Unix(1, 0).UTC(), packet, "test")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.L3Proto != "ipv6" || entry.Protocol != "udp" || entry.SrcAddress != "2001:db8::1" || entry.DstPort != 53 {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestParsePflogTCPDumpUDPLine(t *testing.T) {
	line := `rule 7/0(match): pass out on em1: 172.18.0.101.53168 > 1.1.1.1.53: UDP, length 40`
	entry, ok := parseFirewallLogLine(line, "auto")
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Action != "accept" || entry.Protocol != "udp" || entry.OutIface != "em1" || entry.PacketBytes != 40 {
		t.Fatalf("entry = %+v", entry)
	}
}
