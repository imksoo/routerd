// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNormalizeMobilityPoolExpandsCloudCaptureProfile(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
		Values: map[string]string{
			"nic":    "nic-a",
			"subnet": "subnet-a",
			"region": "us-east-1",
		},
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"aws-edge": {
				Capture: api.MobilityMemberCapture{
					Type:        "provider-secondary-ip",
					ProviderRef: "aws-provider",
					NICRef:      "nic-a",
					Target:      map[string]string{"resourceGroup": "explicit"},
					TargetFrom:  map[string]string{"region": "region"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:                  "provider-private-ip",
					SubnetRefFrom:         "subnet",
					StoppedInstancePolicy: "release",
				},
			},
		}},
	}
	members := []api.ResolvedMobilityPoolMember{{
		NodeRef:    "aws-a",
		Site:       "aws",
		Role:       "cloud",
		ProfileRef: "aws-edge",
		Capture: api.MobilityMemberCapture{
			Target: map[string]string{"region": "override-region"},
		},
		Placement: api.MobilityMemberPlacement{Group: "aws-edge"},
	}}
	got, err := NormalizeResolvedMobilityPoolMembers(spec, members, "aws-a")
	if err != nil {
		t.Fatalf("NormalizeMobilityPool: %v", err)
	}
	member := got[0]
	if member.Capture.ProviderRef != "aws-provider" {
		t.Fatalf("capture provider = %q, want aws-provider", member.Capture.ProviderRef)
	}
	if member.Capture.NICRef != "nic-a" {
		t.Fatalf("nicRef = %q, want nic-a", member.Capture.NICRef)
	}
	if member.Capture.Target["region"] != "override-region" {
		t.Fatalf("target region = %q, want explicit member override", member.Capture.Target["region"])
	}
	if member.OwnershipDiscovery.SubnetRef != "subnet-a" {
		t.Fatalf("subnetRef = %q, want subnet-a", member.OwnershipDiscovery.SubnetRef)
	}
	if member.OwnershipDiscovery.ProviderRef != "aws-provider" {
		t.Fatalf("discovery providerRef = %q, want inherited aws-provider", member.OwnershipDiscovery.ProviderRef)
	}
	if member.OwnershipDiscovery.StoppedInstancePolicy != "release" {
		t.Fatalf("stoppedInstancePolicy = %q, want release", member.OwnershipDiscovery.StoppedInstancePolicy)
	}
	if member.Placement.Priority != 10 {
		t.Fatalf("placement priority = %d, want auto 10", member.Placement.Priority)
	}
}

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

func TestNormalizeMobilityPoolCopiesAndOverridesCaptureExcludes(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"onprem": {Capture: api.MobilityMemberCapture{Type: "proxy-arp", ExcludeAddresses: []string{"10.77.60.1/32"}}},
		}},
	}
	members := []api.ResolvedMobilityPoolMember{{
		NodeRef:    "onprem-a",
		Site:       "aws",
		Role:       "cloud",
		ProfileRef: "onprem",
		Capture:    api.MobilityMemberCapture{ExcludeAddresses: []string{"10.77.60.254/32"}},
	}}
	got, err := NormalizeResolvedMobilityPoolMembers(spec, members, "onprem-a")
	if err != nil {
		t.Fatalf("NormalizeMobilityPool: %v", err)
	}
	want := []string{"10.77.60.254/32"}
	if len(got) != 1 || len(got[0].Capture.ExcludeAddresses) != 1 || got[0].Capture.ExcludeAddresses[0] != want[0] {
		t.Fatalf("capture excludeAddresses = %#v, want %#v", got[0].Capture.ExcludeAddresses, want)
	}
	got[0].Capture.ExcludeAddresses[0] = "mutated"
	if members[0].Capture.ExcludeAddresses[0] != "10.77.60.254/32" {
		t.Fatalf("NormalizeResolvedMobilityPoolMembers returned aliases into input: %#v", members[0].Capture.ExcludeAddresses)
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
