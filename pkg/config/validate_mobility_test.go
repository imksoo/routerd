// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/samenrollment"
)

func TestValidateMobilityPool(t *testing.T) {
	router := mobilityPoolRouter(mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:                  "provider-private-ip",
					StoppedInstancePolicy: "release",
				},
			},
		},
	}, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolRejectsOverlappingPrefixes(t *testing.T) {
	router := mobilityPoolRouter(mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge-a",
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-a", Site: "onprem-a", Role: "onprem"},
			{NodeRef: "cloud-a", Site: "cloud-a", Role: "cloud"},
		},
	}, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge-b"},
		Spec: api.MobilityPoolSpec{
			Prefix:      "10.88.60.128/25",
			GroupRef:    "cloudedge-b",
			MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge-b-members"}},
		},
	}, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: "cloudedge-b-members"},
		Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
			{NodeRef: "onprem-b", Site: "onprem-b", Role: "onprem"},
			{NodeRef: "cloud-b", Site: "cloud-b", Role: "cloud"},
		}},
	}, testEventGroupResource("cloudedge-b", "onprem-b"))
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "MobilityPool prefixes must be disjoint") {
		t.Fatalf("Validate overlap error = %v, want disjoint MobilityPool prefix error", err)
	}
}

func TestValidateSAMTransportProfile(t *testing.T) {
	router := samTransportProfileRouter(validSAMTransportProfileSpec())
	if err := Validate(router); err != nil {
		t.Fatalf("Validate SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportRRWithoutPoolRequiresExplicitTransitPrefixes(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.BGP.RouteReflectorClient = true
	err := Validate(samTransportProfileRouter(spec))
	if err == nil || !strings.Contains(err.Error(), "spec.bgp.importPolicy.allowedPrefixes is required") {
		t.Fatalf("Validate no-pool RR = %v, want explicit transit prefix error", err)
	}

	spec.BGP.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes: []string{"10.77.60.11/24", "10.255.1.0/24"},
	}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate legacy no-pool RR with omitted length bounds: %v", err)
	}

	spec.BGP.ImportPolicy.AllowedPrefixLengthMin = 32
	spec.BGP.ImportPolicy.AllowedPrefixLengthMax = 32
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate no-pool RR with explicit /32 bounds: %v", err)
	}

	spec.BGP.ImportPolicy.AllowedPrefixLengthMin = 0
	spec.BGP.ImportPolicy.AllowedPrefixLengthMax = 32
	if err := Validate(samTransportProfileRouter(spec)); err == nil || !strings.Contains(err.Error(), "must be omitted or both be 32") {
		t.Fatalf("Validate partial no-pool RR bounds = %v, want explicit /32 constraint error", err)
	}

	spec.BGP.ImportPolicy.AllowedPrefixLengthMin = 24
	spec.BGP.ImportPolicy.AllowedPrefixLengthMax = 24
	if err := Validate(samTransportProfileRouter(spec)); err == nil || !strings.Contains(err.Error(), "must be omitted or both be 32") {
		t.Fatalf("Validate broad no-pool RR bounds = %v, want explicit /32 constraint error", err)
	}
}

func TestValidateSAMTransportProfileRejectsInvalidFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*api.SAMTransportProfileSpec)
		want string
	}{
		{
			name: "missing self node",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.SelfNodeRef = "" },
			want: "spec.selfNodeRef is required",
		},
		{
			name: "missing peer source",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.PeersFrom = nil },
			want: "spec.peersFrom or spec.publishPeerGroup requires",
		},
		{
			name: "duplicate selected node",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.PeersFrom[0].NodeRefs = []string{"k8s-rt", "k8s-rt"}
			},
			want: "nodeRefs nodeRef \"k8s-rt\" is duplicated",
		},
		{
			name: "empty selected node",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.PeersFrom[0].NodeRefs = []string{""}
			},
			want: "nodeRefs[0] must not be empty",
		},
		{
			name: "invalid addressing mode",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.AddressingMode = "invalid-mode" },
			want: "spec.addressingMode must be edge-index or pair-stable",
		},
		{
			name: "missing underlay interface",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.UnderlayInterface = "missing" },
			want: "references missing Interface",
		},
		{
			name: "invalid route reflector cluster id",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.BGP.RouteReflectorClusterID = "not-an-ip" },
			want: "spec.bgp.routeReflectorClusterID must be an IPv4 address",
		},
		{
			name: "fou requires encap ports",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.Mode = "fou"
			},
			want: "spec.encapSport is required",
		},
		{
			name: "encap ports require fou or gue",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.EncapSport = 5555
				spec.EncapDport = 5555
			},
			want: "spec.encapSport/spec.encapDport are only supported when spec.mode is fou or gue",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSAMTransportProfileSpec()
			tc.mut(&spec)
			err := Validate(samTransportProfileRouter(spec))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateSAMTransportProfileAllowsPairStableWithoutSharedTopology(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.PeersFrom[0].NodeRefs = []string{"k8s-rt"}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate pair-stable SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileAllowsPeersFromWithoutPeers(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate peersFrom SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileDirectPeerSourceRequiresSafeShape(t *testing.T) {
	base := validSAMTransportProfileSpec()
	base.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "spec.addressingMode must be pair-stable") {
		t.Fatalf("Validate direct source without pair-stable mode = %v, want pair-stable rejection", err)
	}

	base.AddressingMode = "pair-stable"
	base.BGP.RouteReflectorClient = true
	base.BGP.ImportPolicy.AllowedPrefixes = []string{"10.77.60.0/24"}
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "routeReflectorClient must be false") {
		t.Fatalf("Validate direct source on RR client = %v, want RR client rejection", err)
	}

	base.BGP.RouteReflectorClient = false
	base.BGP.ImportPolicy.LocalPreference = 0
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "importPolicy.localPreference is required") {
		t.Fatalf("Validate direct source without explicit RR preference = %v, want preference requirement", err)
	}

	base.BGP.ImportPolicy.AllowedPrefixLengthMin = 32
	base.BGP.ImportPolicy.AllowedPrefixLengthMax = 32
	base.BGP.ImportPolicy.LocalPreference = 100
	base.BGP.ImportPolicy.NextHopRewrite = "unchanged"
	if err := Validate(samTransportProfileRouter(base)); err != nil {
		t.Fatalf("Validate direct source with legacy unchanged RR next hop = %v, want upgrade-compatible acceptance", err)
	}
	base.BGP.ImportPolicy.NextHopRewrite = ""
	base.BGP.DirectLocalPreference = 100
	base.BGP.ImportPolicy.LocalPreference = 100
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "directLocalPreference must exceed") {
		t.Fatalf("Validate direct source below RR preference = %v, want preference rejection", err)
	}

	base.BGP.DirectLocalPreference = 0
	base.BGP.ImportPolicy.LocalPreference = 200
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "directLocalPreference must exceed") {
		t.Fatalf("Validate direct source with equal RR preference = %v, want preference rejection", err)
	}

	base.BGP.ImportPolicy.LocalPreference = 100
	base.BGP.ImportPolicy.AllowedPrefixes = nil
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "importPolicy.allowedPrefixes is required") {
		t.Fatalf("Validate direct source without explicit import allowlist = %v, want allowlist requirement", err)
	}

	base.BGP.ImportPolicy.AllowedPrefixes = []string{"10.77.60.0/24"}
	base.BGP.ImportPolicy.AllowedPrefixLengthMin = 0
	base.BGP.ImportPolicy.AllowedPrefixLengthMax = 0
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "allowedPrefixLengthMin and allowedPrefixLengthMax must both be 32") {
		t.Fatalf("Validate direct source without /32 bounds = %v, want exact-prefix requirement", err)
	}

	base.BGP.ImportPolicy.AllowedPrefixLengthMin = 32
	base.BGP.ImportPolicy.AllowedPrefixLengthMax = 32
	base.BGP.ImportPolicy.AllowedPrefixes = []string{"not-a-cidr"}
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "must be an IPv4 or IPv6 prefix") {
		t.Fatalf("Validate direct source with invalid allowlist entry = %v, want CIDR rejection", err)
	}

	base.BGP.ImportPolicy.AllowedPrefixes = []string{"2001:db8::/64"}
	if err := Validate(samTransportProfileRouter(base)); err == nil || !strings.Contains(err.Error(), "must be an IPv4 CIDR") {
		t.Fatalf("Validate direct source with IPv6 allowlist entry = %v, want IPv4 rejection", err)
	}

	base.BGP.ImportPolicy.AllowedPrefixes = []string{"10.77.60.0/24"}
	if err := Validate(samTransportProfileRouter(base)); err != nil {
		t.Fatalf("Validate direct source with safe RR fallback = %v", err)
	}
}

func TestValidateSAMTransportProfileDirectPeerSourceRequiresRRFallback(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true}}
	if err := Validate(samTransportProfileRouter(spec)); err == nil || !strings.Contains(err.Error(), "requires a preceding non-optional SAMRRSet") {
		t.Fatalf("Validate direct-only source = %v, want RR fallback rejection", err)
	}

	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs", Optional: true},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	if err := Validate(samTransportProfileRouter(spec)); err == nil || !strings.Contains(err.Error(), "requires a preceding non-optional SAMRRSet") {
		t.Fatalf("Validate optional RR fallback = %v, want RR fallback rejection", err)
	}

	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
		{Resource: "SAMNodeSet/late"},
	}
	if err := Validate(samTransportProfileRouter(spec)); err == nil || !strings.Contains(err.Error(), "direct must be the final peer source") {
		t.Fatalf("Validate direct source followed by another source = %v, want terminal-source rejection", err)
	}
}

func TestValidateSAMTransportProfileAllowsSAMNodeSetPeersFromWithoutPeers(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate SAMNodeSet peersFrom SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileAllowsPublishPeerGroupWithoutPeers(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.PeersFrom = nil
	spec.PublishPeerGroup = true
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate publish-only SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileRejectsInvalidPeersFrom(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "BGPPeer/rr"}}
	err := Validate(samTransportProfileRouter(spec))
	if err == nil || !strings.Contains(err.Error(), "spec.peersFrom[0].resource must reference SAMPeerGroup/<name>, SAMNodeSet/<name>, SAMEnrollmentPolicy/<name>, or SAMRRSet/<name>") {
		t.Fatalf("Validate peersFrom error = %v, want SAMPeerGroup/SAMNodeSet/SAMEnrollmentPolicy ref error", err)
	}
}

func TestValidateSAMTransportProfileRejectsMissingSAMNodeSetSource(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/missing"}}
	err := Validate(samTransportProfileRouter(spec))
	if err == nil || !strings.Contains(err.Error(), `spec.peersFrom[0].resource references missing SAMNodeSet "SAMNodeSet/missing"`) {
		t.Fatalf("Validate missing SAMNodeSet source error = %v, want missing SAMNodeSet error", err)
	}
}

func TestValidateSAMTransportProfileRejectsNodeRefOutsideSAMNodeSet(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.PeersFrom[0].NodeRefs = []string{"missing"}
	err := Validate(samTransportProfileRouter(spec))
	if err == nil || !strings.Contains(err.Error(), `spec.peersFrom[0].nodeRefs[0] "missing" is not present in SAMNodeSet/svnet1-nodes`) {
		t.Fatalf("Validate missing SAMNodeSet nodeRef error = %v, want nodeRef membership error", err)
	}
}

func TestValidateMobilityPoolAllowsMembersFromWithoutMembers(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:      "10.88.60.0/24",
		GroupRef:    "cloudedge",
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/svnet1-members"}},
	}
	nodeSet := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: "svnet1-members"},
		Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{NodeRef: "aws-router", Site: "aws", Role: "cloud"},
		}},
	}
	if err := Validate(mobilityPoolRouter(spec, nodeSet, testEventGroupResource("cloudedge", "onprem-router"))); err != nil {
		t.Fatalf("Validate SAMNodeSet membersFrom MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolResolvesNodeSetIdentityForSelfOverlay(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:      "10.88.60.0/24",
		GroupRef:    "cloudedge",
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge-nodes"}},
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"aws-self": {Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "aws-provider",
				NICRef:      "eni-self",
			}},
		}},
		Members: []api.MobilityPoolMemberOverlay{{
			NodeRef:    "aws-router",
			ProfileRef: "aws-self",
		}},
	}
	nodeSet := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: "cloudedge-nodes"},
		Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
			NodeRef: "aws-router",
			Site:    "aws",
			Role:    "cloud",
		}}},
	}
	if err := ValidateForOS(mobilityPoolRouter(spec, nodeSet, testEventGroupResource("cloudedge", "aws-router")), platform.OSLinux); err != nil {
		t.Fatalf("Validate NodeSet-backed self overlay: %v", err)
	}

	spec.MembersFrom = nil
	err := ValidateForOS(mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "aws-router")), platform.OSLinux)
	if err == nil || !strings.Contains(err.Error(), "spec.membersFrom requires at least one SAMNodeSet source") {
		t.Fatalf("Validate missing membersFrom error = %v, want source requirement", err)
	}
}

func TestValidateMobilityPoolRejectsInvalidMembersFrom(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:      "10.88.60.0/24",
		GroupRef:    "cloudedge",
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}},
	}
	err := Validate(mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "test-local-node")))
	if err == nil || !strings.Contains(err.Error(), "spec.membersFrom[0].resource must reference SAMNodeSet/<name>") {
		t.Fatalf("Validate membersFrom error = %v, want SAMNodeSet ref error", err)
	}
}

func TestValidateSAMNodeSet(t *testing.T) {
	router := samNodeSetRouter(api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
		NodeRef:        "pve-rt01",
		Site:           "pve01",
		Role:           "onprem",
		EventEndpoint:  "http://10.99.0.11:9443",
		SAMEndpoint:    "10.99.0.11",
		MACAddresses:   []string{"02:00:00:00:00:aa", "02:00:00:00:00:bb"},
		RouteReflector: true,
		WireGuard: api.SAMNodeWireGuardSpec{
			PublicKey:           "pubkey",
			Endpoint:            "pve-rt01.example.net:51820",
			AllowedIPs:          []string{"10.99.0.11/32"},
			PersistentKeepalive: 25,
		},
	}}})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate SAMNodeSet: %v", err)
	}
}

func TestValidateSAMNodeSetPlacementAndCapacity(t *testing.T) {
	valid := validSAMNodeSetSpec()
	valid.Nodes[0].Role = "cloud"
	valid.Nodes[0].Placement = api.MobilityMemberPlacement{Group: "aws-a", Priority: 10}
	valid.Nodes[0].MaxSecondaryIPs = 32
	if err := Validate(samNodeSetRouter(valid)); err != nil {
		t.Fatalf("Validate SAMNodeSet cloud placement: %v", err)
	}

	tests := []struct {
		name string
		mut  func(*api.SAMNodeSetSpec)
		want string
	}{
		{
			name: "negative capacity",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].MaxSecondaryIPs = -1 },
			want: "maxSecondaryIPs must be >= 0",
		},
		{
			name: "capacity requires group",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].MaxSecondaryIPs = 1 },
			want: "maxSecondaryIPs requires placement.group",
		},
		{
			name: "priority requires group",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].Placement.Priority = 1 },
			want: "placement.priority requires placement.group",
		},
		{
			name: "drain requires group",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].Maintenance.Drain = true },
			want: "maintenance.drain requires placement.group",
		},
		{
			name: "group requires cloud role",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].Placement.Group = "aws-a" },
			want: "placement.group is supported only for role cloud",
		},
		{
			name: "priority range",
			mut: func(spec *api.SAMNodeSetSpec) {
				spec.Nodes[0].Role = "cloud"
				spec.Nodes[0].Placement = api.MobilityMemberPlacement{Group: "aws-a", Priority: 1000001}
			},
			want: "placement.priority must be between 0 and 1000000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSAMNodeSetSpec()
			tt.mut(&spec)
			err := Validate(samNodeSetRouter(spec))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate SAMNodeSet placement error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateSAMNodeSetRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*api.SAMNodeSetSpec)
		want string
	}{
		{
			name: "duplicate nodeRef",
			mut: func(spec *api.SAMNodeSetSpec) {
				spec.Nodes = append(spec.Nodes, spec.Nodes[0])
			},
			want: `spec.nodes nodeRef "pve-rt01" is duplicated`,
		},
		{
			name: "invalid role",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].Role = "edge" },
			want: "spec.nodes[0].role must be onprem or cloud",
		},
		{
			name: "invalid event endpoint",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].EventEndpoint = "grpc://10.99.0.11:9443" },
			want: "spec.nodes[0].eventEndpoint: must use http or https",
		},
		{
			name: "invalid sam endpoint",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].SAMEndpoint = "fd00::1" },
			want: "spec.nodes[0].samEndpoint: must be IPv4",
		},
		{
			name: "invalid member mac address",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].MACAddresses = []string{"not-a-mac"} },
			want: "spec.nodes[0].macAddresses[0] must be a MAC address",
		},
		{
			name: "sam endpoint with source",
			mut: func(spec *api.SAMNodeSetSpec) {
				spec.Nodes[0].SAMEndpointFrom = api.StatusValueSourceSpec{Resource: "DHCPv4Client/wan", Field: "currentAddress"}
			},
			want: "spec.nodes[0].samEndpoint and samEndpointFrom are mutually exclusive",
		},
		{
			name: "sam endpoint source missing field",
			mut: func(spec *api.SAMNodeSetSpec) {
				spec.Nodes[0].SAMEndpoint = ""
				spec.Nodes[0].SAMEndpointFrom = api.StatusValueSourceSpec{Resource: "DHCPv4Client/wan"}
			},
			want: "spec.nodes[0].samEndpointFrom.field is required",
		},
		{
			name: "wireguard public key required",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].WireGuard.PublicKey = "" },
			want: "spec.nodes[0].wireGuard.publicKey is required when wireGuard is set",
		},
		{
			name: "wireguard allowed IP required",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].WireGuard.AllowedIPs = nil },
			want: "spec.nodes[0].wireGuard.allowedIPs is required when wireGuard is set",
		},
		{
			name: "wireguard endpoint host port",
			mut:  func(spec *api.SAMNodeSetSpec) { spec.Nodes[0].WireGuard.Endpoint = "missing-port" },
			want: "spec.nodes[0].wireGuard.endpoint: must be host:port",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSAMNodeSetSpec()
			tt.mut(&spec)
			err := Validate(samNodeSetRouter(spec))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate SAMNodeSet error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateSAMEnrollmentPolicyAndClaim(t *testing.T) {
	router := samEnrollmentRouter()
	if err := Validate(router); err != nil {
		t.Fatalf("Validate SAMEnrollmentPolicy/SAMEnrollmentClaim: %v", err)
	}

	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.LeafID = "bad leaf"
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.leafID") {
		t.Fatalf("expected leafID policy error, got %v", err)
	}

	claim.LeafID = "leaf-pve"
	claim.TunnelAddress = "10.254.0.21/32"
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.tunnelAddress") {
		t.Fatalf("expected tunnelAddress policy error, got %v", err)
	}

	claim.TunnelAddress = "10.255.0.21/32"
	claim.Mobility.OwnedAddresses = []string{"10.88.60.21/32"}
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "outside authorized mobility prefixes") {
		t.Fatalf("expected MobilityPool authorization error, got %v", err)
	}
}

func TestValidateSAMEnrollmentClaimDirectMeshRequiresPolicyOptIn(t *testing.T) {
	router := samEnrollmentRouter()
	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.DirectMesh = true
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.directMesh requires SAMEnrollmentPolicy/cloudedge-leaves spec.directMesh.peerGroupRef") {
		t.Fatalf("Validate direct claim without policy direct mesh = %v, want policy opt-in rejection", err)
	}

	policyIndex := claimIndex - 1
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.DirectMesh.PeerGroupRef = "SAMPeerGroup/cloudedge-direct-leaves"
	policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
	policy.RRNodeSetRef = "SAMNodeSet/cloudedge-members"
	router.Spec.Resources[policyIndex].Spec = policy
	claim.RRSetRef = policy.RRSetRef
	router.Spec.Resources[claimIndex].Spec = claim
	for i := range router.Spec.Resources {
		if router.Spec.Resources[i].APIVersion == api.MobilityAPIVersion && router.Spec.Resources[i].Kind == "SAMNodeSet" && router.Spec.Resources[i].Metadata.Name == "cloudedge-members" {
			nodes := router.Spec.Resources[i].Spec.(api.SAMNodeSetSpec)
			for j := range nodes.Nodes {
				nodes.Nodes[j].RouteReflector = true
			}
			router.Spec.Resources[i].Spec = nodes
		}
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.addressingMode must be pair-stable") {
		t.Fatalf("Validate direct claim with edge-index policy transport = %v, want pair-stable rejection", err)
	}
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" || resource.Metadata.Name != "aws-rr-a" {
			continue
		}
		transport := resource.Spec.(api.SAMTransportProfileSpec)
		transport.AddressingMode = "pair-stable"
		router.Spec.Resources[i].Spec = transport
		break
	}
	if err := Validate(router); err != nil {
		t.Fatalf("Validate direct claim with policy opt-in = %v", err)
	}
}

func TestValidateSAMEnrollmentPoliciesRequireUniqueRuntimeTopologyRefs(t *testing.T) {
	router := samEnrollmentRouter()
	var policy api.SAMEnrollmentPolicySpec
	policyIndex := -1
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != "cloudedge-leaves" {
			continue
		}
		var err error
		policy, err = resource.SAMEnrollmentPolicySpec()
		if err != nil {
			t.Fatalf("policy spec: %v", err)
		}
		policyIndex = i
		break
	}
	if policyIndex < 0 {
		t.Fatal("cloudedge-leaves policy not found")
	}
	policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
	policy.RRNodeSetRef = "SAMNodeSet/cloudedge-members"
	policy.DirectMesh.PeerGroupRef = "SAMPeerGroup/cloudedge-direct-leaves"
	router.Spec.Resources[policyIndex].Spec = policy
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" || resource.Metadata.Name != "aws-rr-a" {
			continue
		}
		transport := resource.Spec.(api.SAMTransportProfileSpec)
		transport.AddressingMode = "pair-stable"
		router.Spec.Resources[i].Spec = transport
		break
	}
	claim := router.Spec.Resources[len(router.Spec.Resources)-1].Spec.(api.SAMEnrollmentClaimSpec)
	claim.RRSetRef = policy.RRSetRef
	router.Spec.Resources[len(router.Spec.Resources)-1].Spec = claim
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMNodeSet" || resource.Metadata.Name != "cloudedge-members" {
			continue
		}
		nodes, err := resource.SAMNodeSetSpec()
		if err != nil {
			t.Fatalf("RR node set spec: %v", err)
		}
		for j := range nodes.Nodes {
			nodes.Nodes[j].RouteReflector = true
		}
		router.Spec.Resources[i].Spec = nodes
	}

	duplicate := policy
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
		Metadata: api.ObjectMeta{Name: "other-leaves"},
		Spec:     duplicate,
	})
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "SAM enrollment RRSet refs must be unique per policy") {
		t.Fatalf("Validate duplicate RRSet ref = %v, want unique RRSet rejection", err)
	}

	duplicate.RRSetRef = "SAMRRSet/other-rrs"
	router.Spec.Resources[len(router.Spec.Resources)-1].Spec = duplicate
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "SAM enrollment direct peer-group refs must be unique per policy") {
		t.Fatalf("Validate duplicate direct peer-group ref = %v, want unique peer-group rejection", err)
	}

	duplicate.DirectMesh.PeerGroupRef = "SAMPeerGroup/other-direct-leaves"
	router.Spec.Resources[len(router.Spec.Resources)-1].Spec = duplicate
	if err := Validate(router); err != nil {
		t.Fatalf("Validate unique enrollment runtime topology refs: %v", err)
	}
}

func TestValidateSAMEnrollmentPolicyAllowsDirectMobilityPrefixesWithoutPool(t *testing.T) {
	router := samEnrollmentRouter()
	filtered := router.Spec.Resources[:0]
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityPool" {
			continue
		}
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentPolicy" {
			policy := resource.Spec.(api.SAMEnrollmentPolicySpec)
			policy.MobilityPoolRefs = nil
			policy.MobilityPrefixes = []string{"10.77.60.0/24"}
			resource.Spec = policy
		}
		filtered = append(filtered, resource)
	}
	router.Spec.Resources = filtered
	if err := Validate(router); err != nil {
		t.Fatalf("Validate SAMEnrollmentPolicy with direct mobilityPrefixes: %v", err)
	}

	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.Mobility.OwnedAddresses = []string{"10.88.60.21/32"}
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "outside authorized mobility prefixes") {
		t.Fatalf("expected direct mobilityPrefixes authorization error, got %v", err)
	}
}

func TestValidateFetchedSAMEnrollmentTopologyAllowsDirectLeafWithoutOwnedAddress(t *testing.T) {
	rrSet := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "svnet1-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/svnet1-leaves",
			Nodes: []api.SAMNodeSpec{
				{NodeRef: "pve-rr01", SAMEndpoint: "10.20.0.2", RouteReflector: true},
				{NodeRef: "pve-rr02", SAMEndpoint: "10.20.0.3", RouteReflector: true},
			},
		},
	}
	direct := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "svnet1-direct-leaves"},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  "SAMEnrollmentPolicy/svnet1-leaves",
			TransportFingerprint: "sha256:test-direct-mesh",
			Nodes: []api.SAMNodeSpec{
				{NodeRef: "pve-rt01", SAMEndpoint: "10.20.0.21"},
				{NodeRef: "pve-rt02", SAMEndpoint: "10.20.0.22"},
			},
			OwnedPrefixesByNode: map[string][]string{
				"pve-rt02": {"10.77.60.22/32"},
				// pve-rt01 is enrolled and directMesh=true but currently owns no
				// mobility /32. The runtime snapshot must still be accepted.
			},
		},
	}
	if err := ValidateFetchedSAMEnrollmentTopology(rrSet, &direct); err != nil {
		t.Fatalf("ValidateFetchedSAMEnrollmentTopology empty direct ownership: %v", err)
	}
}

func TestValidateSAMEnrollmentJoinTokenRequiresHMACFields(t *testing.T) {
	router := samEnrollmentRouter()
	policyIndex := len(router.Spec.Resources) - 2
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.JoinTokenFrom = api.SecretValueSourceSpec{Env: "ROUTERD_TEST_JOIN_TOKEN"}
	policy.JoinAudience = "cloudedge"
	router.Spec.Resources[policyIndex].Spec = policy

	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.joinNonce is required") {
		t.Fatalf("Validate missing join fields = %v, want joinNonce error", err)
	}

	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.JoinAudience = "cloudedge"
	claim.JoinNonce = "nonce-1"
	claim.JoinTimestamp = "2026-06-28T00:00:00Z"
	claim.JoinHMAC = "example-hmac"
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err != nil {
		t.Fatalf("Validate join-token enrollment with HMAC fields: %v", err)
	}

	claim.JoinAudience = "wrong"
	router.Spec.Resources[claimIndex].Spec = claim
	err = Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.joinAudience") {
		t.Fatalf("Validate join audience mismatch = %v, want joinAudience error", err)
	}
}

func TestValidateSAMEnrollmentJoinTokenVerifiesHMACWhenSecretAvailable(t *testing.T) {
	const envName = "ROUTERD_TEST_JOIN_TOKEN_VALUE"
	t.Setenv(envName, "test-join-token")
	router := samEnrollmentRouter()
	policyIndex := len(router.Spec.Resources) - 2
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.JoinTokenFrom = api.SecretValueSourceSpec{Env: envName}
	policy.JoinAudience = "cloudedge"
	router.Spec.Resources[policyIndex].Spec = policy

	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.JoinAudience = "cloudedge"
	claim.JoinNonce = "nonce-1"
	claim.JoinTimestamp = "2026-06-28T00:00:00Z"
	claim.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claim)
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err != nil {
		t.Fatalf("Validate join-token enrollment with valid HMAC: %v", err)
	}

	claim.JoinHMAC = "bad-hmac"
	router.Spec.Resources[claimIndex].Spec = claim
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.joinHMAC does not match") {
		t.Fatalf("Validate bad join HMAC = %v, want mismatch error", err)
	}
}

func TestValidateSAMEnrollmentJoinTokenRejectsDuplicateNonce(t *testing.T) {
	router := samEnrollmentRouter()
	policyIndex := len(router.Spec.Resources) - 2
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.JoinTokenFrom = api.SecretValueSourceSpec{Env: "ROUTERD_TEST_JOIN_TOKEN"}
	policy.JoinAudience = "cloudedge"
	router.Spec.Resources[policyIndex].Spec = policy

	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.JoinAudience = "cloudedge"
	claim.JoinNonce = "nonce-1"
	claim.JoinTimestamp = "2026-06-28T00:00:00Z"
	claim.JoinHMAC = "example-hmac"
	router.Spec.Resources[claimIndex].Spec = claim

	duplicate := router.Spec.Resources[claimIndex]
	duplicate.Metadata.Name = "leaf-other"
	duplicateClaim := duplicate.Spec.(api.SAMEnrollmentClaimSpec)
	duplicateClaim.LeafID = "leaf-other"
	duplicateClaim.TunnelAddress = "10.255.0.22/32"
	duplicateClaim.Endpoint = "198.51.100.22"
	duplicateClaim.Mobility.OwnedAddresses = []string{"10.77.60.22/32"}
	duplicate.Spec = duplicateClaim
	router.Spec.Resources = append(router.Spec.Resources, duplicate)

	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.joinNonce duplicates") {
		t.Fatalf("Validate duplicate join nonce = %v, want duplicate nonce error", err)
	}
}

func TestValidateSAMEnrollmentClaimRequiresExistingPolicy(t *testing.T) {
	router := samEnrollmentRouter()
	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.PolicyRef = "SAMEnrollmentPolicy/missing"
	router.Spec.Resources[claimIndex].Spec = claim
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "references missing SAMEnrollmentPolicy") {
		t.Fatalf("Validate missing enrollment policy = %v, want missing policy error", err)
	}
}

func TestValidateSAMEnrollmentClaimRejectsExpiresAtBeyondPolicyTTL(t *testing.T) {
	router := samEnrollmentRouter()
	policyIndex := len(router.Spec.Resources) - 2
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.TTL = "1h"
	router.Spec.Resources[policyIndex].Spec = policy
	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.JoinTimestamp = "2026-06-28T00:00:00Z"
	claim.ExpiresAt = "2026-06-28T02:00:00Z"
	router.Spec.Resources[claimIndex].Spec = claim
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.expiresAt") {
		t.Fatalf("Validate expiresAt beyond ttl = %v, want expiresAt error", err)
	}
}

func TestValidateSAMEnrollmentClaimRejectsWireGuardEndpointOutsidePolicy(t *testing.T) {
	router := samEnrollmentRouter()
	policyIndex := len(router.Spec.Resources) - 2
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.EndpointPrefixes = []string{"10.20.0.0/24"}
	policy.WireGuard.EndpointPrefixes = []string{"198.51.100.0/24"}
	router.Spec.Resources[policyIndex].Spec = policy
	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.Endpoint = "10.20.0.21"
	claim.WireGuard.Endpoint = "203.0.113.21:51820"
	router.Spec.Resources[claimIndex].Spec = claim
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.wireGuard.endpoint") {
		t.Fatalf("Validate WG endpoint outside policy = %v, want endpoint prefix error", err)
	}
}

func TestValidateSAMEnrollmentClientRequiresExistingLocalClaim(t *testing.T) {
	router := samEnrollmentRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClient"},
		Metadata: api.ObjectMeta{Name: "leaf-pve"},
		Spec: api.SAMEnrollmentClientSpec{
			ClaimRef:           "SAMEnrollmentClaim/missing-leaf",
			BootstrapEndpoints: []string{"http://10.30.0.10:65432"},
		},
	})
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), `spec.claimRef references missing SAMEnrollmentClaim "SAMEnrollmentClaim/missing-leaf"`) {
		t.Fatalf("Validate error = %v, want missing SAMEnrollmentClaim claimRef rejection", err)
	}
}

func TestValidateSAMEnrollmentClaimRejectsDuplicatePolicyFields(t *testing.T) {
	base := samEnrollmentRouter()
	claimIndex := len(base.Spec.Resources) - 1
	for _, tc := range []struct {
		name   string
		mutate func(api.SAMEnrollmentClaimSpec) api.SAMEnrollmentClaimSpec
		want   string
	}{
		{
			name: "leafID",
			mutate: func(claim api.SAMEnrollmentClaimSpec) api.SAMEnrollmentClaimSpec {
				claim.TunnelAddress = "10.255.0.22/32"
				claim.WireGuard.PublicKey = "leafpub-2"
				claim.Mobility.OwnedAddresses = []string{"10.77.60.22/32"}
				claim.BGP.RouterID = "10.255.0.22"
				return claim
			},
			want: "spec.leafID duplicates",
		},
		{
			name: "tunnelAddress",
			mutate: func(claim api.SAMEnrollmentClaimSpec) api.SAMEnrollmentClaimSpec {
				claim.LeafID = "leaf-other"
				claim.WireGuard.PublicKey = "leafpub-2"
				claim.Mobility.OwnedAddresses = []string{"10.77.60.22/32"}
				claim.BGP.RouterID = "10.255.0.22"
				return claim
			},
			want: "spec.tunnelAddress duplicates",
		},
		{
			name: "wireGuardPublicKey",
			mutate: func(claim api.SAMEnrollmentClaimSpec) api.SAMEnrollmentClaimSpec {
				claim.LeafID = "leaf-other"
				claim.TunnelAddress = "10.255.0.22/32"
				claim.Mobility.OwnedAddresses = []string{"10.77.60.22/32"}
				claim.BGP.RouterID = "10.255.0.22"
				return claim
			},
			want: "spec.wireGuard.publicKey duplicates",
		},
		{
			name: "mobilityOwnedAddress",
			mutate: func(claim api.SAMEnrollmentClaimSpec) api.SAMEnrollmentClaimSpec {
				claim.LeafID = "leaf-other"
				claim.TunnelAddress = "10.255.0.22/32"
				claim.WireGuard.PublicKey = "leafpub-2"
				claim.BGP.RouterID = "10.255.0.22"
				return claim
			},
			want: "spec.mobility.ownedAddresses[0] duplicates",
		},
		{
			name: "bgpRouterID",
			mutate: func(claim api.SAMEnrollmentClaimSpec) api.SAMEnrollmentClaimSpec {
				claim.LeafID = "leaf-other"
				claim.TunnelAddress = "10.255.0.22/32"
				claim.WireGuard.PublicKey = "leafpub-2"
				claim.Mobility.OwnedAddresses = []string{"10.77.60.22/32"}
				return claim
			},
			want: "spec.bgp.routerID duplicates",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := *base
			router.Spec.Resources = append([]api.Resource(nil), base.Spec.Resources...)
			duplicate := router.Spec.Resources[claimIndex]
			duplicate.Metadata.Name = "leaf-other"
			duplicate.Spec = tc.mutate(duplicate.Spec.(api.SAMEnrollmentClaimSpec))
			router.Spec.Resources = append(router.Spec.Resources, duplicate)
			err := Validate(&router)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate duplicate %s = %v, want %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestValidateSAMEnrollmentRejectsRRSetScopeMismatch(t *testing.T) {
	router := samEnrollmentRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: "rrs"},
		Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
			NodeRef: "aws-rr-a", RouteReflector: true, SAMEndpoint: "10.99.0.2",
		}}},
	})
	policyIndex := len(router.Spec.Resources) - 3
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
	policy.RRNodeSetRef = "SAMNodeSet/rrs"
	router.Spec.Resources[policyIndex].Spec = policy
	claimIndex := len(router.Spec.Resources) - 2
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.RRSetRef = "SAMRRSet/other"
	router.Spec.Resources[claimIndex].Spec = claim
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.rrSetRef") {
		t.Fatalf("Validate RRSet scope mismatch = %v, want rrSetRef error", err)
	}
}

func TestValidateSAMEnrollmentAllowsNonWireGuardClaim(t *testing.T) {
	router := samEnrollmentRouter()
	router.Spec.Resources = append(router.Spec.Resources[:0], router.Spec.Resources[1:]...)
	profileIndex := -1
	policyIndex := -1
	claimIndex := -1
	for i, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "SAMTransportProfile":
			profileIndex = i
		case "SAMEnrollmentPolicy":
			policyIndex = i
		case "SAMEnrollmentClaim":
			claimIndex = i
		}
	}
	profile := router.Spec.Resources[profileIndex].Spec.(api.SAMTransportProfileSpec)
	profile.UnderlayInterface = "private-wan"
	profile.Encryption = "none"
	router.Spec.Resources[profileIndex].Spec = profile
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "private-wan"},
		Spec:     api.InterfaceSpec{IfName: "private-wan", Managed: false},
	})
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.WireGuard = api.SAMEnrollmentWireGuardSpec{}
	policy.EndpointPrefixes = []string{"10.20.0.0/24"}
	router.Spec.Resources[policyIndex].Spec = policy
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.Endpoint = "10.20.0.21"
	claim.WireGuard = api.SAMEnrollmentClaimWireGuardSpec{}
	router.Spec.Resources[claimIndex].Spec = claim
	if err := Validate(router); err != nil {
		t.Fatalf("Validate non-WireGuard SAM enrollment: %v", err)
	}
}

func TestValidateRejectsTopLevelSAMRRSet(t *testing.T) {
	router := samEnrollmentRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "cloudedge-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/cloudedge-leaves",
			Nodes: []api.SAMNodeSpec{
				{NodeRef: "aws-rr-a", RouteReflector: true, SAMEndpoint: "203.0.113.10"},
				{NodeRef: "aws-rr-b", RouteReflector: true, SAMEndpoint: "203.0.113.11"},
			},
		},
	})
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "is runtime-only") {
		t.Fatalf("Validate static SAMRRSet = %v, want runtime-only error", err)
	}
}

func TestValidateSAMEnrollmentPolicyRequiresRRNodeSetRefForRRSet(t *testing.T) {
	router := samEnrollmentRouter()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" {
			continue
		}
		policy := resource.Spec.(api.SAMEnrollmentPolicySpec)
		policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
		router.Spec.Resources[i].Spec = policy
		break
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.rrNodeSetRef is required") {
		t.Fatalf("Validate policy without rrNodeSetRef = %v, want required rrNodeSetRef error", err)
	}
}

func TestValidateSAMEnrollmentPolicyRRNodeSetRequiresRouteReflectors(t *testing.T) {
	router := samEnrollmentRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: "rrs"},
		Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
			NodeRef: "rr-a", SAMEndpoint: "10.99.0.2",
		}}},
	})
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" {
			continue
		}
		policy := resource.Spec.(api.SAMEnrollmentPolicySpec)
		policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
		policy.RRNodeSetRef = "SAMNodeSet/rrs"
		router.Spec.Resources[i].Spec = policy
		break
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.nodes[0].routeReflector must be true") {
		t.Fatalf("Validate policy RR node set without route reflector = %v, want routeReflector error", err)
	}
}

func TestValidateSAMEnrollmentClientAllowsRemotePolicyClaim(t *testing.T) {
	router := samEnrollmentRouter()
	resources := make([]api.Resource, 0, len(router.Spec.Resources))
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentPolicy" {
			continue
		}
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentClaim" {
			claim := resource.Spec.(api.SAMEnrollmentClaimSpec)
			claim.RRSetRef = "SAMRRSet/cloudedge-rrs"
			resource.Spec = claim
		}
		resources = append(resources, resource)
	}
	router.Spec.Resources = resources
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClient"},
		Metadata: api.ObjectMeta{Name: "leaf-pve"},
		Spec: api.SAMEnrollmentClientSpec{
			ClaimRef:           "SAMEnrollmentClaim/leaf-pve",
			BootstrapEndpoints: []string{"https://203.0.113.10:65432"},
		},
	})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate client-side remote policy claim: %v", err)
	}
}

func TestValidateSAMEnrollmentClaimWithoutLocalPolicyRequiresClient(t *testing.T) {
	router := samEnrollmentRouter()
	resources := make([]api.Resource, 0, len(router.Spec.Resources))
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentPolicy" {
			continue
		}
		resources = append(resources, resource)
	}
	router.Spec.Resources = resources
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing SAMEnrollmentPolicy") {
		t.Fatalf("Validate claim without local policy/client = %v, want missing policy error", err)
	}
}

func TestValidateRejectsTopLevelSAMPeerGroup(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
			Metadata: api.ObjectMeta{Name: "svnet1-rrs"},
			Spec: api.SAMPeerGroupSpec{Nodes: []api.SAMNodeSpec{{
				NodeRef:     "k8s-rt01",
				SAMEndpoint: "203.0.113.11",
			}}},
		}}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "is runtime-only") {
		t.Fatalf("Validate top-level SAMPeerGroup error = %v, want runtime-only rejection", err)
	}
}

func validSAMTransportProfileSpec() api.SAMTransportProfileSpec {
	return api.SAMTransportProfileSpec{
		SelfNodeRef:       "pve-rt",
		Mode:              "ipip",
		InnerPrefix:       "10.255.1.0/24",
		UnderlayInterface: "wan",
		LocalEndpoint:     "198.51.100.10",
		BGP: api.SAMTransportBGPProfileSpec{
			RouterRef:    "BGPRouter/mobility",
			PeerASN:      64512,
			TimersPreset: "fast",
		},
		PeersFrom: []api.SAMTransportPeersSourceSpec{{
			Resource: "SAMNodeSet/svnet1-nodes",
			NodeRefs: []string{"k8s-rt"},
		}},
	}
}

func samTransportProfileRouter(spec api.SAMTransportProfileSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "eth0", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
				Metadata: api.ObjectMeta{Name: "mobility"},
				Spec: api.BGPRouterSpec{
					ASN:      64500,
					RouterID: "192.0.2.1",
					Communities: api.BGPCommunitiesSpec{Set: api.BGPCommunitySetSpec{
						Out: []string{bgpstate.MobilityNodeIdentityCommunity("pve-rt")},
					}},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
				Metadata: api.ObjectMeta{Name: "lab"},
				Spec:     spec,
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
				Metadata: api.ObjectMeta{Name: "svnet1-nodes"},
				Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
					{NodeRef: "pve-rt", SAMEndpoint: "198.51.100.10"},
					{NodeRef: "k8s-rt", SAMEndpoint: "203.0.113.20"},
				}},
			},
		}},
	}
}

func validSAMNodeSetSpec() api.SAMNodeSetSpec {
	return api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
		NodeRef:       "pve-rt01",
		Site:          "pve01",
		Role:          "onprem",
		EventEndpoint: "http://10.99.0.11:9443",
		SAMEndpoint:   "10.99.0.11/32",
		MACAddresses:  []string{"02:00:00:00:00:aa"},
		WireGuard: api.SAMNodeWireGuardSpec{
			PublicKey:  "pubkey",
			Endpoint:   "pve-rt01.example.net:51820",
			AllowedIPs: []string{"10.99.0.11/32"},
		},
	}}}
}

func samNodeSetRouter(spec api.SAMNodeSetSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
			Metadata: api.ObjectMeta{Name: "svnet1-nodes"},
			Spec:     spec,
		}}},
	}
}

func samEnrollmentRouter() *api.Router {
	spec := validSAMTransportProfileSpec()
	spec.SelfNodeRef = "aws-rr-a"
	spec.UnderlayInterface = "wg-hybrid"
	spec.LocalEndpoint = "10.99.0.2"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/cloudedge-rrs"}}
	spec.PublishPeerGroup = true
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "aws-rr-a"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-hybrid"}, Spec: api.WireGuardInterfaceSpec{PrivateKey: "priv", ListenPort: 51820}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "mobility"}, Spec: api.BGPRouterSpec{ASN: 64577, RouterID: "10.99.0.2"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}, Metadata: api.ObjectMeta{Name: "cloudedge"}, Spec: api.EventGroupSpec{NodeName: "test-local-node"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"}, Metadata: api.ObjectMeta{Name: "cloudedge-members"}, Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
				{NodeRef: "test-local-node", Site: "aws", Role: "cloud"},
				{NodeRef: "aws-rr-a", Site: "aws", Role: "cloud"},
			}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "cloudedge"}, Spec: api.MobilityPoolSpec{
				Prefix:      "10.77.60.0/24",
				GroupRef:    "cloudedge",
				MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge-members"}},
				Members: []api.MobilityPoolMemberOverlay{{
					NodeRef: "test-local-node",
					Capture: api.MobilityMemberCapture{
						Type:        "provider-secondary-ip",
						ProviderRef: "aws-provider",
						NICRef:      "eni-test-local-node",
					},
				}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"}, Metadata: api.ObjectMeta{Name: "aws-rr-a"}, Spec: spec},
			{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"}, Metadata: api.ObjectMeta{Name: "cloudedge-leaves"}, Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/aws-rr-a",
				AllowedLeafIDs:        api.SAMEnrollmentLeafIDPolicySpec{Pattern: `^leaf-[a-z0-9-]+$`},
				TunnelAddressPrefixes: []string{"10.255.0.0/20"},
				WireGuard:             api.SAMEnrollmentWireGuardSpec{Interface: "wg-hybrid", AllowedExtraIPPrefixes: []string{"10.255.0.0/20"}, PersistentKeepalive: 25},
				MobilityPoolRefs:      []string{"MobilityPool/cloudedge"},
				TTL:                   "24h",
				RevokeAfterInactive:   "168h",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"}, Metadata: api.ObjectMeta{Name: "leaf-pve"}, Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-pve",
				TunnelAddress: "10.255.0.21/32",
				WireGuard:     api.SAMEnrollmentClaimWireGuardSpec{PublicKey: "leafpub", Endpoint: "198.51.100.21:51820"},
				Mobility:      api.SAMEnrollmentClaimMobilitySpec{OwnedAddresses: []string{"10.77.60.21/32"}},
				BGP:           api.SAMEnrollmentClaimBGPSpec{ASN: 64577, RouterID: "10.255.0.21"},
			}},
		}},
	}
}

func TestValidateMobilityPoolAllowsExplicitSingleOnpremProxyARPWithoutVRRP(t *testing.T) {
	router := mobilityPoolRouter(mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				},
			},
		},
	}, testInterfaceResource("lan"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate single onprem proxy-arp MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolRejectsRedundantCaptureStrategy(t *testing.T) {
	cases := []struct {
		name   string
		member api.ResolvedMobilityPoolMember
		want   string
	}{
		{
			name: "provider secondary ip",
			member: api.ResolvedMobilityPoolMember{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:            "provider-secondary-ip",
					CaptureStrategy: "secondary-ip",
					ProviderRef:     "azure-provider",
					NICRef:          "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				},
			},
			want: "only supports route-table",
		},
		{
			name: "proxy arp",
			member: api.ResolvedMobilityPoolMember{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:            "proxy-arp",
					CaptureStrategy: "proxy-arp",
					Interface:       "lan",
					ActiveWhen:      api.CaptureActiveWhen{Type: "single-router"},
				},
			},
			want: "only supported for provider-secondary-ip route-table",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := mobilityPoolRouter(mobilityPoolFixture{
				Prefix:   "10.88.60.0/24",
				GroupRef: "cloudedge",
				Members:  []api.ResolvedMobilityPoolMember{tc.member},
			}, testInterfaceResource("lan"))
			if err := Validate(router); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate redundant captureStrategy error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateMobilityPoolAllowsRouteTableCaptureStrategy(t *testing.T) {
	router := mobilityPoolRouter(mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{{
			NodeRef: "azure-router",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:            "provider-secondary-ip",
				ProviderRef:     "azure-provider",
				CaptureStrategy: "route-table",
				NICRef:          "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				Target:          map[string]string{"routeTableRef": "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/routeTables/rt-cloudedge"},
			},
		}},
	})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate route-table capture strategy: %v", err)
	}
}

func TestValidateMobilityPoolAllowsOnPremL2OwnershipDiscoverySources(t *testing.T) {
	router := mobilityPoolRouter(mobilityPoolFixture{
		Prefix:   "192.168.123.0/24",
		GroupRef: "svnet1",
		Members: []api.ResolvedMobilityPoolMember{
			{
				NodeRef: "pve-rt01",
				Site:    "pve01",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "eth1",
					ActiveWhen: api.CaptureActiveWhen{Type: "single-router"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:     "onprem-l2",
					LeaseTTL: "2m",
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: "dhcpv4-lease", Resource: "DHCPv4Server/pve-ipam"},
						{Type: "arp-observer", Interface: "eth1"},
						{Type: "on-demand-arp", Interface: "eth1", ProbeTimeout: "500ms", ProbeRetries: 2},
						{Type: "pve-svnet", Network: "svnet1", Bridge: "vmbr123"},
					},
				},
			},
			{
				NodeRef: "k8s-rt01",
				Site:    "core",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "aws-provider",
					NICRef:      "eni-router",
				},
			},
		},
	}, testInterfaceResource("eth1"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate onprem-l2 ownership discovery sources: %v", err)
	}
}

func TestValidateMobilityPoolAllowsDiscoveredCloudNICWithProviderDiscovery(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:        "provider-private-ip",
					ProviderRef: "azure-provider",
					SubnetRef:   "/subnets/demo",
					Scope: api.MobilityOwnershipDiscoveryScope{
						IncludeAddresses: []string{"10.88.60.0/25"},
						ExcludeAddresses: []string{"10.88.60.7"},
					},
				},
			},
		},
	}
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err != nil {
		t.Fatalf("Validate discovered NIC MobilityPool: %v", err)
	}

	spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{}
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err == nil || !strings.Contains(err.Error(), "capture.nicRef is required") {
		t.Fatalf("Validate without discovery err = %v, want nicRef required", err)
	}

	spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ProviderRef: "azure-provider"}
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err != nil {
		t.Fatalf("Validate default-BGP discovery err = %v", err)
	}
}

func TestValidateMobilityPoolActiveWhenVirtualAddressReferenceIsLocalToSelfNode(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "lan",
					ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"},
				},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "nic-1",
				},
			},
		},
	}
	router := mobilityPoolRouter(localMobilityPoolSpecForValidation(spec, "azure-router"), testEventGroupResource("cloudedge", "azure-router"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate cloud node with non-local onprem VirtualAddress ref: %v", err)
	}
	router = mobilityPoolRouter(localMobilityPoolSpecForValidation(spec, "onprem-router"), testEventGroupResource("cloudedge", "onprem-router"))
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing VirtualAddress") {
		t.Fatalf("Validate onprem node without local VirtualAddress err = %v", err)
	}
	router = mobilityPoolRouter(localMobilityPoolSpecForValidation(spec, "onprem-router"), testEventGroupResource("cloudedge", "onprem-router"), testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate onprem node with local VirtualAddress: %v", err)
	}
}

func TestValidateMobilityPoolPlacement(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef: "azure-router-a",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "nic-a",
				},
				Placement:   api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
				Maintenance: api.MobilityMemberMaintenance{Drain: true},
			},
			{
				NodeRef: "azure-router-b",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "nic-b",
				},
				Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
			},
		},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate placement MobilityPool: %v", err)
	}

	autoPriority := spec
	autoPriority.Members = append([]api.ResolvedMobilityPoolMember(nil), spec.Members...)
	autoPriority.Members[1].Placement.Priority = 0
	autoPriority.Members[2].Placement.Priority = 0
	if err := Validate(mobilityPoolRouter(autoPriority)); err != nil {
		t.Fatalf("Validate auto-priority placement MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolAllowsIdentityOnlyPlacementMember(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef: "aws-router-a",
				Site:    "aws",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "aws-provider",
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{Mode: "provider-private-ip"},
				Placement:          api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
			},
			{
				NodeRef:     "aws-router-b",
				Site:        "aws",
				Role:        "cloud",
				Placement:   api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20},
				Maintenance: api.MobilityMemberMaintenance{Drain: true},
			},
		},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate identity-only placement member: %v", err)
	}
}

func TestValidateMobilityPoolCloudCaptureProfile(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Values: map[string]string{
			"subnet": "subnet-a",
			"region": "eastus",
		},
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"azure-edge": {
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					TargetFrom:  map[string]string{"region": "region"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:          "provider-private-ip",
					SubnetRefFrom: "subnet",
				},
			},
		}},
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef:    "azure-router",
				Site:       "azure",
				Role:       "cloud",
				ProfileRef: "azure-edge",
				Placement:  api.MobilityMemberPlacement{Group: "azure-edge"},
			},
		},
	}
	router := mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "azure-router"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate profile-backed MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolSelfCloudMemberMustResolveCapture(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
		},
	}
	router := mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "azure-router"))
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "must resolve provider-secondary-ip capture details") {
		t.Fatalf("Validate identity-only self cloud member err = %v, want capture completeness error", err)
	}

	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate identity-only cloud member without self node should remain offline-compatible: %v", err)
	}

	err = Validate(mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "azure-router-alias")))
	if err == nil || !strings.Contains(err.Error(), `self node "azure-router-alias" is not a member`) {
		t.Fatalf("Validate EventGroup identity mismatch = %v, want exact SAMNodeSet membership error", err)
	}
}

func TestValidateMobilityPoolProfileReferenceErrors(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud", ProfileRef: "missing"},
		},
	}
	err := Validate(mobilityPoolRouter(spec))
	if err == nil || !strings.Contains(err.Error(), "profileRef") {
		t.Fatalf("Validate missing profile err = %v, want profileRef failure", err)
	}

	spec.Profiles = api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
		"azure": {OwnershipDiscovery: api.MobilityOwnershipDiscovery{SubnetRefFrom: "missing"}},
	}}
	spec.Members[1].ProfileRef = "azure"
	err = Validate(mobilityPoolRouter(spec))
	if err == nil || !strings.Contains(err.Error(), "subnetRefFrom") {
		t.Fatalf("Validate missing values err = %v, want subnetRefFrom failure", err)
	}
}

func TestValidateMobilityPoolRejectsRemoteDetailsWhenSelfKnown(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:             "10.88.60.0/24",
		GroupRef:           "cloudedge",
		IncludeAllOverlays: true,
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "aws-router", Site: "aws", Role: "cloud"},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "azure-nic",
				},
			},
		},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate without a concrete self node: %v", err)
	}
	err := Validate(mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "aws-router")))
	if err == nil || !strings.Contains(err.Error(), "resolved member[1]") || !strings.Contains(err.Error(), "remote to local node") {
		t.Fatalf("Validate remote member details error = %v, want identity-only rejection", err)
	}
}

func TestValidateMobilityPoolStaticOwnedAndHandover(t *testing.T) {
	spec := mobilityPoolFixture{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.ResolvedMobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
		},
		StaticHandovers: []api.MobilityStaticHandover{{
			Address:     "10.88.60.10/32",
			FromNodeRef: "onprem-router",
			ToNodeRef:   "azure-router",
		}},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate static mobility pool: %v", err)
	}
}

func TestValidateMobilityPoolRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*mobilityPoolFixture)
		want string
	}{
		{
			name: "missing group",
			mut:  func(spec *mobilityPoolFixture) { spec.GroupRef = "" },
			want: "spec.groupRef is required",
		},
		{
			name: "ipv6 prefix",
			mut:  func(spec *mobilityPoolFixture) { spec.Prefix = "2001:db8::/64" },
			want: "spec.prefix must be an IPv4 CIDR",
		},
		{
			name: "missing role",
			mut:  func(spec *mobilityPoolFixture) { spec.Members[0].Role = "" },
			want: "role must be onprem or cloud",
		},
		{
			name: "placement priority without group",
			mut:  func(spec *mobilityPoolFixture) { spec.Members[1].Placement.Priority = 10 },
			want: "placement.priority requires placement.group",
		},
		{
			name: "drain without placement",
			mut:  func(spec *mobilityPoolFixture) { spec.Members[1].Maintenance.Drain = true },
			want: "maintenance.drain requires placement.group",
		},
		{
			name: "ownership discovery requires cloud",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip"}
			},
			want: "ownershipDiscovery is supported only for role cloud",
		},
		{
			name: "provider ownership discovery rejects allow empty",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", AllowEmptyAfter: "30s"}
			},
			want: "ownershipDiscovery.allowEmptyAfter is supported only when mode is onprem-l2",
		},
		{
			name: "provider ownership discovery rejects unknown stopped instance policy",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", StoppedInstancePolicy: "drop"}
			},
			want: "ownershipDiscovery.stoppedInstancePolicy \"drop\" is not supported",
		},
		{
			name: "stopped instance policy requires provider discovery",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2", StoppedInstancePolicy: "hold"}
			},
			want: "ownershipDiscovery.stoppedInstancePolicy is supported only when mode is provider-private-ip",
		},
		{
			name: "onprem l2 discovery requires onprem",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2", Sources: []api.MobilityOwnershipDiscoverySource{{Type: "arp-observer"}}}
			},
			want: "mode onprem-l2 is supported only for role onprem",
		},
		{
			name: "onprem l2 discovery requires sources",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2"}
			},
			want: "ownershipDiscovery.sources requires at least one source",
		},
		{
			name: "onprem l2 discovery rejects unknown source",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2", Sources: []api.MobilityOwnershipDiscoverySource{{Type: "neighbor-cache"}}}
			},
			want: "ownershipDiscovery.sources[0].type",
		},
		{
			name: "onprem l2 discovery allow empty duration must be positive",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
					Mode:            "onprem-l2",
					AllowEmptyAfter: "0s",
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: "arp-observer", Interface: "lan"},
					},
				}
			},
			want: "ownershipDiscovery.allowEmptyAfter must be > 0",
		},
		{
			name: "ownership discovery scan interval minimum",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ScanInterval: "5s"}
			},
			want: "ownershipDiscovery.scanInterval must be >= 30s",
		},
		{
			name: "ownership discovery include address outside pool",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
					Mode: "provider-private-ip",
					Scope: api.MobilityOwnershipDiscoveryScope{
						IncludeAddresses: []string{"10.88.61.1"},
					},
				}
			},
			want: "ownershipDiscovery.scope.includeAddresses[0]",
		},
		{
			name: "ownership discovery exclude aggregate outside pool",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
					Mode: "provider-private-ip",
					Scope: api.MobilityOwnershipDiscoveryScope{
						ExcludeAddresses: []string{"10.88.60.0/23"},
					},
				}
			},
			want: "ownershipDiscovery.scope.excludeAddresses[0]",
		},
		{
			name: "placement priority range",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", NICRef: "nic-1"}
				spec.Members[1].Placement = api.MobilityMemberPlacement{Group: "azure-edge", Priority: -1}
			},
			want: "placement.priority must be between 0 and 1000000",
		},
		{
			name: "placement role",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].Placement = api.MobilityMemberPlacement{Group: "onprem-edge", Priority: 10}
			},
			want: "placement.group is supported only for role cloud",
		},
		{
			name: "onprem proxy arp missing activeWhen",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
			},
			want: "capture.activeWhen.type is required",
		},
		{
			name: "cloud capture type",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
			},
			want: "capture.type must be provider-secondary-ip for role cloud",
		},
		{
			name: "cloud provider capture rejects activeWhen",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "nic-1",
					ActiveWhen: api.CaptureActiveWhen{
						Type:              "vrrp-master",
						VirtualAddressRef: "VirtualAddress/cloud-vip",
					},
				}
			},
			want: "capture.activeWhen is supported only for role onprem proxy-arp capture",
		},
		{
			name: "secret target",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[1].Capture = api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "azure-provider",
					NICRef:      "nic-1",
					Target:      map[string]string{"accessToken": "nope"},
				}
			},
			want: "looks secret-like",
		},
		{
			name: "activeWhen missing ref",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master"}}
			},
			want: "capture.activeWhen.virtualAddressRef is required",
		},
		{
			name: "activeWhen unresolved virtual address",
			mut: func(spec *mobilityPoolFixture) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}}
			},
			want: "references missing VirtualAddress",
		},
		{
			name: "static owned on cloud",
			mut:  func(spec *mobilityPoolFixture) { spec.Members[1].StaticOwnedAddresses = []string{"10.88.60.20/32"} },
			want: "staticOwnedAddresses is supported only for role onprem",
		},
		{
			name: "static owned outside prefix",
			mut:  func(spec *mobilityPoolFixture) { spec.Members[0].StaticOwnedAddresses = []string{"10.88.61.10/32"} },
			want: "must be within spec.prefix",
		},
		{
			name: "static owned requires host prefix",
			mut:  func(spec *mobilityPoolFixture) { spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/24"} },
			want: "must be an IPv4 /32 CIDR",
		},
		{
			name: "handover from missing",
			mut: func(spec *mobilityPoolFixture) {
				spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "missing", ToNodeRef: "azure-router"}}
			},
			want: "fromNodeRef \"missing\" must be one of the member nodeRefs",
		},
		{
			name: "handover from must be onprem",
			mut: func(spec *mobilityPoolFixture) {
				spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "azure-router", ToNodeRef: "onprem-router"}}
			},
			want: "must reference an onprem member",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := mobilityPoolFixture{
				Prefix:   "10.88.60.0/24",
				GroupRef: "cloudedge",
				Members: []api.ResolvedMobilityPoolMember{
					{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
					{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
				},
			}
			tt.mut(&spec)
			err := Validate(mobilityPoolRouter(spec))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

// mobilityPoolFixture keeps the former full-member test notation out of the
// public API.  The helper below materializes its topology as SAMNodeSet and
// converts only capture/ownership fields into MobilityPool overlays.
//
// Tests that need to exercise the declared API directly use
// api.MobilityPoolSpec instead.  Keeping that distinction in tests makes it
// impossible for a fixture to accidentally preserve the removed direct-member
// configuration surface.
type mobilityPoolFixture struct {
	Prefix          string
	GroupRef        string
	Values          map[string]string
	Profiles        api.MobilityPoolProfiles
	MembersFrom     []api.MobilityMembersSourceSpec
	Members         []api.ResolvedMobilityPoolMember
	StaticHandovers []api.MobilityStaticHandover
	// LocalNode selects which SAMNodeSet member receives this router's local
	// overlay. An explicit EventGroup passed to mobilityPoolRouter takes
	// precedence. Empty chooses the last member carrying local details so
	// compact negative fixtures naturally validate the member they mutate.
	LocalNode string
	// IncludeAllOverlays is reserved for tests that deliberately verify the
	// rejected remote-overlay shape. Normal fixtures model the production
	// contract and emit exactly one local overlay.
	IncludeAllOverlays bool
}

func (fixture mobilityPoolFixture) declared(eventGroupNode string) (api.MobilityPoolSpec, api.Resource, string) {
	sources := append([]api.MobilityMembersSourceSpec(nil), fixture.MembersFrom...)
	nodeSetName := "cloudedge-members"
	if len(sources) == 0 {
		sources = []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/" + nodeSetName}}
	} else if resource := strings.TrimSpace(sources[0].Resource); strings.HasPrefix(resource, "SAMNodeSet/") {
		nodeSetName = strings.TrimPrefix(resource, "SAMNodeSet/")
	}

	declared := api.MobilityPoolSpec{
		Prefix:          fixture.Prefix,
		GroupRef:        fixture.GroupRef,
		Values:          fixture.Values,
		Profiles:        fixture.Profiles,
		MembersFrom:     sources,
		StaticHandovers: fixture.StaticHandovers,
		Members:         make([]api.MobilityPoolMemberOverlay, 0, len(fixture.Members)),
	}
	nodes := make([]api.SAMNodeSpec, 0, len(fixture.Members))
	localNode := fixture.localNode(eventGroupNode)
	for _, member := range fixture.Members {
		nodes = append(nodes, api.SAMNodeSpec{
			NodeRef:         member.NodeRef,
			Site:            member.Site,
			Role:            member.Role,
			Placement:       member.Placement,
			Maintenance:     member.Maintenance,
			MaxSecondaryIPs: member.MaxSecondaryIPs,
		})
		if fixture.IncludeAllOverlays || strings.TrimSpace(member.NodeRef) == localNode {
			declared.Members = append(declared.Members, api.MobilityPoolMemberOverlay{
				NodeRef:              member.NodeRef,
				ProfileRef:           member.ProfileRef,
				Capture:              member.Capture,
				StaticOwnedAddresses: append([]string(nil), member.StaticOwnedAddresses...),
				OwnershipDiscovery:   member.OwnershipDiscovery,
			})
		}
	}
	return declared, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: nodeSetName},
		Spec:     api.SAMNodeSetSpec{Nodes: nodes},
	}, localNode
}

func mobilityPoolRouter(spec any, extra ...api.Resource) *api.Router {
	resources := make([]api.Resource, 0, len(extra)+3)
	switch value := spec.(type) {
	case mobilityPoolFixture:
		eventGroupNode := testMobilityEventGroupNode(extra, value.GroupRef)
		declared, nodeSet, localNode := value.declared(eventGroupNode)
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     declared,
		})
		if !hasTestMobilityResource(extra, "SAMNodeSet", nodeSet.Metadata.Name) {
			resources = append(resources, nodeSet)
		}
		if strings.TrimSpace(declared.GroupRef) != "" && !hasTestMobilityResource(extra, "EventGroup", declared.GroupRef) {
			resources = append(resources, testEventGroupResource(declared.GroupRef, localNode))
		}
	case api.MobilityPoolSpec:
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     value,
		})
	default:
		panic("mobilityPoolRouter requires mobilityPoolFixture or api.MobilityPoolSpec")
	}
	resources = append(resources, extra...)
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: resources},
	}
}

func (fixture mobilityPoolFixture) localNode(eventGroupNode string) string {
	if nodeRef := strings.TrimSpace(eventGroupNode); nodeRef != "" {
		return nodeRef
	}
	if nodeRef := strings.TrimSpace(fixture.LocalNode); nodeRef != "" {
		return nodeRef
	}
	for i := len(fixture.Members) - 1; i >= 0; i-- {
		if mobilityFixtureMemberHasLocalOverlay(fixture.Members[i]) {
			return strings.TrimSpace(fixture.Members[i].NodeRef)
		}
	}
	if len(fixture.Members) > 0 {
		return strings.TrimSpace(fixture.Members[0].NodeRef)
	}
	return "test-local-node"
}

func mobilityFixtureMemberHasLocalOverlay(member api.ResolvedMobilityPoolMember) bool {
	discovery := member.OwnershipDiscovery
	capture := member.Capture
	return strings.TrimSpace(member.ProfileRef) != "" ||
		strings.TrimSpace(capture.Type) != "" ||
		strings.TrimSpace(capture.ProviderRef) != "" ||
		strings.TrimSpace(capture.Interface) != "" ||
		strings.TrimSpace(capture.NICRef) != "" ||
		strings.TrimSpace(capture.CaptureStrategy) != "" ||
		len(capture.Target) != 0 || len(capture.TargetFrom) != 0 ||
		len(member.StaticOwnedAddresses) != 0 ||
		strings.TrimSpace(discovery.Mode) != "" ||
		strings.TrimSpace(discovery.ProviderRef) != "" ||
		len(discovery.Sources) != 0
}

func hasTestMobilityResource(resources []api.Resource, kind, name string) bool {
	for _, resource := range resources {
		if resource.Kind == kind && resource.Metadata.Name == name {
			return true
		}
	}
	return false
}

func testMobilityEventGroupNode(resources []api.Resource, name string) string {
	for _, resource := range resources {
		if resource.Kind != "EventGroup" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err == nil {
			return strings.TrimSpace(spec.NodeName)
		}
	}
	return ""
}

func localMobilityPoolSpecForValidation(spec mobilityPoolFixture, selfNode string) mobilityPoolFixture {
	out := spec
	out.Members = append([]api.ResolvedMobilityPoolMember(nil), spec.Members...)
	for i := range out.Members {
		if strings.TrimSpace(out.Members[i].NodeRef) == strings.TrimSpace(selfNode) {
			continue
		}
		out.Members[i].ProfileRef = ""
		out.Members[i].Capture = api.MobilityMemberCapture{}
		out.Members[i].StaticOwnedAddresses = nil
		out.Members[i].OwnershipDiscovery = api.MobilityOwnershipDiscovery{}
	}
	return out
}

func testVirtualAddressResource(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.VirtualAddressSpec{
			Family:    "ipv4",
			Interface: "lan",
			Address:   "10.88.60.1/32",
			Mode:      "vrrp",
			VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 60, Peers: []string{"10.88.60.2"}},
		},
	}
}

func testEventGroupResource(name, nodeName string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.EventGroupSpec{
			NodeName: nodeName,
			Auth:     api.EventGroupAuth{Mode: "hmac", SecretFile: "/run/routerd/event.key"},
		},
	}
}

func testInterfaceResource(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.InterfaceSpec{IfName: name, Managed: true},
	}
}

func TestValidateSAMSubnetPolicy(t *testing.T) {
	validPolicy := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMSubnetPolicy"},
		Metadata: api.ObjectMeta{Name: "office-10-net"},
		Spec: api.SAMSubnetPolicySpec{
			SourcePrefix: "10.0.0.0/8",
			PoolRef:      "cloudedge",
			GroupRef:     "cloudedge",
			Shards: []api.SAMSubnetShard{
				{Prefix: "10.0.1.0/25", AssignedNodes: []string{"oci-a", "oci-b"}},
				{Prefix: "10.0.2.0/25", AssignedNodes: []string{"aws-a"}},
			},
		},
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: []api.Resource{validPolicy}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid SAMSubnetPolicy rejected: %v", err)
	}
}

func TestValidateSAMSubnetPolicyRejects(t *testing.T) {
	tests := []struct {
		name string
		spec api.SAMSubnetPolicySpec
		want string
	}{
		{
			name: "empty sourcePrefix",
			spec: api.SAMSubnetPolicySpec{PoolRef: "p", GroupRef: "g", Shards: []api.SAMSubnetShard{{Prefix: "10.0.1.0/25", AssignedNodes: []string{"a"}}}},
			want: "sourcePrefix is required",
		},
		{
			name: "invalid sourcePrefix",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "not-a-cidr", PoolRef: "p", GroupRef: "g", Shards: []api.SAMSubnetShard{{Prefix: "10.0.1.0/25", AssignedNodes: []string{"a"}}}},
			want: "must be a CIDR",
		},
		{
			name: "empty poolRef",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "10.0.0.0/8", GroupRef: "g", Shards: []api.SAMSubnetShard{{Prefix: "10.0.1.0/25", AssignedNodes: []string{"a"}}}},
			want: "poolRef is required",
		},
		{
			name: "empty groupRef",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "10.0.0.0/8", PoolRef: "p", Shards: []api.SAMSubnetShard{{Prefix: "10.0.1.0/25", AssignedNodes: []string{"a"}}}},
			want: "groupRef is required",
		},
		{
			name: "no shards",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "10.0.0.0/8", PoolRef: "p", GroupRef: "g"},
			want: "requires at least one shard",
		},
		{
			name: "shard outside source prefix",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "10.0.0.0/16", PoolRef: "p", GroupRef: "g", Shards: []api.SAMSubnetShard{{Prefix: "10.1.0.0/25", AssignedNodes: []string{"a"}}}},
			want: "is not within sourcePrefix",
		},
		{
			name: "overlapping shards",
			spec: api.SAMSubnetPolicySpec{
				SourcePrefix: "10.0.0.0/8", PoolRef: "p", GroupRef: "g",
				Shards: []api.SAMSubnetShard{
					{Prefix: "10.0.1.0/24", AssignedNodes: []string{"a"}},
					{Prefix: "10.0.1.0/25", AssignedNodes: []string{"b"}},
				},
			},
			want: "overlaps",
		},
		{
			name: "empty assignedNodes",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "10.0.0.0/8", PoolRef: "p", GroupRef: "g", Shards: []api.SAMSubnetShard{{Prefix: "10.0.1.0/25", AssignedNodes: []string{}}}},
			want: "requires at least one node",
		},
		{
			name: "duplicate node in shard",
			spec: api.SAMSubnetPolicySpec{SourcePrefix: "10.0.0.0/8", PoolRef: "p", GroupRef: "g", Shards: []api.SAMSubnetShard{{Prefix: "10.0.1.0/25", AssignedNodes: []string{"a", "a"}}}},
			want: "duplicate",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMSubnetPolicy"},
				Metadata: api.ObjectMeta{Name: "test"},
				Spec:     tc.spec,
			}
			router := &api.Router{
				TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
				Metadata: api.ObjectMeta{Name: "test"},
				Spec:     api.RouterSpec{Resources: []api.Resource{res}},
			}
			err := Validate(router)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}
