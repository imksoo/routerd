package pppoeclient

import (
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
)

func TestLinuxPeerRendersSoftEtherCredential(t *testing.T) {
	cfg := Config{
		Resource:  "softether",
		Interface: "ens18",
		Spec: api.PPPoESessionSpec{
			Username:        "open@open.ad.jp",
			Password:        "open",
			AuthMethod:      "chap",
			MTU:             1454,
			MRU:             1454,
			LCPEchoInterval: 30,
			LCPEchoFailure:  4,
		},
		RuntimeDir: "/run/routerd/pppoe-client/softether",
	}
	got := string(LinuxPeer(cfg))
	for _, want := range []string{
		"plugin rp-pppoe.so",
		"nic-ens18",
		`user "open@open.ad.jp"`,
		`password "open"`,
		"mtu 1454",
		"lcp-echo-interval 30",
		"-pap",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Linux peer missing %q:\n%s", want, got)
		}
	}
}

func TestParseLogLineExtractsIPCPState(t *testing.T) {
	now := time.Unix(100, 0)
	s := Snapshot{Phase: PhaseConnecting}
	s = ParseLogLine(s, "local  IP address 198.51.100.10", now)
	s = ParseLogLine(s, "remote IP address 198.51.100.1", now)
	s = ParseLogLine(s, "primary   DNS address 203.0.113.53", now)
	if s.Phase != PhaseConnected || s.CurrentAddress != "198.51.100.10" || s.PeerAddress != "198.51.100.1" {
		t.Fatalf("unexpected snapshot: %#v", s)
	}
	if len(s.DNSServers) != 1 || s.DNSServers[0] != "203.0.113.53" {
		t.Fatalf("dns servers = %#v", s.DNSServers)
	}
}
