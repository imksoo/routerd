package observe

import (
	"strings"
	"testing"
)

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

func TestParsePFStateEntry(t *testing.T) {
	line := "all tcp 172.18.0.101:53168 -> 93.184.216.34:443       ESTABLISHED:ESTABLISHED"
	entry, ok := parsePFStateEntry(line)
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Family != "ipv4" || entry.Protocol != "tcp" || entry.State != "ESTABLISHED:ESTABLISHED" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Original.Source != "172.18.0.101" || entry.Original.SourcePort != "53168" {
		t.Fatalf("original = %+v", entry.Original)
	}
	if entry.Original.Destination != "93.184.216.34" || entry.Original.DestinationPort != "443" {
		t.Fatalf("original destination = %+v", entry.Original)
	}
	if entry.Reply.Source != "93.184.216.34" || entry.Reply.Destination != "172.18.0.101" {
		t.Fatalf("reply = %+v", entry.Reply)
	}
}

func TestParsePFStateEntryWithNAT(t *testing.T) {
	line := "all udp 198.51.100.10:62000 (10.0.0.2:53168) -> 1.1.1.1:53       MULTIPLE:SINGLE"
	entry, ok := parsePFStateEntry(line)
	if !ok {
		t.Fatal("parse failed")
	}
	if entry.Original.Source != "10.0.0.2" || entry.Original.SourcePort != "53168" {
		t.Fatalf("original = %+v", entry.Original)
	}
	if entry.Reply.Destination != "198.51.100.10" || entry.Reply.DestinationPort != "62000" {
		t.Fatalf("reply = %+v", entry.Reply)
	}
}

func TestParsePFStateEntriesLimit(t *testing.T) {
	output := strings.Join([]string{
		"all tcp 172.18.0.101:53168 -> 93.184.216.34:443       ESTABLISHED:ESTABLISHED",
		"   [123 + 64] wscale 7",
		"all udp 10.0.0.2:53168 -> 1.1.1.1:53       MULTIPLE:SINGLE",
	}, "\n")
	entries := parsePFStateEntries(output, 1)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
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

func TestSortConnectionEntriesStableKey(t *testing.T) {
	entries := []ConnectionEntry{
		{Family: "ipv4", Protocol: "udp", Original: ConntrackTuple{Source: "172.18.0.20", Destination: "1.1.1.1", SourcePort: "50000", DestinationPort: "53"}},
		{Family: "ipv4", Protocol: "tcp", State: "ESTABLISHED", Original: ConntrackTuple{Source: "172.18.0.10", Destination: "93.184.216.34", SourcePort: "40000", DestinationPort: "443"}},
		{Family: "ipv4", Protocol: "tcp", State: "SYN_SENT", Original: ConntrackTuple{Source: "172.18.0.10", Destination: "93.184.216.34", SourcePort: "40001", DestinationPort: "443"}},
	}
	sortConnectionEntries(entries)
	got := []string{entries[0].Protocol + "/" + entries[0].State, entries[1].Protocol + "/" + entries[1].State, entries[2].Protocol + "/" + entries[2].State}
	want := []string{"tcp/ESTABLISHED", "tcp/SYN_SENT", "udp/"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSelectConnectionEntriesKeepsIPv6WhenIPv4Dominates(t *testing.T) {
	var entries []ConnectionEntry
	for i := 0; i < 300; i++ {
		entries = append(entries, ConnectionEntry{
			Family:   "ipv4",
			Protocol: "tcp",
			Original: ConntrackTuple{
				Source:          "172.18.0.101",
				Destination:     "198.51.100.10",
				SourcePort:      "40000",
				DestinationPort: "443",
			},
		})
	}
	for i := 0; i < 2; i++ {
		entries = append(entries, ConnectionEntry{
			Family:   "ipv6",
			Protocol: "tcp",
			Original: ConntrackTuple{
				Source:          "2001:db8::1",
				Destination:     "2001:db8::2",
				SourcePort:      "50000",
				DestinationPort: "443",
			},
		})
	}
	sortConnectionEntries(entries)
	selected := selectConnectionEntries(entries, 30)
	if len(selected) != 30 {
		t.Fatalf("selected = %d, want 30", len(selected))
	}
	counts := conntrackEntriesByFamily(selected)
	if counts["ipv6"] != 2 {
		t.Fatalf("selected by family = %+v, want all ipv6 entries retained", counts)
	}
	if counts["ipv4"] != 28 {
		t.Fatalf("selected by family = %+v, want remaining slots filled by ipv4", counts)
	}
}

func TestConntrackEntriesByFamilyNormalizesEmptyFamily(t *testing.T) {
	counts := conntrackEntriesByFamily([]ConnectionEntry{
		{Family: "ipv4"},
		{Family: "IPv6"},
		{Family: ""},
	})
	if counts["ipv4"] != 1 || counts["ipv6"] != 1 || counts["other"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}
