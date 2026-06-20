// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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
			"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
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

func TestOwnershipResolverSkipsUnresolvedReturnRoutePeer(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 58, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Status: map[string]any{
			"discoveryLocalInventory": []map[string]any{
				{"address": "10.88.60.5/32", "nicRef": "eni-peer", "subnetRef": "subnet-a", "providerRef": "aws-provider", "resourceType": "instance-nic", "primary": true},
			},
			"discoverySelfPrivateIPs": []string{"10.88.60.4"},
		},
		BGPReturnRoutes: map[string]bool{
			"10.88.60.5/32": true,
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	for _, decision := range decisions {
		if decision.Address == "10.88.60.5/32" {
			t.Fatalf("return-route peer leaked into ownership resolver decisions: %#v", decision)
		}
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
		Status:        map[string]any{"discoverySelfCapturedAddresses": []string{"10.88.60.11"}},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		BGPHomeOwnerNodes: map[string]string{
			"10.88.60.11/32": "aws-router-a",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassConfirmedCapture {
		t.Fatalf("decision = %#v, want confirmed capture separated from router self", decision)
	}
	if decision.HomeOwnerNode != "aws-router-a" {
		t.Fatalf("decision = %#v, want confirmed capture to retain remote home owner", decision)
	}
	if decision.AdvertiseOwnerNode != "" {
		t.Fatalf("decision = %#v, confirmed capture must not advertise as owner", decision)
	}
	if decision.CaptureState != captureStateConfirmed || decision.CaptureHolderNode != "aws-router-b" {
		t.Fatalf("decision = %#v, want confirmed same-provider capture state", decision)
	}
}

func TestOwnershipResolverClearsDisprovedStaleCaptureForRemoteBGPOwner(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", address, "assign-secondary-ip", now.Add(-time.Minute))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Status: map[string]any{
			"discoverySelfPrivateIPs":        []string{"10.88.60.6/32"},
			"discoverySelfCapturedAddresses": []string{},
			"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		BGPHomeOwnerNodes: map[string]string{
			address: "aws-router-a",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class != ownershipClassRemoteHomeOwned || decision.CaptureState != captureStateNone || decision.CaptureHolderNode != "" {
		t.Fatalf("decision = %#v, want remote BGP owner without stale capture after provider inventory disproves self capture", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverStaleCount"] != 0 {
		t.Fatalf("status = %#v, want no stale claims for disproved standby capture", status)
	}
}

func TestOwnershipResolverClearsDisprovedStaleCaptureForStaticRemoteOwner(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 35, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.10/32"
	spec.Members[0].StaticOwnedAddresses = []string{address}
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", address, "assign-secondary-ip", now.Add(-time.Minute))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Status: map[string]any{
			"discoverySelfPrivateIPs":        []string{"10.88.60.6/32"},
			"discoverySelfCapturedAddresses": []string{},
			"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class != ownershipClassRemoteHomeOwned || decision.SuppressionReason != "static-owned-by-remote" || decision.CaptureState != captureStateNone {
		t.Fatalf("decision = %#v, want static remote owner without stale capture after provider inventory disproves self capture", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverStaleCount"] != 0 {
		t.Fatalf("status = %#v, want no stale claims for disproved static remote capture", status)
	}
}

func TestOwnershipResolverKeepsObservedSelfCapture(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 40, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", address, "assign-secondary-ip", now.Add(-time.Minute))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Status: map[string]any{
			"discoverySelfPrivateIPs":        []string{"10.88.60.6/32"},
			"discoverySelfCapturedAddresses": []string{address},
			"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		BGPHomeOwnerNodes: map[string]string{
			address: "aws-router-a",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class != ownershipClassConfirmedCapture || decision.CaptureState != captureStateConfirmed || decision.CaptureHolderNode != "aws-router-b" {
		t.Fatalf("decision = %#v, want observed self capture to stay confirmed", decision)
	}
}

func TestOwnershipResolverDoesNotConfirmCaptureWithoutProviderObservation(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 45, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", address, "assign-secondary-ip", now.Add(-time.Minute))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Status:   map[string]any{},
		ActionJournal: []routerstate.ActionExecutionRecord{
			action,
		},
		BGPHomeOwnerNodes: map[string]string{
			address: "aws-router-a",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class == ownershipClassConfirmedCapture || decision.CaptureState == captureStateConfirmed {
		t.Fatalf("decision = %#v, action journal without provider observation must not confirm capture", decision)
	}
	if decision.CaptureState != captureStateStale {
		t.Fatalf("decision = %#v, want historical assign kept only as stale diagnostic evidence", decision)
	}
}

func TestOwnershipResolverDoesNotClearOtherHolderStaleCapture(t *testing.T) {
	address := "10.88.60.11/32"
	decision := ownershipDecision{
		Address:            address,
		Class:              ownershipClassRemoteHomeOwned,
		HomeOwnerNode:      "aws-router-a",
		CaptureHolderNode:  "aws-router-c",
		CaptureProviderRef: "aws-provider",
		CaptureTargetRef:   "eni-c",
		CaptureStrategy:    captureStrategySecondaryIP,
		CaptureState:       captureStateStale,
	}

	clearDisprovedStaleCapture(&decision, "aws-router-b", map[string]bool{}, true, address)

	if decision.Class != ownershipClassRemoteHomeOwned || decision.CaptureState != captureStateStale || decision.CaptureHolderNode != "aws-router-c" {
		t.Fatalf("decision = %#v, want self observation not to clear another holder stale capture", decision)
	}
}

func TestOwnershipResolverDoesNotConfirmRouterPrimaryFromActionJournal(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	action := resolverSucceededAction(t, "aws-provider", "eni-a", "aws-router-a", "10.88.60.4/32", actionAssignSecondaryIP, now.Add(-time.Second))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Status: map[string]any{
			"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
			"discoverySelfCapturedAddresses": []string{},
			"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.4/32")
	if decision.Class != ownershipClassLocalRouterSelf || decision.AdvertiseOwnerNode != "" {
		t.Fatalf("decision = %#v, want router primary to remain non-advertised LocalRouterSelf", decision)
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
			"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
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

func TestOwnershipResolverScenario398RemoteHomeSuppressesCrossCapture(t *testing.T) {
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
			"discoverySelfPrivateIPs": []string{"10.88.60.11"},
		},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassStaleCapture || decision.HomeOwnerNode != "aws-router-a" || decision.SuppressionReason != "fresh-home-owner" {
		t.Fatalf("decision = %#v, want remote AWS home to mark OCI capture stale", decision)
	}
}

func TestOwnershipResolverReportsRemoteHomeLocalInventoryConflict(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	homeEvent := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", "10.88.60.11/32", "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-a",
		Spec:     spec,
		Events:   []routerstate.EventRecord{homeEvent},
		Status: map[string]any{
			"discoveryLocalInventory": []map[string]any{
				{
					"address":      "10.88.60.11/32",
					"nicRef":       "eni-client",
					"subnetRef":    "subnet-a",
					"providerRef":  "aws-provider",
					"resourceRef":  "i-aws-client",
					"resourceType": "instance-nic",
				},
			},
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "oci-router" {
		t.Fatalf("decision = %#v, want remote home owner preserved", decision)
	}
	if decision.ConflictReason != "remote-home-owner-overlaps-local-inventory" {
		t.Fatalf("decision = %#v, want local/remote inventory conflict", decision)
	}
	if decision.LocalProviderRef != "aws-provider" || decision.LocalSubnetRef != "subnet-a" || decision.LocalNICRef != "eni-client" || decision.LocalResourceRef != "i-aws-client" {
		t.Fatalf("decision = %#v, want local inventory refs recorded", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverPhase"] != "Conflict" || status["ownershipResolverConflictCount"] != 1 {
		t.Fatalf("status = %#v, want conflict phase and count", status)
	}
	items := status["ownershipResolverDecisions"].([]map[string]any)
	item := items[0]
	if item["conflictReason"] != "remote-home-owner-overlaps-local-inventory" || item["localProviderRef"] != "aws-provider" || item["homeProviderRef"] != "oci-provider" {
		t.Fatalf("decision status item = %#v, want remote/local conflict refs", item)
	}
	conflicts := status["ownershipResolverConflicts"].([]map[string]any)
	if len(conflicts) != 1 || conflicts[0]["address"] != "10.88.60.11/32" || conflicts[0]["localNICRef"] != "eni-client" {
		t.Fatalf("conflicts = %#v, want address and local refs", conflicts)
	}
	ownerTable := status["ownershipResolverOwnerTable"].([]map[string]any)
	if len(ownerTable) != 1 {
		t.Fatalf("owner table = %#v, want one row", ownerTable)
	}
	row := ownerTable[0]
	if row["state"] != "Conflict" || row["ownerNode"] != "oci-router" || row["ownerProviderRef"] != "oci-provider" || row["localNode"] != "aws-router-a" || row["localProviderRef"] != "aws-provider" {
		t.Fatalf("owner table row = %#v, want remote owner and local inventory conflict", row)
	}
	controlTable := status["ownershipResolverControlPlaneOwnerTable"].([]map[string]any)
	if len(controlTable) != 1 {
		t.Fatalf("control-plane owner table = %#v, want one row", controlTable)
	}
	controlRow := controlTable[0]
	if controlRow["state"] != "Conflict" ||
		controlRow["ownerNode"] != "oci-router" ||
		controlRow["ownerProviderRef"] != "oci-provider" ||
		controlRow["ownerNICRef"] != "oci-client" ||
		controlRow["ownerResourceRef"] != "ocid1.instance.oc1.test.client" ||
		controlRow["localEvidenceNode"] != "aws-router-a" ||
		controlRow["localEvidenceProviderRef"] != "aws-provider" ||
		controlRow["localEvidenceNICRef"] != "eni-client" ||
		controlRow["localEvidenceResourceRef"] != "i-aws-client" ||
		controlRow["conflictReason"] != "remote-home-owner-overlaps-local-inventory" {
		t.Fatalf("control-plane owner table row = %#v, want centralized conflict evidence", controlRow)
	}
	verdicts := status["ownershipResolverFIBVerdicts"].([]map[string]any)
	if len(verdicts) != 1 || verdicts[0]["address"] != "10.88.60.11/32" || verdicts[0]["action"] != "local-route" {
		t.Fatalf("fib verdicts = %#v, want local-route for conflict with local evidence", verdicts)
	}
}

func TestOwnershipResolverStatusDistinguishesStaleConflictAndUnknown(t *testing.T) {
	decisions := []ownershipDecision{
		{
			Address:            "10.88.60.11/32",
			Class:              ownershipClassStaleCapture,
			Source:             "provider-action",
			CaptureState:       captureStateStale,
			CaptureHolderNode:  "aws-router-a",
			SuppressionReason:  "capture-not-desired",
			CaptureTargetRef:   "eni-a",
			CaptureProviderRef: "aws-provider",
		},
		{
			Address: "10.88.60.12/32",
			Class:   ownershipClassUnknown,
			Source:  "bgp-rib",
		},
		{
			Address:        "10.88.60.13/32",
			Class:          ownershipClassRemoteHomeOwned,
			Source:         providerDiscoverySource,
			HomeOwnerNode:  "oci-router",
			ConflictReason: "remote-home-owner-overlaps-local-inventory",
			LocalNodeRef:   "aws-router-a",
			LocalSource:    "local-inventory",
		},
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverPhase"] != "Conflict" || status["ownershipResolverConflictCount"] != 1 {
		t.Fatalf("status = %#v, want conflict phase with one conflict", status)
	}
	if status["ownershipResolverStaleCount"] != 1 || status["ownershipResolverUnknownCount"] != 1 {
		t.Fatalf("status = %#v, want explicit stale and unknown counts", status)
	}
	stale := status["ownershipResolverStaleClaims"].([]map[string]any)
	if len(stale) != 1 || stale[0]["address"] != "10.88.60.11/32" || stale[0]["state"] != "Stale" || stale[0]["suppressionReason"] != "capture-not-desired" {
		t.Fatalf("stale claims = %#v, want stale capture row", stale)
	}
	unknown := status["ownershipResolverUnknownClaims"].([]map[string]any)
	if len(unknown) != 1 || unknown[0]["address"] != "10.88.60.12/32" || unknown[0]["state"] != "Unknown" || unknown[0]["source"] != "bgp-rib" {
		t.Fatalf("unknown claims = %#v, want unknown BGP row", unknown)
	}
	ownerTable := status["ownershipResolverOwnerTable"].([]map[string]any)
	rows := map[string]map[string]any{}
	for _, row := range ownerTable {
		rows[row["address"].(string)] = row
	}
	if rows["10.88.60.11/32"]["state"] != "Stale" || rows["10.88.60.12/32"]["state"] != "Unknown" || rows["10.88.60.13/32"]["state"] != "Conflict" {
		t.Fatalf("owner table = %#v, want distinct stale/unknown/conflict states", ownerTable)
	}
	controlTable := status["ownershipResolverControlPlaneOwnerTable"].([]map[string]any)
	controlRows := map[string]map[string]any{}
	for _, row := range controlTable {
		controlRows[row["address"].(string)] = row
	}
	if controlRows["10.88.60.11/32"]["state"] != "Stale" ||
		controlRows["10.88.60.11/32"]["captureHolderNode"] != "aws-router-a" ||
		controlRows["10.88.60.12/32"]["state"] != "Unknown" ||
		controlRows["10.88.60.13/32"]["state"] != "Conflict" {
		t.Fatalf("control-plane owner table = %#v, want stale/unknown/conflict rows preserved", controlTable)
	}
}

func TestOwnershipResolverReportsRemoteHomeLocalOwnershipEventConflict(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 25, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	cloudHome := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "eni-client",
		SubnetRef:    "subnet-a",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	onPremObserved := onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "onprem-router", address, onPremObservation{
		Address:    address,
		MAC:        "02:00:00:00:00:11",
		Interface:  "lan0",
		SourceType: OnPremSourceARPObserver,
		ObservedAt: now,
	}, now, time.Hour)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "onprem-router",
		Spec:     spec,
		Events:   []routerstate.EventRecord{cloudHome, onPremObserved},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "aws-router-a" || decision.LocalNodeRef != "onprem-router" {
		t.Fatalf("decision = %#v, want remote cloud owner with local onprem observation recorded", decision)
	}
	if decision.ConflictReason != "remote-home-owner-overlaps-local-ownership-event" || decision.LocalSource != onPremDiscoverySource || decision.LocalSourceType != OnPremSourceARPObserver {
		t.Fatalf("decision = %#v, want onprem ownership event conflict", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverPhase"] != "Conflict" || status["ownershipResolverConflictCount"] != 1 {
		t.Fatalf("status = %#v, want conflict phase and count", status)
	}
	ownerTable := status["ownershipResolverOwnerTable"].([]map[string]any)
	row := ownerTable[0]
	if row["state"] != "Conflict" || row["ownerNode"] != "aws-router-a" || row["localNode"] != "onprem-router" || row["localSource"] != onPremDiscoverySource || row["localSourceType"] != OnPremSourceARPObserver {
		t.Fatalf("owner table row = %#v, want cloud/onprem conflict", row)
	}
	verdicts := status["ownershipResolverFIBVerdicts"].([]map[string]any)
	if len(verdicts) != 1 || verdicts[0]["address"] != address || verdicts[0]["action"] != "local-route" {
		t.Fatalf("fib verdicts = %#v, want local-route for onprem conflict evidence", verdicts)
	}
}

func TestOwnershipResolverTreatsOnPremObservedStatusAsLocalOwner(t *testing.T) {
	now := time.Date(2026, 6, 18, 6, 40, 0, 0, time.UTC)
	spec := api.MobilityPoolSpec{
		Prefix: "192.168.123.0/24",
		Members: []api.MobilityPoolMember{
			{
				NodeRef: "pve-rt08",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:      "proxy-arp",
					Interface: "svnet1",
					ExcludeAddresses: []string{
						"192.168.123.1/32",
					},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode: "onprem-l2",
					Scope: api.MobilityOwnershipDiscoveryScope{
						ExcludeAddresses: []string{"192.168.123.1/32"},
					},
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: OnPremSourceARPObserver, Interface: "svnet1"},
						{Type: OnPremSourceOnDemandARP, Interface: "svnet1"},
						{Type: OnPremSourcePVESVNet, Interface: "svnet1", Network: "svnet1"},
					},
				},
			},
		},
	}
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "svnet1",
		SelfNode: "pve-rt08",
		Spec:     spec,
		Status: map[string]any{
			"interface":  "svnet1",
			"network":    "svnet1",
			"sourceType": OnPremSourcePVESVNet,
			"observedClients": `[{"ip":"192.168.123.1","mac":"b6:83:16:4a:f1:88","sourceType":"pve-svnet","seenAt":"2026-06-18T06:31:57.938717262Z"},` +
				`{"ip":"192.168.123.129","mac":"bc:24:11:82:0d:3f","sourceType":"pve-svnet","seenAt":"2026-06-18T06:35:27.444010391Z"}]`,
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want only non-excluded observed client", decisions)
	}
	decision := ownershipDecisionByAddress(t, decisions, "192.168.123.129/32")
	if decision.Class != ownershipClassLocalHomeOwned || decision.AdvertiseOwnerNode != "pve-rt08" || decision.AdvertiseReason != "ownership-event" {
		t.Fatalf("decision = %#v, want observed onprem client advertised as local owner", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverAddressCount"] != 1 {
		t.Fatalf("status = %#v, want one resolver address", status)
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
	if decision.Class != ownershipClassStaleCapture || decision.SuppressionReason != "self-captured-secondary" {
		t.Fatalf("decision = %#v, want self captured secondary marked stale instead of LocalHomeOwned", decision)
	}
}

func TestOwnershipResolverConfirmedSelfCapturedSecondaryDeliversToRemoteOwner(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	action := resolverSucceededAction(t, "aws-provider", "eni-a", "aws-router-a", "10.88.60.12/32", actionAssignSecondaryIP, now.Add(-time.Second))
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName:      "cloudedge",
		SelfNode:      "aws-router-a",
		Spec:          spec,
		Status:        map[string]any{"discoverySelfCapturedAddresses": []string{"10.88.60.12/32"}},
		ActionJournal: []routerstate.ActionExecutionRecord{action},
		BGPHomeOwnerNodes: map[string]string{
			"10.88.60.12/32": "azure-router",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.12/32")
	if decision.Class != ownershipClassConfirmedCapture || decision.HomeOwnerNode != "azure-router" {
		t.Fatalf("decision = %#v, want confirmed self captured secondary tied to remote home owner", decision)
	}
	if decision.AdvertiseOwnerNode != "" {
		t.Fatalf("decision = %#v, confirmed capture must not advertise as owner", decision)
	}
	status := ownershipResolverStatus(decisions)
	verdicts := status["ownershipResolverFIBVerdicts"].([]map[string]any)
	verdict := map[string]any{}
	for _, row := range verdicts {
		if row["address"] == "10.88.60.12/32" {
			verdict = row
			break
		}
	}
	if verdict["action"] != "deliver-remote" || verdict["ownerNode"] != "azure-router" {
		t.Fatalf("fib verdicts = %#v, want confirmed capture delivered to remote home owner", verdicts)
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

func TestOwnershipResolverReportsDuplicateProviderHomeOwnerConflict(t *testing.T) {
	now := time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	awsHome := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "eni-client",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	ociHome := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "subnet-oci",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Events:   []routerstate.EventRecord{awsHome, ociHome},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.ConflictReason != "duplicate-provider-home-owners" {
		t.Fatalf("decision = %#v, want duplicate provider home-owner conflict", decision)
	}
	if len(decision.ConflictOwners) != 2 {
		t.Fatalf("decision = %#v, want both provider owner facts retained", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverPhase"] != "Conflict" || status["ownershipResolverConflictCount"] != 1 {
		t.Fatalf("status = %#v, want conflict phase and count", status)
	}
	conflicts := status["ownershipResolverConflicts"].([]map[string]any)
	owners, ok := conflicts[0]["owners"].([]map[string]any)
	if !ok || len(owners) != 2 {
		t.Fatalf("conflicts = %#v, want both conflicting owners in status", conflicts)
	}
	controlTable := status["ownershipResolverControlPlaneOwnerTable"].([]map[string]any)
	if len(controlTable) != 1 || controlTable[0]["state"] != "Conflict" || controlTable[0]["conflictReason"] != "duplicate-provider-home-owners" {
		t.Fatalf("control-plane owner table = %#v, want duplicate conflict row", controlTable)
	}
	conflictOwners, ok := controlTable[0]["conflictOwners"].([]map[string]any)
	if !ok || len(conflictOwners) != 2 {
		t.Fatalf("control-plane owner table = %#v, want both duplicate owners retained", controlTable)
	}
	verdicts := status["ownershipResolverFIBVerdicts"].([]map[string]any)
	if len(verdicts) != 1 || verdicts[0]["address"] != address || verdicts[0]["action"] != "withhold" {
		t.Fatalf("fib verdicts = %#v, want withhold for duplicate remote owners", verdicts)
	}
}

func TestOwnershipResolverCoalescesRedundantProviderHomeOwnerObservers(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	for i := range spec.Members {
		if spec.Members[i].NodeRef == "oci-router" {
			spec.Members[i].Placement = api.MobilityMemberPlacement{Group: "oci-edge", Priority: 10}
		}
	}
	spec.Members = append(spec.Members, api.MobilityPoolMember{
		NodeRef: "oci-router-b",
		Site:    "oci",
		Role:    "cloud",
		Capture: api.MobilityMemberCapture{
			Type:         "provider-secondary-ip",
			ProviderRef:  "oci-provider",
			ProviderMode: "vnic-secondary-ip",
			NICRef:       "oci-vnic-b",
		},
		Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
		Placement: api.MobilityMemberPlacement{Group: "oci-edge", Priority: 20},
	})
	address := "10.88.60.13/32"
	ociA := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.13",
		NICRef:       "oci-client-nic",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	ociB := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router-b", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.13",
		NICRef:       "oci-client-nic",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-2*time.Second), time.Hour)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Events:   []routerstate.EventRecord{ociB, ociA},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.ConflictReason != "" || len(decision.ConflictOwners) != 0 {
		t.Fatalf("decision = %#v, want redundant same-endpoint observers coalesced", decision)
	}
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "oci-router" {
		t.Fatalf("decision = %#v, want priority OCI endpoint observer selected", decision)
	}
	status := ownershipResolverStatus(decisions)
	if status["ownershipResolverPhase"] != "Resolved" || status["ownershipResolverConflictCount"] != 0 {
		t.Fatalf("status = %#v, want no duplicate conflict for same provider endpoint", status)
	}

	activeDecisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "oci-router",
		Spec:     spec,
		Events:   []routerstate.EventRecord{ociB, ociA},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership active: %v", err)
	}
	activeDecision := ownershipDecisionByAddress(t, activeDecisions, address)
	if activeDecision.AdvertiseOwnerNode != "oci-router" || activeDecision.AdvertiseReason != "provider-home-owner" {
		t.Fatalf("active decision = %#v, want exactly the priority winner to advertise", activeDecision)
	}
	standbyDecisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "oci-router-b",
		Spec:     spec,
		Events:   []routerstate.EventRecord{ociB, ociA},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership standby: %v", err)
	}
	standbyDecision := ownershipDecisionByAddress(t, standbyDecisions, address)
	if standbyDecision.AdvertiseOwnerNode != "" || standbyDecision.Class != ownershipClassRemoteHomeOwned {
		t.Fatalf("standby decision = %#v, want non-winner observer not to advertise", standbyDecision)
	}
}

func TestBGPLocalOwnedAddressesSelectsSingleProviderDiscoveryAdvertiser(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	for i := range spec.Members {
		if spec.Members[i].NodeRef == "oci-router" {
			spec.Members[i].Placement = api.MobilityMemberPlacement{Group: "oci-edge", Priority: 10}
		}
	}
	spec.Members = append(spec.Members, api.MobilityPoolMember{
		NodeRef: "oci-router-b",
		Site:    "oci",
		Role:    "cloud",
		Capture: api.MobilityMemberCapture{
			Type:         "provider-secondary-ip",
			ProviderRef:  "oci-provider",
			ProviderMode: "vnic-secondary-ip",
			NICRef:       "oci-vnic-b",
		},
		Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
		Placement: api.MobilityMemberPlacement{Group: "oci-edge", Priority: 20},
	})
	address := "10.88.60.13/32"
	ociA := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.13",
		NICRef:       "oci-client-nic",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Minute), time.Hour)
	ociB := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router-b", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.13",
		NICRef:       "oci-client-nic",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	prefix := netip.MustParsePrefix("10.88.60.0/24")

	activeOwned := bgpLocalOwnedAddressesFromConfigAndEvents("cloudedge", "oci-router", spec, []routerstate.EventRecord{ociB, ociA}, nil, false, nil, false, prefix, now)
	if !bgpOwnedAddressSet(activeOwned)[address] {
		t.Fatalf("active owned = %#v, want priority winner to advertise %s", activeOwned, address)
	}
	standbyOwned := bgpLocalOwnedAddressesFromConfigAndEvents("cloudedge", "oci-router-b", spec, []routerstate.EventRecord{ociB, ociA}, nil, false, nil, false, prefix, now)
	if bgpOwnedAddressSet(standbyOwned)[address] {
		t.Fatalf("standby owned = %#v, want non-winner observer not to advertise %s", standbyOwned, address)
	}
}

func bgpOwnedAddressSet(rows []bgpOwnedAddress) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.Address] = true
	}
	return out
}

func TestOwnershipResolverIgnoresExpiredDuplicateProviderHomeOwner(t *testing.T) {
	now := time.Date(2026, 6, 10, 16, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	awsHome := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "eni-client",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Minute), time.Hour)
	ociHome := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "subnet-oci",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-2*time.Minute), time.Minute)
	ociExpired := providerDiscoveryExpiredEvent("cloudedge", "cloudedge", "oci-router", address, ociHome, now.Add(-30*time.Second), time.Minute)
	decisions, err := resolveAddressOwnership(ownershipResolverInput{
		PoolName: "cloudedge",
		SelfNode: "aws-router-b",
		Spec:     spec,
		Events:   []routerstate.EventRecord{awsHome, ociHome, ociExpired},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.ConflictReason != "" {
		t.Fatalf("decision = %#v, want expired duplicate owner ignored", decision)
	}
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "aws-router-a" {
		t.Fatalf("decision = %#v, want fresh AWS owner selected", decision)
	}
	status := ownershipResolverStatus(decisions)
	controlTable := status["ownershipResolverControlPlaneOwnerTable"].([]map[string]any)
	if len(controlTable) != 1 || controlTable[0]["state"] != "OK" || controlTable[0]["ownerNode"] != "aws-router-a" {
		t.Fatalf("control-plane owner table = %#v, want expired duplicate cleaned up", controlTable)
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
			"discoveryOwnedAddresses":  []string{"10.88.60.11/32"},
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
		decision.LocalNodeRef,
		decision.LocalProviderRef,
		decision.LocalSubnetRef,
		decision.LocalNICRef,
		decision.LocalResourceRef,
		decision.LocalResourceType,
		decision.LocalSource,
		decision.LocalSourceType,
		decision.CaptureHolderNode,
		decision.CaptureProviderRef,
		decision.CaptureTargetRef,
		decision.CaptureStrategy,
		decision.CaptureState,
		decision.AdvertiseOwnerNode,
		decision.AdvertiseReason,
		decision.SuppressionReason,
		decision.ConflictReason,
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
