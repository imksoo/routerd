// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/render"
)

const firewallDecisionBase = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: firewall-test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: wan}
      spec: {ifname: test-wan, managed: false}
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: lan}
      spec: {ifname: test-lan, managed: false}
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata: {name: wan}
      spec: {role: untrust, interfaces: [Interface/wan]}
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata: {name: lan}
      spec: {role: trust, interfaces: [Interface/lan]}
`

func firewallDecisionConfig(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "router.yaml")
	if err := os.WriteFile(path, []byte(firewallDecisionBase+extra), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func firewallCLIOutput(t *testing.T, path string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	command := append([]string{"firewall", "test", "--config", path}, args...)
	if err := run(command, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func renderedFirewallChain(t *testing.T, path, chain string) string {
	t.Helper()
	router, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := render.NftablesFirewall(router, render.InternalFirewallHoles(router))
	if err != nil {
		t.Fatal(err)
	}
	_, after, exists := strings.Cut(string(data), "chain "+chain+" {\n")
	if !exists {
		t.Fatalf("missing rendered chain %s:\n%s", chain, data)
	}
	body, _, exists := strings.Cut(after, "  }\n")
	if !exists {
		t.Fatal("missing chain end")
	}
	return body
}

func TestFirewallCLIUsesRenderedInternalHoleAndRuleOrder(t *testing.T) {
	path := firewallDecisionConfig(t, `    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata: {name: wan-client}
      spec: {interface: Interface/wan}
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallRule
      metadata: {name: deny-dhcp}
      spec: {fromZone: wan, toZone: self, protocol: udp, port: 68, action: drop}
`)
	chain := renderedFirewallChain(t, path, "wan_to_self")
	accept, drop := strings.Index(chain, "udp dport 68 counter accept"), strings.Index(chain, "udp dport 68 counter drop")
	if accept < 0 || drop <= accept {
		t.Fatalf("DHCP service exception must precede the drop in actual rendered output:\n%s", chain)
	}
	out := firewallCLIOutput(t, path, "from=wan", "to=self", "proto=udp", "dport=68")
	if !strings.Contains(out, "action=accept source=internal/wan-client-dhcpv4-client") || !strings.Contains(out, "scope=static-nftables-new-flow") {
		t.Fatalf("CLI disagrees with rendered first verdict: %s", out)
	}
	out = firewallCLIOutput(t, path, "from=wan", "to=self", "proto=udp")
	if !strings.Contains(out, "action=indeterminate") || !strings.Contains(out, "destination port is required") {
		t.Fatalf("missing port treated as DHCP wildcard: %s", out)
	}
	path = firewallDecisionConfig(t, `    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallRule
      metadata: {name: z-allow}
      spec: {fromZone: wan, toZone: self, protocol: tcp, port: 22, action: accept}
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallRule
      metadata: {name: a-deny}
      spec: {fromZone: wan, toZone: self, protocol: tcp, port: 22, action: drop}
`)
	chain = renderedFirewallChain(t, path, "wan_to_self")
	if drop, accept = strings.Index(chain, "tcp dport 22 counter drop"), strings.Index(chain, "tcp dport 22 counter accept"); drop < 0 || accept <= drop {
		t.Fatalf("unexpected rendered rule order:\n%s", chain)
	}
	if out = firewallCLIOutput(t, path, "from=wan", "proto=tcp", "dport=22"); !strings.Contains(out, "action=drop source=user/a-deny") {
		t.Fatalf("CLI used declaration order instead of rendered order: %s", out)
	}
}

func TestFirewallCLIDoesNotInventMissingPacketSelectors(t *testing.T) {
	for _, test := range []struct {
		name, selector, rendered, reason string
	}{
		{"source address", "srcCIDRs: [192.0.2.0/24]", "ip saddr 192.0.2.0/24", "address"},
		{"destination address", "destinationCIDRs: [198.51.100.1/32]", "ip daddr 198.51.100.1/32", "address"},
		{"source port", "sourcePorts: [1234]", "tcp sport 1234", "source port"},
		{"rate state", "rateLimit: {packetsPerSecond: 10}", "limit rate", "state is unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := firewallDecisionConfig(t, `    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallRule
      metadata: {name: selective}
      spec: {fromZone: wan, toZone: self, protocol: tcp, port: 22, action: accept, `+test.selector+`}
`)
			if chain := renderedFirewallChain(t, path, "wan_to_self"); !strings.Contains(chain, test.rendered) {
				t.Fatalf("actual rule is missing selector %s:\n%s", test.rendered, chain)
			}
			if out := firewallCLIOutput(t, path, "from=wan", "proto=tcp", "dport=22"); !strings.Contains(out, "action=indeterminate source=user/selective") || !strings.Contains(out, test.reason) {
				t.Fatalf("CLI discarded unavailable selector: %s", out)
			}
			if out := firewallCLIOutput(t, path, "from=wan", "proto=tcp", "dport=80"); !strings.Contains(out, "action=drop source=implicit/matrix") {
				t.Fatalf("definite port mismatch must preserve useful implicit result: %s", out)
			}
		})
	}
	path := firewallDecisionConfig(t, `    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallRule
      metadata: {name: web}
      spec: {fromZone: wan, toZone: self, protocol: tcp, destinationPorts: [80, 443], action: accept}
`)
	if chain := renderedFirewallChain(t, path, "wan_to_self"); !strings.Contains(chain, "tcp dport { 80, 443 }") {
		t.Fatalf("missing actual port-set selector:\n%s", chain)
	}
	for _, test := range []struct{ args, want string }{
		{"from=wan proto=tcp dport=443", "action=accept source=user/web"},
		{"from=wan proto=tcp dport=22", "action=drop source=implicit/matrix"},
		{"from=wan proto=tcp", "action=indeterminate source=user/web"},
		{"from=wan dport=443", "action=indeterminate source=packet"},
		{"from=lan to=wan proto=tcp dport=443", "action=accept source=implicit/matrix"},
	} {
		if out := firewallCLIOutput(t, path, strings.Fields(test.args)...); !strings.Contains(out, test.want) {
			t.Fatalf("%s: %s, want %s", test.args, out, test.want)
		}
	}
}

func TestFirewallCLIRequiresCurrentConditionalOrDynamicConfiguration(t *testing.T) {
	for _, test := range []struct{ resource, reason string }{
		{`    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata: {name: gated}
      spec: {interface: Interface/wan, when: {state: {uplinkReady: {equals: "true"}}}}
`, "current resource when-state is unavailable"},
		{`    - apiVersion: plugin.routerd.net/v1alpha1
      kind: DynamicConfigSource
      metadata: {name: cloud}
      spec: {pluginRef: cloud-plugin}
`, "current effective dynamic configuration is unavailable"},
	} {
		path := firewallDecisionConfig(t, test.resource)
		out := firewallCLIOutput(t, path, "from=wan", "proto=udp", "dport=68")
		if !strings.Contains(out, "action=indeterminate") || !strings.Contains(out, test.reason) {
			t.Fatalf("unobserved effective configuration treated as current: %s", out)
		}
	}
}

func TestFirewallCLIPreservesInputLoopbackException(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		path := firewallDecisionConfig(t, "")
		text := strings.Replace(firewallDecisionBase, "ifname: test-wan", "ifname: lo", 1)
		if mixed {
			text = strings.Replace(text, "interfaces: [Interface/wan]", "interfaces: [Interface/wan, Interface/lan]", 1)
			text = strings.Replace(text, `    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata: {name: lan}
      spec: {role: trust, interfaces: [Interface/lan]}
`, "", 1)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		router, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := render.NftablesFirewall(router, render.InternalFirewallHoles(router))
		if err != nil {
			t.Fatal(err)
		}
		input := string(data)
		accept, dispatch := strings.Index(input, `iifname "lo" counter accept`), strings.Index(input, "iifname @if_wan jump wan_to_self")
		if accept < 0 || dispatch <= accept {
			t.Fatalf("actual loopback accept must precede zone dispatch:\n%s", data)
		}
		out := firewallCLIOutput(t, path, "from=wan", "proto=tcp", "dport=22")
		want := "action=accept source=input/loopback"
		if mixed {
			want = "action=indeterminate source=input/loopback"
		}
		if !strings.Contains(out, want) {
			t.Fatalf("loopback/mixed-interface verdict = %s, want %s", out, want)
		}
		if !mixed {
			text += `    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6Server
      metadata: {name: ipv6-only}
      spec: {interface: wan}
`
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			router, err = config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err = render.NftablesFirewall(router, render.InternalFirewallHoles(router))
			if err != nil {
				t.Fatal(err)
			}
			input = string(data)
			drop := strings.Index(input, `udp dport 67 counter drop comment "routerd DHCPv4 excluded`)
			if accept = strings.Index(input, `iifname "lo" counter accept`); drop < 0 || accept <= drop {
				t.Fatalf("actual DHCPv4 exclusion must precede loopback accept:\n%s", data)
			}
			if out = firewallCLIOutput(t, path, "from=wan", "proto=udp", "dport=67"); !strings.Contains(out, "action=indeterminate source=input/dhcpv4-exclusion") {
				t.Fatalf("loopback bypassed earlier DHCP exclusion: %s", out)
			}
		}
	}
}
