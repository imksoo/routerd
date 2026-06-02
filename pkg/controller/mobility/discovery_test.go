// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type fakeInventoryRunner struct {
	calls  int
	last   providerinventory.ObservePrivateIPsRequest
	result providerinventory.ObservePrivateIPsResult
	err    error
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
}

func TestDiscoveryControllerSkipsStandbyPlacementMember(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status:   providerinventory.ObservePrivateIPsResultStatus{Status: providerinventory.ResultSucceeded},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-b", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want standby to skip scan", runner.calls)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Standby" || !strings.Contains(status["discoveryReason"].(string), "active node") {
		t.Fatalf("status = %#v", status)
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
	lease, found, err := store.GetAddressLease("cloudedge", "10.88.60.11/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if !found || lease.OwnerNode != "azure-router-a" || lease.Status != routerstate.AddressLeaseStatusActive {
		t.Fatalf("lease = %+v found=%t", lease, found)
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
