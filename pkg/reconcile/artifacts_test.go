package reconcile

import (
	"testing"

	"routerd/pkg/api"
)

func TestKnownResourceKindsDeclareArtifactIntents(t *testing.T) {
	aliases := map[string]string{
		"wan":       "ens18",
		"lan":       "ens19",
		"wan-pppoe": "ppp0",
		"transix-a": "ds-transix-a",
	}
	resources := []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"}, Metadata: api.ObjectMeta{Name: "syslog"}, Spec: api.LogSinkSpec{Type: "syslog"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "forwarding"}, Spec: api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: "1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"}, Metadata: api.ObjectMeta{Name: "time"}, Spec: api.NTPClientSpec{Provider: "systemd-timesyncd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"}, Metadata: api.ObjectMeta{Name: "wan-pppoe"}, Spec: api.PPPoEInterfaceSpec{Interface: "wan", IfName: "ppp0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.160.3/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPAddress"}, Metadata: api.ObjectMeta{Name: "wan-v4"}, Spec: api.IPv4DHCPAddressSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPServer"}, Metadata: api.ObjectMeta{Name: "dhcp4"}, Spec: api.IPv4DHCPServerSpec{Server: "dnsmasq"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPScope"}, Metadata: api.ObjectMeta{Name: "lan-scope"}, Spec: api.IPv4DHCPScopeSpec{Server: "dhcp4", Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPAddress"}, Metadata: api.ObjectMeta{Name: "wan-v6"}, Spec: api.IPv6DHCPAddressSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.IPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", AddressSuffix: "::3"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPServer"}, Metadata: api.ObjectMeta{Name: "dhcp6"}, Spec: api.IPv6DHCPServerSpec{Server: "dnsmasq"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPScope"}, Metadata: api.ObjectMeta{Name: "lan-v6-scope"}, Spec: api.IPv6DHCPScopeSpec{Server: "dhcp6"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "SelfAddressPolicy"}, Metadata: api.ObjectMeta{Name: "self"}, Spec: api.SelfAddressPolicySpec{AddressFamily: "ipv6"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSConditionalForwarder"}, Metadata: api.ObjectMeta{Name: "aftr"}, Spec: api.DNSConditionalForwarderSpec{Domain: "gw.transix.jp"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "transix-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-transix-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "wan-check"}, Spec: api.HealthCheckSpec{Type: "ping"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default-v4"}, Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{{Name: "pppoe", Interface: "wan-pppoe", Priority: 10, Mark: 0x111, Table: 111}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"}, Metadata: api.ObjectMeta{Name: "nat"}, Spec: api.IPv4SourceNATSpec{OutboundInterface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRoute"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.IPv4PolicyRouteSpec{OutboundInterface: "wan", Priority: 100, Mark: 0x120, Table: 120}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"}, Metadata: api.ObjectMeta{Name: "set"}, Spec: api.IPv4PolicyRouteSetSpec{Targets: []api.IPv4PolicyRouteTarget{{OutboundInterface: "transix-a", Priority: 10000, Mark: 0x100, Table: 100}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4ReversePathFilter"}, Metadata: api.ObjectMeta{Name: "rp"}, Spec: api.IPv4ReversePathFilterSpec{Target: "interface", Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"}, Metadata: api.ObjectMeta{Name: "mtu"}, Spec: api.PathMTUPolicySpec{FromInterface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "Zone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.ZoneSpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.FirewallPolicySpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ExposeService"}, Metadata: api.ObjectMeta{Name: "https"}, Spec: api.ExposeServiceSpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"}, Metadata: api.ObjectMeta{Name: "hostname"}, Spec: api.HostnameSpec{Hostname: "router.example"}},
	}

	for _, res := range resources {
		t.Run(res.Kind, func(t *testing.T) {
			intents := resourceArtifactIntents(res, aliases)
			if len(intents) == 0 {
				t.Fatalf("%s declared no artifact intents", res.Kind)
			}
			for _, intent := range intents {
				if intent.Artifact.Kind == "" || intent.Artifact.Name == "" || intent.Action == "" || intent.ApplyWith == "" {
					t.Fatalf("%s has incomplete intent: %+v", res.Kind, intent)
				}
			}
		})
	}
}

func TestParseIPv4RouteTableArtifactsIncludesDefaultDev(t *testing.T) {
	got := parseIPv4RouteTableArtifacts(`
default dev ppp0 table 111 metric 10
192.0.2.0/24 dev ppp0 table 111 proto kernel scope link
192.168.1.0/24 dev ens18 table 112 proto kernel scope link
default via 192.168.1.1 dev ens18 table 112 metric 600
`)
	if len(got) != 2 {
		t.Fatalf("route table artifacts = %+v, want two", got)
	}
	if got[0].Name != "table=111" || got[0].Attributes["ifname"] != "ppp0" {
		t.Fatalf("first artifact = %+v, want table=111 ifname=ppp0", got[0])
	}
	if got[1].Name != "table=112" || got[1].Attributes["ifname"] != "ens18" {
		t.Fatalf("second artifact = %+v, want table=112 ifname=ens18", got[1])
	}
}
