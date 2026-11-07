// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestBGPCommunityAdmissionRejectsUnimplementedPolicies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		peer        bool
		communities api.BGPCommunitiesSpec
		field       string
	}{
		{"router-accept", false, api.BGPCommunitiesSpec{Accept: []string{"64512:1"}}, "communities.accept"},
		{"peer-accept", true, api.BGPCommunitiesSpec{Accept: []string{"64512:1"}}, "communities.accept"},
		{"router-set-in", false, api.BGPCommunitiesSpec{Set: api.BGPCommunitySetSpec{In: []string{"64512:1"}}}, "communities.set.in"},
		{"peer-set-in", true, api.BGPCommunitiesSpec{Set: api.BGPCommunitySetSpec{In: []string{"64512:1"}}}, "communities.set.in"},
		{"peer-set-out", true, api.BGPCommunitiesSpec{Set: api.BGPCommunitySetSpec{Out: []string{"64512:1"}}}, "communities.set.out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routerSpec := api.BGPRouterSpec{ASN: 64512, RouterID: "192.0.2.1"}
			peerSpec := api.BGPPeerSpec{RouterRef: "BGPRouter/main", PeerASN: 64513, Peers: []string{"192.0.2.2"}}
			if tc.peer {
				peerSpec.Communities = tc.communities
			} else {
				routerSpec.Communities = tc.communities
			}
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "main"}, Spec: routerSpec},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: peerSpec},
			}}}
			if err := Validate(router); err == nil || !strings.Contains(err.Error(), tc.field+" is not supported") {
				t.Fatalf("expected explicit unsupported %s: %v", tc.field, err)
			}
		})
	}
}

func TestBGPCommunityAdmissionPreservesOriginatedSetOut(t *testing.T) {
	if err := validateBGPRouterPolicy("BGPRouter/main", api.BGPRouterSpec{Communities: api.BGPCommunitiesSpec{Send: "both", Set: api.BGPCommunitySetSpec{Out: []string{"64512:1", "no-export"}}}}); err != nil {
		t.Fatal(err)
	}
}

func TestBGPPrefixLengthAdmissionRequiresAnExplicitPrefixFilter(t *testing.T) {
	for _, kind := range []string{"BGPRouter", "BGPPeer", "BGPDynamicPeer"} {
		t.Run(kind, func(t *testing.T) {
			bgp := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "core"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "192.0.2.1"}}
			policy := api.BGPImportPolicySpec{AllowedPrefixLengthMin: 32, AllowedPrefixLengthMax: 32}
			resources := []api.Resource{bgp}
			switch kind {
			case "BGPRouter":
				spec := bgp.Spec.(api.BGPRouterSpec)
				spec.ImportPolicy = policy
				resources[0].Spec = spec
			case "BGPPeer":
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/core", PeerASN: 64513, Peers: []string{"192.0.2.2"}, ImportPolicy: policy}})
			case "BGPDynamicPeer":
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "leaves"}, Spec: api.BGPDynamicPeerSpec{RouterRef: "BGPRouter/core", PeerASN: 64513, Listen: api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"192.0.2.0/24"}}, ImportPolicy: policy}})
			}
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: resources}}
			if err := Validate(router); err == nil || !strings.Contains(err.Error(), "allowedPrefixes is required when prefix length bounds are configured") {
				t.Fatalf("length-only %s policy admitted: %v", kind, err)
			}
		})
	}
}
