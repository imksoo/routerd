// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDecisionEligibleForCaptureAllowsCrossProviderRemoteHome(t *testing.T) {
	self := memberPlanInfo{
		NodeRef: "aws-router-a",
		Site:    "aws",
		Capture: api.AddressCapture{
			Type:        "provider-secondary-ip",
			ProviderRef: "aws-provider",
		},
		PlacementGroup: "aws-edge",
	}
	members := map[string]memberPlanInfo{
		"aws-router-a": self,
		"azure-router": {
			NodeRef:           "azure-router",
			Site:              "azure",
			PlacementGroup:    "azure-edge",
			PlacementPriority: 10,
			Capture: api.AddressCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "azure-provider",
			},
		},
	}
	decision := ownershipDecision{
		Address:            "10.77.60.11/32",
		Class:              ownershipClassRemoteHomeOwned,
		HomeOwnerNode:      "azure-router",
		HomeProviderRef:    "azure-provider",
		AdvertiseOwnerNode: "azure-router",
	}

	if !decisionEligibleForCapture(decision, self, members, PlacementDecision{Active: true, ActiveNode: self.NodeRef}) {
		t.Fatalf("cross-provider remote home was not eligible for local ingress capture")
	}
}

func TestDecisionEligibleForCaptureBlocksSamePlacementRemoteHomeWithoutSeize(t *testing.T) {
	self := memberPlanInfo{
		NodeRef:        "aws-router-a",
		Site:           "aws",
		PlacementGroup: "aws-edge",
		Capture: api.AddressCapture{
			Type:        "provider-secondary-ip",
			ProviderRef: "aws-provider",
		},
	}
	members := map[string]memberPlanInfo{
		"aws-router-a": self,
		"aws-router-b": {
			NodeRef:        "aws-router-b",
			Site:           "aws",
			PlacementGroup: "aws-edge",
			Capture: api.AddressCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "aws-provider",
			},
		},
	}
	decision := ownershipDecision{
		Address:            "10.77.60.12/32",
		Class:              ownershipClassRemoteHomeOwned,
		HomeOwnerNode:      "aws-router-b",
		HomeProviderRef:    "aws-provider",
		AdvertiseOwnerNode: "aws-router-b",
	}

	if decisionEligibleForCapture(decision, self, members, PlacementDecision{Active: true, ActiveNode: self.NodeRef}) {
		t.Fatalf("same-placement remote home became eligible without seize")
	}
	if !decisionEligibleForCapture(decision, self, members, PlacementDecision{Active: true, ActiveNode: self.NodeRef, Seize: true}) {
		t.Fatalf("same-placement remote home should be eligible during explicit seize")
	}
}

func TestDecisionEligibleForCaptureBlocksSelfAdvertisedOwner(t *testing.T) {
	self := memberPlanInfo{
		NodeRef: "aws-router-a",
		Site:    "aws",
		Capture: api.AddressCapture{
			Type:        "provider-secondary-ip",
			ProviderRef: "aws-provider",
		},
	}
	decision := ownershipDecision{
		Address:            "10.77.60.13/32",
		Class:              ownershipClassRemoteHomeOwned,
		HomeOwnerNode:      "azure-router",
		HomeProviderRef:    "azure-provider",
		AdvertiseOwnerNode: "aws-router-a",
	}

	if decisionEligibleForCapture(decision, self, nil, PlacementDecision{Active: true, ActiveNode: self.NodeRef}) {
		t.Fatalf("self-advertised owner should not become a capture candidate")
	}
}
