package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestNftablesIPv4SourceNAT(t *testing.T) {
	router := &api.Router{
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
							Start: 1024,
							End:   65535,
						},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table ip routerd_nat",
		"type nat hook postrouting priority srcnat; policy accept;",
		`oifname "ens18" ip saddr 192.168.10.0/24 meta l4proto { tcp, udp } masquerade to :1024-65535`,
		`oifname "ens18" ip saddr 192.168.10.0/24 masquerade`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIPv4SourceNATCanUseDSLiteTunnel(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:  "wan",
					TunnelName: "ds-transix",
					AFTRFQDN:   "gw.transix.jp",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-transix"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "transix",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if !strings.Contains(string(data), `oifname "ds-transix" ip saddr 192.168.10.0/24 masquerade`) {
		t.Fatalf("nftables output missing DS-Lite tunnel oif:\n%s", string(data))
	}
}

func TestNftablesIPv4SourceNATAddressPortRange(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ds0", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation: api.IPv4NATTranslationSpec{
						Type:    "address",
						Address: "192.0.0.2",
						PortMapping: api.IPv4NATPortMappingSpec{
							Type:  "range",
							Start: 1024,
							End:   65535,
						},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if !strings.Contains(string(data), `snat to 192.0.0.2:1024-65535`) {
		t.Fatalf("nftables output missing address SNAT port range:\n%s", string(data))
	}
}

func TestNftablesIPv4PolicyRoute(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRoute"},
				Metadata: api.ObjectMeta{Name: "lan-via-transix"},
				Spec: api.IPv4PolicyRouteSpec{
					OutboundInterface: "transix",
					Table:             100,
					Priority:          10000,
					Mark:              256,
					SourceCIDRs:       []string{"192.168.10.0/24"},
					DestinationCIDRs:  []string{"0.0.0.0/0"},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table ip routerd_policy",
		"type filter hook prerouting priority mangle; policy accept;",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 meta mark set 0x100",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIPv4PolicyRouteSet(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"},
				Metadata: api.ObjectMeta{Name: "lan-balance"},
				Spec: api.IPv4PolicyRouteSetSpec{
					Mode:             "hash",
					HashFields:       []string{"sourceAddress", "destinationAddress"},
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Targets: []api.IPv4PolicyRouteTarget{
						{OutboundInterface: "transix-a", Table: 100, Priority: 10000, Mark: 256},
						{OutboundInterface: "transix-b", Table: 101, Priority: 10001, Mark: 257},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark != 0x0 meta mark set ct mark",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark 0x0 meta mark set jhash ip saddr . ip daddr mod 2 map { 0 : 0x100, 1 : 0x101 }",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark 0x0 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesTCPMSSClamp(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "wan-pppoe"},
				Spec: api.PPPoEInterfaceSpec{
					Interface: "wan",
					IfName:    "ppp0",
					Username:  "user@example.jp",
					Password:  "secret",
					MTU:       1492,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "ds-lite-a"},
				Spec:     api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-lite-a", RemoteAddress: "2001:db8::1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"},
				Metadata: api.ObjectMeta{Name: "lan-wan-mtu"},
				Spec: api.PathMTUPolicySpec{
					FromInterface: "lan",
					ToInterfaces:  []string{"wan-pppoe", "ds-lite-a"},
					MTU:           api.PathMTUPolicyMTUSpec{Source: "minInterface"},
					TCPMSSClamp: api.PathMTUPolicyTCPMSSSpec{
						Enabled:  true,
						Families: []string{"ipv4", "ipv6"},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table inet routerd_mss",
		"type filter hook forward priority mangle; policy accept;",
		`iifname "ens19" oifname { "ds-lite-a", "ppp0" } ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size set 1414`,
		`iifname "ens19" oifname { "ds-lite-a", "ppp0" } meta nfproto ipv6 tcp flags syn / syn,rst tcp option maxseg size set 1394`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallHomeRouter(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "wan-pppoe"},
				Spec: api.PPPoEInterfaceSpec{
					Interface: "wan-ether",
					IfName:    "ppp0",
					Username:  "user@example.jp",
					Password:  "secret",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "Zone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.ZoneSpec{Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "Zone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.ZoneSpec{Interfaces: []string{"wan-pppoe"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec: api.FirewallPolicySpec{
					Preset:  "home-router",
					Input:   api.FirewallChainPolicySpec{Default: "drop"},
					Forward: api.FirewallChainPolicySpec{Default: "drop"},
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
					ViaInterface:    "wan-pppoe",
					Protocol:        "tcp",
					ExternalPort:    443,
					InternalAddress: "192.168.10.20",
					InternalPort:    443,
					Sources:         []string{"203.0.113.0/24"},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table inet routerd_filter",
		"type filter hook input priority filter; policy drop;",
		"ct state invalid drop",
		"ct state { established, related } accept",
		"meta l4proto ipv6-icmp accept",
		`iifname "ppp0" udp dport 546 accept`,
		`iifname "ens19" tcp dport 22 accept`,
		`iifname "ens19" oifname "ppp0" accept`,
		`iifname "ppp0" ip saddr 203.0.113.0/24 ip daddr 192.168.10.20 tcp dport 443 accept`,
		"table ip routerd_dnat",
		`iifname "ppp0" ip saddr 203.0.113.0/24 tcp dport 443 dnat to 192.168.10.20:443`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesKeepsProtectedZoneSSHOpen(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Apply: api.ApplyPolicySpec{ProtectedZones: []string{"mgmt"}},
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
					Metadata: api.ObjectMeta{Name: "mgmt"},
					Spec:     api.InterfaceSpec{IfName: "ens20", Managed: true, Owner: "routerd"},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "Zone"},
					Metadata: api.ObjectMeta{Name: "mgmt"},
					Spec:     api.ZoneSpec{Interfaces: []string{"mgmt"}},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
					Metadata: api.ObjectMeta{Name: "default-home"},
					Spec: api.FirewallPolicySpec{
						Input:   api.FirewallChainPolicySpec{Default: "drop"},
						Forward: api.FirewallChainPolicySpec{Default: "drop"},
					},
				},
			},
		},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `iifname "ens20" tcp dport 22 accept`) {
		t.Fatalf("nftables output does not keep protected mgmt SSH open:\n%s", got)
	}
}

func TestNftablesAllowsWANIPv6ClientControlPlane(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
					Metadata: api.ObjectMeta{Name: "wan"},
					Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
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
						Input:   api.FirewallChainPolicySpec{Default: "drop"},
						Forward: api.FirewallChainPolicySpec{Default: "drop"},
					},
				},
			},
		},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"meta l4proto ipv6-icmp accept",
		`iifname "ens18" udp dport 546 accept`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "udp sport 547") {
		t.Fatalf("DHCPv6 client rule must not constrain server source port:\n%s", got)
	}
}
