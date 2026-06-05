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
					Type:         "provider-secondary-ip",
					ProviderRef:  "aws-provider",
					ProviderMode: "secondary-private-ip",
					Target:       map[string]string{"resourceGroup": "explicit"},
					TargetFrom:   map[string]string{"nicRef": "nic", "region": "region"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:          "provider-private-ip",
					SubnetRefFrom: "subnet",
				},
			},
		}},
		Members: []api.MobilityPoolMember{{
			NodeRef:    "aws-a",
			Site:       "aws",
			Role:       "cloud",
			ProfileRef: "aws-edge",
			Capture: api.MobilityMemberCapture{
				Target: map[string]string{"region": "override-region"},
			},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge"},
		}},
	}
	got, diagnostics, err := NormalizeMobilityPool(spec, "aws-a")
	if err != nil {
		t.Fatalf("NormalizeMobilityPool: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
	member := got.Members[0]
	if member.Capture.ProviderRef != "aws-provider" || member.Capture.ProviderMode != "secondary-private-ip" {
		t.Fatalf("capture provider = %q/%q, want aws-provider/secondary-private-ip", member.Capture.ProviderRef, member.Capture.ProviderMode)
	}
	if member.Capture.Target["nicRef"] != "nic-a" {
		t.Fatalf("target nicRef = %q, want nic-a", member.Capture.Target["nicRef"])
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
	if member.Placement.Priority != 10 {
		t.Fatalf("placement priority = %d, want auto 10", member.Placement.Priority)
	}
}

func TestNormalizeMobilityPoolWarnsOnRemoteDetails(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"cloud": {Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip"}},
		}},
		Members: []api.MobilityPoolMember{
			{NodeRef: "aws-a", Site: "aws", Role: "cloud"},
			{NodeRef: "azure-a", Site: "azure", Role: "cloud", ProfileRef: "cloud"},
		},
	}
	_, diagnostics, err := NormalizeMobilityPool(spec, "aws-a")
	if err != nil {
		t.Fatalf("NormalizeMobilityPool: %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != DiagnosticWarning || !strings.Contains(diagnostics[0].Message, "remote member") {
		t.Fatalf("diagnostics = %#v, want one remote-member warning", diagnostics)
	}
}

func TestNormalizeMobilityPoolCopiesAndOverridesCaptureExcludes(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"onprem": {Capture: api.MobilityMemberCapture{Type: "proxy-arp", ExcludeAddresses: []string{"10.77.60.1/32"}}},
		}},
		Members: []api.MobilityPoolMember{{
			NodeRef:    "onprem-a",
			Site:       "aws",
			Role:       "cloud",
			ProfileRef: "onprem",
			Capture:    api.MobilityMemberCapture{ExcludeAddresses: []string{"10.77.60.254/32"}},
		}},
	}
	got, _, err := NormalizeMobilityPool(spec, "onprem-a")
	if err != nil {
		t.Fatalf("NormalizeMobilityPool: %v", err)
	}
	want := []string{"10.77.60.254/32"}
	if len(got.Members) != 1 || len(got.Members[0].Capture.ExcludeAddresses) != 1 || got.Members[0].Capture.ExcludeAddresses[0] != want[0] {
		t.Fatalf("capture excludeAddresses = %#v, want %#v", got.Members[0].Capture.ExcludeAddresses, want)
	}
	got.Members[0].Capture.ExcludeAddresses[0] = "mutated"
	if spec.Members[0].Capture.ExcludeAddresses[0] != "10.77.60.254/32" {
		t.Fatalf("NormalizeMobilityPool returned aliases into input: %#v", spec.Members[0].Capture.ExcludeAddresses)
	}
}

func TestNormalizeMobilityPoolRejectsMissingProfileValue(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.77.60.0/24",
		GroupRef: "cloudedge",
		Profiles: api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
			"aws-edge": {Capture: api.MobilityMemberCapture{TargetFrom: map[string]string{"nicRef": "missing"}}},
		}},
		Members: []api.MobilityPoolMember{{NodeRef: "aws-a", Site: "aws", Role: "cloud", ProfileRef: "aws-edge"}},
	}
	_, _, err := NormalizeMobilityPool(spec, "aws-a")
	if err == nil || !strings.Contains(err.Error(), "spec.values") {
		t.Fatalf("NormalizeMobilityPool err = %v, want missing spec.values", err)
	}
}
