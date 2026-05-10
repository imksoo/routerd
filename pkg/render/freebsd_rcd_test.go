// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestFreeBSDRCDScript(t *testing.T) {
	data, err := FreeBSDRCDScript("routerd-dns-resolver.service", api.SystemdUnitSpec{
		ExecStartPre:             []string{"/usr/local/sbin/routerd", "apply", "--once"},
		ExecStart:                []string{"/usr/local/sbin/routerd-dns-resolver", "--config", "/usr/local/etc/routerd/dns-resolver.yaml"},
		RuntimeDirectory:         []string{"routerd/dns-resolver"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/dns-resolver"},
		LogsDirectory:            []string{"routerd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`# PROVIDE: routerd_dns_resolver`,
		`command="/usr/sbin/daemon"`,
		`procname="/usr/sbin/daemon"`,
		`command_args="-P ${pidfile} -r -f -- '/usr/local/sbin/routerd-dns-resolver' '--config' '/usr/local/etc/routerd/dns-resolver.yaml'"`,
		`routerd_dns_resolver_prestart() {`,
		`mkdir -p '/var/run/routerd/dns-resolver'`,
		`mkdir -p '/var/db/routerd/dns-resolver'`,
		`mkdir -p '/var/log/routerd'`,
		`'/usr/local/sbin/routerd' 'apply' '--once'`,
		`: ${routerd_dns_resolver_enable:="YES"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, got)
		}
	}
}

func TestFreeBSDRenderSynthesizesDHCPv6ClientRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "1"},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	script := string(cfg.RCDScripts["routerd_dhcpv6_client_wan_pd"])
	for _, want := range []string{
		`PROVIDE: routerd_dhcpv6_client_wan_pd`,
		`procname="/usr/sbin/daemon"`,
		`'/usr/local/sbin/routerd-dhcpv6-client'`,
		`'--interface' 'vtnet0'`,
		`'--socket' '/var/run/routerd/dhcpv6-client/wan-pd.sock'`,
		`'--lease-file' '/var/db/routerd/dhcpv6-client/wan-pd/lease.json'`,
		`'--event-file' '/var/db/routerd/dhcpv6-client/wan-pd/events.jsonl'`,
		`'--iaid' '1'`,
		`mkdir -p '/var/run/routerd/dhcpv6-client'`,
		`mkdir -p '/var/db/routerd/dhcpv6-client/wan-pd'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, script)
		}
	}
}

func TestFreeBSDRenderIncludesSystemdUnitAsRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"},
			Metadata: api.ObjectMeta{Name: "routerd-healthcheck@internet.service"},
			Spec: api.SystemdUnitSpec{
				ExecStart:        []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", "internet"},
				RuntimeDirectory: []string{"routerd/healthcheck"},
				StateDirectory:   []string{"routerd/healthcheck"},
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg.RCConf), `routerd_healthcheck_internet_enable="YES"`) {
		t.Fatalf("rc.conf missing enable flag:\n%s", cfg.RCConf)
	}
	if _, ok := cfg.RCDScripts["routerd_healthcheck_internet"]; !ok {
		t.Fatalf("rc.d scripts = %#v", cfg.RCDScripts)
	}
}

func TestFreeBSDRenderSynthesizesHealthCheckDaemonRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec: api.HealthCheckSpec{
				Daemon:          "routerd-healthcheck",
				Target:          "1.1.1.1",
				Protocol:        "icmp",
				SourceInterface: "wan",
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	script := string(cfg.RCDScripts["routerd_healthcheck_internet"])
	for _, want := range []string{
		`--source-interface' 'vtnet0'`,
		`--socket' '/var/run/routerd/healthcheck/internet.sock'`,
		`--state-file' '/var/db/routerd/healthcheck/internet/state.json'`,
		`mkdir -p '/var/run/routerd/healthcheck'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, script)
		}
	}
}

func TestFreeBSDHealthCheckDaemonResolvesDSLiteSourceInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite"},
			Spec:     api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "gif40", LocalAddress: "2001:db8::1", RemoteAddress: "2001:db8::2"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "ds-lite-source"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "ds-lite", Address: "192.0.0.2/29"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec: api.HealthCheckSpec{
				Daemon:            "routerd-healthcheck",
				Target:            "1.1.1.1",
				Protocol:          "tcp",
				Port:              443,
				SourceInterface:   "ds-lite",
				SourceAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-source", Field: "address"},
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	script := string(cfg.RCDScripts["routerd_healthcheck_internet"])
	if !strings.Contains(script, `--source-interface' 'gif40'`) {
		t.Fatalf("rc.d script did not resolve DSLite source interface:\n%s", script)
	}
	if !strings.Contains(script, `--source-address' '192.0.0.2'`) {
		t.Fatalf("rc.d script did not resolve source address:\n%s", script)
	}
}
