// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type fakeInventoryRunner struct {
	calls  int
	last   providerinventory.ObservePrivateIPsRequest
	result providerinventory.ObservePrivateIPsResult
	err    error
}

func boolPtr(value bool) *bool {
	return &value
}

func (f *fakeInventoryRunner) run(_ context.Context, _ api.PluginSpec, req providerinventory.ObservePrivateIPsRequest) (providerinventory.ObservePrivateIPsResult, providerinventory.RunOutcome, error) {
	f.calls++
	f.last = req
	return f.result, providerinventory.RunOutcome{}, f.err
}

func TestDiscoveryControllerEmitsObservedEventsForActiveCloudMember(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self: &providerinventory.PrivateIPSelf{
				NICRef:            "plugin-router-nic",
				SubnetRef:         "plugin-subnet",
				PrivateIPs:        []string{"10.88.60.21"},
				ForwardingEnabled: boolPtr(false),
			},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.99", NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.61.10", NICRef: "client-nic-2", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.12", NICRef: "client-nic-3", Tags: map[string]string{"cloudedge-mobility": "false"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if runner.last.Spec.Provider != "azure" || runner.last.Spec.ProviderRef != "azure-provider" || !strings.Contains(runner.last.Spec.SelfNICRef, "router-nic-a") {
		t.Fatalf("request spec = %#v", runner.last.Spec)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one accepted discovered IP", events)
	}
	ev := events[0]
	if ev.Type != ObservedEventType || ev.SourceNode != "azure-router-a" || ev.Subject != "10.88.60.11/32" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.Payload["source"] != providerDiscoverySource || ev.Payload["provider"] != "azure" || ev.Payload["nicRef"] != "client-nic" {
		t.Fatalf("event payload = %#v", ev.Payload)
	}
	if !ev.ExpiresAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("expiresAt = %s, want leaseTTL", ev.ExpiresAt)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Observed" || fmt.Sprint(status["discoveryObserved"]) != "1" || fmt.Sprint(status["discoveryExcluded"]) != "3" {
		t.Fatalf("status = %#v", status)
	}
	if status["discoverySelfNICRef"] != "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a" || status["discoverySelfSubnetRef"] != "plugin-subnet" {
		t.Fatalf("self status = %#v", status)
	}
	if status["discoverySelfForwardingEnabled"] != false {
		t.Fatalf("forwarding status = %#v, want false", status)
	}
}

func TestDiscoveryControllerUsesPluginResolvedSelfNICWhenCaptureNICIsImplicit(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].Capture.NICRef = ""
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "resolved-router-nic", SubnetRef: "subnet-a", PrivateIPs: []string{"10.88.60.21"}},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.21", NICRef: "resolved-router-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.last.Spec.SelfNICRef != "" {
		t.Fatalf("request selfNicRef = %q, want empty for plugin self resolution", runner.last.Spec.SelfNICRef)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.11/32" {
		t.Fatalf("events = %#v, want only client IP", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoverySelfNICRef"] != "resolved-router-nic" || status["discoverySelfSubnetRef"] != "subnet-a" {
		t.Fatalf("status = %#v", status)
	}
}

func TestDiscoveryControllerExcludesPluginSelfPrivateIPs(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].Capture.NICRef = ""
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "resolved-router-nic", SubnetRef: "subnet-a", PrivateIPs: []string{"10.88.60.10", "10.88.60.12/32"}},
			IPs: []providerinventory.PrivateIPRecord{
				// Missing NICRef reproduces the provider-inventory shape that used
				// to turn a trap secondary on the router NIC into an ownership fact.
				{Address: "10.88.60.10", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.12", NICRef: "different-router-nic-ref", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.11/32" {
		t.Fatalf("events = %#v, want only client IP", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedSelfIP"]) != "2" {
		t.Fatalf("status = %#v, want two self-private-IP exclusions", status)
	}
}

func TestDiscoveryControllerDoesNotStealStaticOwnedAddress(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.10", NICRef: "client-looking-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.11/32" {
		t.Fatalf("events = %#v, want static-owned address excluded and client IP accepted", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedStatic"]) != "1" {
		t.Fatalf("status = %#v, want one static-owned exclusion", status)
	}
}

func TestDiscoveryControllerDoesNotUseLeaseTableForRemoteExclusion(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "subnet-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want lease table ignored in BGP clean mode", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedRemote"]) != "0" {
		t.Fatalf("status = %#v, want no lease-driven remote exclusion", status)
	}
}

func TestDiscoveryControllerAllowsSameSiteLeaseHandoverDiscovery(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.IPOwnershipPolicy.PreferNodes = []string{"azure-router-b", "azure-router-a"}
	spec.Members[1].Placement.Priority = 20
	spec.Members[2].Placement.Priority = 10
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-b-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-b", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].SourceNode != "azure-router-b" || events[0].Subject != "10.88.60.11/32" {
		t.Fatalf("events = %#v, want same-site handover discovery accepted", events)
	}
}

func TestDiscoveryControllerExcludesCurrentTrapActionTargets(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	rawPlans, err := json.Marshal([]dynamicconfig.ActionPlan{{
		Name:   "trap-remote",
		Action: "assign-secondary-ip",
		Target: map[string]string{"address": "10.88.60.12/32", "nicRef": "router-nic"},
	}})
	if err != nil {
		t.Fatalf("marshal action plans: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          DynamicSource("cloudedge", "azure-router-a"),
		Generation:      1,
		ObservedAt:      now.Add(-time.Second),
		ExpiresAt:       now.Add(time.Hour),
		ActionPlansJSON: string(rawPlans),
		Status:          "active",
	}); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "unknown-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.11/32" {
		t.Fatalf("events = %#v, want current trap action target excluded", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedTrap"]) != "1" {
		t.Fatalf("status = %#v, want one trap action exclusion", status)
	}
}

func TestDiscoveryControllerScopeExcludesProviderPrimaryAddresses(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].OwnershipDiscovery.Scope.IncludePrimary = boolPtr(false)
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.7", NICRef: "client-nic", SubnetRef: "subnet-a", Primary: true, Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.13", NICRef: "client-nic", SubnetRef: "subnet-a", Primary: false, Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.13/32" {
		t.Fatalf("events = %#v, want only secondary mobility address", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryObserved"]) != "1" || fmt.Sprint(status["discoveryExcludedPrimary"]) != "1" || fmt.Sprint(status["discoveryExcluded"]) != "1" {
		t.Fatalf("status = %#v", status)
	}
}

func TestDiscoveryControllerDefaultScopeAllowsProviderPrimaryAddresses(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.7", NICRef: "client-nic", SubnetRef: "subnet-a", Primary: true, Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.7/32" {
		t.Fatalf("events = %#v, want default primary address accepted", events)
	}
}

func TestDiscoveryControllerScopeIncludeExcludeAddresses(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].OwnershipDiscovery.Scope.IncludeAddresses = []string{"10.88.60.10/31"}
	spec.Members[1].OwnershipDiscovery.Scope.ExcludeAddresses = []string{"10.88.60.10"}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.9", NICRef: "client-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.10", NICRef: "client-b", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "client-c", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "10.88.60.11/32" {
		t.Fatalf("events = %#v, want only address allowed by include and not excluded", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedScope"]) != "2" {
		t.Fatalf("status = %#v, want two scope exclusions", status)
	}
}

func TestDiscoveryControllerResolvesSelfNICForStandbyPlacementMember(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "standby-router-nic", SubnetRef: "subnet-b", PrivateIPs: []string{"10.88.60.22"}},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "standby-client-nic", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-b", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want standby to scan for self NIC", runner.calls)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want no standby ownership observations", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Standby" || !strings.Contains(status["discoveryReason"].(string), "active node") {
		t.Fatalf("status = %#v", status)
	}
	if status["discoverySelfNICRef"] != "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b" || status["discoverySelfSubnetRef"] != "subnet-b" {
		t.Fatalf("self status = %#v, want standby self NIC resolved", status)
	}
}

func TestDiscoveryControllerExpiresPreviousProviderDiscoveryWhenStandby(t *testing.T) {
	now := time.Date(2026, 6, 3, 13, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-b", "10.88.60.13/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.13",
		NICRef:    "standby-router-nic",
		SubnetRef: "subnet-b",
	}, now.Add(-time.Minute), 2*time.Minute))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "standby-router-nic", SubnetRef: "subnet-b", PrivateIPs: []string{"10.88.60.13"}},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.13", NICRef: "standby-router-nic", SubnetRef: "subnet-b", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-b", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if countEvents(events, ExpiredEventType, "azure-router-b", "10.88.60.13/32") != 1 {
		t.Fatalf("events = %#v, want standby to expire stale provider-discovery ownership", events)
	}
	bgp := &fakeBGPPaths{}
	mobilityB := Controller{Router: routerWithBGPRouter(discoveryRouter("azure-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobilityB.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility B Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "azure-router-b"), "10.88.60.13/32"); ok {
		t.Fatalf("standby B still advertised stale provider-discovery .13 after expiry")
	}
}

func TestDiscoveryControllerExpiredStandbyOwnershipAllowsActiveRestoreTrap(t *testing.T) {
	now := time.Date(2026, 6, 3, 13, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-b", "10.88.60.13/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.13",
		NICRef:    "standby-router-nic",
		SubnetRef: "subnet-b",
	}, now.Add(-time.Minute), 2*time.Minute))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "standby-router-nic", SubnetRef: "subnet-b", PrivateIPs: []string{"10.88.60.13"}},
		},
	}}
	discoveryB := DiscoveryController{Router: discoveryRouter("azure-router-b", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := discoveryB.Reconcile(context.Background()); err != nil {
		t.Fatalf("discovery B Reconcile: %v", err)
	}
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.6"},
		"10.88.60.13/32": {"10.99.0.4"},
	})
	seedSucceededBGPCaptureAction(t, store, "azure-provider", "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b", "azure-router-b", "10.88.60.13/32", "assign-secondary-ip", 1, now.Add(-30*time.Second))
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfNICRef":     "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
		"discoverySelfSubnetRef":  "subnet-a",
		"discoverySelfPrivateIPs": []string{"10.88.60.11"},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	mobilityA := Controller{Router: routerWithBGPRouter(discoveryRouter("azure-router-a", spec)), Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobilityA.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility A Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router-a")).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.13/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want restored active to recapture .13 after standby ownership expiry", plans)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want allowReassignment for restore recapture from standby", assign.Parameters)
	}
}

func TestDiscoveryControllerStandbySelfNICEnablesLivenessSeizeActions(t *testing.T) {
	now := time.Date(2026, 6, 3, 11, 56, 21, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].Capture.NICRef = ""
	spec.Members[2].Capture.NICRef = ""
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "standby-router-nic", SubnetRef: "subnet-b", PrivateIPs: []string{"10.88.60.22"}},
		},
	}}
	router := discoveryRouter("azure-router-b", spec)
	discovery := DiscoveryController{Router: router, Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("discovery Reconcile: %v", err)
	}
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.2"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	})
	mobility := Controller{Router: routerWithBGPRouter(router), Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobility.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router-b")).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want standby seize assign after self NIC discovery", plans)
	}
	if assign.Target["nicRef"] != "standby-router-nic" || assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign = %#v, want discovered standby NIC and allowReassignment", assign)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["generatedActions"] == 0 || status["plannerPhase"] == "Degraded" {
		t.Fatalf("mobility status = %#v, want generated actions after self NIC discovery", status)
	}
}

func TestDiscoveryControllerObservedEventFeedsBGPAdvertisement(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.11", NICRef: "client-nic", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	router := discoveryRouter("azure-router-a", spec)
	discovery := DiscoveryController{Router: router, Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("discovery Reconcile: %v", err)
	}
	bgp := &fakeBGPPaths{}
	mobility := Controller{Router: routerWithBGPRouter(router), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobility.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility Reconcile: %v", err)
	}
	if len(bgp.upserts) != 1 || bgp.upserts[0].Prefix != "10.88.60.11/32" || bgp.upserts[0].Source != DynamicSource("cloudedge", "azure-router-a") {
		t.Fatalf("bgp upserts = %#v, want discovered local /32 advertisement", bgp.upserts)
	}
}

func TestDiscoveryControllerHonorsScanInterval(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status:   providerinventory.ObservePrivateIPsResultStatus{Status: providerinventory.ResultSucceeded},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(30 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want second scan suppressed by interval", runner.calls)
	}
}

func discoveryPoolSpec() api.MobilityPoolSpec {
	spec := centralizedOwnershipPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
		Mode:         "provider-private-ip",
		PluginRef:    "azure-inventory",
		ScanInterval: "1m",
		LeaseTTL:     "2m",
		Selector:     api.MobilityOwnershipDiscoverySelector{Tags: map[string]string{"cloudedge-mobility": "true"}},
	}
	spec.Members[2].OwnershipDiscovery = spec.Members[1].OwnershipDiscovery
	return spec
}

func discoveryRouter(nodeName string, spec api.MobilityPoolSpec) *api.Router {
	router := planningRouterForNode(nodeName, spec)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
		Metadata: api.ObjectMeta{Name: "azure-inventory"},
		Spec: api.PluginSpec{
			Executable:   "/usr/local/libexec/routerd/plugins/azure-inventory",
			Capabilities: []string{providerinventory.CapabilityObserveProviderPrivateIPs},
			Context: api.PluginContextSpec{Resources: []api.PluginContextResourceRef{{
				APIVersion: api.HybridAPIVersion,
				Kind:       "CloudProviderProfile",
				Name:       "azure-provider",
			}}},
		},
	})
	return router
}
