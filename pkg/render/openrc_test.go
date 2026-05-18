// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestOpenRCScriptFromSystemdUnitSpec(t *testing.T) {
	data, err := OpenRCScript("routerd-healthcheck@internet.service", api.SystemdUnitSpec{
		Description:      "routerd healthcheck internet",
		ExecStartPre:     []string{"/usr/local/sbin/routerd", "apply", "--once", "--dry-run"},
		ExecStart:        []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", "internet"},
		RuntimeDirectory: []string{"routerd/healthcheck"},
		StateDirectory:   []string{"routerd/healthcheck"},
		LogsDirectory:    []string{"routerd"},
		Environment:      []string{"OTEL_SERVICE_NAME=routerd-healthcheck"},
		After:            []string{"network-online.target", "routerd.service"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`#!/sbin/openrc-run`,
		`name='routerd_healthcheck_internet'`,
		`command='/usr/local/sbin/routerd-healthcheck'`,
		`command_args="'daemon' '--resource' 'internet'"`,
		`command_background="yes"`,
		`pidfile="/run/routerd/openrc/${RC_SVCNAME}.pid"`,
		`use net`,
		`after routerd`,
		`checkpath -d -m 0755 '/run/routerd/healthcheck'`,
		`checkpath -d -m 0755 '/var/lib/routerd/healthcheck'`,
		`checkpath -d -m 0755 '/var/log/routerd'`,
		`export OTEL_SERVICE_NAME='routerd-healthcheck'`,
		`'/usr/local/sbin/routerd' 'apply' '--once' '--dry-run'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("OpenRC script missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\tneed net\n") {
		t.Fatalf("OpenRC script must not force-start Alpine networking:\n%s", got)
	}
}

func TestOpenRCRenderSynthesizesHealthCheckAndDnsmasq(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "eth1", Managed: true, AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ip"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.1/24"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
			Metadata: api.ObjectMeta{Name: "lan-dhcp"},
			Spec: api.DHCPv4ServerSpec{
				Interface: "lan",
				AddressPool: api.DHCPAddressPoolSpec{
					Start: "192.168.10.100",
					End:   "192.168.10.150",
				},
				Gateway:    "192.168.10.1",
				DNSServers: []string{"192.168.10.1"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec: api.HealthCheckSpec{
				Daemon:   "routerd-healthcheck",
				Target:   "1.1.1.1",
				Protocol: "icmp",
			},
		},
	}}}
	got, err := OpenRC(router)
	if err != nil {
		t.Fatal(err)
	}
	healthcheck := string(got.InitScripts["routerd_healthcheck_internet"])
	if !strings.Contains(healthcheck, `command='/usr/local/sbin/routerd-healthcheck'`) ||
		!strings.Contains(healthcheck, `'--target' '1.1.1.1'`) {
		t.Fatalf("healthcheck OpenRC script missing expected command:\n%s", healthcheck)
	}
	dnsmasq := string(got.InitScripts["routerd_dnsmasq"])
	if !strings.Contains(dnsmasq, `command='/usr/sbin/dnsmasq'`) ||
		!strings.Contains(dnsmasq, `'--conf-file=/usr/local/etc/routerd/dnsmasq.conf'`) {
		t.Fatalf("dnsmasq OpenRC script missing expected command:\n%s", dnsmasq)
	}
}

func TestOpenRCRenderSynthesizesHelperDaemons(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "eth0", Managed: true, AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease"},
			Metadata: api.ObjectMeta{Name: "wan-v4"},
			Spec:     api.DHCPv4LeaseSpec{Interface: "wan", Hostname: "router04"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
			Metadata: api.ObjectMeta{Name: "wan-pppoe"},
			Spec:     api.PPPoESessionSpec{Interface: "wan", Username: "user", Password: "pass"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.DNSResolverSpec{
				Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 5053}},
				Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://1.1.1.1:53"}}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallLog"},
			Metadata: api.ObjectMeta{Name: "log"},
			Spec:     api.FirewallLogSpec{Enabled: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"},
			Metadata: api.ObjectMeta{Name: "edge"},
			Spec:     api.TailscaleNodeSpec{AdvertiseExitNode: true},
		},
	}}}
	got, err := OpenRCWithOptions(router, OpenRCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"routerd_dhcpv4_client_wan_v4":   "'/usr/local/sbin/routerd-dhcpv4-client'",
		"routerd_dhcpv6_client_wan_pd":   "'/usr/local/sbin/routerd-dhcpv6-client'",
		"routerd_pppoe_client_wan_pppoe": "'/usr/local/sbin/routerd-pppoe-client'",
		"routerd_dns_resolver_lan":       "'/usr/local/sbin/routerd-dns-resolver'",
		"routerd_firewall_logger":        "'/usr/local/sbin/routerd-firewall-logger'",
		"routerd_tailscale_edge":         "'/usr/bin/tailscale'",
	} {
		script := string(got.InitScripts[name])
		if !strings.Contains(script, want) {
			t.Fatalf("%s script missing %q:\n%s", name, want, script)
		}
	}
	services := map[string]OpenRCService{}
	for _, service := range got.Services {
		services[service.Name] = service
	}
	if services["routerd_dns_resolver_lan"].Enabled || services["routerd_dns_resolver_lan"].Started {
		t.Fatalf("DNS resolver OpenRC service should render without activation until runtime config exists")
	}
}

func TestOpenRCRenderSynthesizesNDPIAgentForAutoClassifier(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"},
			Metadata: api.ObjectMeta{Name: "routerd-dpi-classifier.service"},
			Spec: api.SystemdUnitSpec{
				ExecStart: []string{"/usr/local/sbin/routerd-dpi-classifier", "daemon", "--engine", "auto"},
			},
		},
	}}}
	got, err := OpenRCWithOptions(router, OpenRCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	agent := string(got.InitScripts["routerd_ndpi_agent"])
	if !strings.Contains(agent, "'/usr/local/sbin/routerd-ndpi-agent'") || !strings.Contains(agent, "'--socket' '/run/routerd/ndpi-agent/default.sock'") {
		t.Fatalf("ndpi agent script =\n%s", agent)
	}
	classifier := string(got.InitScripts["routerd_dpi_classifier"])
	if !strings.Contains(classifier, "after routerd_ndpi_agent") {
		t.Fatalf("classifier script missing ndpi dependency:\n%s", classifier)
	}
}
