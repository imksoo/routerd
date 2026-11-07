// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"fmt"
	"github.com/imksoo/routerd/pkg/logstore"
	"strconv"
	"strings"
	"time"
)

func firewallLogEntryFromPflogPacket(packet []byte) (logstore.FirewallLogEntry, bool) {
	entry, _, ok := firewallLogEntryAndPayloadFromPflogPacket(packet)
	return entry, ok
}

func firewallLogEntryAndPayloadFromPflogPacket(packet []byte) (logstore.FirewallLogEntry, []byte, bool) {
	if len(packet) < 61 {
		return logstore.FirewallLogEntry{}, nil, false
	}
	// FreeBSD net/if_pflog.h: length excludes pad2; the BPF payload uses
	// PFLOG_HDRLEN = BPF_WORDALIGN(length), not the unaligned length byte.
	length := int(packet[0])
	alignment := strconv.IntSize / 8
	offset := (length + alignment - 1) &^ (alignment - 1)
	if length < 61 || offset > len(packet) {
		return logstore.FirewallLogEntry{}, nil, false
	}
	payload := packet[offset:]
	entry, ok := firewallLogEntryFromIPPacket(time.Now().UTC(), payload, "pflog-bpf")
	if !ok {
		return logstore.FirewallLogEntry{}, nil, false
	}
	entry.Action = pflogAction(packet[2])
	entry.RuleName = fmt.Sprintf("pf-rule-%d", binary.BigEndian.Uint32(packet[36:40]))
	ifname := strings.TrimRight(string(packet[4:20]), "\x00")
	switch packet[60] {
	case 1:
		entry.InIface = ifname
	case 2:
		entry.OutIface = ifname
	}
	return entry, payload, true
}

func pflogAction(action byte) string {
	switch action {
	case 0:
		return "pass"
	case 1:
		return "drop"
	case 2:
		return "scrub"
	default:
		return fmt.Sprintf("pf-action-%d", action)
	}
}
