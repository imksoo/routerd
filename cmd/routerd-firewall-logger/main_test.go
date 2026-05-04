package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestDaemonReadsKeyValueLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	input := strings.NewReader(`action=drop src=172.18.0.10 dst=198.51.100.10 proto=tcp rule_name=test
`)
	if err := run([]string{"daemon", "--path", path}, &bytes.Buffer{}, input); err != nil {
		t.Fatal(err)
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
