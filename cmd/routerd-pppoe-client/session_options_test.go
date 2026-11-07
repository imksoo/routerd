// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/pppoeclient"
)

func TestDaemonCarriesConfiguredPPPoESessionOptions(t *testing.T) {
	opts, err := parseOptions("daemon", []string{
		"--resource", "wan",
		"--interface", "eth1",
		"--ifname", "ppp-uplink",
		"--username", "subscriber",
		"--password", "secret",
		"--auth-method", "pap",
		"--service-name", "internet service",
		"--ac-name", "access concentrator",
		"--mtu", "1480",
		"--mru", "1480",
		"--default-route",
		"--use-peer-dns",
		"--persist=false",
		"--ipv6",
		"--lcp-echo-interval", "17",
		"--lcp-echo-failure", "5",
		"--runtime-dir", t.TempDir(),
	})
	if err != nil {
		t.Fatalf("parse PPPoE options: %v", err)
	}
	d := newDaemon(opts, nil)
	if d.snapshot.IfName != "ppp-uplink" {
		t.Fatalf("snapshot interface = %q, want configured PPP interface", d.snapshot.IfName)
	}
	cfg := d.config()
	if cfg.IfName != "ppp-uplink" || !cfg.Spec.DefaultRoute || !cfg.Spec.UsePeerDNS || !cfg.Spec.IPv6 || cfg.Spec.Persist == nil || *cfg.Spec.Persist {
		t.Fatalf("daemon config lost PPPoE settings: %#v", cfg)
	}
	peer := string(pppoeclient.LinuxPeer(cfg))
	for _, want := range []string{"ifname ppp-uplink\n", "defaultroute\n", "usepeerdns\n", "+ipv6\n", "lcp-echo-interval 17\n", "lcp-echo-failure 5\n", "-chap\n", "rp_pppoe_service \"internet service\"\n", "rp_pppoe_ac \"access concentrator\"\n", "mtu 1480\n", "mru 1480\n"} {
		if !strings.Contains(peer, want) {
			t.Errorf("rendered peer missing %q:\n%s", want, peer)
		}
	}
	if strings.Contains(peer, "persist\n") {
		t.Fatalf("disabled reconnect option still rendered:\n%s", peer)
	}
}

func TestDaemonUsesRouterdOnePointZeroPPPoEDefaults(t *testing.T) {
	opts, err := parseOptions("daemon", []string{
		"--interface", "eth1",
		"--username", "subscriber",
		"--password", "secret",
		"--runtime-dir", t.TempDir(),
	})
	if err != nil {
		t.Fatalf("parse PPPoE options: %v", err)
	}
	cfg := newDaemon(opts, nil).config()
	if cfg.Spec.MTU != 1492 || cfg.Spec.MRU != 1492 || cfg.Spec.LCPEchoInterval != 20 || cfg.Spec.LCPEchoFailure != 3 {
		t.Fatalf("default PPPoE negotiation settings = mtu %d mru %d LCP %d/%d, want 1492/1492 and 20/3", cfg.Spec.MTU, cfg.Spec.MRU, cfg.Spec.LCPEchoInterval, cfg.Spec.LCPEchoFailure)
	}
}
