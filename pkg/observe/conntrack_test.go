package observe

import "testing"

func TestParseConntrackEntry(t *testing.T) {
	line := "ipv4     2 tcp      6 299 ESTABLISHED src=192.168.1.33 dst=192.168.1.32 sport=44882 dport=22 src=192.168.1.32 dst=192.168.1.33 sport=22 dport=44882 [ASSURED] mark=256 use=1"
	entry, ok := parseConntrackEntry(line)
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Family != "ipv4" || entry.Protocol != "tcp" || entry.State != "ESTABLISHED" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Original.Source != "192.168.1.33" || entry.Original.DestinationPort != "22" {
		t.Fatalf("original = %+v", entry.Original)
	}
	if entry.Reply.Source != "192.168.1.32" || entry.Reply.SourcePort != "22" {
		t.Fatalf("reply = %+v", entry.Reply)
	}
	if entry.Mark != "256" || !entry.Assured {
		t.Fatalf("mark/assured = %s/%t", entry.Mark, entry.Assured)
	}
}

func TestParseConntrackEntriesLimitAndMarkCount(t *testing.T) {
	output := `ipv4 2 udp 17 20 src=192.0.2.1 dst=198.51.100.1 sport=12345 dport=53 src=198.51.100.1 dst=192.0.2.1 sport=53 dport=12345 mark=0 use=1
ipv4 2 tcp 6 30 SYN_SENT src=192.0.2.2 dst=198.51.100.2 sport=23456 dport=443 src=198.51.100.2 dst=192.0.2.2 sport=443 dport=23456 mark=257 use=1
`
	entries := parseConntrackEntries(output, 1)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	byMark := conntrackEntriesByMark(parseConntrackEntries(output, 0))
	if byMark["0"] != 1 || byMark["257"] != 1 {
		t.Fatalf("byMark = %+v", byMark)
	}
}

func TestParseConntrackEntriesZeroLimitMeansAllForParser(t *testing.T) {
	output := `ipv4 2 udp 17 20 src=192.0.2.1 dst=198.51.100.1 sport=12345 dport=53 src=198.51.100.1 dst=192.0.2.1 sport=53 dport=12345 mark=0 use=1
ipv4 2 tcp 6 30 SYN_SENT src=192.0.2.2 dst=198.51.100.2 sport=23456 dport=443 src=198.51.100.2 dst=192.0.2.2 sport=443 dport=23456 mark=257 use=1
`
	entries := parseConntrackEntries(output, 0)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
}
