package apply

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
		"wg-lab":    "wg-lab",
		"vrf-guest": "vrf-guest",
		"vx240":     "vx240",
	}
	resources := []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"}, Metadata: api.ObjectMeta{Name: "syslog"}, Spec: api.LogSinkSpec{Type: "syslog"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "forwarding"}, Spec: api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: "1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"}, Metadata: api.ObjectMeta{Name: "time"}, Spec: api.NTPClientSpec{Provider: "systemd-timesyncd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"}, Metadata: api.ObjectMeta{Name: "wan-pppoe"}, Spec: api.PPPoEInterfaceSpec{Interface: "wan", IfName: "ppp0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "wan-session"}, Spec: api.PPPoESessionSpec{Interface: "wan", Username: "user", Password: "secret"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-lab"}, Spec: api.WireGuardInterfaceSpec{ListenPort: 51820}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"}, Metadata: api.ObjectMeta{Name: "peer-a"}, Spec: api.WireGuardPeerSpec{Interface: "wg-lab", PublicKey: "pub", AllowedIPs: []string{"10.44.0.2/32"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"}, Metadata: api.ObjectMeta{Name: "aws-a"}, Spec: api.IPsecConnectionSpec{LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.10", PreSharedKey: "secret", LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "vrf-guest"}, Spec: api.VRFSpec{RouteTable: 1001}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx240"}, Spec: api.VXLANTunnelSpec{VNI: 240, LocalAddress: "10.44.0.1", UnderlayInterface: "wg-lab", Peers: []string{"10.44.0.2"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Address"}, Metadata: api.ObjectMeta{Name: "wan-v4"}, Spec: api.DHCPv4AddressSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease"}, Metadata: api.ObjectMeta{Name: "wan-lease"}, Spec: api.DHCPv4LeaseSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "dhcpv4"}, Spec: api.DHCPv4ServerSpec{Server: "dnsmasq"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"}, Metadata: api.ObjectMeta{Name: "lan-scope"}, Spec: api.DHCPv4ScopeSpec{Server: "dhcpv4", Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"}, Metadata: api.ObjectMeta{Name: "wan-v6"}, Spec: api.DHCPv6AddressSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer-v2"}, Spec: api.DHCPv4ReservationSpec{Server: "dhcpv4", MACAddress: "aa:bb:cc:dd:ee:01", IPAddress: "192.168.10.51"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", AddressSuffix: "::3"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "dhcpv6"}, Spec: api.DHCPv6ServerSpec{Server: "dnsmasq"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "dhcp6-v2"}, Spec: api.DHCPv6ServerSpec{Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Scope"}, Metadata: api.ObjectMeta{Name: "lan-v6-scope"}, Spec: api.DHCPv6ScopeSpec{Server: "dhcpv6"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"lan"}, Upstream: "192.0.2.53"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "SelfAddressPolicy"}, Metadata: api.ObjectMeta{Name: "self"}, Spec: api.SelfAddressPolicySpec{AddressFamily: "ipv6"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.DNSZoneSpec{Zone: "lab.example"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}}}, Sources: []api.DNSResolverSourceSpec{{Kind: "zone", Match: []string{"lab.example"}, ZoneRef: []string{"DNSZone/lan-zone"}}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "transix-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-transix-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "wan-check"}, Spec: api.HealthCheckSpec{Type: "ping"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default-v4"}, Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{{Name: "pppoe", Interface: "wan-pppoe", Priority: 10, Mark: 0x111, Table: 111}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"}, Metadata: api.ObjectMeta{Name: "nat"}, Spec: api.IPv4SourceNATSpec{OutboundInterface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "nat44"}, Spec: api.NAT44RuleSpec{Type: "masquerade", EgressInterface: "wan", SourceRanges: []string{"192.168.0.0/16"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRoute"}, Metadata: api.ObjectMeta{Name: "policy"}, Spec: api.IPv4PolicyRouteSpec{OutboundInterface: "wan", Priority: 100, Mark: 0x120, Table: 120}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"}, Metadata: api.ObjectMeta{Name: "set"}, Spec: api.IPv4PolicyRouteSetSpec{Targets: []api.IPv4PolicyRouteTarget{{OutboundInterface: "transix-a", Priority: 10000, Mark: 0x100, Table: 100}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4ReversePathFilter"}, Metadata: api.ObjectMeta{Name: "rp"}, Spec: api.IPv4ReversePathFilterSpec{Target: "interface", Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"}, Metadata: api.ObjectMeta{Name: "mtu"}, Spec: api.PathMTUPolicySpec{FromInterface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.FirewallPolicySpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "https"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Protocol: "tcp", Port: 443, Action: "accept"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br-vxlan-test"}, Spec: api.BridgeSpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "lab"}, Spec: api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10", UnderlayInterface: "wan", Bridge: "br-vxlan-test"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"}, Metadata: api.ObjectMeta{Name: "lab-v4"}, Spec: api.IPv4StaticRouteSpec{Interface: "lan", Destination: "10.0.0.0/24", Via: "192.168.10.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "lab-v6"}, Spec: api.IPv6StaticRouteSpec{Interface: "lan", Destination: "2001:db8::/64", Via: "fe80::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{Scope: "lan-scope", MACAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.10.50"}},
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

func TestVXLANSegmentClaimsL2FilterTable(t *testing.T) {
	res := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"},
		Metadata: api.ObjectMeta{Name: "lab"},
		Spec:     api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10", UnderlayInterface: "wan"},
	}
	intents := resourceArtifactIntents(res, nil)
	var hasL2Filter bool
	for _, intent := range intents {
		if intent.Artifact.Kind == "nft.table" && intent.Artifact.Name == "routerd_l2_filter" {
			hasL2Filter = true
		}
	}
	if !hasL2Filter {
		t.Fatalf("VXLANSegment with default l2Filter must claim routerd_l2_filter, intents=%+v", intents)
	}

	res.Spec = api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10", UnderlayInterface: "wan", L2Filter: "none"}
	intents = resourceArtifactIntents(res, nil)
	for _, intent := range intents {
		if intent.Artifact.Kind == "nft.table" && intent.Artifact.Name == "routerd_l2_filter" {
			t.Fatalf("VXLANSegment with l2Filter=none must NOT claim routerd_l2_filter, intents=%+v", intents)
		}
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

func TestParseIfconfigAddressArtifacts(t *testing.T) {
	got := parseIfconfigAddressArtifacts(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet 192.168.10.1 netmask 0xffffff00 broadcast 192.168.10.255
	inet6 fe80::be24:11ff:fe5d:e063%vtnet1 prefixlen 64 scopeid 0x2
`)
	if len(got) != 2 {
		t.Fatalf("ifconfig artifacts = %+v, want two", got)
	}
	if got[0].Kind != "net.ipv4.address" || got[0].Name != "vtnet1:192.168.10.1/24" {
		t.Fatalf("first artifact = %+v", got[0])
	}
	if got[1].Kind != "net.ipv6.address" || got[1].Name != "vtnet1:fe80::be24:11ff:fe5d:e063/64" {
		t.Fatalf("second artifact = %+v", got[1])
	}
}

func TestAppliedOwnedArtifactsRequireObservedInventory(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"}, Metadata: api.ObjectMeta{Name: "host"}, Spec: api.HostnameSpec{Hostname: "router.example", Managed: true}},
		}},
	}
	engine := &Engine{
		Command: fakeCommand(map[string]string{
			"hostname": "router.example\n",
		}),
	}
	got, err := engine.AppliedOwnedArtifacts(router)
	if err != nil {
		t.Fatalf("applied artifacts: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "host.hostname" {
		t.Fatalf("applied artifacts = %+v, want only observed hostname", got)
	}
}
