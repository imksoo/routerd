// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestResolveMobilityPoolMembersRejectsTopologyOutsideSAMNodeSet(t *testing.T) {
	t.Run("no source", func(t *testing.T) {
		_, err := ResolveMobilityPoolMembers(nil, api.MobilityPoolSpec{Members: []api.MobilityPoolMemberOverlay{{NodeRef: "onprem-router"}}})
		if err == nil || !strings.Contains(err.Error(), "membersFrom") {
			t.Fatalf("ResolveMobilityPoolMembers error = %v, want membersFrom rejection", err)
		}
	})
	t.Run("unknown overlay node", func(t *testing.T) {
		router := mobilityMembersTestRouter(api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"}}})
		_, err := ResolveMobilityPoolMembers(router, api.MobilityPoolSpec{
			MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge"}},
			Members:     []api.MobilityPoolMemberOverlay{{NodeRef: "injected-router"}},
		})
		if err == nil || !strings.Contains(err.Error(), "not declared") {
			t.Fatalf("ResolveMobilityPoolMembers error = %v, want undeclared node rejection", err)
		}
	})
}

func TestResolveMobilityPoolMembersDoesNotUseOverlayWhileSourceIsMissing(t *testing.T) {
	resolved, err := ResolveMobilityPoolMembers(&api.Router{}, api.MobilityPoolSpec{
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge"}},
		Members:     []api.MobilityPoolMemberOverlay{{NodeRef: "onprem-router", Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}}},
	})
	if err != nil {
		t.Fatalf("ResolveMobilityPoolMembers: %v", err)
	}
	if len(resolved.Members) != 0 {
		t.Fatalf("missing source resolved overlay as topology: %#v", resolved.Members)
	}
	if len(resolved.Sources) != 1 || resolved.Sources[0].Found {
		t.Fatalf("source resolution = %#v, want missing source", resolved.Sources)
	}
}

func mobilityMembersTestRouter(nodeSet api.SAMNodeSetSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec:     nodeSet,
	}}}}
}
