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

func TestValidateIPv4DefaultRouteStaticRequiresGateway(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoute"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec:     api.IPv4DefaultRouteSpec{Interface: "wan", GatewaySource: "static"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected static default route without gateway to be rejected")
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
				Spec:     api.IPv4DHCPServerSpec{Server: "dnsmasq", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPScope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
				Spec: api.IPv4DHCPScopeSpec{
					Server:     "dhcp4",
					Interface:  "lan",
					RangeStart: "192.168.160.199",
					RangeEnd:   "192.168.160.100",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected reversed DHCP range to be rejected")
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
					SourceCIDRs:       []string{"192.168.160.0/24"},
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
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.160.3/24", AllowOverlap: true},
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
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.160.3/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr-b"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.160.3/24"},
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
