// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// ConntrackdConfig renders a reliable, bidirectional conntrackd FTFW peer.
// FTFW keeps received state in the external cache while BACKUP. The VRRP
// promotion hook commits it before the node becomes the active firewall.
func ConntrackdConfig(spec api.ConntrackdSyncSpec) ([]byte, error) {
	local, err := netip.ParseAddr(strings.TrimSpace(spec.LocalAddress))
	if err != nil || !local.Is4() {
		return nil, fmt.Errorf("conntrackd localAddress must be IPv4")
	}
	peer, err := netip.ParseAddr(strings.TrimSpace(spec.PeerAddress))
	if err != nil || !peer.Is4() {
		return nil, fmt.Errorf("conntrackd peerAddress must be IPv4")
	}
	if strings.TrimSpace(spec.Interface) == "" || strings.ContainsAny(spec.Interface, "\r\n \t") {
		return nil, fmt.Errorf("conntrackd interface is required")
	}
	port := spec.Port
	if port == 0 {
		port = 3780
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("conntrackd port must be between 1 and 65535")
	}
	ignored := append([]string{"127.0.0.1"}, spec.IgnoreIPv4...)
	seen := map[string]bool{}
	valid := make([]string, 0, len(ignored))
	for _, raw := range ignored {
		addr, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil || !addr.Is4() {
			return nil, fmt.Errorf("conntrackd ignoreIPv4 contains invalid IPv4 address %q", raw)
		}
		if !seen[addr.String()] {
			seen[addr.String()] = true
			valid = append(valid, addr.String())
		}
	}
	sort.Strings(valid)
	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n")
	b.WriteString("Sync {\n  Mode FTFW {\n    ResendQueueSize 131072\n    CommitTimeout 180\n    PurgeTimeout 60\n    ACKWindowSize 300\n    DisableExternalCache no\n    StartupResync yes\n  }\n")
	fmt.Fprintf(&b, "  UDP {\n    IPv4_address %s\n    IPv4_Destination_Address %s\n    Port %d\n    Interface %s\n    SndSocketBuffer 2097152\n    RcvSocketBuffer 2097152\n    Checksum yes\n  }\n", local, peer, port, spec.Interface)
	// Keep TCP window tracking disabled.  The peer can receive a segment that
	// was already in flight when VRRP moved; strict window validation then
	// classifies an otherwise replicated established flow as INVALID before the
	// application can make progress.  The conntrack mark and TCP state remain
	// synchronized, while NAT44SessionSync validation and doctor require the
	// kernel's tcp_be_liberal sysctl for that small hand-over window.
	b.WriteString("  Options {\n    TCPWindowTracking no\n    ExpectationSync yes\n  }\n}\n")
	b.WriteString("General {\n  Systemd yes\n  HashSize 32768\n  HashLimit 262144\n  LogFile no\n  Syslog yes\n  LockFile /run/routerd/conntrackd.lock\n  UNIX {\n    Path /run/routerd/conntrackd.ctl\n  }\n  NetlinkBufferSize 2097152\n  NetlinkBufferSizeMaxGrowth 8388608\n  NetlinkOverrunResync yes\n  EventIterationLimit 100\n  Filter From Userspace {\n    Protocol Accept {\n      TCP\n      UDP\n      ICMP\n    }\n    Address Ignore {\n")
	for _, addr := range valid {
		fmt.Fprintf(&b, "      IPv4_address %s\n", addr)
	}
	b.WriteString("      IPv6_address ::1\n    }\n  }\n}\n")
	return []byte(b.String()), nil
}
