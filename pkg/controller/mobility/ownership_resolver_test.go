// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestOwnershipResolverScenario391BaselineSameSubnetHome(t *testing.T) {
	now := time.Date(2026, 6, 9, 22, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Status: map[string]any{
			"discoveryLocalInventory": []map[string]any{
				{"address": "10.88.60.11/32", "nicRef": "eni-client", "subnetRef": "subnet-a", "providerRef": "aws-provider", "resourceType": "instance-nic"},
			},
			"discoverySelfPrivateIPs": []string{"10.88.60.4"},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassLocalHomeOwned || decision.AdvertiseOwnerNode != "aws-router-a" || decision.AdvertiseReason != "local-home-inventory" {
		t.Fatalf("decision = %#v, want local home direct advertisement", decision)
	}
}

func TestOwnershipResolverReleasePolicyIgnoresStoppedLocalInventory(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.IPOwnershipPolicy.StoppedInstancePolicy = "release"
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Status: map[string]any{
			"discoveryLocalInventory": []map[string]any{
				{
					"address":       "10.88.60.11/32",
					"nicRef":        "eni-stopped",
					"subnetRef":     "subnet-a",
					"providerRef":   "aws-provider",
					"resourceType":  "instance-nic",
					"resourceRef":   "i-stopped",
					"instanceState": "stopped",
				},
			},
			"discoverySelfPrivateIPs": []string{"10.88.60.4"},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassUnknown || decision.AdvertiseOwnerNode != "" || decision.SuppressionReason != "stopped-instance-release" {
		t.Fatalf("decision = %#v, want stopped local inventory released instead of advertised as local home", decision)
	}
}

func TestOwnershipResolverScenario392SameProviderConfirmedCapture(t *testing.T) {
	now := time.Date(2026, 6, 9, 22, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", "10.88.60.11/32", "assign-secondary-ip", now.Add(-time.Second))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName:      "cloudedge",
		SelfNode:      "aws-router-b",
		Spec:          spec,
		Status:        map[string]any{"discoverySelfPrivateIPs": []string{"10.88.60.11"}},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassConfirmedCapture {
		t.Fatalf("decision = %#v, want confirmed capture separated from router self", decision)
	}
	if decision.CaptureState != captureStateConfirmed || decision.CaptureHolderNode != "aws-router-b" {
		t.Fatalf("decision = %#v, want confirmed same-provider capture state", decision)
	}
}

func TestOwnershipResolverScenario394RouteTablePreviousPlanIsStaleUntilConfirmed(t *testing.T) {
	now := time.Date(2026, 6, 9, 22, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.Members[2].Capture.CaptureStrategy = captureStrategyRouteTable
	spec.Members[2].Capture.Target = map[string]string{"routeTableRef": "rtb-123"}
	plan := dynamicconfig.ActionPlan{
		Provider:    "aws",
		ProviderRef: "aws-provider",
		Action:      actionAssignRouteTableRoute,
		Target: map[string]string{
			"address":         "10.88.60.12/32",
			"providerRef":     "aws-provider",
			"captureStrategy": captureStrategyRouteTable,
			"routeTableRef":   "rtb-123",
		},
		Parameters: map[string]string{captureParamHolder: "aws-router-b"},
	}
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName:      "cloudedge",
		SelfNode:      "aws-router-b",
		Spec:          spec,
		PreviousPlans: []dynamicconfig.ActionPlan{plan},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.12/32")
	if decision.Class != ownershipClassStaleCapture || decision.CaptureStrategy != captureStrategyRouteTable || decision.CaptureTargetRef != "rtb-123" {
		t.Fatalf("decision = %#v, want route-table capture target normalized as stale until journal/inventory confirms", decision)
	}
}

func TestOwnershipResolverScenario397MigrationExpiredOldHomeNewLocalHome(t *testing.T) {
	now := time.Date(2026, 6, 9, 22, 15, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	old := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", "10.88.60.11/32", "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.11",
		NICRef:    "oci-client",
		SubnetRef: "oci-subnet",
	}, now.Add(-10*time.Minute), time.Minute)
	expired := providerDiscoveryExpiredEvent("cloudedge", "cloudedge", "oci-router", "10.88.60.11/32", old, now.Add(-5*time.Minute), time.Minute)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Events:   []routerstate.EventRecord{old, expired},
		Status: map[string]any{
			"discoveryLocalInventory": []map[string]any{
				{"address": "10.88.60.11/32", "nicRef": "eni-client", "subnetRef": "subnet-a", "providerRef": "aws-provider"},
			},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassLocalHomeOwned || decision.HomeOwnerNode != "aws-router-a" {
		t.Fatalf("decision = %#v, want expired remote home ignored and new local home selected", decision)
	}
}

func TestOwnershipResolverScenario398RemoteHomeKeepsConfirmedCrossCapture(t *testing.T) {
	now := time.Date(2026, 6, 9, 22, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	homeEvent := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "aws-router-a", "10.88.60.11/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.11",
		NICRef:    "eni-client",
		SubnetRef: "subnet-a",
	}, now.Add(-time.Second), time.Hour)
	action := resolverSucceededAction(t, "oci-provider", "oci-vnic", "oci-router", "10.88.60.11/32", "assign-secondary-ip", now.Add(-time.Second))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "oci-router",
		Spec:     spec,
		Events:   []routerstate.EventRecord{homeEvent},
		Status: map[string]any{
			"discoverySelfPrivateIPs":        []string{"10.88.60.250/32"},
			"discoverySelfCapturedAddresses": []string{"10.88.60.11/32"},
		},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassConfirmedCapture || decision.HomeOwnerNode != "aws-router-a" || decision.AdvertiseOwnerNode != "" || decision.AdvertiseReason != "confirmed-cross-provider-capture" {
		t.Fatalf("decision = %#v, want confirmed OCI capture retained for remote AWS home", decision)
	}
}

func TestOwnershipResolverDoesNotClassifyCapturedSecondaryAsRouterSelf(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 55, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Status: map[string]any{
			"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
			"discoverySelfCapturedAddresses": []string{"10.88.60.12/32"},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	primary := ownershipDecisionByAddress(t, decisions, "10.88.60.4/32")
	if primary.Class != ownershipClassLocalRouterSelf {
		t.Fatalf("primary decision = %#v, want router self", primary)
	}
	captured := ownershipDecisionByAddress(t, decisions, "10.88.60.12/32")
	if captured.Class == ownershipClassLocalRouterSelf {
		t.Fatalf("captured decision = %#v, want captured secondary not classified as router self", captured)
	}
}

func TestOwnershipResolverSelfCapturedSecondaryIsNotLocalHomeOwned(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	plan := dynamicconfig.ActionPlan{
		Provider:    "aws",
		ProviderRef: "aws-provider",
		Action:      actionAssignSecondaryIP,
		Target: map[string]string{
			"address":     "10.88.60.12/32",
			"providerRef": "aws-provider",
			"nicRef":      "eni-a",
		},
		Parameters: map[string]string{captureParamHolder: "aws-router-a"},
	}
	selfEvent := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "aws-router-a", "10.88.60.12/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.12",
		NICRef:       "eni-a",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-a",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName:      "cloudedge",
		SelfNode:      "aws-router-a",
		Spec:          spec,
		Events:        []routerstate.EventRecord{selfEvent},
		PreviousPlans: []dynamicconfig.ActionPlan{plan},
		Status: map[string]any{
			"discoverySelfResourceRef":       "i-aws-a",
			"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
			"discoverySelfCapturedAddresses": []string{"10.88.60.12/32"},
			"discoveryLocalInventory":        []map[string]any{{"address": "10.88.60.12/32", "nicRef": "eni-a", "subnetRef": "subnet-aws", "providerRef": "aws-provider", "resourceRef": "i-aws-a", "resourceType": "instance-nic"}},
			"discoveryOwnedAddresses":        []string{"10.88.60.12/32"},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.12/32")
	if decision.Class != ownershipClassConfirmedCapture || decision.CaptureState != captureStateConfirmed || decision.AdvertiseOwnerNode != "aws-router-a" {
		t.Fatalf("decision = %#v, want self captured secondary confirmed from provider observation instead of LocalHomeOwned", decision)
	}
}

func TestOwnershipResolverConfirmedSelfCapturedSecondaryAdvertisesAsCapture(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	action := resolverSucceededAction(t, "aws-provider", "eni-a", "aws-router-a", "10.88.60.12/32", actionAssignSecondaryIP, now.Add(-time.Second))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName:      "cloudedge",
		SelfNode:      "aws-router-a",
		Spec:          spec,
		Status:        map[string]any{"discoverySelfCapturedAddresses": []string{"10.88.60.12/32"}},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.12/32")
	if decision.Class != ownershipClassConfirmedCapture || decision.AdvertiseOwnerNode != "aws-router-a" {
		t.Fatalf("decision = %#v, want confirmed self captured secondary advertised as capture", decision)
	}
}

func TestProviderInventoryHomeOwnerFactsExcludeRouterNICPrimary(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	event := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "aws-router-b", "10.88.60.5/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.5",
		NICRef:       "eni-b",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-b",
		ResourceType: "router-nic",
		Primary:      true,
	}, now.Add(-time.Second), time.Hour)
	facts := providerInventoryHomeOwnerFacts("cloudedge", spec, []routerstate.EventRecord{event}, now)
	if _, ok := facts["10.88.60.5/32"]; ok {
		t.Fatalf("facts = %#v, want router/member primary excluded from home-owner facts", facts)
	}
}

func TestOwnershipResolverNilStatusValuesDoNotLeakNilStrings(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 15, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Status: map[string]any{
			"discoverySelfResourceRef": nil,
			"discoverySelfPrivateIPs":  []string{"10.88.60.4/32"},
			"discoverySelfSubnetRef":   nil,
			"discoveryLocalInventory": []map[string]any{
				{"address": "10.88.60.11/32", "nicRef": nil, "subnetRef": nil, "providerRef": nil, "resourceRef": nil, "resourceType": nil},
			},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	localHome := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if localHome.Class != ownershipClassLocalHomeOwned {
		t.Fatalf("localHome = %#v, want nil resource refs not to remove local inventory", localHome)
	}
	routerSelf := ownershipDecisionByAddress(t, decisions, "10.88.60.4/32")
	for _, decision := range []ownershipDecision{localHome, routerSelf} {
		if ownershipDecisionContainsNilString(decision) {
			t.Fatalf("decision = %#v, want no <nil> status string leaks", decision)
		}
	}
}

func ownershipDecisionContainsNilString(decision ownershipDecision) bool {
	values := []string{
		decision.Address,
		decision.Class,
		decision.HomeOwnerNode,
		decision.HomeProviderRef,
		decision.HomeSubnetRef,
		decision.HomeNICRef,
		decision.CaptureHolderNode,
		decision.CaptureProviderRef,
		decision.CaptureTargetRef,
		decision.CaptureStrategy,
		decision.CaptureState,
		decision.AdvertiseOwnerNode,
		decision.AdvertiseReason,
		decision.SuppressionReason,
		decision.Source,
	}
	for _, value := range values {
		if value == "<nil>" {
			return true
		}
	}
	return false
}

func ownershipDecisionByAddress(t *testing.T, decisions []ownershipDecision, address string) ownershipDecision {
	t.Helper()
	for _, decision := range decisions {
		if decision.Address == address {
			return decision
		}
	}
	t.Fatalf("address %s not found in %#v", address, decisions)
	return ownershipDecision{}
}

func resolverSucceededAction(t *testing.T, providerRef, targetRef, holder, address, action string, at time.Time) routerstate.ActionExecutionRecord {
	t.Helper()
	target, err := json.Marshal(map[string]string{"address": address, "providerRef": providerRef, "nicRef": targetRef})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	params, err := json.Marshal(map[string]string{captureParamHolder: holder})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return routerstate.ActionExecutionRecord{
		ID:             at.UnixNano(),
		Provider:       strings.TrimSuffix(providerRef, "-provider"),
		ProviderRef:    providerRef,
		Action:         action,
		TargetJSON:     string(target),
		ParametersJSON: string(params),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     at,
		UpdatedAt:      at,
	}
}
