package render

import (
	"strings"
	"testing"
)

func TestDHCP6ClientSystemdUnit(t *testing.T) {
	unit := string(DHCP6ClientSystemdUnit(DHCP6ClientSystemdOptions{
		BinaryPath: "/usr/local/sbin/routerd-dhcp6-client",
		Resource:   "wan-pd",
		Interface:  "wan0",
		SocketPath: "/run/routerd/dhcp6-client/wan-pd.sock",
		LeaseFile:  "/var/lib/routerd/dhcp6-client/wan-pd/lease.json",
		EventFile:  "/var/lib/routerd/dhcp6-client/wan-pd/events.jsonl",
		IAID:       1,
	}))
	for _, want := range []string{
		"Description=routerd DHCPv6 client wan-pd",
		"ExecStart=/usr/local/sbin/routerd-dhcp6-client --resource \"wan-pd\" --interface \"wan0\"",
		"--socket \"/run/routerd/dhcp6-client/wan-pd.sock\"",
		"ProtectSystem=strict",
		"CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN CAP_NET_BIND_SERVICE",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}
