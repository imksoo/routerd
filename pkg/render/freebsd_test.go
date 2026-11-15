// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFreeBSDRendersRouter01Basics(t *testing.T) {
	enabled, disabled := true, false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true, AdminUp: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "mgmt"}, Spec: api.InterfaceSpec{IfName: "vtnet2", Managed: true, AdminUp: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "lan-bridge"}, Spec: api.BridgeSpec{IfName: "bridge0", Members: []string{"lan", "home-vxlan"}, RSTP: &enabled}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "home-vxlan"}, Spec: api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10", Remotes: []string{"192.0.2.20"}, UnderlayInterface: "wan", UDPPort: 4789, MTU: 1450, Bridge: "lan-bridge"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan-dhcpv4"}, Spec: api.DHCPv4ClientSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "mgmt-dhcpv4"}, Spec: api.DHCPv4ClientSpec{Interface: "mgmt", UseRoutes: &disabled, UseDNS: &disabled}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"}, Metadata: api.ObjectMeta{Name: "wan-dhcpv6"}, Spec: api.DHCPv6AddressSpec{Interface: "wan", Client: "dhcp6c"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", Client: "dhcp6c", Profile: "ntt-hgw-lan-pd", PrefixLength: 60, IAID: "00000001"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "wan-pppoe"}, Spec: api.PPPoESessionSpec{Interface: "wan", IfName: "ppp0", Username: "user@example.jp", Password: "secret", Managed: true, DefaultRoute: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "bridge-ipv4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan-bridge", Address: "192.0.2.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"}, Metadata: api.ObjectMeta{Name: "lab-v4"}, Spec: api.IPv4StaticRouteSpec{Interface: "lan", Destination: "192.0.2.0/24", Via: "192.168.10.254"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "lab-v6"}, Spec: api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:1::/64", Via: "fe80::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::1", Announce: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-nat"}, Spec: api.NAT44RuleSpec{OutboundInterface: "wan", SourceCIDRs: []string{"192.168.10.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "interfaceAddress"}}},
	}}}

	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	rc := string(got.RCConf)
	for _, want := range []string{
		`ifconfig_vtnet0="up"`,
		`ifconfig_vtnet1="inet 192.168.10.1/24"`,
		`ifconfig_vtnet2="up"`,
		`mpd_enable="YES"`,
		`mpd_flags="-b"`,
		`cloned_interfaces="bridge0"`,
		`routerd_vxlan_home_vxlan_enable="YES"`,
		`ifconfig_bridge0="addm vtnet1 stp vtnet1 up"`,
		`ifconfig_bridge0_alias0="inet 192.0.2.1/24"`,
		`static_routes="lab_v4"`,
		`route_lab_v4="-net 192.0.2.0/24 192.168.10.254"`,
		`ipv6_static_routes="lab_v6"`,
		`ipv6_route_lab_v6="2001:db8:1::/64 fe80::1%vtnet0"`,
		`pf_enable="YES"`,
		`pflog_enable="YES"`,
	} {
		if !strings.Contains(rc, want) {
			t.Fatalf("rc.conf output missing %q:\n%s", want, rc)
		}
	}
	vxlanScript := string(got.RCDScripts["routerd_vxlan_home_vxlan"])
	for _, want := range []string{`/sbin/ifconfig "${ifname}" create || /sbin/ifconfig vxlan create name "${ifname}" || return 1`, `vxlanid' '100'`, `vxlanremote' '192.0.2.20'`, `vxlandev' 'vtnet0'`, `vxlanlocalport' '4789'`, `vxlanremoteport' '4789'`, `ifconfig 'bridge0' addm "${ifname}"`, `ifconfig 'bridge0' deletem "${ifname}"`, `unable to publish routerd VXLAN ownership marker`, `load_rc_config $name`, `routerd ownership marker`} {
		if !strings.Contains(vxlanScript, want) {
			t.Fatalf("VXLAN rc.d output missing %q:\n%s", want, vxlanScript)
		}
	}
	if strings.Contains(vxlanScript, "vxlanport") {
		t.Fatalf("FreeBSD VXLAN rc.d output uses unsupported vxlanport token:\n%s", vxlanScript)
	}
	pf := string(got.PF)
	for _, want := range []string{
		`wan_if = "vtnet0"`,
		`lan_if = "vtnet1"`,
		`nat on vtnet0 from 192.168.10.0/24 to any -> (vtnet0)`,
		`block drop all`,
	} {
		if !strings.Contains(pf, want) {
			t.Fatalf("pf output missing %q:\n%s", want, pf)
		}
	}
	if dhclient := string(got.DHCPClient); strings.TrimSpace(dhclient) != "" {
		t.Fatalf("FreeBSD renderer must not emit legacy dhclient config for DHCPv4Client:\n%s", dhclient)
	}
	for _, unwanted := range []string{"dhcp6c_enable", "dhcp6c_interfaces", "dhcp6c_flags"} {
		if strings.Contains(rc, unwanted) {
			t.Fatalf("FreeBSD rc.conf must not render legacy DHCPv6 client key %q:\n%s", unwanted, rc)
		}
	}
	mpd5 := string(got.MPD5)
	for _, want := range []string{
		"default:",
		"load routerd_wan_pppoe",
		"routerd_wan_pppoe:",
		"create bundle static Bwan_pppoe",
		"set iface name ppp0",
		"set iface route default",
		"create link static Lwan_pppoe pppoe",
		`set auth authname "user@example.jp"`,
		`set auth password "secret"`,
		"set pppoe iface vtnet0",
		"open",
	} {
		if !strings.Contains(mpd5, want) {
			t.Fatalf("mpd5 output missing %q:\n%s", want, mpd5)
		}
	}
}

func TestFreeBSDRendersCARPRCDScript(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "api-vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
			Interface: "lan",
			Address:   "10.240.70.10/32",
			Mode:      "vrrp",
			VRRP: api.VirtualAddressVRRPSpec{
				VirtualRouterID: 50,
				Priority:        150,
				AdvertInterval:  "2s",
				Authentication:  "secret",
			},
		}},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	rc := string(got.RCConf)
	if !strings.Contains(rc, `routerd_carp_enable="YES"`) {
		t.Fatalf("rc.conf did not enable routerd_carp:\n%s", rc)
	}
	script := string(got.RCDScripts["routerd_carp"])
	for _, want := range []string{
		`kldload carp >/dev/null 2>&1 || true`,
		`foreign CARP address is already present; refusing mutation`,
		`foreign CARP VHID is already present; refusing mutation`,
		`foreign CARP ownership is unknown; refusing mutation`,
		`${ROUTERD_RUNTIME_DIR:-/var/run/routerd}/carp`,
		`unable to publish routerd CARP ownership marker`,
		`grep -Fq 'inet 10.240.70.10 '`,
		`grep -Eq 'vhid 50([[:space:]]|$)'`,
		`kldload carp >/dev/null 2>&1 || true`,
		`preempt_before=$(sysctl -n net.inet.carp.preempt) || { echo "unable to read CARP preempt" >&2; return 1; }`,
		`unable to configure CARP preempt`,
		`unable to configure routerd CARP address`,
		`applied_0=1`,
		`${applied_0:-0}`,
		`printf '%s\n%s\n' "${name}" "${preempt_before}"`,
		`{ read owned; read preempt_before; } < "${marker}"`,
		`routerd CARP cleanup incomplete; retaining ownership marker`,
		`if routerd_carp_status; then return 0; fi`,
		`routerd_carp_stop || return 1`,
		`if ifconfig 'vtnet1' | grep -Fq 'inet 10.240.70.10 '; then`,
		`sysctl net.inet.carp.preempt='0'`,
		`ifconfig 'vtnet1' 'inet' 'vhid' '50' 'advbase' '2' 'advskew' '104' 'pass' 'secret' 'alias' '10.240.70.10/32'`,
		`ifconfig 'vtnet1' 'inet' '10.240.70.10/32' -alias`,
		`grep -Eq 'vhid 50([[:space:]]|$)'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("routerd_carp script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "grep -Fq '10.240.70.10/32'") {
		t.Fatalf("CARP ownership probes must not grep CIDR-form addresses:\n%s", script)
	}
}

func TestFreeBSDRendersNTPServerWithRestrictedListen(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Spec: api.NTPClientSpec{
				Provider:        "ntpd",
				Managed:         true,
				Source:          "auto",
				FallbackServers: []string{"ntp.nict.jp"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"},
			Metadata: api.ObjectMeta{Name: "lan-time"},
			Spec: api.NTPServerSpec{
				Provider:          "ntpd",
				Managed:           true,
				Source:            "auto",
				FallbackServers:   []string{"ntp.jst.mfeed.ad.jp"},
				ListenAddresses:   []string{"192.168.160.4", "2409:10:3d60:1250::4/64"},
				ListenAddressFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan", Field: "address"}},
			},
		},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	ntp := string(got.NTP)
	for _, want := range []string{
		"interface ignore all\n",
		"interface listen 127.0.0.1\n",
		"interface listen ::1\n",
		"interface listen 192.168.160.4\n",
		"interface listen 2409:10:3d60:1250::4\n",
		"server ntp.jst.mfeed.ad.jp iburst\n",
	} {
		if !strings.Contains(ntp, want) {
			t.Fatalf("ntp.conf output missing %q:\n%s", want, ntp)
		}
	}
	if strings.Contains(ntp, "server ntp.nict.jp iburst") {
		t.Fatalf("NTPClient must not overwrite NTPServer config when both use ntpd:\n%s", ntp)
	}
}

func TestFreeBSDRendersTailscaleAndFirewallLoggerRCDScripts(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"},
			Metadata: api.ObjectMeta{Name: "home"},
			Spec: api.TailscaleNodeSpec{
				Hostname:          "router01",
				AdvertiseExitNode: true,
				AdvertiseRoutes:   []string{"192.168.0.0/16"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.FirewallLogSpec{Enabled: true, Path: "/var/db/routerd/firewall-logs.db"},
		},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	tailscale := string(got.RCDScripts["routerd_tailscale_home"])
	for _, want := range []string{
		`PROVIDE: routerd_tailscale_home`,
		`foreign tailscaled service is already running; refusing mutation`,
		`service tailscaled onestart`,
		`service tailscaled onestop`,
		`--timeout=30s`,
		`>/dev/null 2>&1`,
		`routerd ownership marker mismatch`,
		`unable to publish routerd tailscaled ownership marker`,
		`load_rc_config $name`,
		`/usr/local/bin/tailscale`,
		`up`,
		`--hostname=router01`,
		`--advertise-exit-node`,
		`--advertise-routes=192.168.0.0/16`,
	} {
		if !strings.Contains(tailscale, want) {
			t.Fatalf("tailscale rc.d script missing %q:\n%s", want, tailscale)
		}
	}
	if count := strings.Count(tailscale, "--timeout=30s"); count != 1 {
		t.Fatalf("tailscale timeout argument count = %d, want 1:\n%s", count, tailscale)
	}
	if !strings.Contains(tailscale, "service tailscaled onestart >/dev/null 2>&1 &") || !strings.Contains(tailscale, "pidfile=\"${TAILSCALED_PIDFILE:-/var/run/tailscaled.pid}\"") || !strings.Contains(tailscale, "tailscaled_running") || !strings.Contains(tailscale, "clear_stale_tailscaled_pidfile") || !strings.Contains(tailscale, "/usr/bin/timeout -k 2 15 service tailscaled onestop") {
		t.Fatalf("production rc.d script must asynchronously start and bounded-poll the canonical tailscaled pidfile:\n%s", tailscale)
	}
	if !strings.Contains(tailscale, "/usr/bin/timeout -k 2 45 '/usr/local/bin/tailscale' 'up'") {
		t.Fatalf("production rc.d must bound tailscale up as well as use the shared CLI timeout:\n%s", tailscale)
	}
	firewall := string(got.RCDScripts["routerd_firewall_logger"])
	for _, want := range []string{
		`PROVIDE: routerd_firewall_logger`,
		`/usr/local/sbin/routerd-firewall-logger`,
		`daemon`,
		`--path`,
		`/var/db/routerd/firewall-logs.db`,
		`--pflog-interface`,
		`pflog0`,
		`--dpi-socket`,
		`/var/run/routerd/dpi-classifier/default.sock`,
	} {
		if !strings.Contains(firewall, want) {
			t.Fatalf("firewall logger rc.d script missing %q:\n%s", want, firewall)
		}
	}
}

func TestFreeBSDRendersWireGuardRCDScript(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg0"},
			Spec: api.WireGuardInterfaceSpec{
				PrivateKeyFile: "/usr/local/etc/routerd/secrets/wg0.key",
				ListenPort:     51824,
				MTU:            1420,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
			Metadata: api.ObjectMeta{Name: "peer-a"},
			Spec: api.WireGuardPeerSpec{
				Interface:           "wg0",
				PublicKey:           "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
				AllowedIPs:          []string{"10.44.4.2/32"},
				Endpoint:            "192.0.2.2:51824",
				PersistentKeepalive: 25,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "wg0-ipv4"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "wg0", Address: "10.44.4.4/24"},
		},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	rc := string(got.RCConf)
	if strings.Contains(rc, "ifconfig_wg0=\"inet") {
		t.Fatalf("WireGuard address should be owned by rc.d script, not rc.conf:\n%s", rc)
	}
	script := string(got.RCDScripts["routerd_wireguard_wg0"])
	for _, want := range []string{
		`PROVIDE: routerd_wireguard_wg0`,
		`kldload if_wg`,
		`ifconfig 'wg0' create`,
		`wg set 'wg0' listen-port '51824' private-key '/usr/local/etc/routerd/secrets/wg0.key'`,
		`wg set 'wg0' peer 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' allowed-ips '10.44.4.2/32' endpoint '192.0.2.2:51824' persistent-keepalive '25'`,
		`ifconfig 'wg0' inet '10.44.4.4/24' alias`,
		`foreign WireGuard interface is already present; refusing mutation`,
		`unable to publish routerd WireGuard ownership marker`,
		`routerd_wireguard_wg0_rollback`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("wireguard rc.d script missing %q:\n%s", want, script)
		}
	}
}

func TestFreeBSDVXLANMultipleRemotesEmitsWarningAndUsesSeed(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "lab"}, Spec: api.VXLANSegmentSpec{
			IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10",
			Remotes:           []string{"192.0.2.20", "192.0.2.30", "192.0.2.40"},
			UnderlayInterface: "wan",
		}},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected at least one warning for multi-remote VXLAN on FreeBSD")
	}
	want := "FreeBSD vxlan(4) supports a single unicast remote"
	if !strings.Contains(got.Warnings[0], want) {
		t.Fatalf("warning %q does not mention single-remote limitation", got.Warnings[0])
	}
	script := string(got.RCDScripts["routerd_vxlan_lab"])
	if !strings.Contains(script, "vxlanremote' '192.0.2.20'") {
		t.Fatalf("FreeBSD VXLAN rc.d script must use the first remote as seed:\n%s", script)
	}
	if strings.Contains(script, "vxlanremote' '192.0.2.30'") || strings.Contains(script, "vxlanremote' '192.0.2.40'") {
		t.Fatalf("FreeBSD VXLAN rc.d script must not emit additional remotes:\n%s", script)
	}
}

func TestFreeBSDRendersNAT44ExcludedDestinations(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type:                    "masquerade",
			EgressInterface:         "wan",
			SourceRanges:            []string{"192.168.160.0/24"},
			ExcludeDestinationCIDRs: []string{"192.168.0.0/16", "172.16.0.0/12", "10.0.0.0/8"},
		}},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	for _, want := range []string{
		`no nat on vtnet0 from 192.168.160.0/24 to { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 }`,
		`nat on vtnet0 from 192.168.160.0/24 to any -> (vtnet0)`,
		`pass all keep state`,
	} {
		if !strings.Contains(string(got.PF), want) {
			t.Fatalf("pf output missing %q:\n%s", want, string(got.PF))
		}
	}
	if strings.Contains(string(got.PF), "block drop all") {
		t.Fatalf("NAT-only pf output must not enable default-deny filtering:\n%s", string(got.PF))
	}
}

func TestFreeBSDSkipsDynamicDSLiteGIFWithoutWarning(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{
			Interface:  "wan",
			TunnelName: "ds-routerd",
			AFTRFQDN:   "gw.example.net",
		}},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	if strings.Contains(string(got.RCConf), "ifconfig_gif") {
		t.Fatalf("dynamic DS-Lite must not render a static gif:\n%s", got.RCConf)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("dynamic DS-Lite is runtime-applied and should not warn: %#v", got.Warnings)
	}
}
