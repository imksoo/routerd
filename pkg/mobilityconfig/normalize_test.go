// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNormalizeMobilityPoolRejectsRemoteLocalOverlay(t *testing.T) {
	base := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
	}
	baseMembers := []api.ResolvedMobilityPoolMember{
		{NodeRef: "aws-a", Site: "aws", Role: "cloud"},
		{
			NodeRef:         "azure-a",
			Site:            "azure",
			Role:            "cloud",
			Placement:       api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
			Maintenance:     api.MobilityMemberMaintenance{Drain: true},
			MaxSecondaryIPs: 8,
		},
	}
	if _, err := NormalizeResolvedMobilityPoolMembers(base, baseMembers, "aws-a"); err != nil {
		t.Fatalf("NormalizeMobilityPool identity-only remote member: %v", err)
	}

	tests := []struct {
		name string
		mut  func(*api.ResolvedMobilityPoolMember)
	}{
		{
			name: "profile",
			mut:  func(member *api.ResolvedMobilityPoolMember) { member.ProfileRef = "azure-self" },
		},
		{
			name: "capture",
			mut: func(member *api.ResolvedMobilityPoolMember) {
				member.Capture = api.MobilityMemberCapture{CaptureStrategy: "route-table"}
			},
		},
		{
			name: "static owner",
			mut:  func(member *api.ResolvedMobilityPoolMember) { member.StaticOwnedAddresses = []string{"10.77.60.10/32"} },
		},
		{
			name: "discovery",
			mut: func(member *api.ResolvedMobilityPoolMember) {
				member.OwnershipDiscovery = api.MobilityOwnershipDiscovery{Sources: []api.MobilityOwnershipDiscoverySource{{Type: "arp-observer"}}}
			},
		},
		{
			name: "stopped instance policy",
			mut: func(member *api.ResolvedMobilityPoolMember) {
				member.OwnershipDiscovery = api.MobilityOwnershipDiscovery{StoppedInstancePolicy: "release"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			members := copyTestMembers(baseMembers)
			tt.mut(&members[1])
			_, err := NormalizeResolvedMobilityPoolMembers(base, members, "aws-a")
			if err == nil || !strings.Contains(err.Error(), "carries a local overlay") || !strings.Contains(err.Error(), "resolved member[1]") {
				t.Fatalf("NormalizeMobilityPool error = %v, want remote identity-only rejection", err)
			}
		})
	}
}

func TestNormalizeMobilityPoolRejectsMissingProfileValue(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"aws-edge": {Capture: api.MobilityMemberCapture{TargetFrom: map[string]string{"region": "missing"}}},
		}},
	}
	_, err := NormalizeResolvedMobilityPoolMembers(spec, []api.ResolvedMobilityPoolMember{{NodeRef: "aws-a", Site: "aws", Role: "cloud", ProfileRef: "aws-edge"}}, "aws-a")
	if err == nil || !strings.Contains(err.Error(), "spec.values") {
		t.Fatalf("NormalizeMobilityPool err = %v, want missing spec.values", err)
	}
}

func TestNormalizeMobilityPoolRejectsLegacyCaptureTargetNICRef(t *testing.T) {
	for name, capture := range map[string]api.MobilityMemberCapture{
		"target":     {Target: map[string]string{"nicRef": "eni-a"}},
		"targetFrom": {TargetFrom: map[string]string{"nicRef": "nic"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeResolvedMobilityPoolMembers(api.MobilityPoolSpec{
				Prefix:   "10.77.60.0/24",
				GroupRef: "cloudedge",
			}, []api.ResolvedMobilityPoolMember{{
				NodeRef: "aws-a",
				Site:    "aws",
				Role:    "cloud",
				Capture: capture,
			}}, "aws-a")
			if err == nil || !strings.Contains(err.Error(), "nicRef is not supported") {
				t.Fatalf("NormalizeMobilityPool error = %v, want legacy nicRef rejection", err)
			}
		})
	}
}

func copyTestMembers(in []api.ResolvedMobilityPoolMember) []api.ResolvedMobilityPoolMember {
	out := make([]api.ResolvedMobilityPoolMember, len(in))
	for i, member := range in {
		out[i] = copyMember(member)
	}
	return out
}
