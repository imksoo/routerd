// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	provideraction "github.com/imksoo/routerd/pkg/provideraction"
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
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 1 || got[0] != "10.88.60.11/32" {
		t.Fatalf("owned address status = %#v, want fresh observed owner", status)
	}
}

func TestDiscoveryControllerOnPremL2DHCPLeaseEventFeedsBGPAdvertisement(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := api.MobilityPoolSpec{
		Prefix:         "192.168.123.0/24",
		GroupRef:       "cloudedge",
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
		Members: []api.MobilityPoolMember{
			{
				NodeRef: "pve-rt01",
				Site:    "pve01",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "eth1",
					ActiveWhen: api.CaptureActiveWhen{Type: "single-router"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode: "onprem-l2",
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: OnPremSourceDHCPv4Lease, Interface: "eth1", LeaseTTL: "2m"},
						{Type: OnPremSourceARPObserver, Interface: "eth1"},
						{Type: OnPremSourceOnDemandARP, Interface: "eth1", ProbeTimeout: "500ms"},
						{Type: OnPremSourcePVESVNet, Network: "svnet1", Bridge: "vmbr123"},
					},
				},
			},
			{NodeRef: "k8s-rt01", Site: "core", Role: "cloud"},
		},
	}
	router := staticRouter("pve-rt01", spec)
	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	event := daemonapi.DaemonEvent{
		Type:     "routerd.dhcp.lease.add",
		Severity: daemonapi.SeverityInfo,
		Time:     now,
		Attributes: map[string]string{
			"ip":        "192.168.123.201",
			"mac":       "02:00:c0:a8:7b:c9",
			"interface": "eth1",
		},
	}
	if err := discovery.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("Discovery HandleEvent: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != ObservedEventType || events[0].Subject != "192.168.123.201/32" {
		t.Fatalf("events = %#v, want one observed ownership fact", events)
	}
	if events[0].Payload["source"] != onPremDiscoverySource || events[0].Payload["sourceType"] != OnPremSourceDHCPv4Lease {
		t.Fatalf("payload = %#v, want onprem dhcpv4 source", events[0].Payload)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Mobility Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "pve-rt01"), "192.168.123.201/32"); !ok {
		t.Fatalf("paths = %#v, want DHCP observed owner advertised", bgp.paths)
	}
}

func TestDiscoveryControllerOnPremL2StatusObservedClientsFeedBGPAdvertisement(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := api.MobilityPoolSpec{
		Prefix:         "192.168.123.0/24",
		GroupRef:       "cloudedge",
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
		Members: []api.MobilityPoolMember{
			{
				NodeRef: "pve-rt08",
				Site:    "pve08",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "svnet1",
					ActiveWhen: api.CaptureActiveWhen{Type: "single-router"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:     "onprem-l2",
					LeaseTTL: "2m",
					Scope: api.MobilityOwnershipDiscoveryScope{
						IncludeAddresses: []string{"192.168.123.132/32"},
						ExcludeAddresses: []string{"192.168.123.1/32"},
					},
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: OnPremSourceOnDemandARP, Interface: "svnet1", LeaseTTL: "2m"},
					},
				},
			},
			{NodeRef: "k8s-rt02", Site: "core", Role: "cloud"},
		},
	}
	router := staticRouter("pve-rt08", spec)
	clients := []onPremObservedClientStatus{{
		IP:         "192.168.123.132",
		MAC:        "bc:24:11:c9:33:c2",
		SourceType: OnPremSourceOnDemandARP,
		SeenAt:     now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}}
	encoded, err := json.Marshal(clients)
	if err != nil {
		t.Fatalf("marshal clients: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"interface":       "svnet1",
		"ifname":          "eth1",
		"sourceType":      OnPremSourceOnDemandARP,
		"observedClients": string(encoded),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != ObservedEventType || events[0].Subject != "192.168.123.132/32" {
		t.Fatalf("events = %#v, want one status-backed ownership fact", events)
	}
	if events[0].Payload["source"] != onPremDiscoverySource || events[0].Payload["sourceType"] != OnPremSourceOnDemandARP {
		t.Fatalf("payload = %#v, want on-demand onprem source", events[0].Payload)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryObserved"]) != "1" {
		t.Fatalf("status = %#v, want discoveryObserved=1", status)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Mobility Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "pve-rt08"), "192.168.123.132/32"); !ok {
		t.Fatalf("paths = %#v, want status-observed owner advertised", bgp.paths)
	}
}

func TestDiscoveryControllerOnPremL2StatusObservedClientsBySourceSurvivesEmptyOnDemandStatus(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 35, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := api.MobilityPoolSpec{
		Prefix:         "192.168.123.0/24",
		GroupRef:       "cloudedge",
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
		Members: []api.MobilityPoolMember{
			{
				NodeRef: "pve-rt06",
				Site:    "pve06",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "svnet1",
					ActiveWhen: api.CaptureActiveWhen{Type: "single-router"},
				},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:     "onprem-l2",
					LeaseTTL: "2m",
					Scope: api.MobilityOwnershipDiscoveryScope{
						IncludeAddresses: []string{"192.168.123.132/32"},
						ExcludeAddresses: []string{"192.168.123.1/32"},
					},
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: OnPremSourceARPObserver, Interface: "svnet1", LeaseTTL: "2m"},
						{Type: OnPremSourceOnDemandARP, Interface: "svnet1", LeaseTTL: "2m"},
					},
				},
			},
			{NodeRef: "k8s-rt02", Site: "core", Role: "cloud"},
		},
	}
	router := staticRouter("pve-rt06", spec)
	arpClients := []onPremObservedClientStatus{{
		IP:         "192.168.123.132",
		MAC:        "bc:24:11:c9:33:c2",
		SourceType: OnPremSourceARPObserver,
		SeenAt:     now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}}
	encodedARP, err := json.Marshal(arpClients)
	if err != nil {
		t.Fatalf("marshal arp clients: %v", err)
	}
	encodedEmpty, err := json.Marshal([]onPremObservedClientStatus{})
	if err != nil {
		t.Fatalf("marshal empty clients: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"interface":       "svnet1",
		"ifname":          "eth1",
		"sourceType":      OnPremSourceOnDemandARP,
		"observedClients": string(encodedEmpty),
		"observedClientsBySource": map[string]any{
			OnPremSourceARPObserver: map[string]any{
				"interface":       "svnet1",
				"ifname":          "eth1",
				"sourceType":      OnPremSourceARPObserver,
				"observedClients": string(encodedARP),
			},
			OnPremSourceOnDemandARP: map[string]any{
				"interface":       "svnet1",
				"ifname":          "eth1",
				"sourceType":      OnPremSourceOnDemandARP,
				"observedClients": string(encodedEmpty),
			},
		},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "192.168.123.132/32" {
		t.Fatalf("events = %#v, want source-specific ARP observer ownership fact", events)
	}
	if events[0].Payload["sourceType"] != OnPremSourceARPObserver {
		t.Fatalf("payload = %#v, want arp-observer source", events[0].Payload)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Mobility Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "pve-rt06"), "192.168.123.132/32"); !ok {
		t.Fatalf("paths = %#v, want source-specific ARP observer owner advertised", bgp.paths)
	}
}

func TestDiscoveryControllerProfileSpecMatchesInlineRequest(t *testing.T) {
	now := time.Date(2026, 6, 4, 11, 0, 0, 0, time.UTC)
	inlineSpec := discoveryPoolSpec()
	profileSpec := profileDiscoveryPoolSpecForNode("azure-router-a")
	inlineReq := reconcileDiscoveryRequest(t, "azure-router-a", inlineSpec, now)
	profileReq := reconcileDiscoveryRequest(t, "azure-router-a", profileSpec, now)
	if got, want := canonicalJSON(t, profileReq.Spec), canonicalJSON(t, inlineReq.Spec); got != want {
		t.Fatalf("profile discovery request differs from inline\nprofile=%s\ninline=%s", got, want)
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

func TestDiscoveryControllerScopesProviderInventoryToSelfNICAndSubnet(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 50, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self: &providerinventory.PrivateIPSelf{
				NICRef:     "plugin-router-nic",
				SubnetRef:  "subnet-a",
				PrivateIPs: []string{"10.88.60.12", "10.88.60.13", "10.88.60.4"},
			},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.4", NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a", SubnetRef: "subnet-a", Primary: true, Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.13", NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "client-nic-a", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.12", NICRef: "foreign-nic", SubnetRef: "foreign-subnet", ProviderRef: "aws-provider", Tags: map[string]string{"cloudedge-mobility": "true"}},
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
		t.Fatalf("events = %#v, want only local subnet client ownership", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if got := statusStringSlice(status["discoverySelfPrivateIPs"]); len(got) != 1 || got[0] != "10.88.60.4/32" {
		t.Fatalf("discoverySelfPrivateIPs = %#v, want only self NIC primary", status["discoverySelfPrivateIPs"])
	}
	if got, ok := statusBool(status["discoverySelfPrimaryObserved"]); !ok || !got {
		t.Fatalf("discoverySelfPrimaryObserved = %#v, want true", status["discoverySelfPrimaryObserved"])
	}
	if got := statusStringSlice(status["discoverySelfCapturedAddresses"]); len(got) != 1 || got[0] != "10.88.60.13/32" {
		t.Fatalf("discoverySelfCapturedAddresses = %#v, want self NIC secondary split out", status["discoverySelfCapturedAddresses"])
	}
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 1 || got[0] != "10.88.60.11/32" {
		t.Fatalf("discoveryOwnedAddresses = %#v, want only provider-local client", status["discoveryOwnedAddresses"])
	}
	if got := statusStringSlice(status["discoveryLocalInventoryIPs"]); !stringSliceContains(got, "10.88.60.11/32") || stringSliceContains(got, "10.88.60.4/32") || stringSliceContains(got, "10.88.60.12/32") || stringSliceContains(got, "10.88.60.13/32") {
		t.Fatalf("discoveryLocalInventoryIPs = %#v, want only non-router local subnet inventory", got)
	}
}

func TestScopedDiscoverySelfInventoryPreservesRouteTableCapturedAddresses(t *testing.T) {
	prefix := netip.MustParsePrefix("10.88.60.0/24")
	self := discoverySelfInventory{
		NICRef:            "eni-router",
		SubnetRef:         "subnet-a",
		PrivateIPs:        []string{"10.88.60.4"},
		CapturedAddresses: []string{"10.88.60.12/32"},
	}
	local := []providerinventory.PrivateIPRecord{{
		Address:   "10.88.60.4",
		NICRef:    "eni-router",
		SubnetRef: "subnet-a",
		Primary:   true,
	}}
	got := scopedDiscoverySelfInventory(self, local, prefix)
	if len(got.CapturedAddresses) != 1 || got.CapturedAddresses[0] != "10.88.60.12/32" {
		t.Fatalf("capturedAddresses = %#v, want route-table capture preserved", got.CapturedAddresses)
	}
}

func TestDiscoveryControllerExcludesSelfResourceSecondaryFromOwnership(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 15, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self: &providerinventory.PrivateIPSelf{
				NICRef:       "eni-a",
				SubnetRef:    "subnet-a",
				ResourceRef:  "i-router-a",
				ResourceType: "router-nic",
				PrivateIPs:   []string{"10.88.60.4"},
			},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.4", NICRef: "eni-a", SubnetRef: "subnet-a", ResourceRef: "i-router-a", ResourceType: "router-nic", Primary: true, Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.12", NICRef: "eni-a", SubnetRef: "subnet-a", ResourceRef: "i-router-a", ResourceType: "instance-nic", Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "eni-client", SubnetRef: "subnet-a", ResourceRef: "i-client", ResourceType: "instance-nic", Tags: map[string]string{"cloudedge-mobility": "true"}},
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
	if len(events) != 1 || events[0].Subject != "10.88.60.11/32" || events[0].Payload["resourceRef"] != "i-client" {
		t.Fatalf("events = %#v, want only non-self client ownership with resourceRef", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 1 || got[0] != "10.88.60.11/32" {
		t.Fatalf("discoveryOwnedAddresses = %#v, want only client", got)
	}
	if got := statusStringSlice(status["discoverySelfCapturedAddresses"]); len(got) != 1 || got[0] != "10.88.60.12/32" {
		t.Fatalf("discoverySelfCapturedAddresses = %#v, want self resource secondary split out", got)
	}
	if got, ok := statusBool(status["discoverySelfPrimaryObserved"]); !ok || !got {
		t.Fatalf("discoverySelfPrimaryObserved = %#v, want true", status["discoverySelfPrimaryObserved"])
	}
	if got := statusStringSlice(status["discoveryLocalInventoryIPs"]); stringSliceContains(got, "10.88.60.12/32") {
		t.Fatalf("discoveryLocalInventoryIPs = %#v, want self resource secondary excluded", got)
	}
	if status["discoverySelfResourceRef"] != "i-router-a" || status["discoverySelfResourceType"] != "router-nic" {
		t.Fatalf("status = %#v, want self resource identity", status)
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

func TestDiscoveryControllerExcludesRemoteRouterNICFromOwnership(t *testing.T) {
	now := time.Date(2026, 6, 10, 17, 45, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self: &providerinventory.PrivateIPSelf{
				NICRef:      "eni-router-a",
				SubnetRef:   "subnet-a",
				PrivateIPs:  []string{"10.88.60.4"},
				ResourceRef: "i-router-a",
			},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.4", NICRef: "eni-router-a", SubnetRef: "subnet-a", ResourceRef: "i-router-a", ResourceType: "router-nic", Primary: true, Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.5", NICRef: "eni-router-b", SubnetRef: "subnet-a", ResourceRef: "i-router-b", ResourceType: "router-nic", Primary: true, Tags: map[string]string{"cloudedge-mobility": "true"}},
				{Address: "10.88.60.11", NICRef: "eni-client", SubnetRef: "subnet-a", ResourceRef: "i-client", ResourceType: "instance-nic", Tags: map[string]string{"cloudedge-mobility": "true"}},
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
		t.Fatalf("events = %#v, want only client ownership", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 1 || got[0] != "10.88.60.11/32" {
		t.Fatalf("discoveryOwnedAddresses = %#v, want only client", got)
	}
	if got := statusStringSlice(status["discoveryLocalInventoryIPs"]); stringSliceContains(got, "10.88.60.5/32") {
		t.Fatalf("discoveryLocalInventoryIPs = %#v, want remote router primary excluded", got)
	}
	if fmt.Sprint(status["discoveryExcludedRouterNIC"]) != "2" {
		t.Fatalf("discoveryExcludedRouterNIC = %#v, want 2", status["discoveryExcludedRouterNIC"])
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

func TestDiscoveryControllerDoesNotExcludeRemoteProviderTrapActionTargets(t *testing.T) {
	now := time.Date(2026, 6, 9, 17, 45, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	seedSucceededBGPCaptureAction(t, store, "oci-provider", "oci-vnic", "oci-router", "10.88.60.12/32", "assign-secondary-ip", 1, now.Add(-time.Second))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
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
	if len(events) != 1 || events[0].Subject != "10.88.60.12/32" {
		t.Fatalf("events = %#v, want remote provider trap not to hide local home inventory", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedTrap"]) != "0" {
		t.Fatalf("status = %#v, want no trap action exclusion for remote holder/provider", status)
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
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 0 {
		t.Fatalf("status = %#v, want standby to publish empty owned-address backing", status)
	}
	if got := statusStringSlice(status["discoveryLocalInventoryIPs"]); len(got) != 0 {
		t.Fatalf("status = %#v, want standby to clear local inventory backing", status)
	}
	if status["discoverySelfNICRef"] != "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b" || status["discoverySelfSubnetRef"] != "subnet-b" {
		t.Fatalf("self status = %#v, want standby self NIC resolved", status)
	}
}

func TestDiscoveryControllerProfileOnlyActivePeerRunsProviderDiscovery(t *testing.T) {
	now := time.Date(2026, 6, 6, 15, 40, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Values = map[string]string{
		"aws.region":    "ap-northeast-1",
		"aws.subnetRef": "subnet-a",
	}
	spec.Profiles = api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
		"aws-self": {
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				Interface:    "ens5",
				ProviderRef:  "aws-provider",
				ProviderMode: "eni-secondary-ip",
				TargetFrom:   map[string]string{"region": "aws.region"},
			},
			OwnershipDiscovery: api.MobilityOwnershipDiscovery{
				Mode:          "provider-private-ip",
				SubnetRefFrom: "aws.subnetRef",
				ScanInterval:  "60s",
				LeaseTTL:      "10m",
				Scope:         api.MobilityOwnershipDiscoveryScope{},
			},
		},
	}}
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		{
			NodeRef:    "aws-router-b",
			Site:       "aws",
			Role:       "cloud",
			ProfileRef: "aws-self",
			Placement:  api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20},
		},
		{
			NodeRef:     "aws-router-a",
			Site:        "aws",
			Role:        "cloud",
			Placement:   api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
			Maintenance: api.MobilityMemberMaintenance{Drain: true},
		},
	}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "eni-b", SubnetRef: "subnet-a", PrivateIPs: []string{"10.88.60.22"}},
		},
	}}
	router := discoveryRouter("aws-router-b", spec)
	for i := range router.Spec.Resources {
		if router.Spec.Resources[i].APIVersion != api.HybridAPIVersion || router.Spec.Resources[i].Kind != "CloudProviderProfile" {
			continue
		}
		router.Spec.Resources[i].Metadata.Name = "aws-provider"
		profile := router.Spec.Resources[i].Spec.(api.CloudProviderProfileSpec)
		profile.Provider = "aws"
		router.Spec.Resources[i].Spec = profile
	}
	controller := DiscoveryController{Router: router, Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want profile-only active peer to run discovery", runner.calls)
	}
	if runner.last.Spec.ProviderRef != "aws-provider" || runner.last.Spec.SelfNICRef != "" || runner.last.Spec.SubnetRef != "subnet-a" {
		t.Fatalf("request spec = %#v", runner.last.Spec)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoverySelfNICRef"] != "eni-b" || status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v, want discovered self NIC on active profile-only peer", status)
	}
}

func TestDiscoveryControllerLivenessSeizedStandbyAdvertisesOwnedAddress(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "standby-router-nic", SubnetRef: "subnet-b", PrivateIPs: []string{"10.88.60.22"}},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "standby-client-nic", SubnetRef: "subnet-b", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	saveBGPStatus(t, store, map[string][]string{}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	})
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "azure-router-b", spec, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	}, now)
	router := routerWithBGPRouter(discoveryRouter("azure-router-b", spec))
	discovery := DiscoveryController{Router: router, Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("discovery Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 || events[0].SourceNode != "azure-router-b" || events[0].Subject != "10.88.60.12/32" {
		t.Fatalf("events = %#v, want seized standby provider-discovery owner event", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Observed" || fmt.Sprint(status["discoveryObserved"]) != "1" {
		t.Fatalf("status = %#v, want seized standby discovery observed", status)
	}
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 1 || got[0] != "10.88.60.12/32" {
		t.Fatalf("status = %#v, want seized standby owned address", status)
	}

	bgp := &fakeBGPPaths{}
	mobility := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobility.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility Reconcile: %v", err)
	}
	path := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "azure-router-b"), "10.88.60.12/32")
	if path.Attrs.LocalPref != bgpMobilityLocalPref(1) {
		t.Fatalf("path attrs = %#v, want seized standby to advertise as active owner", path.Attrs)
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
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.6"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, []map[string]any{
		bgpOwnerPrefix("10.88.60.13/32", "10.99.0.4", "azure-router-b"),
	}, nil)
	seedSucceededBGPCaptureAction(t, store, "azure-provider", "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b", "azure-router-b", "10.88.60.13/32", "assign-secondary-ip", 1, now.Add(-30*time.Second))
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfNICRef":     "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
		"discoverySelfSubnetRef":  "subnet-a",
		"discoverySelfPrivateIPs": []string{"10.88.60.11"},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	mobilityA := Controller{Router: routerWithBGPRouter(discoveryRouter("azure-router-a", spec)), Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now.Add(3 * time.Minute) }}
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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "azure-router-b", spec, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	}, now.Add(time.Second))
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
	ownerUpserts := nonLivenessUpserts(bgp.upserts)
	if len(ownerUpserts) != 1 || ownerUpserts[0].Prefix != "10.88.60.11/32" || ownerUpserts[0].Source != DynamicSource("cloudedge", "azure-router-a") {
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

func TestDiscoveryControllerProviderCaptureEventBypassesScanInterval(t *testing.T) {
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
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "provider-action-execution", Kind: "provider-action-execution"}, provideraction.ProviderCaptureChangedEvent, daemonapi.SeverityInfo)
	if err := controller.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if runner.calls != 2 {
		t.Fatalf("runner calls = %d, want provider capture event to force second scan", runner.calls)
	}
}

func TestDiscoveryControllerLivenessChangeBypassesScanInterval(t *testing.T) {
	now := time.Date(2026, 6, 3, 16, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "standby-router-nic", SubnetRef: "subnet-b", PrivateIPs: []string{"10.88.60.22"}},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "standby-client-nic", SubnetRef: "subnet-b", Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	saveBGPStatus(t, store, map[string][]string{}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-a"): "10.99.0.2/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	})
	router := routerWithBGPRouter(discoveryRouter("azure-router-b", spec))
	controller := DiscoveryController{Router: router, Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want initial scan", runner.calls)
	}
	saveBGPStatus(t, store, map[string][]string{}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	})
	controller.Now = func() time.Time { return now.Add(10 * time.Second) }
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "mobility-bgp", Kind: "BGPRouter"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	if err := controller.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if runner.calls != 2 {
		t.Fatalf("runner calls = %d, want BGP liveness loss to bypass scan interval", runner.calls)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPlacementSeize"] != false || status["discoveryPlacementSeizeHoldDown"] != true || status["discoveryPhase"] != "Standby" {
		t.Fatalf("status = %#v, want hold-down standby after active marker loss", status)
	}
	controller.Now = func() time.Time { return now.Add(10*time.Second + bgpSeizeLivenessMissingHold + time.Second) }
	if err := controller.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent after hold-down: %v", err)
	}
	if runner.calls != 3 {
		t.Fatalf("runner calls = %d, want hold-down expiry to bypass scan interval", runner.calls)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPlacementSeize"] != true || status["discoveryPlacementSeizeHoldDown"] != false || status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v, want seized discovery after hold-down", status)
	}
}

func TestDiscoveryControllerRescansWhenForwardingStatusMissing(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryLastScanAt": now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a", ForwardingEnabled: boolPtr(false)},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want scan despite recent legacy status without forwarding observation", runner.calls)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoverySelfForwardingObserved"] != true || status["discoverySelfForwardingEnabled"] != false {
		t.Fatalf("status = %#v, want forwarding observation populated", status)
	}
}

func TestDiscoveryControllerRescansWhenImplicitSelfNICMissing(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].Capture.NICRef = ""
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryLastScanAt":             now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"discoverySelfForwardingObserved": true,
		"discoverySelfForwardingEnabled":  false,
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a", ForwardingEnabled: boolPtr(false)},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want scan despite recent status without self NIC", runner.calls)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoverySelfNICRef"] != "router-nic" {
		t.Fatalf("status = %#v, want self NIC populated", status)
	}
}

func TestDiscoveryControllerDoesNotExpireProviderDiscoveryOnTransientActiveMiss(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-a", "10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.12",
		NICRef:    "client-nic",
		SubnetRef: "subnet-a",
		Tags:      map[string]string{"cloudedge-mobility": "true"},
	}, now.Add(-90*time.Second), 2*time.Minute))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a", ForwardingEnabled: boolPtr(true)},
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
	if countEvents(events, ExpiredEventType, "azure-router-a", "10.88.60.12/32") != 0 {
		t.Fatalf("events = %#v, want no immediate active expire for transient missing scan", events)
	}
}

func TestDiscoveryControllerExpiresProviderDiscoveryAfterMissingInventoryHold(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-a", "10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.12",
		NICRef:    "client-nic",
		SubnetRef: "subnet-a",
		Tags:      map[string]string{"cloudedge-mobility": "true"},
	}, now.Add(-3*time.Minute), 10*time.Minute))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a", ForwardingEnabled: boolPtr(true)},
		},
	}}
	router := discoveryRouter("azure-router-a", spec)
	discovery := DiscoveryController{Router: router, Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("discovery Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if countEvents(events, ExpiredEventType, "azure-router-a", "10.88.60.12/32") != 1 {
		t.Fatalf("events = %#v, want missing inventory after hold to expire provider discovery", events)
	}

	bgp := &fakeBGPPaths{}
	mobility := Controller{Router: routerWithBGPRouter(router), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobility.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "azure-router-a"), "10.88.60.12/32"); ok {
		t.Fatalf("paths = %#v, expired missing-inventory claim must not keep advertising", bgp.paths)
	}
}

func TestDiscoveryProviderDiscoveredAddressesHonorsLatestExpiredEvent(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	prefix := netip.MustParsePrefix("10.88.60.0/24")
	observed := providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-a", "10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:     "10.88.60.12",
		NICRef:      "client-nic",
		ProviderRef: "azure-provider",
		SubnetRef:   "subnet-a",
	}, now.Add(-2*time.Minute), 10*time.Minute)
	expired := providerDiscoveryExpiredEvent("cloudedge", "cloudedge", "azure-router-a", "10.88.60.12/32", observed, now.Add(-time.Minute), 10*time.Minute)
	recordEvent(t, store, observed)
	recordEvent(t, store, expired)

	controller := DiscoveryController{Store: store, Now: func() time.Time { return now }}
	addresses := controller.providerDiscoveredAddresses("cloudedge", "cloudedge", prefix, now)
	if addresses["10.88.60.12/32"] {
		t.Fatalf("providerDiscoveredAddresses = %#v, want latest Expired event to remove address", addresses)
	}
}

func TestDiscoveryControllerExpiresProviderDiscoveryAddressExcludedBySelector(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-a", "10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:   "10.88.60.12",
		NICRef:    "client-nic",
		SubnetRef: "subnet-a",
		Tags:      map[string]string{"cloudedge-mobility": "true"},
	}, now.Add(-3*time.Minute), 5*time.Minute))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a", ForwardingEnabled: boolPtr(true)},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "client-nic", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "false"}},
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
	if countEvents(events, ExpiredEventType, "azure-router-a", "10.88.60.12/32") != 1 {
		t.Fatalf("events = %#v, want visible selector-excluded address expired", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryExcludedSelector"]) != "1" {
		t.Fatalf("status = %#v, want selector exclusion counted", status)
	}
}

func TestDiscoveryControllerExpiresProviderDiscoveryAddressScopedOutByProvider(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "azure-router-a", "10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{
		Address:     "10.88.60.12",
		NICRef:      "stale-foreign-nic",
		ProviderRef: "azure-provider",
		SubnetRef:   "subnet-a",
		Tags:        map[string]string{"cloudedge-mobility": "true"},
	}, now.Add(-time.Minute), 5*time.Minute))
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic", SubnetRef: "subnet-a", ForwardingEnabled: boolPtr(true)},
			IPs: []providerinventory.PrivateIPRecord{
				{Address: "10.88.60.12", NICRef: "foreign-nic", ProviderRef: "aws-provider", SubnetRef: "subnet-a", Tags: map[string]string{"cloudedge-mobility": "true"}},
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
	if countEvents(events, ExpiredEventType, "azure-router-a", "10.88.60.12/32") != 1 {
		t.Fatalf("events = %#v, want provider-scoped-out stale address expired", events)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if got := statusStringSlice(status["discoveryOwnedAddresses"]); len(got) != 0 {
		t.Fatalf("discoveryOwnedAddresses = %#v, want provider-scoped-out address excluded", got)
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

func profileDiscoveryPoolSpecForNode(selfNode string) api.MobilityPoolSpec {
	spec := discoveryPoolSpec()
	spec.Values = map[string]string{
		"azure.nic": map[string]string{
			"azure-router-a": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
			"azure-router-b": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b",
		}[selfNode],
		"azure.ipConfigName": map[string]string{
			"azure-router-a": "capture-a",
			"azure-router-b": "capture-b",
		}[selfNode],
		"azure.region": "japaneast",
	}
	spec.Profiles = api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
		"azure-edge": {
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       spec.Values["azure.nic"],
				TargetFrom:   map[string]string{"ipConfigName": "azure.ipConfigName", "region": "azure.region"},
			},
			OwnershipDiscovery: api.MobilityOwnershipDiscovery{
				Mode:         "provider-private-ip",
				PluginRef:    "azure-inventory",
				ScanInterval: "1m",
				LeaseTTL:     "2m",
				Selector:     api.MobilityOwnershipDiscoverySelector{Tags: map[string]string{"cloudedge-mobility": "true"}},
			},
		},
	}}
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		{NodeRef: "azure-router-a", Site: "azure", Role: "cloud", ProfileRef: "azure-edge", Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10}},
		{NodeRef: "azure-router-b", Site: "azure", Role: "cloud", Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20}},
	}
	return spec
}

func reconcileDiscoveryRequest(t *testing.T, selfNode string, spec api.MobilityPoolSpec, now time.Time) providerinventory.ObservePrivateIPsRequest {
	t.Helper()
	store := testStore(t, now)
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "plugin-router-nic", SubnetRef: "plugin-subnet"},
		},
	}}
	controller := DiscoveryController{Router: discoveryRouter(selfNode, spec), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile(%s): %v", selfNode, err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	return runner.last
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
