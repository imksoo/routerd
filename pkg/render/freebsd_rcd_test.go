// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
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
		`# Managed by routerd.`,
		`# PROVIDE: routerd_dns_resolver`,
		`daemon_command="/usr/sbin/daemon"`,
		`daemon_pidfile="/var/run/${name}/${name}.daemon.pid"`,
		`child_pidfile="/var/run/${name}/${name}.pid"`,
		`daemon_args="-P ${daemon_pidfile} -p ${child_pidfile} -r -f -- '/usr/local/sbin/routerd-dns-resolver' '--config' '/usr/local/etc/routerd/dns-resolver.yaml'"`,
		`routerd_dns_resolver_start() {`,
		`eval "${daemon_command} ${daemon_args}"`,
		`routerd_dns_resolver_pgrep_child() {`,
		`ps -axo pid,command | awk -v exe='/usr/local/sbin/routerd-dns-resolver' -v pat='/usr/local/sbin/routerd-dns-resolver .*--config .*/usr/local/etc/routerd/dns-resolver\.yaml' '$0 ~ exe && $0 ~ pat { print $1; exit }'`,
		`routerd_dns_resolver_parent_daemon_pid() {`,
		`routerd_dns_resolver_managed_child_pid() {`,
		`ps -o ppid= -p "${_child_pid}"`,
		`daemon:*|*/daemon*)`,
		`routerd_dns_resolver_read_pidfile "${daemon_pidfile}" || routerd_dns_resolver_parent_daemon_pid`,
		`routerd_dns_resolver_read_pidfile "${child_pidfile}" || routerd_dns_resolver_pgrep_child`,
		`routerd_dns_resolver_stop() {`,
		`kill -KILL "${_child_pid}"`,
		`rm -f "${daemon_pidfile}" "${child_pidfile}"`,
		`routerd_dns_resolver_prestart() {`,
		`mkdir -p "/var/run/${name}"`,
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

func TestFreeBSDRenderRoutesDHCPv6ClientThroughRouterd(t *testing.T) {
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
	if _, ok := cfg.RCDScripts["routerd_dhcpv6_client_wan_pd"]; ok {
		t.Fatalf("DHCPv6 client rc.d script must not be synthesized when routerd supervises clients")
	}
	script := string(cfg.RCDScripts["routerd"])
	for _, want := range []string{
		`PROVIDE: routerd`,
		`daemon_command="/usr/sbin/daemon"`,
		`'/usr/local/sbin/routerd' 'serve'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, `'/usr/local/sbin/routerd' 'check'`) {
		t.Fatalf("routerd rc.d script must not call removed routerd check verb:\n%s", script)
	}
	if strings.Contains(script, "controller"+"-chain") {
		t.Fatalf("routerd rc.d script must not expose legacy controller flags:\n%s", script)
	}
}

func TestFreeBSDRCDPgrepPatternIncludesResourceName(t *testing.T) {
	got := freeBSDRCDPgrepPattern([]string{
		"/usr/local/sbin/routerd-healthcheck",
		"daemon",
		"--resource",
		"internet-via-dslite-c",
		"--target",
		"9.9.9.9",
	})
	if !strings.Contains(got, regexp.QuoteMeta("internet-via-dslite-c")) {
		t.Fatalf("pgrep pattern should include resource name, got %q", got)
	}
}

func TestFreeBSDRendersBGPRCDScript(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "edge"},
			Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "192.0.2.1"},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	script, ok := cfg.RCDScripts["routerd_bgp"]
	if !ok {
		t.Fatalf("BGPRouter did not render routerd_bgp: %#v", cfg.RCDScripts)
	}
	for _, want := range []string{
		`'/usr/local/sbin/routerd-bgp' 'daemon'`,
		`'--socket' '/var/run/routerd/bgp/gobgp.sock'`,
		`'--control-socket' '/var/run/routerd/bgp/control.sock'`,
		`'--state-file' '/var/db/routerd/bgp/applied.json'`,
	} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("routerd_bgp missing %q:\n%s", want, script)
		}
	}
}

func TestFreeBSDRenderRoutesDHCPv4ClientThroughRouterd(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"},
			Metadata: api.ObjectMeta{Name: "wan-v4"},
			Spec:     api.DHCPv4ClientSpec{Interface: "wan", Hostname: "router04"},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.RCDScripts["routerd_dhcpv4_client_wan_v4"]; ok {
		t.Fatalf("DHCPv4 client rc.d script must not be synthesized when routerd supervises clients")
	}
	script := string(cfg.RCDScripts["routerd"])
	for _, want := range []string{
		`PROVIDE: routerd`,
		`daemon_command="/usr/sbin/daemon"`,
		`'/usr/local/sbin/routerd' 'serve'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, `'/usr/local/sbin/routerd' 'check'`) {
		t.Fatalf("routerd rc.d script must not call removed routerd check verb:\n%s", script)
	}
	if strings.Contains(script, "controller"+"-chain") {
		t.Fatalf("routerd rc.d script must not expose legacy controller flags:\n%s", script)
	}
}

func TestFreeBSDRenderSkipsDHCPClientRCDWhenRouterdSupervisesClients(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"},
			Metadata: api.ObjectMeta{Name: "wan-v4"},
			Spec:     api.DHCPv4ClientSpec{Interface: "wan", Hostname: "router04"},
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
	if _, ok := cfg.RCDScripts["routerd"]; !ok {
		t.Fatalf("rc.d scripts missing routerd: %#v", cfg.RCDScripts)
	}
	if _, ok := cfg.RCDScripts["routerd_dhcpv4_client_wan_v4"]; ok {
		t.Fatalf("DHCPv4 rc.d script should not be synthesized when routerd supervises clients")
	}
	if _, ok := cfg.RCDScripts["routerd_dhcpv6_client_wan_pd"]; ok {
		t.Fatalf("DHCPv6 rc.d script should not be synthesized when routerd supervises clients")
	}
	routerdScript := string(cfg.RCDScripts["routerd"])
	if strings.Contains(routerdScript, "controller"+"-chain") {
		t.Fatalf("routerd rc.d script must not expose legacy controller flags:\n%s", routerdScript)
	}
	if !strings.Contains(routerdScript, "'serve'") || strings.Contains(routerdScript, "--skip-service-manager") {
		t.Fatalf("routerd rc.d script must run serve without skip-service-manager:\n%s", routerdScript)
	}
	if strings.Contains(routerdScript, `'/usr/local/sbin/routerd' 'check'`) {
		t.Fatalf("routerd rc.d script must not call removed routerd check verb:\n%s", routerdScript)
	}
	if strings.Contains(routerdScript, `$("`) {
		t.Fatalf("routerd rc.d script contains quoted command substitution:\n%s", routerdScript)
	}
}

func TestFreeBSDRenderSynthesizesHealthCheckResourceAsRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
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

func TestFreeBSDEventdRCDScript(t *testing.T) {
	data, err := FreeBSDRCDScript("routerd-eventd@cloudedge.service", freeBSDEventdSystemdSpec("cloudedge"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`# PROVIDE: routerd_eventd_cloudedge`,
		`# REQUIRE: NETWORKING`,
		`daemon_command="/usr/sbin/daemon"`,
		`daemon_args="-P ${daemon_pidfile} -p ${child_pidfile} -r -f -- '/usr/local/sbin/routerd-eventd' 'daemon' '--config-file' '/var/db/routerd/eventd/cloudedge/config.json'"`,
		`mkdir -p "/var/run/${name}"`,
		`mkdir -p '/var/run/routerd/eventd'`,
		`mkdir -p '/var/db/routerd/eventd'`,
		`mkdir -p '/var/db/routerd/eventd/cloudedge'`,
		`mkdir -p '/var/log/routerd'`,
		`: ${routerd_eventd_cloudedge_enable:="YES"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("eventd rc.d script missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "apply") || strings.Contains(got, "secretFile") || strings.Contains(got, "/usr/local/etc/routerd/secrets/eventd.key") {
		t.Fatalf("eventd rc.d script must not run routerd apply or expose HMAC secret args:\n%s", got)
	}
}

func TestFreeBSDRenderSynthesizesEventFederationRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.EventGroupSpec{
				NodeName: "freebsd-router",
				Auth:     api.EventGroupAuth{Mode: "hmac", SecretFile: "/usr/local/etc/routerd/secrets/eventd.key"},
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg.RCConf), `routerd_eventd_cloudedge_enable="YES"`) {
		t.Fatalf("rc.conf missing eventd enable flag:\n%s", cfg.RCConf)
	}
	script := string(cfg.RCDScripts["routerd_eventd_cloudedge"])
	for _, want := range []string{
		`PROVIDE: routerd_eventd_cloudedge`,
		`'/usr/local/sbin/routerd-eventd' 'daemon' '--config-file' '/var/db/routerd/eventd/cloudedge/config.json'`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("eventd rc.d script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "/usr/local/etc/routerd/secrets/eventd.key") {
		t.Fatalf("eventd rc.d script must keep HMAC path in runtime config, not command args:\n%s", script)
	}
}

func TestFreeBSDRenderSkipsIdentityOnlyEventFederationRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "identity"},
			Spec: api.EventGroupSpec{
				NodeName: "freebsd-router",
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg.RCConf), "routerd_eventd_identity") {
		t.Fatalf("rc.conf should not enable identity-only eventd:\n%s", cfg.RCConf)
	}
	if _, ok := cfg.RCDScripts["routerd_eventd_identity"]; ok {
		t.Fatalf("identity-only eventd rc.d script was rendered: %#v", cfg.RCDScripts)
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
