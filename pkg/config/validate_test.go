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

func TestValidateIPv4DHCPScopeRange(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPServer"},
				Metadata: api.ObjectMeta{Name: "dhcp4"},
				Spec:     api.IPv4DHCPServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPScope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
				Spec: api.IPv4DHCPScopeSpec{
					Server:     "dhcp4",
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     api.IPv6PrefixDelegationSpec{Interface: "lan"},
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

func TestValidateIPv6PrefixDelegationIdentity(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.IPv6PrefixDelegationSpec{
					Interface:   "wan",
					IAID:        "00000001",
					DUIDType:    "link-layer",
					DUIDRawData: "000102005e102030",
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate prefix delegation identity: %v", err)
	}

	router.Spec.Resources[1].Spec = api.IPv6PrefixDelegationSpec{Interface: "wan", IAID: "not-an-iaid"}
	if err := Validate(router); err == nil {
		t.Fatal("expected invalid IAID to be rejected")
	}

	router.Spec.Resources[1].Spec = api.IPv6PrefixDelegationSpec{Interface: "wan", ServerID: "00030001020000000001", PriorPrefix: "2001:db8:1200:1240::/60", AcquisitionStrategy: "request-claim-only"}
	if err := Validate(router); err != nil {
		t.Fatalf("validate prefix delegation active controller overrides: %v", err)
	}

	router.Spec.Resources[1].Spec = api.IPv6PrefixDelegationSpec{Interface: "wan", ServerID: "not-a-duid"}
	if err := Validate(router); err == nil {
		t.Fatal("expected invalid serverID to be rejected")
	}

	router.Spec.Resources[1].Spec = api.IPv6PrefixDelegationSpec{Interface: "wan", PriorPrefix: "not-a-prefix"}
	if err := Validate(router); err == nil {
		t.Fatal("expected invalid priorPrefix to be rejected")
	}

	router.Spec.Resources[1].Spec = api.IPv6PrefixDelegationSpec{Interface: "wan", AcquisitionStrategy: "unknown"}
	if err := Validate(router); err == nil {
		t.Fatal("expected invalid acquisitionStrategy to be rejected")
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
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPAddress"},
						Metadata: api.ObjectMeta{Name: "wan-dhcp6"},
						Spec:     api.IPv6DHCPAddressSpec{Interface: "wan", Client: "networkd"},
					},
					{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
						Metadata: api.ObjectMeta{Name: "wan-pd"},
						Spec:     api.IPv6PrefixDelegationSpec{Interface: "wan", Client: client},
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

func TestValidateFirewallPolicyAndExposeService(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "Zone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.ZoneSpec{Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "Zone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.ZoneSpec{Interfaces: []string{"wan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec: api.FirewallPolicySpec{
					Preset: "home-router",
					RouterAccess: api.RouterAccessSpec{
						SSH: api.FirewallRouterServiceSpec{FromZones: []string{"lan"}},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ExposeService"},
				Metadata: api.ObjectMeta{Name: "nas-https"},
				Spec: api.ExposeServiceSpec{
					Family:          "ipv4",
					FromZone:        "wan",
					ViaInterface:    "wan",
					Protocol:        "tcp",
					ExternalPort:    443,
					InternalAddress: "192.168.10.20",
					InternalPort:    443,
					Sources:         []string{"203.0.113.0/24"},
					Hairpin:         true,
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPServer"},
				Metadata: api.ObjectMeta{Name: "dhcp6"},
				Spec:     api.IPv6DHCPServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv6"},
				Spec:     api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::3"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     api.IPv6PrefixDelegationSpec{Interface: "lan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPScope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcp6"},
				Spec:     api.IPv6DHCPScopeSpec{Server: "dhcp6", DelegatedAddress: "lan-ipv6"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"},
				Metadata: api.ObjectMeta{Name: "lan-wan-mtu"},
				Spec: api.PathMTUPolicySpec{
					FromInterface: "lan",
					ToInterfaces:  []string{"transix"},
					MTU:           api.PathMTUPolicyMTUSpec{Source: "minInterface"},
					IPv6RA:        api.PathMTUPolicyIPv6RASpec{Enabled: true, Scope: "lan-dhcp6"},
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

func boolPtr(value bool) *bool {
	return &value
}
