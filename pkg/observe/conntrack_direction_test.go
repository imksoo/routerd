// SPDX-License-Identifier: BSD-3-Clause

package observe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConntrackNonPortTuplesKeepDirectionAndAccounting(t *testing.T) {
	line := "ipv4 2 icmp 1 20 src=192.0.2.10 dst=198.51.100.20 type=8 code=0 id=7 packets=2 bytes=168 src=198.51.100.20 dst=203.0.113.30 type=0 code=0 id=9 packets=3 bytes=252 mark=1 use=1"
	entries, err := parseConntrackEntries(line, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%+v", entries)
	}
	entry := entries[0]
	if entry.Original.Source != "192.0.2.10" || entry.Original.Destination != "198.51.100.20" || entry.Reply.Source != "198.51.100.20" || entry.Reply.Destination != "203.0.113.30" {
		t.Fatalf("original/reply overwritten: %+v", entry)
	}
	if (entry.Original.Packets == nil || *entry.Original.Packets != 2) || (entry.Original.Bytes == nil || *entry.Original.Bytes != 168) || (entry.Reply.Packets == nil || *entry.Reply.Packets != 3) || (entry.Reply.Bytes == nil || *entry.Reply.Bytes != 252) {
		t.Fatalf("directional counters=%+v/%+v", entry.Original, entry.Reply)
	}
	if count := conntrackEntriesBySNAT(entries)["203.0.113.30"]; count.Total != 1 || count.Other != 1 {
		t.Fatalf("non-port SNAT count=%+v", count)
	}
}

func TestPFObserverKeepsIncomingAndInterfaceBoundTranslatedStates(t *testing.T) {
	// FreeBSD's sbin/pfctl/pf_print_state.c (9b0c57fd73d3) prints
	// interface names, <- for incoming states, and translation alternatives
	// on either endpoint. Exercise that actual command format through PFStates.
	dir := t.TempDir()
	fixture := filepath.Join(dir, "states")
	text := "em0 tcp 10.0.0.2:443 (198.51.100.10:8443) <- 192.0.2.30:50123 ESTABLISHED:ESTABLISHED\n" +
		"   age 00:00:03, expires in 00:00:20, 3:2 pkts, 168:120 bytes\n" +
		"all udp 203.0.113.10:50000 (10.0.0.3:40000) -> 198.51.100.20:53 MULTIPLE:SINGLE\n"
	if err := os.WriteFile(fixture, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	command := "#!/bin/sh\nif [ \"$1\" = '-sm' ]; then printf 'states hard limit 10000\\n'; else cat \"" + fixture + "\"; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "pfctl"), []byte(command), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	table, err := PFStates(context.Background(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if table.Count != 2 || len(table.Entries) != 2 || table.ByFamily["ipv4"] != 2 {
		t.Fatalf("incoming state omitted from complete snapshot: %+v", table)
	}
	entry := table.Entries[0]
	if entry.Original.Source != "192.0.2.30" || entry.Original.SourcePort != "50123" || entry.Original.Destination != "198.51.100.10" || entry.Original.DestinationPort != "8443" || entry.Reply.Source != "10.0.0.2" || entry.Reply.SourcePort != "443" || entry.Reply.Destination != "192.0.2.30" || entry.State != "ESTABLISHED:ESTABLISHED" {
		t.Fatalf("incoming PF translated tuple=%+v", entry)
	}
	entry = table.Entries[1]
	if entry.Original.Source != "10.0.0.3" || entry.Reply.Destination != "203.0.113.10" {
		t.Fatalf("outgoing PF translated tuple=%+v", entry)
	}
	table, err = PFStates(context.Background(), 1)
	if err != nil || table.Count != 2 || len(table.Entries) != 1 {
		t.Fatalf("display selection changed complete count: table=%+v err=%v", table, err)
	}
	table, err = PFStateSnapshot(context.Background())
	if err != nil || table.Count != 2 || table.Max != 10000 {
		t.Fatalf("PF count/limit snapshot: table=%+v err=%v", table, err)
	}
}
