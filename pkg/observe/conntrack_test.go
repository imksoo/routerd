package observe

import (
	"strconv"
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

func TestSelectConnectionEntriesPreservesObservedOrder(t *testing.T) {
	entries := []ConnectionEntry{
		{Family: "ipv4", Protocol: "udp", Original: ConntrackTuple{SourcePort: "50000"}},
		{Family: "ipv4", Protocol: "udp", Original: ConntrackTuple{SourcePort: "50001"}},
		{Family: "ipv4", Protocol: "udp", Original: ConntrackTuple{SourcePort: "50002"}},
	}
	selected := selectConnectionEntries(entries, 2)
	got := []string{selected[0].Original.SourcePort, selected[1].Original.SourcePort}
	want := []string{"50000", "50001"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSelectConnectionEntriesLimit(t *testing.T) {
	entries := []ConnectionEntry{{Family: "ipv4", Protocol: "tcp"}, {Family: "ipv6", Protocol: "tcp"}, {Family: "ipv4", Protocol: "tcp"}}
	selected := selectConnectionEntries(entries, 2)
	if len(selected) != 2 {
		t.Fatalf("selected = %d, want 2", len(selected))
	}
	if selected[0].Family != "ipv4" || selected[1].Family != "ipv6" {
		t.Fatalf("selected = %+v, want observed prefix", selected)
	}
	if selected := selectConnectionEntries(entries, 0); selected != nil {
		t.Fatalf("zero limit selected = %+v, want nil", selected)
	}
}

func TestSelectConnectionEntriesGroupsByFamilyAndProtocol(t *testing.T) {
	entries := []ConnectionEntry{
		{Family: "ipv4", Protocol: "udp"},
		{Family: "ipv6", Protocol: "tcp"},
		{Family: "ipv4", Protocol: "tcp"},
		{Family: "ipv6", Protocol: "udp"},
	}
	selected := selectConnectionEntries(entries, 4)
	got := []string{
		connectionSelectionGroupKey(selected[0]),
		connectionSelectionGroupKey(selected[1]),
		connectionSelectionGroupKey(selected[2]),
		connectionSelectionGroupKey(selected[3]),
	}
	want := []string{"ipv4/udp", "ipv6/tcp", "ipv4/tcp", "ipv6/udp"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order without limiting = %v, want observed order %v", got, want)
		}
	}
	selected = selectConnectionEntries(entries, 3)
	got = []string{
		connectionSelectionGroupKey(selected[0]),
		connectionSelectionGroupKey(selected[1]),
		connectionSelectionGroupKey(selected[2]),
	}
	want = []string{"ipv4/tcp", "ipv4/udp", "ipv6/tcp"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("limited grouped order = %v, want %v", got, want)
		}
	}
}

func TestSelectConnectionEntriesKeepsIPv6VisibleWithoutInterleaving(t *testing.T) {
	var entries []ConnectionEntry
	for i := 0; i < 40; i++ {
		entries = append(entries, ConnectionEntry{
			Family:   "ipv4",
			Protocol: "tcp",
			Original: ConntrackTuple{SourcePort: strconv.Itoa(40000 + i)},
		})
	}
	for i := 0; i < 3; i++ {
		entries = append(entries, ConnectionEntry{
			Family:   "ipv6",
			Protocol: "tcp",
			Original: ConntrackTuple{SourcePort: strconv.Itoa(50000 + i)},
		})
	}
	selected := selectConnectionEntries(entries, 30)
	if len(selected) != 30 {
		t.Fatalf("selected = %d, want 30", len(selected))
	}
	counts := conntrackEntriesByFamily(selected)
	if counts["ipv6"] != 3 {
		t.Fatalf("selected by family = %+v, want overflow ipv6 retained", counts)
	}
	for i := 0; i < 27; i++ {
		if selected[i].Family != "ipv4" {
			t.Fatalf("selected[%d] = %+v, want conntrack order kept until reserve tail", i, selected[i])
		}
	}
	for i := 27; i < 30; i++ {
		if selected[i].Family != "ipv6" {
			t.Fatalf("selected[%d] = %+v, want ipv6 visible at tail", i, selected[i])
		}
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
