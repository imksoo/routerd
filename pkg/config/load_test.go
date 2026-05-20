// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadRejectsRemovedSystemdUnitResource(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: SystemdUnit
      metadata:
        name: routerd.service
      spec:
        execStart:
          - /usr/local/sbin/routerd
          - serve
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected SystemdUnit resource to be rejected")
	}
	if !strings.Contains(err.Error(), "SystemdUnit") || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v, want unsupported SystemdUnit", err)
	}
}

func TestLoadRejectsRemovedImplementationResources(t *testing.T) {
	for _, tc := range []struct {
		kind       string
		apiVersion string
		spec       string
	}{
		{kind: "KernelModule", apiVersion: "system.routerd.net/v1alpha1", spec: "modules: [nf_conntrack]\n"},
		{kind: "NetworkAdoption", apiVersion: "system.routerd.net/v1alpha1", spec: "interface: wan\n"},
		{kind: "NixOSHost", apiVersion: "system.routerd.net/v1alpha1", spec: "hostname: router\n"},
		{kind: "Link", apiVersion: "net.routerd.net/v1alpha1", spec: "ifname: eth0\n"},
		{kind: "StatePolicy", apiVersion: "net.routerd.net/v1alpha1", spec: "variable: wan.mode\nvalues:\n  - value: ready\n    when: {}\n"},
		{kind: "DHCPv4Lease", apiVersion: "net.routerd.net/v1alpha1", spec: "interface: wan\n"},
		{kind: "PPPoEInterface", apiVersion: "net.routerd.net/v1alpha1", spec: "interface: wan\nusername: user\npassword: secret\n"},
		{kind: "IPv4SourceNAT", apiVersion: "net.routerd.net/v1alpha1", spec: "outboundInterface: wan\nsourceCIDRs: [192.0.2.0/24]\ntranslation:\n  type: interfaceAddress\n"},
		{kind: "VirtualIPv4Address", apiVersion: "net.routerd.net/v1alpha1", spec: "interface: lan\naddress: 192.0.2.10/32\n"},
		{kind: "VirtualIPv6Address", apiVersion: "net.routerd.net/v1alpha1", spec: "interface: lan\naddress: 2001:db8::10/128\n"},
		{kind: "DHCPv4Scope", apiVersion: "net.routerd.net/v1alpha1", spec: "server: dhcpv4\ninterface: lan\nrangeStart: 192.0.2.10\nrangeEnd: 192.0.2.20\n"},
		{kind: "DHCPv6Scope", apiVersion: "net.routerd.net/v1alpha1", spec: "server: dhcpv6\ndelegatedAddress: lan-v6\n"},
		{kind: "FirewallLog", apiVersion: "firewall.routerd.net/v1alpha1", spec: "enabled: true\n"},
		{kind: "IPv4ReversePathFilter", apiVersion: "net.routerd.net/v1alpha1", spec: "target: all\nmode: disabled\n"},
		{kind: "PathMTUPolicy", apiVersion: "net.routerd.net/v1alpha1", spec: "fromInterface: lan\ntoInterfaces: [wan]\n"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: `+tc.apiVersion+`
      kind: `+tc.kind+`
      metadata:
        name: removed
      spec:
`+indentYAML(tc.spec, "        ")+`
`)
			_, err := Load(path)
			if err == nil {
				t.Fatalf("expected %s resource to be rejected", tc.kind)
			}
			if !strings.Contains(err.Error(), tc.kind) || !strings.Contains(err.Error(), "not supported") {
				t.Fatalf("error = %v, want unsupported %s", err, tc.kind)
			}
		})
	}
}

func TestLoadRejectsOldLogForwardingFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "traffic-flow-include-ndpi",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: TrafficFlowLog
      metadata:
        name: default
      spec:
        enabled: true
        path: /var/lib/routerd/traffic-flows.db
        includeNDPI: true
`,
			want: "includeApplicationLayer",
		},
		{
			name: "traffic-flow-retention",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: TrafficFlowLog
      metadata:
        name: default
      spec:
        enabled: true
        path: /var/lib/routerd/traffic-flows.db
        retention: 30d
`,
			want: "LogRetention",
		},
		{
			name: "log-retention-targets",
			body: `
    - apiVersion: system.routerd.net/v1alpha1
      kind: LogRetention
      metadata:
        name: default
      spec:
        targets:
          - file: /var/lib/routerd/routerd.db
            retention: 30d
`,
			want: "spec.retention",
		},
		{
			name: "log-sink-plugin",
			body: `
    - apiVersion: system.routerd.net/v1alpha1
      kind: LogSink
      metadata:
        name: plugin
      spec:
        type: plugin
        plugin:
          path: /usr/local/libexec/routerd/log-sink
`,
			want: "webhook",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
`+tc.body)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsExplicitSocketSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "healthcheck",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        socketSource: /run/routerd/healthcheck/internet.sock
`,
		},
		{
			name: "pppoe",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: PPPoESession
      metadata:
        name: wan
      spec:
        interface: wan
        username: example
        password: secret
        socketSource: /run/routerd/pppoe-client/wan.sock
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
`+tc.body)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected socketSource to be rejected")
			}
			if !strings.Contains(err.Error(), "socketSource") || !strings.Contains(err.Error(), "derives") {
				t.Fatalf("error = %v, want unsupported socketSource", err)
			}
		})
	}
}

func TestLoadRejectsLowLevelMechanicsFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "healthcheck-source",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        target: 1.1.1.1
        sourceInterface: wan
`,
			want: "sourceInterface",
		},
		{
			name: "bgp-timer-field",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata:
        name: lan
      spec:
        asn: 64512
        routerID: 192.0.2.1
        timers:
          keepalive: 3s
`,
			want: "timers.keepalive",
		},
		{
			name: "vrrp-timing",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata:
        name: vip
      spec:
        family: ipv4
        interface: lan
        address: 192.0.2.10/32
        mode: vrrp
        vrrp:
          virtualRouterID: 10
          peers: [192.0.2.11]
          advertInterval: 1s
`,
			want: "vrrp.advertInterval",
		},
		{
			name: "wireguard-table",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg0
      spec:
        table: 200
`,
			want: "table",
		},
		{
			name: "tailscale-binary-path",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: TailscaleNode
      metadata:
        name: ts
      spec:
        binaryPath: /usr/local/bin/tailscale
`,
			want: "binaryPath",
		},
		{
			name: "dhcpv6-pd-iaid",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        iaid: "1"
`,
			want: "iaid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
`+tc.body)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "not supported") {
				t.Fatalf("error = %v, want unsupported %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsDNSResolverInlineSources(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan
      spec:
        listen:
          - addresses: [127.0.0.1]
            port: 53
        sources:
          - name: default
            kind: upstream
            match: ["."]
            upstreams: [udp://1.1.1.1:53]
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected DNSResolver spec.sources to be rejected")
	}
	if !strings.Contains(err.Error(), "spec.sources") || !strings.Contains(err.Error(), "DNSForwarder") || !strings.Contains(err.Error(), "DNSUpstream") {
		t.Fatalf("error = %v, want DNSForwarder/DNSUpstream migration guide", err)
	}
}

func TestLoadRejectsBGPPeerInlineBFD(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPPeer
      metadata:
        name: fabric
      spec:
        routerRef: BGPRouter/lan
        peerASN: 64513
        peers: [192.0.2.2]
        bfd:
          enabled: true
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected BGPPeer inline BFD to be rejected")
	}
	if !strings.Contains(err.Error(), "spec.bfd inline BFD settings") || !strings.Contains(err.Error(), "BFD/<name>") {
		t.Fatalf("error = %v, want BFD migration guide", err)
	}
}

func TestLoadRejectsUnknownResourceKind(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: TypoedKind
      metadata:
        name: bad
      spec: {}
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported resource kind TypoedKind") {
		t.Fatalf("error = %v, want unsupported kind", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/router.yaml"
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func indentYAML(value, prefix string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
