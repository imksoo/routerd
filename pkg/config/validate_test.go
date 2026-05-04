package config

import (
	"testing"

	"routerd/pkg/api"
)

func TestValidateRouterLabExample(t *testing.T) {
	router, err := Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load router-lab example: %v", err)
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate router-lab example: %v", err)
	}
}

func TestValidateSysctl(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"},
				Metadata: api.ObjectMeta{Name: "ipv4-forwarding"},
				Spec:     api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: "1", Runtime: boolPtr(true)},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate sysctl: %v", err)
	}
}

func TestValidateLogSinkSyslog(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"},
				Metadata: api.ObjectMeta{Name: "local-syslog"},
				Spec: api.LogSinkSpec{
					Type:     "syslog",
					MinLevel: "info",
					Syslog:   api.LogSinkSyslogSpec{Facility: "local6", Tag: "routerd"},
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate log sink: %v", err)
	}
}

func TestValidateLogSinkPluginRequiresPath(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"},
				Metadata: api.ObjectMeta{Name: "remote-log"},
				Spec:     api.LogSinkSpec{Type: "plugin"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected plugin log sink without path to be rejected")
	}
}

func TestValidateHealthCheckRole(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
				Metadata: api.ObjectMeta{Name: "wan-next-hop"},
				Spec: api.HealthCheckSpec{
					Type:         "ping",
					Role:         "next-hop",
					TargetSource: "defaultGateway",
					Interface:    "wan",
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate health check role: %v", err)
	}
}

func TestValidateHealthCheckRejectsUnknownRole(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
				Metadata: api.ObjectMeta{Name: "wan-unknown"},
				Spec: api.HealthCheckSpec{
					Type:         "ping",
					Role:         "mystery",
					TargetSource: "defaultGateway",
					Interface:    "wan",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected unknown health check role to be rejected")
	}
}

func TestValidatePPPoEInterface(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan-ether"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "wan-ppp"},
				Spec: api.PPPoEInterfaceSpec{
					Interface: "wan-ether",
					IfName:    "ppp0",
					Username:  "user@example.jp",
					Password:  "secret",
					Managed:   true,
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate PPPoE interface: %v", err)
	}
}

func TestValidatePPPoEInterfaceRequiresOnePasswordSource(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan-ether"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "wan-ppp"},
				Spec: api.PPPoEInterfaceSpec{
					Interface:    "wan-ether",
					IfName:       "ppp0",
					Username:     "user@example.jp",
					Password:     "secret",
					PasswordFile: "/usr/local/etc/routerd/pppoe.pass",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected PPPoE interface with password and passwordFile to be rejected")
	}
}

func TestValidatePPPoESession(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan-ether"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
				Metadata: api.ObjectMeta{Name: "softether"},
				Spec: api.PPPoESessionSpec{
					Interface:       "wan-ether",
					AuthMethod:      "chap",
					Username:        "open@open.ad.jp",
					Password:        "open",
					MTU:             1454,
					MRU:             1454,
					LCPEchoInterval: 30,
					LCPEchoFailure:  4,
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate PPPoE session: %v", err)
	}
}

func TestValidateTierSResources(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br240"}, Spec: api.BridgeSpec{IfName: "br240", Members: []string{"vx240"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-lab"}, Spec: api.WireGuardInterfaceSpec{ListenPort: 51820, MTU: 1420}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"}, Metadata: api.ObjectMeta{Name: "peer-a"}, Spec: api.WireGuardPeerSpec{Interface: "wg-lab", PublicKey: "pub", AllowedIPs: []string{"10.44.0.2/32"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"}, Metadata: api.ObjectMeta{Name: "aws-a"}, Spec: api.IPsecConnectionSpec{LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.10", PreSharedKey: "secret", LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16", CloudProviderHint: "aws"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "vrf-guest"}, Spec: api.VRFSpec{RouteTable: 1001, Members: []string{"wg-lab"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx240"}, Spec: api.VXLANTunnelSpec{VNI: 240, LocalAddress: "10.44.0.1", UnderlayInterface: "wg-lab", Peers: []string{"10.44.0.2"}, Bridge: "br240"}},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate Tier S resources: %v", err)
	}
}

func TestValidatePhase15LANServiceKinds(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
				Interface:   "lan",
				AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
				Gateway:     "192.168.10.1",
				DNSServers:  []string{"192.168.10.1"},
				NTPServers:  []string{"192.168.10.1"},
				Options:     []api.DHCPv4OptionSpec{{Name: "domain-search", Value: "lan"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{Server: "lan-v4", MACAddress: "02:00:00:00:01:50", Hostname: "printer", IPAddress: "192.168.10.150"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{
				Interface:    "lan",
				Mode:         "both",
				AddressPool:  api.DHCPAddressPoolSpec{Start: "::100", End: "::1ff", LeaseTime: "6h"},
				DNSServers:   []string{"2001:db8::53"},
				SNTPServers:  []string{"2001:db8::123"},
				DomainSearch: []string{"lan"},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "prefix"}, RDNSS: []string{"2001:db8::53"}, DNSSL: []string{"lan"}, MTU: 1500, PRFPreference: "high"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSZoneSpec{
				Zone: "lan",
				Records: []api.DNSZoneRecordSpec{
					{Hostname: "router.lan", IPv4: "192.168.10.1", IPv6: "2001:db8::1"},
					{Hostname: "router6.lan", IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "address"}},
				},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "resolver"}, Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{
					{Name: "local", Kind: "zone", Match: []string{"lan"}, ZoneRef: []string{"local"}},
					{Name: "default", Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://192.0.2.53:53"}},
				},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"lan"}, Upstream: "192.0.2.53"}},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate Phase 1.5 LAN service kinds: %v", err)
	}
}

func TestValidateIPv4DefaultRoutePolicyStaticRequiresGateway(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{
					{Interface: "wan", GatewaySource: "static", Priority: 10, Table: 100, Mark: 256},
				}},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected static default route without gateway to be rejected")
	}
}

func TestValidateIPv4DefaultRoutePolicyRouteSetCandidate(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"},
				Metadata: api.ObjectMeta{Name: "wan-balance"},
				Spec: api.IPv4PolicyRouteSetSpec{
					Mode:             "hash",
					HashFields:       []string{"sourceAddress", "destinationAddress"},
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Targets: []api.IPv4PolicyRouteTarget{
						{OutboundInterface: "wan", Table: 100, Priority: 10000, Mark: 256},
						{OutboundInterface: "wan", Table: 101, Priority: 10001, Mark: 257},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{
					{Name: "balanced", RouteSet: "wan-balance", Priority: 10},
				}},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("routeSet default route candidate should be valid: %v", err)
	}
}

func TestValidateIPv4DefaultRoutePolicyRouteSetCandidateRejectsDirectFields(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"},
				Metadata: api.ObjectMeta{Name: "wan-balance"},
				Spec: api.IPv4PolicyRouteSetSpec{
					Mode:             "hash",
					HashFields:       []string{"sourceAddress", "destinationAddress"},
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Targets: []api.IPv4PolicyRouteTarget{
						{OutboundInterface: "wan", Table: 100, Priority: 10000, Mark: 256},
						{OutboundInterface: "wan", Table: 101, Priority: 10001, Mark: 257},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{
					{Name: "balanced", RouteSet: "wan-balance", Priority: 10, Mark: 256},
				}},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected routeSet candidate with direct route mark to be rejected")
	}
}

func TestValidateDHCPv4ScopeRange(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:     "dhcpv4",
					Interface:  "lan",
					RangeStart: "192.168.10.199",
					RangeEnd:   "192.168.10.100",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected reversed DHCP range to be rejected")
	}
}

func TestValidateDHCPv4ReservationRange(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:     "dhcpv4",
					Interface:  "lan",
					RangeStart: "192.0.2.100",
					RangeEnd:   "192.0.2.150",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"},
				Metadata: api.ObjectMeta{Name: "printer"},
				Spec: api.DHCPv4ReservationSpec{
					Scope:      "lan-dhcp4",
					MACAddress: "02:00:00:00:01:50",
					IPAddress:  "192.0.2.200",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected host reservation outside the scope range to be rejected")
	}
}

func TestValidateStaticRoutes(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"},
				Metadata: api.ObjectMeta{Name: "lab-v4"},
				Spec:     api.IPv4StaticRouteSpec{Interface: "wan", Destination: "192.0.2.0/24", Via: "198.51.100.1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"},
				Metadata: api.ObjectMeta{Name: "lab-v6"},
				Spec:     api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:1::/64", Via: "fe80::1"},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate static routes: %v", err)
	}
}

func TestValidateSelfAddressPolicy(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "lan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv6"},
				Spec: api.IPv6DelegatedAddressSpec{
					PrefixDelegation: "wan-pd",
					Interface:        "lan",
					AddressSuffix:    "::3",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "SelfAddressPolicy"},
				Metadata: api.ObjectMeta{Name: "lan-self"},
				Spec: api.SelfAddressPolicySpec{
					AddressFamily: "ipv6",
					Candidates: []api.SelfAddressPolicyCandidate{
						{Source: "delegatedAddress", DelegatedAddress: "lan-ipv6", AddressSuffix: "::3"},
						{Source: "interfaceAddress", Interface: "lan", MatchSuffix: "::3"},
					},
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate self address policy: %v", err)
	}
}

func TestValidateDHCPv6PrefixDelegationIdentity(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.DHCPv6PrefixDelegationSpec{
					Interface: "wan",
					IAID:      "00000001",
					DUIDType:  "link-layer",
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate prefix delegation identity: %v", err)
	}

	router.Spec.Resources[1].Spec = api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "not-an-iaid"}
	if err := Validate(router); err == nil {
		t.Fatal("expected invalid IAID to be rejected")
	}

	router.Spec.Resources[1].Spec = api.DHCPv6PrefixDelegationSpec{Interface: "wan", DUIDType: "unknown"}
	if err := Validate(router); err == nil {
		t.Fatal("expected invalid duidType to be rejected")
	}
}

func TestValidateRejectsExternalPDClientAndNetworkdDHCPv6OnSameInterface(t *testing.T) {
	for _, client := range []string{"dhcp6c", "dhcpcd"} {
		t.Run(client, func(t *testing.T) {
			router := &api.Router{
				TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
				Metadata: api.ObjectMeta{Name: "test"},
				Spec: api.RouterSpec{Resources: []api.Resource{
					{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
						Metadata: api.ObjectMeta{Name: "wan"},
						Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
					},
					{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"},
						Metadata: api.ObjectMeta{Name: "wan-dhcpv6"},
						Spec:     api.DHCPv6AddressSpec{Interface: "wan", Client: "networkd"},
					},
					{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
						Metadata: api.ObjectMeta{Name: "wan-pd"},
						Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan", Client: client},
					},
				}},
			}
			if err := Validate(router); err == nil {
				t.Fatal("expected DHCPv6 client conflict to be rejected")
			}
		})
	}
}

func TestValidateIPv4SourceNATRequiresValidCIDR(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"not-a-cidr"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected invalid NAT source CIDR to be rejected")
	}
}

func TestValidateNAT44Rule(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.NAT44RuleSpec{
					Type:            "masquerade",
					EgressPolicyRef: "ipv4-default",
					SourceRanges:    []string{"192.168.0.0/16"},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate NAT44Rule: %v", err)
	}
	router.Spec.Resources[0].Spec = api.NAT44RuleSpec{Type: "snat", EgressInterface: "wan", SourceRanges: []string{"192.168.0.0/16"}}
	if err := Validate(router); err == nil {
		t.Fatal("expected snat without snatAddress to be rejected")
	}
}

func TestValidateIPv4SourceNATRejectsInvalidPortRange(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation: api.IPv4NATTranslationSpec{
						Type: "interfaceAddress",
						PortMapping: api.IPv4NATPortMappingSpec{
							Type:  "range",
							Start: 65535,
							End:   1024,
						},
					},
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected invalid NAT port range to be rejected")
	}
}

func TestValidateFirewallPolicyAndRule(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec:     api.FirewallPolicySpec{LogDeny: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"},
				Metadata: api.ObjectMeta{Name: "allow-https"},
				Spec: api.FirewallRuleSpec{
					FromZone:    "wan",
					ToZone:      "self",
					Protocol:    "tcp",
					Port:        443,
					SourceCIDRs: []string{"203.0.113.0/24"},
					Action:      "accept",
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate firewall resources: %v", err)
	}
}

func TestValidatePathMTUPolicy(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec:     api.DSLiteTunnelSpec{Interface: "lan", TunnelName: "ds-transix", RemoteAddress: "2001:db8::1", MTU: 1460},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv6"},
				Spec:     api.DHCPv6ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv6"},
				Spec:     api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::3"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "lan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv6"},
				Spec:     api.DHCPv6ScopeSpec{Server: "dhcpv6", DelegatedAddress: "lan-ipv6"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"},
				Metadata: api.ObjectMeta{Name: "lan-wan-mtu"},
				Spec: api.PathMTUPolicySpec{
					FromInterface: "lan",
					ToInterfaces:  []string{"transix"},
					MTU:           api.PathMTUPolicyMTUSpec{Source: "minInterface"},
					IPv6RA:        api.PathMTUPolicyIPv6RASpec{Enabled: true, Scope: "lan-dhcpv6"},
					TCPMSSClamp:   api.PathMTUPolicyTCPMSSSpec{Enabled: true, Families: []string{"ipv4", "ipv6"}},
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate path MTU policy: %v", err)
	}
}

func TestValidateRejectsMissingInterfaceReference(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "missing", Address: "192.168.1.32/24"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected missing interface reference to be rejected")
	}
}

func TestValidateRejectsInvalidStaticAddress(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "not-a-prefix"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected invalid IPv4 prefix to be rejected")
	}
}

func TestValidateRequiresOverlapReason(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24", AllowOverlap: true},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected allowOverlap without reason to be rejected")
	}
}

func TestValidateRejectsDuplicateStaticOnSameInterface(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr-a"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr-b"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected duplicate static address on same interface to be rejected")
	}
}

func TestValidateBridge(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"},
				Metadata: api.ObjectMeta{Name: "br-home"},
				Spec:     api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, RSTP: boolPtr(true), MulticastSnooping: boolPtr(false)},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "bridge-address"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "br-home", Address: "192.0.2.1/24"},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate bridge config: %v", err)
	}
}

func TestValidateVXLANSegment(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "underlay"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"},
				Metadata: api.ObjectMeta{Name: "br-home"},
				Spec:     api.BridgeSpec{IfName: "br0", Members: []string{"home-vxlan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"},
				Metadata: api.ObjectMeta{Name: "home-vxlan"},
				Spec: api.VXLANSegmentSpec{
					IfName:            "vxlan100",
					VNI:               100,
					LocalAddress:      "192.0.2.10",
					Remotes:           []string{"192.0.2.20", "192.0.2.30"},
					UnderlayInterface: "underlay",
					UDPPort:           4789,
					MTU:               1450,
					Bridge:            "br-home",
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate vxlan config: %v", err)
	}
}

func TestValidateRejectsInvalidVXLANFilterMode(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "underlay"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"},
				Metadata: api.ObjectMeta{Name: "home-vxlan"},
				Spec:     api.VXLANSegmentSpec{VNI: 100, LocalAddress: "192.0.2.10", Remotes: []string{"192.0.2.20"}, UnderlayInterface: "underlay", L2Filter: "permit"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected invalid VXLAN l2Filter to be rejected")
	}
}

func TestValidateDHCPServerTransitRole(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Role: "transit"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv6"},
				Spec:     api.DHCPv6ServerSpec{Server: "dnsmasq", Role: "transit"},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate transit DHCP roles: %v", err)
	}
}

func boolPtr(value bool) *bool {
	return &value
}
