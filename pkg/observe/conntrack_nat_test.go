package observe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSNATAvailabilityJSON(t *testing.T) {
	unknown, err := json.Marshal(ConnectionTable{})
	if err != nil || strings.Contains(string(unknown), "bySNAT") {
		t.Fatalf("unknown: %s %v", unknown, err)
	}
	counts := conntrackEntriesBySNAT(nil)
	empty, err := json.Marshal(ConnectionTable{BySNAT: &counts})
	if err != nil || !strings.Contains(string(empty), `"bySNAT":{}`) {
		t.Fatalf("empty: %s %v", empty, err)
	}
}

func TestSNATCountsUseFullSnapshot(t *testing.T) {
	var entries []ConnectionEntry
	for i := 0; i < 701; i++ {
		entries = append(entries, ConnectionEntry{Family: "ipv4", Protocol: "tcp", Original: ConntrackTuple{Source: "172.18.1.95"}, Reply: ConntrackTuple{Destination: "192.0.0.4"}})
	}
	counts := conntrackEntriesBySNAT(entries)
	limited := selectConnectionEntries(entries, 600)
	if len(limited) != 600 || counts["192.0.0.4"].Total != 701 || counts["192.0.0.4"].TCP != 701 {
		t.Fatalf("limit affected summary: %d %+v", len(limited), counts)
	}
}

func TestSNATCountsExcludeNonSNATAndIPv6(t *testing.T) {
	entries := []ConnectionEntry{
		{Family: "ipv4", Protocol: "udp", Original: ConntrackTuple{Source: "172.18.1.95"}, Reply: ConntrackTuple{Destination: "192.0.0.3"}},
		{Family: "ipv4", Protocol: "icmp", Original: ConntrackTuple{Source: "172.18.1.95"}, Reply: ConntrackTuple{Destination: "192.0.0.2"}},
		{Family: "ipv4", Protocol: "tcp", Original: ConntrackTuple{Source: "172.18.1.95"}, Reply: ConntrackTuple{Destination: "172.18.1.95"}},
		{Family: "ipv6", Protocol: "tcp", Original: ConntrackTuple{Source: "2001:db8::1"}, Reply: ConntrackTuple{Destination: "2001:db8::2"}},
		{Family: "ipv4"},
	}
	counts := conntrackEntriesBySNAT(entries)
	if len(counts) != 2 || counts["192.0.0.3"].UDP != 1 || counts["192.0.0.2"].Other != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
	if counts := conntrackEntriesBySNAT(nil); counts == nil || len(counts) != 0 {
		t.Fatalf("empty successful snapshot must not mean unsupported: %+v", counts)
	}
}
