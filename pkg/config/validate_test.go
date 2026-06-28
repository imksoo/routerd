// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
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

func TestValidateIPv4RoutePreferredSource(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{Name: "delivery"},
				Spec: api.IPv4RouteSpec{
					Destination:     "10.77.60.11/32",
					Device:          "wg-hybrid",
					PreferredSource: "10.77.60.10",
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate route with preferredSource: %v", err)
	}

	router.Spec.Resources[0].Spec = api.IPv4RouteSpec{
		Destination:     "10.77.60.11/32",
		Device:          "wg-hybrid",
		PreferredSource: "10.77.60.0/24",
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.preferredSource must be an IPv4 address") {
		t.Fatalf("Validate invalid preferredSource err = %v", err)
	}
}

func TestValidateManagementAccess(t *testing.T) {
	router := testManagementRouter(
		managementAccess("main", []string{"mgmt0", "Interface/lan0"}, nil),
	)
	router.Spec.Resources[0].Spec = api.ManagementAccessSpec{
		Interfaces:       []string{"mgmt0", "Interface/lan0"},
		AllowSourceCIDRs: []string{"192.168.100.0/24", "2001:db8::/64"},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate ManagementAccess: %v", err)
	}
}

func TestValidateManagementAccessRejectsInvalidSpec(t *testing.T) {
	tests := []struct {
		name string
		spec api.ManagementAccessSpec
		want string
	}{
		{
			name: "missing interfaces",
			spec: api.ManagementAccessSpec{},
			want: "spec.interfaces is required",
		},
		{
			name: "unsupported reference kind",
			spec: api.ManagementAccessSpec{Interfaces: []string{"PPPoESession/wan"}},
			want: "must reference an Interface name or Interface/<name>",
		},
		{
			name: "invalid cidr",
			spec: api.ManagementAccessSpec{Interfaces: []string{"mgmt0"}, AllowSourceCIDRs: []string{"192.168.100.1"}},
			want: "spec.allowSourceCIDRs[0] is invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := testManagementRouter(api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "ManagementAccess"},
				Metadata: api.ObjectMeta{Name: "main"},
				Spec:     tc.spec,
			})
			err := Validate(router)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateResourceWhenAnyAllNested(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec: api.HealthCheckSpec{
				TargetSource: "static",
				Target:       "192.0.2.1",
				When: api.ResourceWhenSpec{Any: []api.ResourceWhenSpec{
					{All: []api.ResourceWhenSpec{
						{State: map[string]api.StateMatchSpec{"wan.a": {Equals: "up"}}},
						{State: map[string]api.StateMatchSpec{"wan.b": {Status: "set"}}},
					}},
					{State: map[string]api.StateMatchSpec{"wan.c": {In: []string{"ready", "fallback"}}}},
				}},
			},
		}}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate nested when: %v", err)
	}
}

func TestValidateResourceWhenRejectsMixedForms(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec: api.HealthCheckSpec{
				TargetSource: "static",
				Target:       "192.0.2.1",
				When: api.ResourceWhenSpec{
					State: map[string]api.StateMatchSpec{"wan.a": {Equals: "up"}},
					Any:   []api.ResourceWhenSpec{{State: map[string]api.StateMatchSpec{"wan.b": {Equals: "up"}}}},
				},
			},
		}}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "exactly one of state, all, or any") {
		t.Fatalf("validate mixed when error = %v", err)
	}
}

func TestValidateResourceWhenStatusReferenceUsesProvidesContract(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
				Metadata: api.ObjectMeta{Name: "lan-gw-v4"},
				Spec: api.VirtualAddressSpec{
					Interface: "lan",
					Address:   "172.18.0.1/32",
					Family:    "ipv4",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.DHCPv6PrefixDelegationSpec{
					Interface: "wan",
					When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
						"VirtualAddress/lan-gw-v4.status.status.role": {Equals: "master"},
					}},
				},
			},
		}},
	}
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "does not provide field \"status.role\"") {
		t.Fatalf("invalid when status reference error = %v", err)
	}
}

func TestValidateResourceWhenStatusReferenceAcceptsStatusScheme(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
				Metadata: api.ObjectMeta{Name: "lan-gw-v4"},
				Spec: api.VirtualAddressSpec{
					Interface: "lan",
					Address:   "172.18.0.1/32",
					Family:    "ipv4",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.DHCPv6PrefixDelegationSpec{
					Interface: "wan",
					When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
						"${VirtualAddress/lan-gw-v4.status.role}": {Equals: "master"},
					}},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid when status reference rejected: %v", err)
	}
}

func TestValidateResourceWhenRejectsMixedFormsForEveryWhenField(t *testing.T) {
	for _, tc := range whenValidationTestResources(invalidMixedResourceWhen()) {
		t.Run(tc.specName, func(t *testing.T) {
			if refs := resourceWhens(tc.resource); len(refs) == 0 {
				t.Fatalf("resourceWhens returned no refs for %s", tc.resource.Kind)
			}
			router := &api.Router{
				TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
				Metadata: api.ObjectMeta{Name: "test"},
				Spec:     api.RouterSpec{Resources: []api.Resource{tc.resource}},
			}
			err := Validate(router)
			if err == nil || !strings.Contains(err.Error(), "exactly one of state, all, or any") {
				t.Fatalf("Validate(%s) error = %v", tc.specName, err)
			}
		})
	}
}

func TestResourceWhensCoversAPISpecWhenFields(t *testing.T) {
	actual := apiSpecStructsWithResourceWhen(t)
	expected := make([]string, 0, len(whenValidationTestResources(api.ResourceWhenSpec{})))
	for _, tc := range whenValidationTestResources(api.ResourceWhenSpec{}) {
		expected = append(expected, tc.specName)
	}
	sort.Strings(expected)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("when validation resource table mismatch\nactual:\n%s\nexpected:\n%s", strings.Join(actual, "\n"), strings.Join(expected, "\n"))
	}
}

func TestReferenceTableMatchesProvidesContract(t *testing.T) {
	for _, row := range referenceProviderRows() {
		t.Run(row.Kind+"."+row.Field, func(t *testing.T) {
			got, ok := api.ResourceProvidesFieldType(row.Kind, row.Field)
			if !ok {
				t.Fatalf("%s does not provide %q", row.Kind, row.Field)
			}
			if got != row.Type {
				t.Fatalf("%s.%s type = %s, want %s", row.Kind, row.Field, got, row.Type)
			}
		})
	}
}

func TestValidateRejectsMissingStatusReference(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"},
			Metadata: api.ObjectMeta{Name: "console"},
			Spec:     api.WebConsoleSpec{ListenAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/missing", Field: "address"}},
		}}},
	}
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "references missing IPv4StaticAddress") {
		t.Fatalf("missing status reference error = %v", err)
	}
}

func TestValidateRejectsStatusReferenceFieldOutsideProvidesContract(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "mgmt"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"},
				Metadata: api.ObjectMeta{Name: "console"},
				Spec:     api.WebConsoleSpec{ListenAddressFrom: api.StatusValueSourceSpec{Resource: "Interface/mgmt", Field: "gateway"}},
			},
		}},
	}
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "does not provide field \"gateway\"") {
		t.Fatalf("unsupported status field error = %v", err)
	}
}

func TestValidateAcceptsNTPServerAllowCIDRFromProvidesContract(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
				Metadata: api.ObjectMeta{Name: "lan-base"},
				Spec: api.IPv6DelegatedAddressSpec{
					PrefixDelegation: "wan-pd",
					Interface:        "lan",
					AddressSuffix:    "::1",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"},
				Metadata: api.ObjectMeta{Name: "lan-time"},
				Spec: api.NTPServerSpec{
					Managed:       true,
					Servers:       []string{"ntp.example.net"},
					AllowCIDRFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate allowCIDRFrom: %v", err)
	}
}

func TestValidateExampleStatusReferencesAgainstProvides(t *testing.T) {
	entries, err := os.ReadDir("../../examples")
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			router, err := Load(filepath.Join("../../examples", entry.Name()))
			if err != nil {
				t.Fatalf("load example: %v", err)
			}
			if err := Validate(router); err != nil {
				t.Fatalf("validate example: %v", err)
			}
		})
	}
}

type referenceProviderRow struct {
	Kind  string
	Field string
	Type  string
}

func referenceProviderRows() []referenceProviderRow {
	return []referenceProviderRow{
		{Kind: "Interface", Field: "phase", Type: api.ProvidesTypeString},
		{Kind: "Interface", Field: "ifname", Type: api.ProvidesTypeString},
		{Kind: "Interface", Field: "ipv4Addresses", Type: api.ProvidesTypeStringList},
		{Kind: "IPv4StaticAddress", Field: "address", Type: api.ProvidesTypeString},
		{Kind: "VirtualAddress", Field: "address", Type: api.ProvidesTypeString},
		{Kind: "IPv6DelegatedAddress", Field: "address", Type: api.ProvidesTypeString},
		{Kind: "DHCPv4Client", Field: "gateway", Type: api.ProvidesTypeString},
		{Kind: "DHCPv4Client", Field: "dnsServers", Type: api.ProvidesTypeStringList},
		{Kind: "DHCPv4Client", Field: "ntpServers", Type: api.ProvidesTypeStringList},
		{Kind: "DHCPv6Information", Field: "dnsServers", Type: api.ProvidesTypeStringList},
		{Kind: "DHCPv6Information", Field: "sntpServers", Type: api.ProvidesTypeStringList},
		{Kind: "DHCPv6Information", Field: "domainSearch", Type: api.ProvidesTypeStringList},
		{Kind: "DHCPv6Information", Field: "aftrName", Type: api.ProvidesTypeString},
		{Kind: "DHCPv6PrefixDelegation", Field: "currentPrefix", Type: api.ProvidesTypeString},
		{Kind: "DNSZone", Field: "zone", Type: api.ProvidesTypeString},
		{Kind: "DNSResolver", Field: "listenAddresses", Type: api.ProvidesTypeStringList},
		{Kind: "DSLiteTunnel", Field: "interface", Type: api.ProvidesTypeString},
		{Kind: "DSLiteTunnel", Field: "device", Type: api.ProvidesTypeString},
		{Kind: "EgressRoutePolicy", Field: "selectedDevice", Type: api.ProvidesTypeString},
		{Kind: "EgressRoutePolicy", Field: "selectedGateway", Type: api.ProvidesTypeString},
		{Kind: "IngressService", Field: "listenAddress", Type: api.ProvidesTypeString},
		{Kind: "IngressService", Field: "activeBackend", Type: api.ProvidesTypeObject},
		{Kind: "IngressService", Field: "activeBackends", Type: api.ProvidesTypeObjectList},
		{Kind: "IPAddressSet", Field: "addresses", Type: api.ProvidesTypeStringList},
		{Kind: "NTPClient", Field: "servers", Type: api.ProvidesTypeStringList},
		{Kind: "NTPServer", Field: "allowCIDRs", Type: api.ProvidesTypeStringList},
		{Kind: "NTPServer", Field: "listenAddresses", Type: api.ProvidesTypeStringList},
		{Kind: "PPPoESession", Field: "interface", Type: api.ProvidesTypeString},
		{Kind: "PPPoESession", Field: "device", Type: api.ProvidesTypeString},
		{Kind: "PPPoESession", Field: "gateway", Type: api.ProvidesTypeString},
	}
}

type whenValidationTestResource struct {
	specName string
	resource api.Resource
}

func whenValidationTestResources(when api.ResourceWhenSpec) []whenValidationTestResource {
	return []whenValidationTestResource{
		{specName: "ObservabilityPipelineSpec", resource: testResource(api.SystemAPIVersion, "ObservabilityPipeline", "observability", api.ObservabilityPipelineSpec{When: when})},
		{specName: "RouterdClusterSpec", resource: testResource(api.SystemAPIVersion, "RouterdCluster", "cluster", api.RouterdClusterSpec{Peers: []string{"router-a", "router-b"}, LeasePath: "/run/routerd/cluster/lease", When: when})},
		{specName: "InterfaceSpec", resource: testResource(api.NetAPIVersion, "Interface", "wan", api.InterfaceSpec{IfName: "eth0", Managed: false, When: when})},
		{specName: "VirtualAddressSpec", resource: testResource(api.NetAPIVersion, "VirtualAddress", "vip", api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "192.0.2.10/32", When: when})},
		{specName: "BGPRouterSpec", resource: testResource(api.NetAPIVersion, "BGPRouter", "main", api.BGPRouterSpec{ASN: 64500, RouterID: "192.0.2.1", When: when})},
		{specName: "BGPPeerSpec", resource: testResource(api.NetAPIVersion, "BGPPeer", "k8s-rt", api.BGPPeerSpec{RouterRef: "BGPRouter/main", PeerASN: 64512, Peers: []string{"192.0.2.2"}, When: when})},
		{specName: "BGPDynamicPeerSpec", resource: testResource(api.NetAPIVersion, "BGPDynamicPeer", "leaves", api.BGPDynamicPeerSpec{RouterRef: "BGPRouter/main", PeerASN: 64500, Listen: api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/20"}}, When: when})},
		{specName: "BFDSpec", resource: testResource(api.NetAPIVersion, "BFD", "k8s-rt", api.BFDSpec{Peer: "BGPPeer/k8s-rt", When: when})},
		{specName: "TailscaleNodeSpec", resource: testResource(api.NetAPIVersion, "TailscaleNode", "home", api.TailscaleNodeSpec{Hostname: "router", When: when})},
		{specName: "NTPClientSpec", resource: testResource(api.SystemAPIVersion, "NTPClient", "system-time", api.NTPClientSpec{Provider: "chrony", Managed: true, Source: "auto", FallbackServers: []string{"192.0.2.123"}, When: when})},
		{specName: "NTPServerSpec", resource: testResource(api.SystemAPIVersion, "NTPServer", "lan-time", api.NTPServerSpec{Provider: "chrony", Managed: true, Source: "auto", FallbackServers: []string{"192.0.2.123"}, ListenAddresses: []string{"192.0.2.1"}, When: when})},
		{specName: "DHCPv4ClientSpec", resource: testResource(api.NetAPIVersion, "DHCPv4Client", "wan-v4", api.DHCPv4ClientSpec{Interface: "wan", When: when})},
		{specName: "IPv4StaticAddressSpec", resource: testResource(api.NetAPIVersion, "IPv4StaticAddress", "wan-v4", api.IPv4StaticAddressSpec{Interface: "wan", Address: "192.0.2.1/32", When: when})},
		{specName: "ClusterNetworkRouteSpec", resource: testResource(api.NetAPIVersion, "ClusterNetworkRoute", "k8s", api.ClusterNetworkRouteSpec{Pods: api.ClusterNetworkRouteCIDRSpec{CIDRs: []string{"10.244.0.0/16"}}, Via: []api.ClusterNetworkRouteViaSpec{{Interface: "lan", NextHop: "192.0.2.2"}}, When: when})},
		{specName: "DHCPv4ServerSpec", resource: testResource(api.NetAPIVersion, "DHCPv4Server", "lan", api.DHCPv4ServerSpec{Server: "dnsmasq", Interface: "lan", RangeStart: "192.0.2.100", RangeEnd: "192.0.2.150", When: when})},
		{specName: "DHCPv4ReservationSpec", resource: testResource(api.NetAPIVersion, "DHCPv4Reservation", "printer", api.DHCPv4ReservationSpec{Server: "lan", MACAddress: "02:00:00:00:01:50", Hostname: "printer", IPAddress: "192.0.2.50", When: when})},
		{specName: "IPv6DelegatedAddressSpec", resource: testResource(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-v6", api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::1", When: when})},
		{specName: "DHCPv6ServerSpec", resource: testResource(api.NetAPIVersion, "DHCPv6Server", "lan-v6", api.DHCPv6ServerSpec{Server: "dnsmasq", DelegatedAddress: "lan-v6", When: when})},
		{specName: "DHCPv4ServerLeaseSyncSpec", resource: testResource(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan-v4-leases", api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "router-b"}}, When: when})},
		{specName: "DHCPv6ServerLeaseSyncSpec", resource: testResource(api.NetAPIVersion, "DHCPv6ServerLeaseSync", "lan-v6-leases", api.DHCPv6ServerLeaseSyncSpec{Source: api.DHCPv6ServerLeaseSyncSourceSpec{Resource: "DHCPv6Server/lan-v6"}, Targets: []api.LeaseSyncTargetSpec{{Host: "router-b"}}, When: when})},
		{specName: "DHCPv6PrefixDelegationLeaseSyncSpec", resource: testResource(api.NetAPIVersion, "DHCPv6PrefixDelegationLeaseSync", "wan-pd-leases", api.DHCPv6PrefixDelegationLeaseSyncSpec{Source: api.DHCPv6PrefixDelegationLeaseSyncSourceSpec{Resource: "DHCPv6PrefixDelegation/wan-pd"}, Targets: []api.LeaseSyncTargetSpec{{Host: "router-b"}}, When: when})},
		{specName: "DHCPv6PrefixDelegationSpec", resource: testResource(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", api.DHCPv6PrefixDelegationSpec{Interface: "wan", When: when})},
		{specName: "DHCPv6InformationSpec", resource: testResource(api.NetAPIVersion, "DHCPv6Information", "wan-info", api.DHCPv6InformationSpec{Interface: "wan", When: when})},
		{specName: "IPv6RouterAdvertisementSpec", resource: testResource(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra", api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8:1::/64", When: when})},
		{specName: "DSLiteTunnelSpec", resource: testResource(api.NetAPIVersion, "DSLiteTunnel", "dslite", api.DSLiteTunnelSpec{Interface: "wan", AFTRIPv6: "2001:db8::1", When: when})},
		{specName: "DNSForwarderSpec", resource: testResource(api.NetAPIVersion, "DNSForwarder", "ad", api.DNSForwarderSpec{Resolver: "DNSResolver/lan", Match: []string{"corp.example"}, Upstreams: []string{"DNSUpstream/ad"}, When: when})},
		{specName: "DNSResolverSpec", resource: testResource(api.NetAPIVersion, "DNSResolver", "lan", api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53}}, When: when})},
		{specName: "DNSUpstreamSpec", resource: testResource(api.NetAPIVersion, "DNSUpstream", "ad", api.DNSUpstreamSpec{Protocol: "udp", Address: "192.0.2.53", When: when})},
		{specName: "EventGroupSpec", resource: testResource(api.FederationAPIVersion, "EventGroup", "edge", api.EventGroupSpec{NodeName: "router-a", When: when})},
		{specName: "HealthCheckSpec", resource: testResource(api.NetAPIVersion, "HealthCheck", "internet", api.HealthCheckSpec{TargetSource: "static", Target: "192.0.2.1", When: when})},
		{specName: "EgressRoutePolicySpec", resource: testResource(api.NetAPIVersion, "EgressRoutePolicy", "default-v4", api.EgressRoutePolicySpec{When: when, Candidates: []api.EgressRoutePolicyCandidate{{Interface: "wan", Priority: 1, Table: 100, Mark: 100}}})},
		{specName: "EgressRoutePolicyCandidate", resource: testResource(api.NetAPIVersion, "EgressRoutePolicy", "default-v4", api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{Interface: "wan", Priority: 1, Table: 100, Mark: 100, When: when}}})},
		{specName: "NAT44RuleSpec", resource: testResource(api.NetAPIVersion, "NAT44Rule", "lan", api.NAT44RuleSpec{OutboundInterface: "wan", SourceCIDRs: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "interfaceAddress"}, When: when})},
		{specName: "NAT44SessionSyncSpec", resource: testResource(api.NetAPIVersion, "NAT44SessionSync", "dslite-sessions", api.NAT44SessionSyncSpec{SNATAddresses: []string{"192.0.2.2"}, Targets: []api.NAT44SessionSyncTargetSpec{{Host: "router-b"}}, When: when})},
		{specName: "PortForwardSpec", resource: testResource(api.FirewallAPIVersion, "PortForward", "web", api.PortForwardSpec{Listen: api.IngressListenSpec{Interface: "wan", Protocol: "tcp", Port: 443}, Target: api.IngressTargetSpec{Address: "192.0.2.10", Port: 8443}, When: when})},
		{specName: "IngressServiceSpec", resource: testResource(api.FirewallAPIVersion, "IngressService", "web", api.IngressServiceSpec{Listen: api.IngressListenSpec{Interface: "wan", Protocol: "tcp", Port: 443}, Backends: []api.IngressBackendSpec{{Address: "192.0.2.10", Port: 8443}}, When: when})},
		{specName: "IPAddressSetSpec", resource: testResource(api.NetAPIVersion, "IPAddressSet", "blocked", api.IPAddressSetSpec{Addresses: []string{"192.0.2.10"}, When: when})},
		{specName: "LocalServiceRedirectSpec", resource: testResource(api.FirewallAPIVersion, "LocalServiceRedirect", "dns", api.LocalServiceRedirectSpec{Interface: "lan", Rules: []api.LocalServiceRedirectRuleSpec{{Protocols: []string{"tcp"}, DestinationSetRef: "dns-servers", DestinationPort: 53, RedirectPort: 5353}}, When: when})},
	}
}

func invalidMixedResourceWhen() api.ResourceWhenSpec {
	return api.ResourceWhenSpec{
		State: map[string]api.StateMatchSpec{"wan.a": {Equals: "up"}},
		Any:   []api.ResourceWhenSpec{{State: map[string]api.StateMatchSpec{"wan.b": {Equals: "up"}}}},
	}
}

func testResource(apiVersion, kind, name string, spec any) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: apiVersion, Kind: kind},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     spec,
	}
}

func apiSpecStructsWithResourceWhen(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../api/specs.go")
	if err != nil {
		t.Fatalf("read api specs: %v", err)
	}
	re := regexp.MustCompile(`(?s)type\s+(\w+)\s+struct\s*\{([^{}]*)\}`)
	var out []string
	for _, match := range re.FindAllSubmatch(data, -1) {
		if bytes.Contains(match[2], []byte("When")) && regexp.MustCompile(`\bWhen\s+ResourceWhenSpec\b`).Match(match[2]) {
			out = append(out, string(match[1]))
		}
	}
	sort.Strings(out)
	return out
}

func TestValidatePackageSupportsUbuntuAPT(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"},
				Metadata: api.ObjectMeta{Name: "router-deps"},
				Spec: api.PackageSpec{Packages: []api.OSPackageSetSpec{
					{OS: "ubuntu", Manager: "apt", Names: []string{"dnsmasq-base", "nftables"}},
				}},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate ubuntu package resource: %v", err)
	}
}

func TestValidatePackageRejectsWrongManagerForOS(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"},
				Metadata: api.ObjectMeta{Name: "router-deps"},
				Spec: api.PackageSpec{Packages: []api.OSPackageSetSpec{
					{OS: "freebsd", Manager: "apt", Names: []string{"dnsmasq"}},
				}},
			},
		}},
	}

	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "manager must be pkg for os freebsd") {
		t.Fatalf("expected manager compatibility error, got %v", err)
	}
}

func TestValidateRejectsNetworkAdoptionOnProtectedInterface(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{
			Apply: api.ApplyPolicySpec{ProtectedInterfaces: []string{"mgmt"}},
			Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "mgmt"}, Spec: api.InterfaceSpec{IfName: "ens20"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption"}, Metadata: api.ObjectMeta{Name: "mgmt-adoption"}, Spec: api.NetworkAdoptionSpec{
					Interface:       "mgmt",
					SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{DisableDHCPv4: true},
				}},
			},
		},
	}

	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "protected interface") {
		t.Fatalf("expected protected interface error, got %v", err)
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

func TestValidateLogSinkWebhookRequiresURL(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"},
				Metadata: api.ObjectMeta{Name: "remote-log"},
				Spec:     api.LogSinkSpec{Type: "webhook"},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected webhook log sink without url to be rejected")
	}
}

func TestValidateLogSinkOTLPReferencesTelemetry(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "Telemetry"},
				Metadata: api.ObjectMeta{Name: "otlp"},
				Spec: api.TelemetrySpec{
					OTLP:    api.TelemetryOTLPSpec{Endpoint: "http://collector.example:4317"},
					Signals: []string{"logs"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"},
				Metadata: api.ObjectMeta{Name: "remote-log"},
				Spec:     api.LogSinkSpec{Type: "otlp", OTLP: api.LogSinkOTLPSpec{TelemetryRef: "Telemetry/otlp"}},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate otlp log sink: %v", err)
	}
}

func TestValidateObservabilityPipelineAndRouterdCluster(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "remote"}, Spec: api.ObservabilityPipelineSpec{
				Sampling: api.ObservabilityPipelineSamplingSpec{Rate: 0.5},
				Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{
					Type: "loki",
					Loki: api.ObservabilityLokiSinkSpec{URL: "http://loki.example/loki/api/v1/push"},
				}}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "RouterdCluster"}, Metadata: api.ObjectMeta{Name: "ha"}, Spec: api.RouterdClusterSpec{
				Peers:     []string{"routerd-01.lain.local", "routerd-02.lain.local"},
				LeaseTTL:  "30s",
				LeasePath: "/var/lib/routerd/ha-lease",
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate observability/ha: %v", err)
	}
	router.Spec.Resources[0].Spec = api.ObservabilityPipelineSpec{Sampling: api.ObservabilityPipelineSamplingSpec{Rate: 1.5}}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "sampling.rate") {
		t.Fatalf("expected sampling validation error, got %v", err)
	}
	router.Spec.Resources[0].Spec = api.ObservabilityPipelineSpec{}
	router.Spec.Resources[1].Spec = api.RouterdClusterSpec{Peers: []string{"routerd-01"}, LeaseTTL: "30s", LeasePath: "/var/lib/routerd/ha-lease"}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "at least 2 peers") {
		t.Fatalf("expected peers validation error, got %v", err)
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

func TestValidatePPPoESessionDaemonFields(t *testing.T) {
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
				Metadata: api.ObjectMeta{Name: "wan-ppp"},
				Spec: api.PPPoESessionSpec{
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

func TestValidatePPPoESessionRequiresOnePasswordSource(t *testing.T) {
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
				Metadata: api.ObjectMeta{Name: "wan-ppp"},
				Spec: api.PPPoESessionSpec{
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
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.TailscaleNodeSpec{Hostname: "router", AdvertiseExitNode: true, AdvertiseRoutes: []string{"172.18.0.0/16"}, AuthKeyEnv: "TS_AUTHKEY"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"}, Metadata: api.ObjectMeta{Name: "aws-a"}, Spec: api.IPsecConnectionSpec{LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.10", PreSharedKey: "secret", LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16", CloudProviderHint: "aws"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "vrf-guest"}, Spec: api.VRFSpec{RouteTable: 1001, Members: []string{"wg-lab"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx240"}, Spec: api.VXLANTunnelSpec{VNI: 240, LocalAddress: "10.44.0.1", UnderlayInterface: "wg-lab", Peers: []string{"10.44.0.2"}, Bridge: "br240"}},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate Tier S resources: %v", err)
	}
}

func TestValidateRejectsTailscaleWireGuardListenPortConflict(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-lab"}, Spec: api.WireGuardInterfaceSpec{ListenPort: 41641}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.TailscaleNodeSpec{AdvertiseExitNode: true}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "reserves Tailscale UDP port 41641") {
		t.Fatalf("expected listen port conflict error, got %v", err)
	}
}

func TestValidateWireGuardInterfacePeersFromOK(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
				Metadata: api.ObjectMeta{Name: "wg-lab"},
				Spec: api.WireGuardInterfaceSpec{
					SelfNodeRef: "router-a",
					ListenPort:  51820,
					PeersFrom:   []api.WireGuardPeersSourceSpec{{Resource: "SAMNodeSet/fabric"}},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate WireGuardInterface peersFrom: %v", err)
	}
}

func TestValidateRejectsInvalidWireGuardInterfacePeersFrom(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
				Metadata: api.ObjectMeta{Name: "wg-lab"},
				Spec: api.WireGuardInterfaceSpec{
					PeersFrom: []api.WireGuardPeersSourceSpec{{Resource: "SAMPeerGroup/rrs"}},
				},
			},
		}},
	}
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.peersFrom[0].resource must reference SAMNodeSet/<name>") {
		t.Fatalf("Validate WireGuardInterface peersFrom error = %v, want SAMNodeSet ref error", err)
	}
}

func TestValidateTailscaleNodeRejectsInvalidRoute(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.TailscaleNodeSpec{AdvertiseRoutes: []string{"172.18.0.0"}}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.advertiseRoutes[0]") {
		t.Fatalf("expected invalid Tailscale route error, got %v", err)
	}
}

func TestValidatePhase15LANServiceKinds(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::1"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
				Interface:   "lan",
				AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
				Gateway:     "192.168.10.1",
				DNSServers:  []string{"192.168.10.1"},
				NTPServers:  []string{"192.168.10.1"},
				DomainFrom:  api.StatusValueSourceSpec{Resource: "DNSZone/local", Field: "zone"},
				Options:     []api.DHCPv4OptionSpec{{Name: "domain-search", Value: "lan"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{Server: "lan-v4", MACAddress: "02:00:00:00:01:50", Hostname: "printer", IPAddress: "192.168.10.150"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{
				Interface:   "lan",
				Mode:        "both",
				AddressPool: api.DHCPAddressPoolSpec{Start: "::100", End: "::1ff", LeaseTime: "6h"},
				DNSServers:  []string{"2001:db8::53"},
				SNTPServers: []string{"2001:db8::123"},
				DomainSearchFrom: []api.StatusValueSourceSpec{
					{Resource: "DNSZone/local", Field: "zone"},
				},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "address"}, RDNSS: []string{"2001:db8::53"}, DNSSLFrom: []api.StatusValueSourceSpec{{Resource: "DNSZone/local", Field: "zone"}}, MTU: 1500, PRFPreference: "high"}},
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

func TestValidateDHCPv4ServerLeaseSyncRejectsTargetUserNewline(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"},
				Metadata: api.ObjectMeta{Name: "lan-v4-leases"},
				Spec: api.DHCPv4ServerLeaseSyncSpec{
					Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"},
					Targets: []api.LeaseSyncTargetSpec{{
						Host: "router-b",
						User: "routerd\nroot",
					}},
				},
			},
		}},
	}

	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.targets[0].user must not contain newline") {
		t.Fatalf("Validate err = %v, want target user newline error", err)
	}
}

func TestValidateEgressRoutePolicyStaticRequiresGateway(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.EgressRoutePolicySpec{Mode: "priority", Candidates: []api.EgressRoutePolicyCandidate{
					{Interface: "wan", GatewaySource: "static", Priority: 10, Table: 100, Mark: 256},
				}},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected static default route without gateway to be rejected")
	}
}

func TestValidateRejectsHealthCheckFwMarkMismatchWithRouteTarget(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
				Metadata: api.ObjectMeta{Name: "internet-via-wan"},
				Spec:     api.HealthCheckSpec{Target: "1.1.1.1", Protocol: "tcp", Port: 443, FwMark: 0x999},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.EgressRoutePolicySpec{Mode: "priority", Candidates: []api.EgressRoutePolicyCandidate{
					{Name: "wan", Interface: "wan", GatewaySource: "none", Priority: 10, Table: 100, Mark: 0x100, HealthCheck: "internet-via-wan"},
				}},
			},
		}},
	}

	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected fwmark validation error, got %v", err)
	}
}

func TestValidateEgressRoutePolicyTargetCandidate(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.EgressRoutePolicySpec{
					Mode:             "priority",
					HashFields:       []string{"sourceAddress", "destinationAddress"},
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Candidates: []api.EgressRoutePolicyCandidate{{
						Name:     "balanced",
						Priority: 10,
						Targets: []api.EgressRoutePolicyTarget{
							{Interface: "wan", Table: 100, Priority: 10000, Mark: 256},
							{Interface: "wan", Table: 101, Priority: 10001, Mark: 257},
						},
					}},
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("target default route candidate should be valid: %v", err)
	}
}

func TestValidateEgressRoutePolicyTargetCandidateRejectsDirectFields(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.EgressRoutePolicySpec{Mode: "priority", HashFields: []string{"sourceAddress"}, Candidates: []api.EgressRoutePolicyCandidate{
					{Name: "balanced", Priority: 10, Mark: 256, Targets: []api.EgressRoutePolicyTarget{{Interface: "wan", Table: 100, Priority: 10000, Mark: 256}}},
				}},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected target candidate with direct route mark to be rejected")
	}
}

func TestValidateEgressRoutePolicyDynamicGatewayAllowsAutoDerivation(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
				Metadata: api.ObjectMeta{Name: "wan-check"},
				Spec:     api.HealthCheckSpec{TargetSource: "static", Target: "1.1.1.1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default"},
				Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{
					Name:          "wan",
					DeviceFrom:    api.StatusValueSourceSpec{Resource: "Interface/wan", Field: "ifname"},
					GatewaySource: "dhcpv4",
					HealthCheck:   "wan-check",
				}}},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("Validate with dynamic gateway auto-derivation: %v", err)
	}
}

func TestValidateDHCPv4ServerPoolRange(t *testing.T) {
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
				Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
				Spec: api.DHCPv4ServerSpec{

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

func TestValidateDHCPv4ReservationMayLiveOutsidePool(t *testing.T) {
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
				Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
				Spec: api.DHCPv4ServerSpec{

					Interface:  "lan",
					RangeStart: "192.0.2.100",
					RangeEnd:   "192.0.2.150",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"},
				Metadata: api.ObjectMeta{Name: "printer"},
				Spec: api.DHCPv4ReservationSpec{
					Server:     "lan-dhcp4",
					MACAddress: "02:00:00:00:01:50",
					IPAddress:  "192.0.2.200",
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("reservation outside dynamic pool should validate: %v", err)
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

func TestValidateClusterNetworkRoute(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "ClusterNetworkRoute"},
				Metadata: api.ObjectMeta{Name: "k8s"},
				Spec: api.ClusterNetworkRouteSpec{
					Pods:     api.ClusterNetworkRouteCIDRSpec{CIDRs: []string{"10.244.0.0/16"}},
					Services: api.ClusterNetworkRouteCIDRSpec{CIDRs: []string{"10.96.0.0/12"}},
					Via:      []api.ClusterNetworkRouteViaSpec{{Interface: "lan", NextHop: "192.168.50.21"}},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate cluster route: %v", err)
	}

	spec := router.Spec.Resources[1].Spec.(api.ClusterNetworkRouteSpec)
	spec.Services.CIDRs = []string{"10.244.10.0/24"}
	router.Spec.Resources[1].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected overlap validation error, got %v", err)
	}
	spec.Services.CIDRs = []string{"10.96.0.0/12"}
	spec.Via[0].Interface = "missing"
	router.Spec.Resources[1].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing Interface") {
		t.Fatalf("expected missing interface validation error, got %v", err)
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
					Profile:   api.IPv6PDProfileNTTNGNDirectHikariDenwa,
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate prefix delegation identity: %v", err)
	}

	router.Spec.Resources[1].Spec = api.DHCPv6PrefixDelegationSpec{Interface: "wan", ClientDUID: "00030001020000000103"}
	if err := Validate(router); err != nil {
		t.Fatalf("expected fixed clientDUID to validate, got %v", err)
	}

	router.Spec.Resources[1].Spec = api.DHCPv6PrefixDelegationSpec{Interface: "wan", ClientDUID: "00:03"}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.clientDUID must be plain hex") {
		t.Fatalf("expected clientDUID to be rejected, got %v", err)
	}

	router.Spec.Resources[1].Spec = api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "not-an-iaid"}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected IAID to be rejected, got %v", err)
	}

	router.Spec.Resources[1].Spec = api.DHCPv6PrefixDelegationSpec{Interface: "wan", DUIDType: "unknown"}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected duidType to be rejected, got %v", err)
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

func TestValidateNAT44RuleRequiresValidCIDR(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.NAT44RuleSpec{
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "ds-lite-source"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "wan", Address: "203.0.113.10/32"},
			},
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
	router.Spec.Resources[2].Spec = api.NAT44RuleSpec{Type: "snat", EgressInterface: "wan", SourceRanges: []string{"192.168.0.0/16"}}
	if err := Validate(router); err == nil {
		t.Fatal("expected snat without snatAddress or snatAddressFrom to be rejected")
	}
	router.Spec.Resources[2].Spec = api.NAT44RuleSpec{
		Type:            "snat",
		EgressInterface: "wan",
		SourceRanges:    []string{"192.168.0.0/16"},
		SNATAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-source", Field: "address"},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate snatAddressFrom: %v", err)
	}
}

func TestValidatePortForwardAndIngressService(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "wan-ip"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "wan", Address: "203.0.113.10/32"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "PortForward"},
				Metadata: api.ObjectMeta{Name: "web-admin"},
				Spec: api.PortForwardSpec{
					Listen: api.IngressListenSpec{Interface: "wan", AddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/wan-ip", Field: "address"}, Protocol: "tcp", Port: 8443},
					Target: api.IngressTargetSpec{Address: "172.18.1.88", Port: 443},
					Hairpin: api.IngressHairpinSpec{
						Enabled:    true,
						Interfaces: []string{"lan"},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
				Metadata: api.ObjectMeta{Name: "app"},
				Spec: api.IngressServiceSpec{
					Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
					Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate ingress resources: %v", err)
	}

	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:      api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends:    []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}, {Address: "172.18.1.90", Port: 8443}},
		HealthCheck: api.IngressHealthCheckSpec{Protocol: "https", Interval: "5s", Timeout: "1s", Path: "/readyz", Host: "k8s-api.example", ExpectedStatus: []int{200, 204}, HealthyThreshold: 2, UnhealthyThreshold: 2},
		Policy:      api.IngressServicePolicySpec{Selection: "failover", OnNoHealthyBackends: "reject"},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate ingress backend pool: %v", err)
	}
	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:      api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends:    []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}},
		HealthCheck: api.IngressHealthCheckSpec{Protocol: "https", Path: "readyz"},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.healthCheck.path") {
		t.Fatalf("expected invalid healthCheck path to be rejected, got %v", err)
	}

	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}, {Address: "172.18.1.90", Port: 8443}},
		Policy:   api.IngressServicePolicySpec{Selection: "random"},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate random ingress selection: %v", err)
	}
	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}, {Address: "172.18.1.90", Port: 8443}},
		Policy:   api.IngressServicePolicySpec{Selection: "sticky"},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.policy.selection must be failover, sourceHash, or random") {
		t.Fatalf("expected unsupported ingress selection to be rejected, got %v", err)
	}
	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}},
		Hairpin:  api.IngressHairpinSpec{Mode: "auto"},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate auto ingress hairpin: %v", err)
	}

	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}},
		Hairpin:  api.IngressHairpinSpec{Enabled: true, Interfaces: []string{"wan"}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate same-interface ingress hairpin: %v", err)
	}

	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}},
		Hairpin:  api.IngressHairpinSpec{Mode: "invalid"},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.hairpin.mode") {
		t.Fatalf("expected invalid hairpin mode to be rejected, got %v", err)
	}
	router.Spec.Resources[4].Spec = api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 443},
		Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 8443}},
	}

	router.Spec.Resources[3].Spec = api.PortForwardSpec{
		Listen:  api.IngressListenSpec{Interface: "wan", Protocol: "tcp", Port: 8443},
		Target:  api.IngressTargetSpec{Address: "172.18.1.88", Port: 443},
		Hairpin: api.IngressHairpinSpec{Enabled: true, Interfaces: []string{"lan"}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "requires spec.listen.address") {
		t.Fatalf("expected hairpin without listen address to be rejected, got %v", err)
	}
}

func TestValidateLocalServiceRedirect(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"},
				Metadata: api.ObjectMeta{Name: "public-dns"},
				Spec: api.IPAddressSetSpec{
					Addresses: []string{"8.8.8.8"},
					Names:     []string{"time.google.com"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"},
				Metadata: api.ObjectMeta{Name: "lan-local-services"},
				Spec: api.LocalServiceRedirectSpec{
					Interface: "lan",
					Rules: []api.LocalServiceRedirectRuleSpec{{
						Name:              "plain-dns",
						Protocols:         []string{"udp", "tcp"},
						DestinationSetRef: "public-dns",
						DestinationPort:   53,
						RedirectPort:      53,
					}},
				},
			},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate LocalServiceRedirect: %v", err)
	}
	router.Spec.Resources[2].Spec = api.LocalServiceRedirectSpec{
		Interface: "lan",
		Rules: []api.LocalServiceRedirectRuleSpec{{
			Protocols:       []string{"udp"},
			DestinationPort: 123,
			RedirectPort:    123,
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "destinationSetRef") {
		t.Fatalf("expected destination requirement error, got %v", err)
	}
}

func TestValidateFirewallRuleAddressSetRef(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud-service"}, Spec: api.IPAddressSetSpec{Names: []string{"service.example.test"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "lan-to-cloud"}, Spec: api.FirewallRuleSpec{
				FromZone:           "lan",
				ToZone:             "wan",
				Protocol:           "tcp",
				Port:               443,
				DestinationSetRefs: []string{"IPAddressSet/cloud-service"},
				Action:             "accept",
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate FirewallRule IPAddressSet ref: %v", err)
	}
	router.Spec.Resources[5].Spec = api.FirewallRuleSpec{
		FromZone:           "lan",
		ToZone:             "wan",
		DestinationSetRefs: []string{"IPAddressSet/missing"},
		Action:             "accept",
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing IPAddressSet") {
		t.Fatalf("expected missing IPAddressSet error, got %v", err)
	}
}

func TestValidateNAT44RuleRejectsInvalidPortRange(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.NAT44RuleSpec{
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

func TestValidateNAT44RuleRejectsMixedShapes(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.NAT44RuleSpec{
					Type:              "masquerade",
					EgressInterface:   "wan",
					SourceRanges:      []string{"192.168.10.0/24"},
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}

	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "must not mix") {
		t.Fatalf("expected mixed NAT44Rule shape to be rejected, got %v", err)
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

func TestValidateFirewallRuleStatefulExpressions(t *testing.T) {
	base := func(spec api.FirewallRuleSpec) *api.Router {
		return &api.Router{
			TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
			Metadata: api.ObjectMeta{Name: "test"},
			Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "rule"}, Spec: spec},
			}},
		}
	}
	valid := api.FirewallRuleSpec{
		FromZone:         "wan",
		ToZone:           "self",
		Protocol:         "tcp",
		DestinationPorts: []api.FirewallPort{"22", "443"},
		Action:           "reject",
		RateLimit:        api.FirewallRateLimitSpec{Rate: 10, Burst: 20, Unit: "packet", Per: "second"},
		ConnLimit:        api.FirewallConnLimitSpec{MaxPerSource: 4},
	}
	if err := Validate(base(valid)); err != nil {
		t.Fatalf("valid stateful firewall rule rejected: %v", err)
	}
	for _, tt := range []struct {
		name string
		spec api.FirewallRuleSpec
		want string
	}{
		{name: "range mixed with list", spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", Protocol: "tcp", DestinationPorts: []api.FirewallPort{"80-90", "443"}, Action: "accept"}, want: "cannot mix"},
		{name: "port without tcp udp", spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", Protocol: "icmp", DestinationPorts: []api.FirewallPort{"443"}, Action: "accept"}, want: "require protocol tcp or udp"},
		{name: "icmp type with tcp", spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", Protocol: "tcp", ICMPType: "echo-request", Action: "accept"}, want: "requires protocol icmp"},
		{name: "bad icmpv6 type", spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", Protocol: "icmpv6", ICMPv6Type: "bogus", Action: "accept"}, want: "not supported"},
		{name: "bad rate unit", spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", Protocol: "tcp", DestinationPorts: []api.FirewallPort{"22"}, Action: "drop", RateLimit: api.FirewallRateLimitSpec{Rate: 1, Unit: "bit", Per: "second"}}, want: "unit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(base(tt.spec))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateClientPolicy(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "lan-v4"},
				Spec: api.DHCPv4ServerSpec{
					Server:      "dnsmasq",
					Managed:     true,
					Interface:   "lan",
					AddressPool: api.DHCPAddressPoolSpec{Start: "172.18.0.64", End: "172.18.0.191"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"},
				Metadata: api.ObjectMeta{Name: "aiseg2"},
				Spec:     api.DHCPv4ReservationSpec{Server: "lan-v4", MACAddress: "18:ec:e7:33:12:6c", Hostname: "aiseg2", IPAddress: "172.18.0.150"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
				Metadata: api.ObjectMeta{Name: "guest-devices"},
				Spec: api.ClientPolicySpec{
					Mode:          "include",
					Interfaces:    []string{"Interface/lan"},
					GuestServices: []string{"dns", "dhcp", "ntp"},
					Classification: []api.ClientPolicyClassSpec{{
						Mode:            "guest",
						Name:            "aiseg2",
						Match:           api.ClientPolicyClassMatchSpec{MACs: []string{"18:ec:e7:33:12:6c"}, OUIPrefixes: []string{"18:ec:e7"}, HostnamePatterns: []string{"aiseg*"}, DHCPFingerprints: []string{"android-dhcp"}},
						IPv4Reservation: "aiseg2",
					}},
				},
			},
		}},
	}

	if err := Validate(router); err != nil {
		t.Fatalf("validate client policy: %v", err)
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
