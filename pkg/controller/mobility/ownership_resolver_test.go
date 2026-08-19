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

// ownershipFactsFixture is test-only shorthand for the same typed resolver
// input used by reconciliation. It intentionally has no status-shaped input:
// provider discovery reaches the resolver through runtime facts.
type ownershipFactsFixtureInput struct {
	SelfPrivateIPs      []string
	SelfCapturedIPs     []string
	DiscoveryLastScanAt time.Time
	SelfSubnetRef       string
}

func ownershipFactsFixture(spec testMobilityPoolSpec, input ownershipFactsFixtureInput) OwnershipFacts {
	prefix, err := netip.ParsePrefix(spec.Prefix)
	if err != nil {
		panic(err)
	}
	poolPrefix := prefix.Masked()
	facts := OwnershipFacts{
		SelfPrivateIPs:      map[string]bool{},
		SelfCapturedIPs:     map[string]bool{},
		SelfSubnetRef:       input.SelfSubnetRef,
		DiscoveryLastScanAt: input.DiscoveryLastScanAt,
	}
	for _, raw := range input.SelfPrivateIPs {
		if address, ok := normalizeLeaseAddress(raw, poolPrefix); ok {
			facts.SelfPrivateIPs[address] = true
		}
	}
	for _, raw := range input.SelfCapturedIPs {
		if address, ok := normalizeLeaseAddress(raw, poolPrefix); ok {
			facts.SelfCapturedIPs[address] = true
		}
	}
	facts.SelfInventoryKnown = input.SelfPrivateIPs != nil || input.SelfCapturedIPs != nil
	return facts
}

func ownershipPoolFixture(poolName, selfNode string, spec testMobilityPoolSpec) NormalizedMobilityPool {
	poolSpec, _ := localizeMobilityPoolSpecForNode(spec, selfNode)
	prefix, err := netip.ParsePrefix(poolSpec.Prefix)
	if err != nil {
		panic(err)
	}
	members := plannerMembers(spec.Members)
	self, ok := lookupMemberByNodeRef(members, selfNode)
	if !ok {
		panic("ownership test self is not a pool member: " + selfNode)
	}
	return NormalizedMobilityPool{
		Name:     poolName,
		Spec:     poolSpec,
		Prefix:   prefix.Masked(),
		SelfNode: self.NodeRef,
		Self:     self,
		Members:  members,
	}
}

func providerRuntimeAddressEvent(t *testing.T, nodeRef, address, provider, providerRef string, record providerinventory.PrivateIPRecord, observedAt time.Time, ttl time.Duration) routerstate.EventRecord {
	t.Helper()
	return providerDiscoveryRuntimeEventForTest(t, nodeRef, providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest(address, provider, providerRef, record, observedAt, ttl),
		},
	}, observedAt, ttl)
}

func ownershipFactsWithProviderRuntime(facts OwnershipFacts, pool NormalizedMobilityPool, events []routerstate.EventRecord, now time.Time) OwnershipFacts {
	runtime := providerDiscoveryRuntimeRecords(pool, events, now)
	facts.providerRuntime = append(make([]providerDiscoveryRuntimeRecord, 0, len(runtime)), runtime...)
	return facts
}

func ownershipFactsWithSelfProviderAddresses(facts OwnershipFacts, pool NormalizedMobilityPool, addresses []string, now time.Time) OwnershipFacts {
	addressFacts := make([]providerDiscoveryAddressFact, 0, len(addresses))
	for _, address := range addresses {
		addressFacts = append(addressFacts, providerDiscoveryAddressFact{
			Address:     address,
			ProviderRef: pool.Self.Capture.ProviderRef,
			ObservedAt:  now,
			ExpiresAt:   now.Add(DefaultLeaseTTL),
		})
	}
	return ownershipFactsWithSelfProviderAddressFacts(facts, pool, addressFacts, now)
}

func ownershipFactsWithSelfProviderAddressFacts(facts OwnershipFacts, pool NormalizedMobilityPool, addressFacts []providerDiscoveryAddressFact, now time.Time) OwnershipFacts {
	facts.providerRuntime = append(facts.providerRuntime, providerDiscoveryRuntimeRecord{
		NodeRef:     pool.SelfNode,
		ObservedAt:  now,
		RuntimeFact: providerDiscoveryRuntimeFact{Addresses: append([]providerDiscoveryAddressFact(nil), addressFacts...)},
	})
	return facts
}

type ownershipSnapshotOption func(*PoolRuntimeSnapshot)

func ownershipResolverSnapshot(pool NormalizedMobilityPool, now time.Time, facts OwnershipFacts, options ...ownershipSnapshotOption) PoolRuntimeSnapshot {
	snapshot := PoolRuntimeSnapshot{
		Pool:      pool,
		Ownership: facts,
		Now:       now,
	}
	for _, option := range options {
		option(&snapshot)
	}
	return snapshot
}

func withOwnershipEvents(events []routerstate.EventRecord) ownershipSnapshotOption {
	return func(snapshot *PoolRuntimeSnapshot) { snapshot.Events = events }
}

func withOwnershipBGP(bgp BGPSnapshot) ownershipSnapshotOption {
	return func(snapshot *PoolRuntimeSnapshot) { snapshot.BGP = bgp }
}

func withOwnershipProviderHistory(history ProviderActionHistory) ownershipSnapshotOption {
	return func(snapshot *PoolRuntimeSnapshot) { snapshot.Provider.ActionHistory = history }
}

func TestOwnershipResolverScenario391BaselineSameSubnetHome(t *testing.T) {
	now := time.Date(2026, 6, 9, 22, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	facts := ownershipFactsWithSelfProviderAddresses(ownershipFactsFixture(spec, ownershipFactsFixtureInput{
		SelfPrivateIPs: []string{"10.88.60.4"},
	}), pool, []string{"10.88.60.11/32"}, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassLocalHomeOwned || decision.AdvertiseOwnerNode != "aws-router-a" || decision.AdvertiseReason != "provider-home-owner" {
		t.Fatalf("decision = %#v, want local home direct advertisement", decision)
	}
}

func TestOwnershipResolverOnlyUsesRuntimeAddressFacts(t *testing.T) {
	now := time.Date(2026, 6, 24, 16, 45, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	facts := ownershipFactsWithSelfProviderAddresses(ownershipFactsFixture(spec, ownershipFactsFixtureInput{
		SelfPrivateIPs: []string{"10.88.60.4"},
	}), pool, []string{"10.88.60.11/32"}, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	home := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if home.Class != ownershipClassLocalHomeOwned {
		t.Fatalf("home = %#v, want runtime provider fact to remain local home", home)
	}
	for _, decision := range decisions {
		if decision.Address == "10.88.60.16/32" {
			t.Fatalf("address absent from runtime fact leaked into ownership decisions: %#v", decision)
		}
	}
}

func TestOwnershipResolverSkipsUnresolvedReturnRoutePeer(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 58, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-a", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{SelfPrivateIPs: []string{"10.88.60.4"}}),
		withOwnershipBGP(BGPSnapshot{ReturnRoutes: map[string]bool{"10.88.60.5/32": true}}),
	))
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
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-b", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfCapturedIPs:     []string{"10.88.60.11"},
			DiscoveryLastScanAt: now,
		}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{"10.88.60.11/32": "aws-router-a"}}),
	))
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
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-b", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfPrivateIPs:      []string{"10.88.60.6/32"},
			SelfCapturedIPs:     []string{},
			DiscoveryLastScanAt: now,
		}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{address: "aws-router-a"}}),
	))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class != ownershipClassRemoteHomeOwned || decision.CaptureState != captureStateNone || decision.CaptureHolderNode != "" {
		t.Fatalf("decision = %#v, want remote BGP owner without stale capture after provider inventory disproves self capture", decision)
	}
	if stale := ownershipResolverStatusRowsWithStateForTest(t, decisions, "Stale"); len(stale) != 0 {
		t.Fatalf("stale rows = %#v, want no stale capture for disproved standby capture", stale)
	}
}

func TestOwnershipResolverKeepsObservedSelfCapture(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 40, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", address, "assign-secondary-ip", now.Add(-time.Minute))
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-b", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfPrivateIPs:      []string{"10.88.60.6/32"},
			SelfCapturedIPs:     []string{address},
			DiscoveryLastScanAt: now,
		}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{address: "aws-router-a"}}),
	))
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
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-b", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{address: "aws-router-a"}}),
	))
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

func TestOwnershipResolverDoesNotConfirmCaptureWithStaleProviderObservation(t *testing.T) {
	now := time.Date(2026, 6, 26, 2, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	action := resolverSucceededAction(t, "aws-provider", "eni-b", "aws-router-b", address, "assign-secondary-ip", now.Add(time.Second))
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-b", spec),
		now.Add(2*time.Second),
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfCapturedIPs:     []string{address},
			DiscoveryLastScanAt: now,
		}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{address: "aws-router-a"}}),
	))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class == ownershipClassConfirmedCapture || decision.CaptureState == captureStateConfirmed {
		t.Fatalf("decision = %#v, stale provider observation must not confirm capture", decision)
	}
	if decision.CaptureState != captureStateStale {
		t.Fatalf("decision = %#v, want stale diagnostic capture state", decision)
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
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-a", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfPrivateIPs:      []string{"10.88.60.4/32"},
			SelfCapturedIPs:     []string{},
			DiscoveryLastScanAt: now,
		}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
	))
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
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-b", spec),
		now,
		OwnershipFacts{},
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision([]dynamicconfig.ActionPlan{plan}, nil, "")),
	))
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
	expired := providerDiscoveryRuntimeEventForTest(t, "oci-router", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "oci", "oci-provider", providerinventory.PrivateIPRecord{NICRef: "oci-client", SubnetRef: "oci-subnet"}, now.Add(-10*time.Minute), time.Minute),
		},
	}, now, time.Hour)
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	events := []routerstate.EventRecord{expired}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{}), pool, events, now)
	facts = ownershipFactsWithSelfProviderAddresses(facts, pool, []string{"10.88.60.11/32"}, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts, withOwnershipEvents(events)))
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
	homeEvent := providerRuntimeAddressEvent(t, "aws-router-a", "10.88.60.11/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.11",
		NICRef:    "eni-client",
		SubnetRef: "subnet-a",
	}, now.Add(-time.Second), time.Hour)
	action := resolverSucceededAction(t, "oci-provider", "oci-vnic", "oci-router", "10.88.60.11/32", "assign-secondary-ip", now.Add(-time.Second))
	pool := ownershipPoolFixture("cloudedge", "oci-router", spec)
	events := []routerstate.EventRecord{homeEvent}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{
		SelfPrivateIPs: []string{"10.88.60.11"},
	}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		pool,
		now,
		facts,
		withOwnershipEvents(events),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
	))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassStaleCapture || decision.HomeOwnerNode != "aws-router-a" || decision.SuppressionReason != "fresh-home-owner" {
		t.Fatalf("decision = %#v, want remote AWS home to mark OCI capture stale", decision)
	}
}

func TestOwnershipResolverReportsProviderRuntimeConflict(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	homeEvent := providerRuntimeAddressEvent(t, "oci-router", "10.88.60.11/32", "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	events := []routerstate.EventRecord{homeEvent}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{}), pool, events, now)
	facts = ownershipFactsWithSelfProviderAddressFacts(facts, pool, []providerDiscoveryAddressFact{
		providerDiscoveryAddressFactForTest("10.88.60.11/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
			Address:      "10.88.60.11",
			NICRef:       "eni-client",
			SubnetRef:    "subnet-a",
			ResourceRef:  "i-aws-client",
			ResourceType: "instance-nic",
		}, now, time.Hour),
	}, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		pool,
		now,
		facts,
		withOwnershipEvents(events),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{"10.88.60.11/32": "oci-router"}}),
	))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "oci-router" {
		t.Fatalf("decision = %#v, want remote home owner preserved", decision)
	}
	if decision.ConflictReason != "duplicate-provider-home-owners" {
		t.Fatalf("decision = %#v, want provider runtime conflict", decision)
	}
	if decision.LocalProviderRef != "aws-provider" || decision.LocalNICRef != "eni-client" || decision.LocalResourceRef != "i-aws-client" {
		t.Fatalf("decision = %#v, want local provider fact refs recorded", decision)
	}
	controlTable := ownershipResolverControlPlaneOwnerTable(decisions)
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
		controlRow["localEvidenceNICRef"] != "eni-client" ||
		controlRow["localEvidenceResourceRef"] != "i-aws-client" ||
		controlRow["conflictReason"] != "duplicate-provider-home-owners" {
		t.Fatalf("control-plane owner table row = %#v, want centralized conflict evidence", controlRow)
	}
}

func TestOwnershipResolverStatusDistinguishesStaleConflictAndUnknown(t *testing.T) {
	decisions := []ownershipDecision{
		{
			Address:            "10.88.60.11/32",
			Class:              ownershipClassStaleCapture,
			Source:             "provider-action",
			CaptureDisposition: dynamicconfig.CaptureHold,
			CaptureReason:      "capture release is fenced while placement is unsettled",
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
	controlTable := ownershipResolverControlPlaneOwnerTable(decisions)
	controlRows := map[string]map[string]any{}
	for _, row := range controlTable {
		controlRows[row["address"].(string)] = row
	}
	if controlRows["10.88.60.11/32"]["state"] != "Stale" ||
		controlRows["10.88.60.11/32"]["captureHolderNode"] != "aws-router-a" ||
		controlRows["10.88.60.11/32"]["captureDisposition"] != string(dynamicconfig.CaptureHold) ||
		controlRows["10.88.60.11/32"]["captureReason"] != "capture release is fenced while placement is unsettled" ||
		controlRows["10.88.60.12/32"]["state"] != "Unknown" ||
		controlRows["10.88.60.13/32"]["state"] != "Conflict" {
		t.Fatalf("control-plane owner table = %#v, want stale/unknown/conflict rows preserved", controlTable)
	}
	if stale := ownershipResolverStatusRowsWithStateForTest(t, decisions, "Stale"); len(stale) != 1 || stale[0]["suppressionReason"] != "capture-not-desired" {
		t.Fatalf("stale rows = %#v, want one stale capture row", stale)
	}
	if unknown := ownershipResolverStatusRowsWithStateForTest(t, decisions, "Unknown"); len(unknown) != 1 || unknown[0]["source"] != "bgp-rib" {
		t.Fatalf("unknown rows = %#v, want one unknown BGP row", unknown)
	}
}

func TestOwnershipResolverReportsRemoteHomeLocalOwnershipEventConflict(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 25, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	cloudHome := providerRuntimeAddressEvent(t, "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
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
	pool := ownershipPoolFixture("cloudedge", "onprem-router", spec)
	events := []routerstate.EventRecord{cloudHome, onPremObserved}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts, withOwnershipEvents(events)))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "aws-router-a" || decision.LocalNodeRef != "onprem-router" {
		t.Fatalf("decision = %#v, want remote cloud owner with local onprem observation recorded", decision)
	}
	if decision.ConflictReason != "remote-home-owner-overlaps-local-ownership-event" || decision.LocalSource != onPremDiscoverySource {
		t.Fatalf("decision = %#v, want onprem ownership event conflict", decision)
	}
	row := ownershipResolverStatusTableForTest(t, decisions)[0]
	if row["state"] != "Conflict" || row["ownerNode"] != "aws-router-a" || row["localEvidenceNode"] != "onprem-router" || row["localEvidenceSource"] != onPremDiscoverySource {
		t.Fatalf("control-plane owner table row = %#v, want cloud/onprem conflict", row)
	}
}

func TestOwnershipResolverTreatsOnPremObservedEventAsLocalOwner(t *testing.T) {
	now := time.Date(2026, 6, 18, 6, 40, 0, 0, time.UTC)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "svnet1",
	}, Members: []api.ResolvedMobilityPoolMember{
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
	observed := onPremDiscoveryObservedEvent("svnet1", "svnet1", "pve-rt08", "192.168.123.129/32", onPremObservation{
		Address:    "192.168.123.129",
		MAC:        "bc:24:11:82:0d:3f",
		Interface:  "svnet1",
		Network:    "svnet1",
		SourceType: OnPremSourcePVESVNet,
	}, now.Add(-5*time.Second), time.Hour)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("svnet1", "pve-rt08", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{}),
		withOwnershipEvents([]routerstate.EventRecord{observed}),
	))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one observed client", decisions)
	}
	decision := ownershipDecisionByAddress(t, decisions, "192.168.123.129/32")
	if decision.Class != ownershipClassLocalHomeOwned || decision.AdvertiseOwnerNode != "pve-rt08" || decision.AdvertiseReason != "ownership-event" {
		t.Fatalf("decision = %#v, want observed onprem client advertised as local owner", decision)
	}
	if table := ownershipResolverStatusTableForTest(t, decisions); len(table) != 1 {
		t.Fatalf("control-plane owner table = %#v, want one resolver address", table)
	}
}

func TestOwnershipResolverDoesNotClassifyCapturedSecondaryAsRouterSelf(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 55, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-a", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfPrivateIPs:  []string{"10.88.60.4/32"},
			SelfCapturedIPs: []string{"10.88.60.12/32"},
		}),
	))
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
	selfEvent := providerRuntimeAddressEvent(t, "aws-router-a", "10.88.60.12/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.12",
		NICRef:       "eni-a",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-a",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	events := []routerstate.EventRecord{selfEvent}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{
		SelfPrivateIPs:  []string{"10.88.60.4/32"},
		SelfCapturedIPs: []string{"10.88.60.12/32"},
	}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		pool,
		now,
		facts,
		withOwnershipEvents(events),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision([]dynamicconfig.ActionPlan{plan}, nil, "")),
	))
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
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		ownershipPoolFixture("cloudedge", "aws-router-a", spec),
		now,
		ownershipFactsFixture(spec, ownershipFactsFixtureInput{
			SelfCapturedIPs:     []string{"10.88.60.12/32"},
			DiscoveryLastScanAt: now,
		}),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{"10.88.60.12/32": "azure-router"}}),
	))
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
}

func TestProviderInventoryHomeOwnerFactsExcludeRouterNICPrimary(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	event := providerRuntimeAddressEvent(t, "aws-router-b", "10.88.60.5/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.5",
		NICRef:       "eni-b",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-b",
		ResourceType: "router-nic",
		Primary:      true,
	}, now.Add(-time.Second), time.Hour)
	pool := ownershipPoolFixture("cloudedge", "aws-router-b", spec)
	facts := providerInventoryHomeOwnerFactSets(pool, netip.MustParsePrefix(spec.Prefix), providerDiscoveryRuntimeRecords(pool, []routerstate.EventRecord{event}, now), now)
	if _, ok := facts["10.88.60.5/32"]; ok {
		t.Fatalf("facts = %#v, want router/member primary excluded from home-owner facts", facts)
	}
}

func TestOwnershipResolverReportsDuplicateProviderHomeOwnerConflict(t *testing.T) {
	now := time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	awsHome := providerRuntimeAddressEvent(t, "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "eni-client",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	ociHome := providerRuntimeAddressEvent(t, "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "subnet-oci",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now, time.Hour)
	pool := ownershipPoolFixture("cloudedge", "aws-router-b", spec)
	events := []routerstate.EventRecord{awsHome, ociHome}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts, withOwnershipEvents(events)))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.ConflictReason != "duplicate-provider-home-owners" {
		t.Fatalf("decision = %#v, want duplicate provider home-owner conflict", decision)
	}
	if decision.ConflictWinnerNode != "aws-router-a" || decision.ConflictResolution != "loser-withhold-local-capture" {
		t.Fatalf("decision = %#v, want deterministic conflict winner and loser withhold", decision)
	}
	controlTable := ownershipResolverControlPlaneOwnerTable(decisions)
	if len(controlTable) != 1 ||
		controlTable[0]["state"] != "Conflict" ||
		controlTable[0]["conflictReason"] != "duplicate-provider-home-owners" ||
		controlTable[0]["conflictWinnerNode"] != "aws-router-a" ||
		controlTable[0]["conflictResolution"] != "loser-withhold-local-capture" {
		t.Fatalf("control-plane owner table = %#v, want duplicate conflict row", controlTable)
	}
}

func TestOwnershipResolverUsesBGPOwnerForDuplicateProviderConflictWinner(t *testing.T) {
	now := time.Date(2026, 6, 10, 16, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	awsHome := providerRuntimeAddressEvent(t, "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "eni-client",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	ociHome := providerRuntimeAddressEvent(t, "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "subnet-oci",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-2*time.Second), time.Hour)
	action := resolverSucceededAction(t, "oci-provider", "oci-vnic", "oci-router", address, "assign-secondary-ip", now.Add(-time.Second))
	pool := ownershipPoolFixture("cloudedge", "oci-router", spec)
	events := []routerstate.EventRecord{awsHome, ociHome}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{
		SelfCapturedIPs:     []string{address},
		DiscoveryLastScanAt: now,
	}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(
		pool,
		now,
		facts,
		withOwnershipEvents(events),
		withOwnershipProviderHistory(newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{action}, "")),
		withOwnershipBGP(BGPSnapshot{HomeOwnerNodes: map[string]string{address: "aws-router-a"}}),
	))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.ConflictWinnerNode != "aws-router-a" || decision.HomeOwnerNode != "aws-router-a" {
		t.Fatalf("decision = %#v, want BGP owner selected as conflict winner", decision)
	}
	if decision.Class != ownershipClassStaleCapture ||
		decision.SuppressionReason != "provider-split-brain-loser" ||
		decision.ConflictResolution != "loser-release-local-capture" {
		t.Fatalf("decision = %#v, want losing local capture marked for release", decision)
	}
	conflicts := ownershipResolverStatusRowsWithStateForTest(t, decisions, "Conflict")
	if len(conflicts) != 1 || conflicts[0]["conflictWinnerNode"] != "aws-router-a" || conflicts[0]["conflictResolution"] != "loser-release-local-capture" {
		t.Fatalf("control-plane conflicts = %#v, want winner and release resolution", conflicts)
	}
}

func TestProviderInventoryConflictWinnerComparisonHandlesEqualFacts(t *testing.T) {
	fact := providerInventoryOwnerFact{
		Address:     "10.88.60.11/32",
		NodeRef:     "aws-router-a",
		Provider:    "aws",
		ProviderRef: "aws-provider",
		ResourceRef: "i-aws-client",
		NICRef:      "eni-client",
		SubnetRef:   "subnet-aws",
	}
	if providerInventoryConflictWinnerLess(fact, fact) {
		t.Fatalf("equal provider inventory facts must not sort before themselves")
	}
}

func TestOwnershipResolverCoalescesDuplicateProviderObserversForSameOwner(t *testing.T) {
	now := time.Date(2026, 6, 24, 17, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.12/32"
	azureA := providerRuntimeAddressEvent(t, "azure-router", address, "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.12",
		NICRef:       "azure-client-nic",
		SubnetRef:    "azure-subnet",
		ResourceRef:  "azure-client-vm",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour)
	azureB := providerRuntimeAddressEvent(t, "azure-router-b", address, "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.12",
		NICRef:       "azure-client-nic",
		SubnetRef:    "azure-subnet",
		ResourceRef:  "azure-client-vm",
		ResourceType: "instance-nic",
	}, now.Add(-2*time.Second), time.Hour)
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	events := []routerstate.EventRecord{azureA, azureB}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts, withOwnershipEvents(events)))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	decision := ownershipDecisionByAddress(t, decisions, address)
	if decision.ConflictReason != "" {
		t.Fatalf("decision = %#v, want same provider owner observations coalesced", decision)
	}
	if decision.Class != ownershipClassRemoteHomeOwned || decision.HomeOwnerNode != "azure-router" {
		t.Fatalf("decision = %#v, want latest same-owner observer selected as remote home", decision)
	}
	if len(ownershipResolverStatusRowsWithStateForTest(t, decisions, "Conflict")) != 0 {
		t.Fatalf("decisions = %#v, want no duplicate conflict for same provider owner", decisions)
	}
}

func TestOwnershipResolverIgnoresExpiredDuplicateProviderHomeOwner(t *testing.T) {
	now := time.Date(2026, 6, 10, 16, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	awsHome := providerRuntimeAddressEvent(t, "aws-router-a", address, "aws", "aws-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "eni-client",
		SubnetRef:    "subnet-aws",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Minute), time.Hour)
	ociHome := providerRuntimeAddressEvent(t, "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "subnet-oci",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-2*time.Minute), time.Minute)
	pool := ownershipPoolFixture("cloudedge", "aws-router-b", spec)
	events := []routerstate.EventRecord{awsHome, ociHome}
	facts := ownershipFactsWithProviderRuntime(ownershipFactsFixture(spec, ownershipFactsFixtureInput{}), pool, events, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts, withOwnershipEvents(events)))
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
	controlTable := ownershipResolverControlPlaneOwnerTable(decisions)
	if len(controlTable) != 1 || controlTable[0]["state"] != "OK" || controlTable[0]["ownerNode"] != "aws-router-a" {
		t.Fatalf("control-plane owner table = %#v, want expired duplicate cleaned up", controlTable)
	}
}

func TestOwnershipResolverTypedFactsDoNotLeakEmptyValues(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 15, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	pool := ownershipPoolFixture("cloudedge", "aws-router-a", spec)
	facts := ownershipFactsWithSelfProviderAddresses(ownershipFactsFixture(spec, ownershipFactsFixtureInput{
		SelfPrivateIPs: []string{"10.88.60.4/32"},
	}), pool, []string{"10.88.60.11/32"}, now)
	decisions, err := resolveAddressOwnership(ownershipResolverSnapshot(pool, now, facts))
	if err != nil {
		t.Fatalf("resolveAddressOwnership: %v", err)
	}
	localHome := ownershipDecisionByAddress(t, decisions, "10.88.60.11/32")
	if localHome.Class != ownershipClassLocalHomeOwned {
		t.Fatalf("localHome = %#v, want typed facts to retain provider evidence", localHome)
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
		decision.HomeNICRef,
		decision.LocalNodeRef,
		decision.LocalProviderRef,
		decision.LocalNICRef,
		decision.LocalResourceRef,
		decision.LocalSource,
		decision.CaptureHolderNode,
		decision.CaptureProviderRef,
		decision.CaptureTargetRef,
		decision.CaptureStrategy,
		decision.CaptureState,
		decision.AdvertiseOwnerNode,
		decision.AdvertiseReason,
		decision.SuppressionReason,
		decision.ConflictReason,
		decision.ConflictWinnerNode,
		decision.ConflictResolution,
		decision.Source,
	}
	for _, value := range values {
		if value == "<nil>" {
			return true
		}
	}
	return false
}

func ownershipResolverStatusTableForTest(t *testing.T, decisions []ownershipDecision) []map[string]any {
	t.Helper()
	return ownershipResolverControlPlaneOwnerTable(decisions)
}

func ownershipResolverStatusRowsWithStateForTest(t *testing.T, decisions []ownershipDecision, state string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, row := range ownershipResolverStatusTableForTest(t, decisions) {
		if row["state"] == state {
			out = append(out, row)
		}
	}
	return out
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
