// SPDX-License-Identifier: BSD-3-Clause

package observe

import "testing"

func TestConntrackCountersPreserveIndependentMissingAndObservedZero(t *testing.T) {
	line := "ipv4 2 udp 17 20 src=192.0.2.1 dst=198.51.100.1 sport=1234 dport=53 packets=7 src=198.51.100.1 dst=192.0.2.1 sport=53 dport=1234 bytes=0"
	entry, ok := parseConntrackEntry(line)
	if !ok || entry.Original.Packets == nil || *entry.Original.Packets != 7 || entry.Original.Bytes != nil || entry.Reply.Packets != nil || entry.Reply.Bytes == nil || *entry.Reply.Bytes != 0 {
		t.Fatalf("independent counter presence lost: %+v", entry)
	}
	line = "ipv4 2 udp 17 20 src=192.0.2.1 dst=198.51.100.1 sport=1234 dport=53 packets=bad bytes=-1 src=198.51.100.1 dst=192.0.2.1 sport=53 dport=1234 packets=0 bytes=9"
	entry, ok = parseConntrackEntry(line)
	if !ok || entry.Original.Packets != nil || entry.Original.Bytes != nil || entry.Reply.Packets == nil || *entry.Reply.Packets != 0 || entry.Reply.Bytes == nil || *entry.Reply.Bytes != 9 {
		t.Fatalf("unusable optional counters erased unrelated observations or became zero: %+v", entry)
	}
}
