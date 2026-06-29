// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/samenrollment"
)

func TestValidateMobilityPool(t *testing.T) {
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
		Authority: api.MobilityAuthority{Mode: "static"},
	}, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolAllowsProviderCaptureConfigureOSAddress(t *testing.T) {
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef: "aws-router",
				Site:    "aws",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:               "provider-secondary-ip",
					Interface:          "ens5",
					ProviderRef:        "aws-provider",
					ProviderMode:       "secondary-ip",
					NICRef:             "eni-router",
					ConfigureOSAddress: true,
				},
				Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
			},
		},
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
	})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolRejectsOverlappingPrefixes(t *testing.T) {
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge-a",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-a", Site: "onprem-a", Role: "onprem"},
			{NodeRef: "cloud-a", Site: "cloud-a", Role: "cloud"},
		},
	}, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge-b"},
		Spec: api.MobilityPoolSpec{
			Prefix:   "10.88.60.128/25",
			GroupRef: "cloudedge-b",
			Members: []api.MobilityPoolMember{
				{NodeRef: "onprem-b", Site: "onprem-b", Role: "onprem"},
				{NodeRef: "cloud-b", Site: "cloud-b", Role: "cloud"},
			},
		},
	})
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
			name: "self peer",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.Peers[0].NodeRef = "pve-rt" },
			want: "must not equal spec.selfNodeRef",
		},
		{
			name: "duplicate peer",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.TopologyNodeRefs = []string{"pve-rt", "k8s-rt"}
				spec.Peers = append(spec.Peers, api.SAMTransportPeerSpec{NodeRef: "k8s-rt", RemoteEndpoint: "203.0.113.30"})
			},
			want: "nodeRef \"k8s-rt\" is duplicated",
		},
		{
			name: "missing topology for multiple peers",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.Peers = append(spec.Peers, api.SAMTransportPeerSpec{NodeRef: "cloud-rt", RemoteEndpoint: "203.0.113.30"})
			},
			want: "spec.topologyNodeRefs is required",
		},
		{
			name: "invalid addressing mode",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.AddressingMode = "invalid-mode" },
			want: "spec.addressingMode must be edge-index or pair-stable",
		},
		{
			name: "peer missing from topology",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.TopologyNodeRefs = []string{"pve-rt", "k8s-rt", "other-rt"}
				spec.Peers = append(spec.Peers, api.SAMTransportPeerSpec{NodeRef: "cloud-rt", RemoteEndpoint: "203.0.113.30"})
			},
			want: "must be listed in spec.topologyNodeRefs",
		},
		{
			name: "override half set",
			mut:  func(spec *api.SAMTransportProfileSpec) { spec.Peers[0].Override.LocalInner = "10.255.1.0/31" },
			want: "localInner and remoteInner must be set together",
		},
		{
			name: "override conflict",
			mut: func(spec *api.SAMTransportProfileSpec) {
				spec.TopologyNodeRefs = []string{"pve-rt", "k8s-rt", "cloud-rt"}
				spec.Peers[0].Override = api.SAMTransportPeerOverrideSpec{LocalInner: "10.255.1.0/31", RemoteInner: "10.255.1.1"}
				spec.Peers = append(spec.Peers, api.SAMTransportPeerSpec{
					NodeRef:        "cloud-rt",
					RemoteEndpoint: "203.0.113.30",
					Override:       api.SAMTransportPeerOverrideSpec{LocalInner: "10.255.1.0/31", RemoteInner: "10.255.1.1"},
				})
			},
			want: "conflicts with spec.peers[0].override",
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
	spec.Peers = append(spec.Peers, api.SAMTransportPeerSpec{
		NodeRef:        "k8s-rr02",
		RemoteEndpoint: "203.0.113.21",
	})
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate pair-stable SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileAllowsPeersFromWithoutPeers(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.Peers = nil
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate peersFrom SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileAllowsSAMNodeSetPeersFromWithoutPeers(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.Peers = nil
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate SAMNodeSet peersFrom SAMTransportProfile: %v", err)
	}
}

func TestValidateSAMTransportProfileAllowsPublishPeerGroupWithoutPeers(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.Peers = nil
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

func TestValidateMobilityPoolAllowsMembersFromWithoutMembers(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:      "10.88.60.0/24",
		GroupRef:    "cloudedge",
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/svnet1-members"}},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate membersFrom MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolRejectsInvalidMembersFrom(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:      "10.88.60.0/24",
		GroupRef:    "cloudedge",
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}},
	}
	err := Validate(mobilityPoolRouter(spec))
	if err == nil || !strings.Contains(err.Error(), "spec.membersFrom[0].resource must reference MobilityMemberSet/<name>") {
		t.Fatalf("Validate membersFrom error = %v, want MobilityMemberSet ref error", err)
	}
}

func TestValidateMobilityMemberSet(t *testing.T) {
	router := mobilityMemberSetRouter(api.MobilityMemberSetSpec{Members: []api.MobilityMemberSetMember{{
		NodeRef: "pve-rt01",
		Site:    "pve01",
		Role:    "onprem",
	}}})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate MobilityMemberSet: %v", err)
	}
}

func TestValidateMobilityMemberSetRejectsInvalidMember(t *testing.T) {
	router := mobilityMemberSetRouter(api.MobilityMemberSetSpec{Members: []api.MobilityMemberSetMember{{Site: "pve01", Role: "onprem"}}})
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.members[0].nodeRef is required") {
		t.Fatalf("Validate MobilityMemberSet error = %v, want nodeRef required", err)
	}
}

func TestValidateSAMNodeSet(t *testing.T) {
	router := samNodeSetRouter(api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
		NodeRef:        "pve-rt01",
		Site:           "pve01",
		Role:           "onprem",
		EventEndpoint:  "http://10.99.0.11:9443",
		SAMEndpoint:    "10.99.0.11",
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
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "outside authorized MobilityPool prefixes") {
		t.Fatalf("expected MobilityPool authorization error, got %v", err)
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
	policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
	router.Spec.Resources[policyIndex].Spec = policy

	claimIndex := len(router.Spec.Resources) - 1
	claim := router.Spec.Resources[claimIndex].Spec.(api.SAMEnrollmentClaimSpec)
	claim.RRSetRef = "SAMRRSet/cloudedge-rrs"
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
	policyIndex := len(router.Spec.Resources) - 2
	policy := router.Spec.Resources[policyIndex].Spec.(api.SAMEnrollmentPolicySpec)
	policy.RRSetRef = "SAMRRSet/cloudedge-rrs"
	router.Spec.Resources[policyIndex].Spec = policy
	claimIndex := len(router.Spec.Resources) - 1
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

func TestValidateSAMRRSetAllowsPlainIPIPWithoutWireGuard(t *testing.T) {
	router := samEnrollmentRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "cloudedge-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/cloudedge-leaves",
			MobilityPoolRefs:    []string{"MobilityPool/cloudedge"},
			RouteAdmission:      api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
			Members: []api.SAMRRSetMember{
				{NodeRef: "aws-rr-a", Endpoint: "203.0.113.10", TunnelAddress: "10.99.0.2/32"},
				{NodeRef: "aws-rr-b", Endpoint: "203.0.113.11", TunnelAddress: "10.99.0.3/32"},
			},
		},
	})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate plain IPIP SAMRRSet without WireGuard: %v", err)
	}
}

func TestValidateSAMPeerGroup(t *testing.T) {
	router := samPeerGroupRouter(api.SAMPeerGroupSpec{Peers: []api.SAMTransportPeerSpec{{
		NodeRef:        "k8s-rt01",
		RemoteEndpoint: "203.0.113.11",
	}}})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate SAMPeerGroup: %v", err)
	}
}

func TestValidateSAMPeerGroupRejectsInvalidPeer(t *testing.T) {
	router := samPeerGroupRouter(api.SAMPeerGroupSpec{Peers: []api.SAMTransportPeerSpec{{RemoteEndpoint: "203.0.113.11"}}})
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), "spec.peers[0].nodeRef is required") {
		t.Fatalf("Validate SAMPeerGroup error = %v, want nodeRef required", err)
	}
}

func TestValidateSAMTransportProfileRejectsPairStableSlotCollision(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.Peers = []api.SAMTransportPeerSpec{
		{NodeRef: "node-03", RemoteEndpoint: "203.0.113.20"},
		{NodeRef: "node-50", RemoteEndpoint: "203.0.113.21"},
	}
	err := Validate(samTransportProfileRouter(spec))
	if err == nil || !strings.Contains(err.Error(), "collides with") {
		t.Fatalf("Validate collision error = %v, want collides with", err)
	}
}

func TestValidateSAMTransportProfileAllowsPairStableCollisionWithOverride(t *testing.T) {
	spec := validSAMTransportProfileSpec()
	spec.AddressingMode = "pair-stable"
	spec.Peers = []api.SAMTransportPeerSpec{
		{NodeRef: "node-03", RemoteEndpoint: "203.0.113.20"},
		{
			NodeRef:        "node-50",
			RemoteEndpoint: "203.0.113.21",
			Override: api.SAMTransportPeerOverrideSpec{
				LocalInner:  "10.255.1.126/31",
				RemoteInner: "10.255.1.127",
			},
		},
	}
	if err := Validate(samTransportProfileRouter(spec)); err != nil {
		t.Fatalf("Validate collision with override: %v", err)
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
		Peers: []api.SAMTransportPeerSpec{{
			NodeRef:        "k8s-rt",
			RemoteEndpoint: "203.0.113.20",
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
				Spec:     api.BGPRouterSpec{ASN: 64500, RouterID: "192.0.2.1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
				Metadata: api.ObjectMeta{Name: "lab"},
				Spec:     spec,
			},
		}},
	}
}

func samPeerGroupRouter(spec api.SAMPeerGroupSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
			Metadata: api.ObjectMeta{Name: "svnet1-rrs"},
			Spec:     spec,
		}}},
	}
}

func mobilityMemberSetRouter(spec api.MobilityMemberSetSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityMemberSet"},
			Metadata: api.ObjectMeta{Name: "svnet1-members"},
			Spec:     spec,
		}}},
	}
}

func validSAMNodeSetSpec() api.SAMNodeSetSpec {
	return api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{
		NodeRef:       "pve-rt01",
		Site:          "pve01",
		Role:          "onprem",
		EventEndpoint: "http://10.99.0.11:9443",
		SAMEndpoint:   "10.99.0.11/32",
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
	spec.Peers = nil
	spec.PublishPeerGroup = true
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "aws-rr-a"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-hybrid"}, Spec: api.WireGuardInterfaceSpec{PrivateKey: "priv", ListenPort: 51820}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "mobility"}, Spec: api.BGPRouterSpec{ASN: 64577, RouterID: "10.99.0.2"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "cloudedge"}, Spec: api.MobilityPoolSpec{
				Prefix:   "10.77.60.0/24",
				GroupRef: "cloudedge",
				Members:  []api.MobilityPoolMember{{NodeRef: "aws-rr-a", Site: "aws", Role: "cloud"}},
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
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
		Authority: api.MobilityAuthority{Mode: "static"},
	}, testInterfaceResource("lan"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate single onprem proxy-arp MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolAllowsExplicitProxyARPCaptureStrategy(t *testing.T) {
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{{
			NodeRef: "onprem-router",
			Site:    "onprem",
			Role:    "onprem",
			Capture: api.MobilityMemberCapture{
				Type:            "proxy-arp",
				CaptureStrategy: "proxy-arp",
				Interface:       "lan",
				ActiveWhen:      api.CaptureActiveWhen{Type: "single-router"},
			},
		}},
	}, testInterfaceResource("lan"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate proxy-arp captureStrategy: %v", err)
	}
}

func TestValidateMobilityPoolAllowsRouteTableCaptureStrategyAndLegacyStrategy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		capture api.MobilityMemberCapture
	}{
		{
			name: "captureStrategy",
			capture: api.MobilityMemberCapture{
				Type:            "provider-secondary-ip",
				ProviderRef:     "azure-provider",
				ProviderMode:    "route-table",
				CaptureStrategy: "route-table",
				NICRef:          "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				Target:          map[string]string{"routeTableRef": "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/routeTables/rt-cloudedge"},
			},
		},
		{
			name: "legacy strategy",
			capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "route-table",
				Strategy:     "route-table",
				NICRef:       "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				Target:       map[string]string{"routeTableRef": "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/routeTables/rt-cloudedge"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := mobilityPoolRouter(api.MobilityPoolSpec{
				Prefix:   "10.88.60.0/24",
				GroupRef: "cloudedge",
				Members: []api.MobilityPoolMember{{
					NodeRef: "azure-router",
					Site:    "azure",
					Role:    "cloud",
					Capture: tc.capture,
				}},
			})
			if err := Validate(router); err != nil {
				t.Fatalf("Validate route-table capture strategy: %v", err)
			}
		})
	}
}

func TestValidateMobilityPoolAllowsOnPremL2OwnershipDiscoverySources(t *testing.T) {
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:         "192.168.123.0/24",
		GroupRef:       "svnet1",
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
		Members: []api.MobilityPoolMember{
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
					Type:         "provider-secondary-ip",
					ProviderRef:  "aws-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "eni-router",
				},
			},
		},
	}, testInterfaceResource("eth1"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate onprem-l2 ownership discovery sources: %v", err)
	}
}

func TestValidateMobilityPoolAllowsDiscoveredCloudNICOnlyInBGPDiscoveryMode(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:         "10.88.60.0/24",
		GroupRef:       "cloudedge",
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
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
	spec.DeliveryPolicy.Mode = ""
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err != nil {
		t.Fatalf("Validate default-BGP discovery err = %v", err)
	}
}

func TestValidateMobilityPoolActiveWhenVirtualAddressReferenceIsLocalToSelfNode(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "lan",
					ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"},
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-1",
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
	}
	router := mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "azure-router"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate cloud node with non-local onprem VirtualAddress ref: %v", err)
	}
	router = mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "onprem-router"))
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing VirtualAddress") {
		t.Fatalf("Validate onprem node without local VirtualAddress err = %v", err)
	}
	router = mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "onprem-router"), testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate onprem node with local VirtualAddress: %v", err)
	}
}

func TestValidateMobilityPoolPlacement(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef: "azure-router-a",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-a",
				},
				Delivery:    api.MobilityMemberDelivery{PeerRef: "onprem"},
				Placement:   api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
				Maintenance: api.MobilityMemberMaintenance{Drain: true},
			},
			{
				NodeRef: "azure-router-b",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-b",
				},
				Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem"},
				Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
			},
		},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate placement MobilityPool: %v", err)
	}

	partial := spec
	partial.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	partial.Members[2].Placement = api.MobilityMemberPlacement{}
	if err := Validate(mobilityPoolRouter(partial)); err == nil || !strings.Contains(err.Error(), "placement.group is required for provider-secondary-ip member") {
		t.Fatalf("Validate partial placement err = %v, want missing placement group failure", err)
	}

	autoPriority := spec
	autoPriority.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	autoPriority.Members[1].Placement.Priority = 0
	autoPriority.Members[2].Placement.Priority = 0
	if err := Validate(mobilityPoolRouter(autoPriority)); err != nil {
		t.Fatalf("Validate auto-priority placement MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolAllowsIdentityOnlyPlacementMember(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef: "aws-router-a",
				Site:    "aws",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "aws-provider",
					ProviderMode: "eni-secondary-ip",
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
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Values: map[string]string{
			"subnet": "subnet-a",
			"region": "eastus",
		},
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"azure-edge": {
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					TargetFrom:   map[string]string{"region": "region"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:          "provider-private-ip",
					SubnetRefFrom: "subnet",
				},
			},
		}},
		Members: []api.MobilityPoolMember{
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
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
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
}

func TestValidateMobilityPoolProfileReferenceErrors(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
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

func TestWarningsMobilityPoolRemoteDetails(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"azure": {Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip"}},
		}},
		Members: []api.MobilityPoolMember{
			{NodeRef: "aws-router", Site: "aws", Role: "cloud"},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud", ProfileRef: "azure"},
		},
	}
	warnings := Warnings(mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "aws-router")))
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "remote member") && strings.Contains(warning, "azure-router") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Warnings = %#v, want remote member warning", warnings)
	}
}

func TestValidateMobilityPoolStaticOwnedAndHandover(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
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
		mut  func(*api.MobilityPoolSpec)
		want string
	}{
		{
			name: "missing group",
			mut:  func(spec *api.MobilityPoolSpec) { spec.GroupRef = "" },
			want: "spec.groupRef is required",
		},
		{
			name: "ipv6 prefix",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Prefix = "2001:db8::/64" },
			want: "spec.prefix must be an IPv4 CIDR",
		},
		{
			name: "missing role",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[0].Role = "" },
			want: "role must be onprem or cloud",
		},
		{
			name: "placement priority without group",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[1].Placement.Priority = 10 },
			want: "placement.priority requires placement.group",
		},
		{
			name: "drain without placement",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[1].Maintenance.Drain = true },
			want: "maintenance.drain requires placement.group",
		},
		{
			name: "delivery policy route mode rejected",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "route"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip"}
			},
			want: "spec.deliveryPolicy.mode \"route\" is not supported; only bgp",
		},
		{
			name: "ownership discovery requires cloud",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip"}
			},
			want: "ownershipDiscovery is supported only for role cloud",
		},
		{
			name: "provider ownership discovery rejects allow empty",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", AllowEmptyAfter: "30s"}
			},
			want: "ownershipDiscovery.allowEmptyAfter is supported only when mode is onprem-l2",
		},
		{
			name: "onprem l2 discovery requires onprem",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2", Sources: []api.MobilityOwnershipDiscoverySource{{Type: "arp-observer"}}}
			},
			want: "mode onprem-l2 is supported only for role onprem",
		},
		{
			name: "onprem l2 discovery requires sources",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2"}
			},
			want: "ownershipDiscovery.sources requires at least one source",
		},
		{
			name: "onprem l2 discovery rejects unknown source",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2", Sources: []api.MobilityOwnershipDiscoverySource{{Type: "neighbor-cache"}}}
			},
			want: "ownershipDiscovery.sources[0].type",
		},
		{
			name: "onprem l2 discovery allow empty duration must be positive",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
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
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ScanInterval: "5s"}
			},
			want: "ownershipDiscovery.scanInterval must be >= 30s",
		},
		{
			name: "ownership discovery include address outside pool",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
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
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
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
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
				spec.Members[1].Placement = api.MobilityMemberPlacement{Group: "azure-edge", Priority: -1}
			},
			want: "placement.priority must be between 0 and 1000000",
		},
		{
			name: "placement role",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
				spec.Members[0].Placement = api.MobilityMemberPlacement{Group: "onprem-edge", Priority: 10}
			},
			want: "placement.group is supported only for role cloud",
		},
		{
			name: "onprem proxy arp missing activeWhen",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
			},
			want: "capture.activeWhen.type is required",
		},
		{
			name: "placement group provider mismatch",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members = append(spec.Members, api.MobilityPoolMember{
					NodeRef: "azure-router-b",
					Site:    "azure",
					Role:    "cloud",
					Capture: api.MobilityMemberCapture{
						Type:         "provider-secondary-ip",
						ProviderRef:  "other-provider",
						ProviderMode: "nic-secondary-ip",
						NICRef:       "nic-2",
					},
					Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem"},
					Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
				})
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
				spec.Members[1].Placement = api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10}
			},
			want: "must use one providerRef",
		},
		{
			name: "unknown authority node",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Authority.NodeRef = "other" },
			want: "must be one of the member nodeRefs",
		},
		{
			name: "bad ownership policy type",
			mut:  func(spec *api.MobilityPoolSpec) { spec.IPOwnershipPolicy.Type = "lock-service" },
			want: "spec.ipOwnershipPolicy.type",
		},
		{
			name: "ownership prefer missing node",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", PreferNodes: []string{"missing-router"}}
			},
			want: "spec.ipOwnershipPolicy.preferNodes[0]",
		},
		{
			name: "ownership prefer duplicate node",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", PreferNodes: []string{"azure-router", "azure-router"}}
			},
			want: "contains duplicate nodeRef",
		},
		{
			name: "cloud capture type",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
			},
			want: "capture.type must be provider-secondary-ip for role cloud",
		},
		{
			name: "deliveryTo selector",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].DeliveryTo = []api.MobilityMemberDeliveryTarget{{PeerRef: "onprem"}}
			},
			want: "must set nodeRef, site, or role",
		},
		{
			name: "secret target",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-1",
					Target:       map[string]string{"accessToken": "nope"},
				}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
			},
			want: "looks secret-like",
		},
		{
			name: "activeWhen missing ref",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master"}}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
			},
			want: "capture.activeWhen.virtualAddressRef is required",
		},
		{
			name: "activeWhen unresolved virtual address",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
			},
			want: "references missing VirtualAddress",
		},
		{
			name: "static owned on cloud",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[1].StaticOwnedAddresses = []string{"10.88.60.20/32"} },
			want: "staticOwnedAddresses is supported only for role onprem",
		},
		{
			name: "static owned outside prefix",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[0].StaticOwnedAddresses = []string{"10.88.61.10/32"} },
			want: "must be within spec.prefix",
		},
		{
			name: "static owned requires host prefix",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/24"} },
			want: "must be an IPv4 /32 CIDR",
		},
		{
			name: "static owned duplicate",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members = append(spec.Members, api.MobilityPoolMember{NodeRef: "onprem-router-b", Site: "onprem", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}})
				spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
			},
			want: "duplicates staticOwnedAddresses",
		},
		{
			name: "handover from missing",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "missing", ToNodeRef: "azure-router"}}
			},
			want: "fromNodeRef \"missing\" must be one of the member nodeRefs",
		},
		{
			name: "handover from must be onprem",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "azure-router", ToNodeRef: "onprem-router"}}
			},
			want: "must reference an onprem member",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := api.MobilityPoolSpec{
				Prefix:   "10.88.60.0/24",
				GroupRef: "cloudedge",
				Members: []api.MobilityPoolMember{
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

func mobilityPoolRouter(spec api.MobilityPoolSpec, extra ...api.Resource) *api.Router {
	resources := []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec:     spec,
	}}
	resources = append(resources, extra...)
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: resources},
	}
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
