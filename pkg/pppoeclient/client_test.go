// SPDX-License-Identifier: BSD-3-Clause

package pppoeclient

import (
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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

func TestFreeBSDMPDConfigUsesPrivateRuntimeBackend(t *testing.T) {
	cfg := Config{
		Resource:   "wan-pppoe",
		Interface:  "vtnet1",
		IfName:     "ppp0",
		Password:   "secret value",
		RuntimeDir: "/var/run/routerd/pppoe-client/wan-pppoe",
		Spec: api.PPPoESessionSpec{
			Username:    "user@example.test",
			ServiceName: "isp",
			AuthMethod:  "pap",
		},
	}
	name, argv := CommandForOS("freebsd", cfg)
	if name != "mpd5" {
		t.Fatalf("FreeBSD command = %q, want mpd5", name)
	}
	wantArgv := []string{"-d", cfg.RuntimeDir, "-f", "mpd.conf", "-p", cfg.RuntimeDir + "/mpd.pid", "wan-pppoe"}
	if strings.Join(argv, "\x00") != strings.Join(wantArgv, "\x00") {
		t.Fatalf("FreeBSD argv = %#v, want %#v", argv, wantArgv)
	}
	file, config := RuntimeConfigForOS("freebsd", cfg)
	if file != "mpd.conf" {
		t.Fatalf("FreeBSD runtime file = %q", file)
	}
	const bundle = "Bc1e91d44"
	const link = "Lc1e91d44"
	if len(bundle) > 15 || len(link) > 15 || bundle == link {
		t.Fatalf("invalid private mpd names: %q %q", bundle, link)
	}
	for _, want := range []string{
		"create bundle static " + bundle,
		"create link static " + link + " pppoe",
		"set link action bundle " + bundle,
		"set link disable pap chap eap",
		"set link accept pap",
		`set auth authname "user@example.test"`,
		`set auth password "secret value"`,
		"set pppoe iface vtnet1",
		`set pppoe service "isp"`,
		"open",
	} {
		if !strings.Contains(string(config), want) {
			t.Fatalf("FreeBSD mpd config missing %q:\n%s", want, config)
		}
	}
	if strings.Contains(strings.Join(argv, " "), "-ddial") {
		t.Fatalf("FreeBSD argv must not use ppp(8) dial mode: %#v", argv)
	}
}

func TestFreeBSDMPDConfigSelectsConfiguredAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth string
		want string
	}{
		{name: "pap", auth: "pap", want: "set link accept pap"},
		{name: "chap", auth: "chap", want: "set link accept chap"},
		{name: "both", auth: "both", want: "set link accept pap chap"},
		{name: "default-chap", want: "set link accept chap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := string(FreeBSDMPDConf(Config{Resource: "wan", Interface: "vtnet0", IfName: "ppp0", Spec: api.PPPoESessionSpec{AuthMethod: tc.auth}}))
			if !strings.Contains(config, "set link disable pap chap eap") || !strings.Contains(config, tc.want) {
				t.Fatalf("FreeBSD mpd authentication config missing %q:\n%s", tc.want, config)
			}
		})
	}
}

func TestRedactedRuntimeConfigNeverReturnsPassword(t *testing.T) {
	cfg := Config{Resource: "wan", Interface: "vtnet0", Password: "secret value", Spec: api.PPPoESessionSpec{Username: "user"}}
	for _, osName := range []string{"linux", "freebsd"} {
		got := string(RedactedRuntimeConfigForOS(osName, cfg))
		if strings.Contains(got, cfg.Password) || !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("%s redacted config = %q", osName, got)
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

func TestParseLogLineDoesNotTreatGenericIPCPAsNegotiated(t *testing.T) {
	now := time.Unix(100, 0)
	for name, line := range map[string]string{
		"freebsd-mpd5": "[B39362e66] IPCP: Open event",
		"linux-pppd":   "sent [IPCP ConfReq id=0x1 <addr 0.0.0.0>]",
	} {
		t.Run(name, func(t *testing.T) {
			got := ParseLogLine(Snapshot{Phase: PhaseConnecting}, line, now)
			if got.Phase != PhaseConnecting || got.CurrentAddress != "" || got.PeerAddress != "" {
				t.Fatalf("generic IPCP log advanced session: %#v", got)
			}
		})
	}
}

func TestParseLogLineAuthenticationStartsIPCP(t *testing.T) {
	got := ParseLogLine(Snapshot{Phase: PhaseConnecting}, "PAP authentication succeeded", time.Unix(100, 0))
	if got.Phase != PhaseIPCP {
		t.Fatalf("phase = %q, want %q", got.Phase, PhaseIPCP)
	}
}
