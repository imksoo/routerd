// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
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

func providerDiscoveryAddressFacts(events []routerstate.EventRecord) []providerDiscoveryAddressFact {
	var out []providerDiscoveryAddressFact
	for _, event := range events {
		if event.Payload["source"] != providerDiscoverySource || event.Payload["sourceType"] != providerDiscoveryRuntimeFactType {
			continue
		}
		var runtime providerDiscoveryRuntimeFact
		if json.Unmarshal([]byte(event.Payload["snapshot"]), &runtime) == nil {
			out = append(out, runtime.Addresses...)
		}
	}
	return out
}

func providerDiscoveryRuntimeFactFromEvents(t *testing.T, events []routerstate.EventRecord) providerDiscoveryRuntimeFact {
	t.Helper()
	var (
		fact  providerDiscoveryRuntimeFact
		found bool
	)
	for _, event := range events {
		if event.Payload["source"] != providerDiscoverySource || event.Payload["sourceType"] != providerDiscoveryRuntimeFactType {
			continue
		}
		if found {
			t.Fatalf("events = %#v, want one provider runtime fact", events)
		}
		if err := json.Unmarshal([]byte(event.Payload["snapshot"]), &fact); err != nil {
			t.Fatalf("decode provider runtime fact: %v", err)
		}
		found = true
	}
	if !found {
		t.Fatalf("events = %#v, want provider runtime fact", events)
	}
	return fact
}

func onPremDiscoveryOwnershipEvents(events []routerstate.EventRecord) []routerstate.EventRecord {
	var out []routerstate.EventRecord
	for _, event := range events {
		if event.Payload["source"] == onPremDiscoverySource && event.Payload["sourceType"] != onPremDiscoveryArmedFactType {
			out = append(out, event)
		}
	}
	return out
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
	if len(events) != 1 || events[0].Payload["sourceType"] != providerDiscoveryRuntimeFactType {
		t.Fatalf("events = %#v, want one provider runtime fact", events)
	}
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if runtime.Self.NICRef != "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a" || runtime.Self.SubnetRef != "plugin-subnet" || runtime.Self.ForwardingEnabled == nil || *runtime.Self.ForwardingEnabled {
		t.Fatalf("runtime self fact = %#v", runtime.Self)
	}
	if got := runtime.Addresses; len(got) != 1 || got[0].Address != "10.88.60.11/32" {
		t.Fatalf("runtime owned addresses = %#v, want fresh observed owner", got)
	}
	if got := runtime.Addresses[0]; got.Provider != "azure" || got.NICRef != "client-nic" || !got.ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("runtime address fact = %#v, want provider identity and lease", got)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Observed" || fmt.Sprint(status["discoveryObserved"]) != "1" {
		t.Fatalf("status = %#v", status)
	}
}

func TestDiscoveryControllerReleasesStoppedProviderRecordWhenConfigured(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].OwnershipDiscovery.StoppedInstancePolicy = "release"
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{
		TypeMeta: providerinventory.TypeMeta{APIVersion: providerinventory.ProtocolAPIVersion, Kind: providerinventory.KindObservePrivateIPsResult},
		Status: providerinventory.ObservePrivateIPsResultStatus{
			Status: providerinventory.ResultSucceeded,
			Self:   &providerinventory.PrivateIPSelf{NICRef: "router-nic-a", SubnetRef: "subnet-a"},
			IPs: []providerinventory.PrivateIPRecord{{
				Address:       "10.88.60.11",
				NICRef:        "client-nic",
				SubnetRef:     "subnet-a",
				InstanceState: "stopped",
				Tags:          map[string]string{"cloudedge-mobility": "true"},
			}},
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 0 {
		t.Fatalf("runtime owned addresses = %#v, want stopped record released", got)
	}
}

func TestDiscoveryControllerOnPremL2DHCPLeaseEventFeedsBGPAdvertisement(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
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
		Type:     daemonapi.EventDHCPLeaseAdded,
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
	var ownership routerstate.EventRecord
	for _, candidate := range events {
		if candidate.Payload["source"] == onPremDiscoverySource && candidate.Payload["sourceType"] == OnPremSourceDHCPv4Lease {
			ownership = candidate
			break
		}
	}
	if ownership.Type != ObservedEventType || ownership.Subject != "192.168.123.201/32" {
		t.Fatalf("events = %#v, want one observed ownership fact", events)
	}
	if ownership.Payload["source"] != onPremDiscoverySource || ownership.Payload["sourceType"] != OnPremSourceDHCPv4Lease {
		t.Fatalf("payload = %#v, want onprem dhcpv4 source", ownership.Payload)
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

func TestOnPremObservationFromDaemonEventUsesCanonicalDHCPTopics(t *testing.T) {
	for _, tt := range []struct {
		topic, action string
		ok            bool
	}{
		{daemonapi.EventDHCPLeaseAdded, "observed", true},
		{daemonapi.EventDHCPLeaseRenewed, "observed", true},
		{daemonapi.EventDHCPLeaseRemoved, "expired", true},
		{"routerd.dhcp.lease.add", "", false},
	} {
		observation, ok := onPremObservationFromDaemonEvent(daemonapi.DaemonEvent{Type: tt.topic, Attributes: map[string]string{"ip": "192.0.2.10", "interface": "lan0"}})
		if ok != tt.ok {
			t.Fatalf("topic %q accepted=%v, want %v", tt.topic, ok, tt.ok)
		}
		if ok && (observation.Action != tt.action || observation.SourceType != OnPremSourceDHCPv4Lease) {
			t.Fatalf("topic %q observation = %#v", tt.topic, observation)
		}
	}
}

func TestOnPremDiscoverySelectorMatchesFailClosed(t *testing.T) {
	if onPremDiscoverySelectorMatches("lan0", "") || onPremDiscoverySelectorMatches("lan0", "lan1") {
		t.Fatal("configured interface selector accepted a missing or mismatched observation")
	}
	if !onPremDiscoverySelectorMatches("lan0", "lan0") || !onPremDiscoverySelectorMatches("", "lan1") {
		t.Fatal("selector matching rejected an exact or unscoped observation")
	}
}

func TestDiscoveryControllerPublishesTypedARPObserverIntents(t *testing.T) {
	now := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
		{
			NodeRef: "pve-rt08",
			Site:    "pve08",
			Role:    "onprem",
			Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "capture"},
			OwnershipDiscovery: api.MobilityOwnershipDiscovery{
				Mode: "onprem-l2",
				Sources: []api.MobilityOwnershipDiscoverySource{
					{Type: OnPremSourceDHCPv4Lease, Interface: "capture"},
					{Type: OnPremSourceARPObserver, Interface: "capture"},
					{Type: OnPremSourceOnDemandARP, Interface: "capture", ProbeTimeout: "500ms", ProbeRetries: 2, ScanInterval: "1s", SourceAddressFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/capture-source", Field: "currentAddress"}},
					{Type: OnPremSourcePVESVNet, Network: "svnet1", Bridge: "vmbr123", ScanInterval: "3s"},
				},
			},
		},
		{NodeRef: "aws-rt01", Site: "aws", Role: "cloud"},
	},
	}
	router := staticRouter("pve-rt08", spec)
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "capture"}, Spec: api.InterfaceSpec{IfName: "ens3"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
			{NodeRef: "pve-rt08", Site: "pve", Role: "onprem", MACAddresses: []string{"02:00:00:00:00:AA"}},
			{NodeRef: "aws-rt01", Site: "aws", Role: "cloud", MACAddresses: []string{"02:00:00:00:00:bb", "02:00:00:00:00:cc"}},
		}}},
	)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "capture-source", map[string]any{"currentAddress": "192.168.123.134/24"}); err != nil {
		t.Fatal(err)
	}
	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile: %v", err)
	}
	part := latestPart(t, store, ARPObserverDynamicSource("cloudedge", "pve-rt08"))
	var intents []dynamicconfig.ARPObserverIntent
	if err := json.Unmarshal([]byte(part.ARPObserverIntentsJSON), &intents); err != nil {
		t.Fatalf("decode ARP observer intents: %v", err)
	}
	if len(intents) != 3 {
		t.Fatalf("ARP observer intents = %#v, want three non-DHCP daemon sources", intents)
	}
	byType := map[string]dynamicconfig.ARPObserverIntent{}
	for _, intent := range intents {
		byType[intent.SourceType] = intent
		if intent.IfName != "ens3" {
			t.Fatalf("%s IfName = %q, want ens3", intent.SourceType, intent.IfName)
		}
	}
	onDemand := byType[OnPremSourceOnDemandARP]
	if onDemand.SourceAddress != "192.168.123.134" || !onDemand.OnDemand || onDemand.Observe || onDemand.ProbeTimeout != "500ms" || onDemand.ProbeRetries != 2 || onDemand.ScanInterval != "1s" {
		t.Fatalf("on-demand intent = %#v", onDemand)
	}
	pve := byType[OnPremSourcePVESVNet]
	if !pve.Observe || pve.Network != "svnet1" || pve.Bridge != "vmbr123" || pve.ScanInterval != "3s" {
		t.Fatalf("pve intent = %#v", pve)
	}
	wantMACs := []string{"02:00:00:00:00:aa", "02:00:00:00:00:bb", "02:00:00:00:00:cc"}
	if got := onDemand.IgnoredSenderMACs; !reflect.DeepEqual(got, wantMACs) {
		t.Fatalf("ignored sender MACs = %#v, want %#v", got, wantMACs)
	}

	spec.Members[0].OwnershipDiscovery.Mode = "disabled"
	discovery.Router = staticRouter("pve-rt08", spec)
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile after disabling source: %v", err)
	}
	part = latestPart(t, store, ARPObserverDynamicSource("cloudedge", "pve-rt08"))
	if part.ARPObserverIntentsJSON != "" {
		t.Fatalf("disabled discovery ARP observer intents = %q, want empty withdrawal", part.ARPObserverIntentsJSON)
	}
}

func TestDiscoveryControllerOnPremL2RepeatedSameOwnerObservationIsNotOwnershipChange(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
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
				},
			},
		},
		{NodeRef: "k8s-rt01", Site: "core", Role: "cloud"},
	},
	}
	poolPrefix := netip.MustParsePrefix("192.168.123.0/24")
	observation := onPremObservation{
		Action:     "observed",
		Address:    "192.168.123.201",
		MAC:        "02:00:c0:a8:7b:c9",
		Interface:  "eth1",
		SourceType: OnPremSourceDHCPv4Lease,
		ObservedAt: now,
	}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt01", "192.168.123.201/32", observation, now.Add(-30*time.Second), 2*time.Minute))
	members := plannerMembers(spec.Members)
	self, ok := lookupMemberByNodeRef(members, "pve-rt01")
	if !ok {
		t.Fatal("missing self member")
	}
	controller := DiscoveryController{Store: store, Now: func() time.Time { return now }}
	changed, err := controller.recordOnPremObservation(NormalizedMobilityPool{Name: "cloudedge", Spec: spec.MobilityPoolSpec, Self: self}, poolPrefix, observation, now)
	if err != nil {
		t.Fatalf("recordOnPremObservation: %v", err)
	}
	if changed {
		t.Fatal("same-owner unexpired observation must not be treated as ownership change")
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v, want existing event only", events)
	}
}

func TestDiscoveryControllerOnPremL2RepeatedSameOwnerObservationRefreshesExpiringLease(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 20, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
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
				},
			},
		},
		{NodeRef: "k8s-rt01", Site: "core", Role: "cloud"},
	},
	}
	poolPrefix := netip.MustParsePrefix("192.168.123.0/24")
	observation := onPremObservation{
		Action:     "observed",
		Address:    "192.168.123.201",
		MAC:        "02:00:c0:a8:7b:c9",
		Interface:  "eth1",
		SourceType: OnPremSourceDHCPv4Lease,
		ObservedAt: now,
	}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt01", "192.168.123.201/32", observation, now.Add(-90*time.Second), 2*time.Minute))
	members := plannerMembers(spec.Members)
	self, ok := lookupMemberByNodeRef(members, "pve-rt01")
	if !ok {
		t.Fatal("missing self member")
	}
	discovery := DiscoveryController{Store: store, Now: func() time.Time { return now }}
	changed, err := discovery.recordOnPremObservation(NormalizedMobilityPool{Name: "cloudedge", Spec: spec.MobilityPoolSpec, Self: self}, poolPrefix, observation, now)
	if err != nil {
		t.Fatalf("recordOnPremObservation: %v", err)
	}
	if changed {
		t.Fatal("same-owner lease refresh must not be treated as ownership change")
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v, want compacted refreshed ownership fact", events)
	}
	if !events[0].ObservedAt.Equal(now) || !events[0].ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("event timestamps = observed %s expires %s, want refreshed lease", events[0].ObservedAt, events[0].ExpiresAt)
	}

	router := staticRouter("pve-rt01", spec)
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(90 * time.Second) }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Mobility Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "pve-rt01"), "192.168.123.201/32"); !ok {
		t.Fatalf("paths = %#v, want refreshed owner advertised after previous lease would have expired", bgp.paths)
	}
}

func TestDiscoveryControllerOnPremL2FederationEventsFeedBGPAdvertisement(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
		{
			NodeRef: "pve-rt08",
			Site:    "pve08",
			Role:    "onprem",
			Capture: api.MobilityMemberCapture{
				Type:          "proxy-arp",
				Interface:     "svnet1",
				SourceAddress: "192.168.123.2",
				ActiveWhen:    api.CaptureActiveWhen{Type: "single-router"},
			},
			OwnershipDiscovery: api.MobilityOwnershipDiscovery{
				Mode:     "onprem-l2",
				LeaseTTL: "2m",
				Scope: api.MobilityOwnershipDiscoveryScope{
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
	for _, observation := range []onPremObservation{
		{Address: "192.168.123.113", MAC: "bc:24:11:fb:ea:f0", Interface: "svnet1", SourceType: OnPremSourceOnDemandARP},
		{Address: "192.168.123.132", MAC: "bc:24:11:c9:33:c2", Interface: "svnet1", SourceType: OnPremSourceOnDemandARP},
	} {
		recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt08", observation.Address, observation, now.Add(-10*time.Second), 2*time.Minute))
	}

	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	ownershipEvents := onPremDiscoveryOwnershipEvents(events)
	if len(ownershipEvents) != 2 || ownershipEvents[0].Type != ObservedEventType || ownershipEvents[1].Type != ObservedEventType {
		t.Fatalf("events = %#v, want two federation ownership facts", events)
	}
	subjects := map[string]bool{}
	for _, event := range ownershipEvents {
		subjects[event.Subject] = true
	}
	if !subjects["192.168.123.113"] || !subjects["192.168.123.132"] {
		t.Fatalf("event subjects = %#v, want 192.168.123.113 and 192.168.123.132", subjects)
	}
	for _, event := range ownershipEvents {
		if event.Payload["source"] != onPremDiscoverySource || event.Payload["sourceType"] != OnPremSourceOnDemandARP {
			t.Fatalf("payload = %#v, want on-demand onprem source", event.Payload)
		}
		if !event.ExpiresAt.Equal(now.Add(110 * time.Second)) {
			t.Fatalf("expiresAt = %s, want original event lease", event.ExpiresAt)
		}
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryObserved"]) != "2" {
		t.Fatalf("status = %#v, want discoveryObserved=2", status)
	}
	if status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v, want observed onprem-l2 discovery", status)
	}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Discovery Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["discoveryObserved"]) != "2" || status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v, want active federation events to keep discovery observed", status)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Mobility Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "pve-rt08"), "192.168.123.132/32"); !ok {
		t.Fatalf("paths = %#v, want event-observed owner advertised", bgp.paths)
	}
}

func TestDiscoveryControllerOnPremL2ArmedUntilClientsObserved(t *testing.T) {
	now := time.Date(2026, 6, 25, 3, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
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
				Mode: "onprem-l2",
				Sources: []api.MobilityOwnershipDiscoverySource{
					{Type: OnPremSourceARPObserver, Interface: "svnet1"},
				},
			},
		},
		{NodeRef: "k8s-rt02", Site: "core", Role: "cloud"},
	},
	}
	router := staticRouter("pve-rt08", spec)
	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Armed" || fmt.Sprint(status["discoveryObserved"]) != "0" {
		t.Fatalf("status = %#v, want armed onprem-l2 discovery before any client is observed", status)
	}
	if _, ok := statusTimeValue(status["discoveryArmedAt"]); !ok {
		t.Fatalf("status = %#v, want discoveryArmedAt", status)
	}
}

func TestDiscoveryControllerOnPremL2CompletesEmptyAfterPolicy(t *testing.T) {
	now := time.Date(2026, 6, 26, 4, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
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
				Mode:            "onprem-l2",
				AllowEmptyAfter: "5s",
				Sources: []api.MobilityOwnershipDiscoverySource{
					{Type: OnPremSourceARPObserver, Interface: "svnet1"},
				},
			},
		},
		{NodeRef: "k8s-rt02", Site: "core", Role: "cloud"},
	},
	}
	router := staticRouter("pve-rt08", spec)
	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Discovery Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Armed" || fmt.Sprint(status["discoveryObserved"]) != "0" {
		t.Fatalf("status = %#v, want armed before allowEmptyAfter", status)
	}

	discovery.Now = func() time.Time { return now.Add(6 * time.Second) }
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Discovery Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Complete" || fmt.Sprint(status["discoveryObserved"]) != "0" {
		t.Fatalf("status = %#v, want empty complete discovery", status)
	}
	if _, ok := statusTimeValue(status["discoveryCompletedAt"]); !ok {
		t.Fatalf("status = %#v, want discoveryCompletedAt", status)
	}
	if _, ok := statusTimeValue(status["discoveryFreshUntil"]); !ok {
		t.Fatalf("status = %#v, want discoveryFreshUntil", status)
	}
}

func TestDiscoveryControllerOnPremL2FederationEventsRespectSourceScopeAndExpiry(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 35, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "192.168.123.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
		{
			NodeRef: "pve-rt06",
			Site:    "pve06",
			Role:    "onprem",
			Capture: api.MobilityMemberCapture{
				Type:          "proxy-arp",
				Interface:     "svnet1",
				SourceAddress: "192.168.123.2",
				ActiveWhen:    api.CaptureActiveWhen{Type: "single-router"},
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
	valid := onPremObservation{Address: "192.168.123.132", MAC: "bc:24:11:c9:33:c2", Interface: "svnet1", SourceType: OnPremSourceARPObserver}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt06", valid.Address, valid, now.Add(-10*time.Second), 2*time.Minute))
	// These malformed or stale records must never become a discovery fact just
	// because they happen to share the MobilityPool event group.
	offScope := onPremObservation{Address: "192.168.123.133", Interface: "svnet1", SourceType: OnPremSourceARPObserver}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt06", offScope.Address, offScope, now.Add(-10*time.Second), 2*time.Minute))
	wrongSource := onPremObservation{Address: "192.168.123.132", Interface: "other", SourceType: OnPremSourceOnDemandARP}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt06", wrongSource.Address, wrongSource, now.Add(-10*time.Second), 2*time.Minute))
	expired := onPremObservation{Address: "192.168.123.132", Interface: "svnet1", SourceType: OnPremSourceARPObserver}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "pve-rt06", expired.Address, expired, now.Add(-3*time.Minute), time.Minute))

	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("Discovery Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Observed" || fmt.Sprint(status["discoveryObserved"]) != "1" {
		t.Fatalf("status = %#v, want one valid source-scoped federation observation", status)
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want only client IP", addressEvents)
	}
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if runtime.Self.NICRef != "resolved-router-nic" || runtime.Self.SubnetRef != "subnet-a" {
		t.Fatalf("runtime self fact = %#v", runtime.Self)
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want only client IP", addressEvents)
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want only local subnet client ownership", addressEvents)
	}
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if got := runtime.Self.PrivateIPs; len(got) != 1 || got[0] != "10.88.60.4/32" || !runtime.Self.PrimaryObserved {
		t.Fatalf("runtime self fact = %#v, want only self NIC primary", runtime.Self)
	}
	if got := runtime.Self.CapturedAddresses; len(got) != 1 || got[0] != "10.88.60.13/32" {
		t.Fatalf("runtime self fact = %#v, want self NIC secondary split out", runtime.Self)
	}
	if got := runtime.Addresses; len(got) != 1 || got[0].Address != "10.88.60.11/32" {
		t.Fatalf("runtime owned addresses = %#v, want only provider-local client", got)
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.11/32" || addressEvents[0].ResourceRef != "i-client" {
		t.Fatalf("provider ownership events = %#v, want only non-self client ownership with resourceRef", addressEvents)
	}
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if got := runtime.Addresses; len(got) != 1 || got[0].Address != "10.88.60.11/32" {
		t.Fatalf("runtime owned addresses = %#v, want only client", got)
	}
	if got := runtime.Self.CapturedAddresses; len(got) != 1 || got[0] != "10.88.60.12/32" || !runtime.Self.PrimaryObserved || runtime.Self.ResourceRef != "i-router-a" {
		t.Fatalf("runtime self fact = %#v, want self resource secondary split out", runtime.Self)
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want only client ownership", addressEvents)
	}
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if got := runtime.Addresses; len(got) != 1 || got[0].Address != "10.88.60.11/32" {
		t.Fatalf("runtime owned addresses = %#v, want only client", got)
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 2 {
		t.Fatalf("provider ownership events = %#v, want lease table ignored in BGP clean mode", got)
	}
}

func TestDiscoveryControllerAllowsSameSiteLeaseHandoverDiscovery(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want same-site handover discovery accepted", addressEvents)
	}
}

func TestDiscoveryControllerExcludesCurrentTrapActionTargets(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	part := dynamicconfig.NewPart(
		"mobility-cloudedge-azure-router-a",
		DynamicSource("cloudedge", "azure-router-a"),
		[]api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}},
		dynamicGeneration,
		now.Add(-time.Second),
		now.Add(DefaultLeaseTTL),
	)
	part.Spec.ActionPlans = []dynamicconfig.ActionPlan{{
		Name:        "trap-remote",
		Provider:    "azure",
		ProviderRef: "azure-provider",
		Action:      actionAssignSecondaryIP,
		Target: map[string]string{
			"address":         "10.88.60.12/32",
			"captureStrategy": captureStrategySecondaryIP,
			"nicRef":          "router-nic",
			"provider":        "azure",
			"providerRef":     "azure-provider",
		},
		Parameters: map[string]string{captureParamHolder: "azure-router-a"},
	}}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatalf("encode action plans: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 1 || got[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want current trap action target excluded", got)
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 1 || got[0].Address != "10.88.60.12/32" {
		t.Fatalf("provider ownership events = %#v, want remote provider trap not to hide local home inventory", got)
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 1 || got[0].Address != "10.88.60.7/32" {
		t.Fatalf("provider ownership events = %#v, want default primary address accepted", got)
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 1 || got[0].Address != "10.88.60.11/32" {
		t.Fatalf("provider ownership events = %#v, want only address allowed by include and not excluded", got)
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
	if got := providerDiscoveryAddressFacts(events); len(got) != 0 {
		t.Fatalf("provider ownership events = %#v, want no standby ownership observations", got)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Standby" || !strings.Contains(status["discoveryReason"].(string), "active node") {
		t.Fatalf("status = %#v", status)
	}
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if len(runtime.Addresses) != 0 {
		t.Fatalf("runtime fact = %#v, want standby to clear ownership backing", runtime)
	}
	if runtime.Self.NICRef != "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b" || runtime.Self.SubnetRef != "subnet-b" {
		t.Fatalf("runtime self fact = %#v, want standby self NIC resolved", runtime.Self)
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
				Type:        "provider-secondary-ip",
				Interface:   "ens5",
				ProviderRef: "aws-provider",
				TargetFrom:  map[string]string{"region": "aws.region"},
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
	spec.Members = []api.ResolvedMobilityPoolMember{
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
	if status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v, want discovered self NIC on active profile-only peer", status)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if runtime := providerDiscoveryRuntimeFactFromEvents(t, events); runtime.Self.NICRef != "eni-b" {
		t.Fatalf("runtime self fact = %#v, want discovered self NIC on active profile-only peer", runtime.Self)
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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "azure-router-b", spec.Members, map[string]string{
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
	addressEvents := providerDiscoveryAddressFacts(events)
	if len(addressEvents) != 1 || addressEvents[0].Address != "10.88.60.12/32" {
		t.Fatalf("provider ownership events = %#v, want seized standby provider-discovery owner event", addressEvents)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["discoveryPhase"] != "Observed" || fmt.Sprint(status["discoveryObserved"]) != "1" {
		t.Fatalf("status = %#v, want seized standby discovery observed", status)
	}
	if got := providerDiscoveryRuntimeFactFromEvents(t, events).Addresses; len(got) != 1 || got[0].Address != "10.88.60.12/32" {
		t.Fatalf("runtime owned addresses = %#v, want seized standby owned address", got)
	}

	bgp := &fakeBGPPaths{}
	mobility := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := mobility.Reconcile(context.Background()); err != nil {
		t.Fatalf("mobility Reconcile: %v", err)
	}
	path := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "azure-router-b"), "10.88.60.12/32")
	if path.Attrs.LocalPref != bgpMobilityLocalPrefBase+1 {
		t.Fatalf("path attrs = %#v, want seized standby to advertise as active owner", path.Attrs)
	}
}

func TestDiscoveryControllerExpiresPreviousProviderDiscoveryWhenStandby(t *testing.T) {
	now := time.Date(2026, 6, 3, 13, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordProviderDiscoveryRuntime(t, store, "azure-router-b", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.13/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "standby-router-nic", SubnetRef: "subnet-b"}, now.Add(-time.Minute), 2*time.Minute),
		},
	}, now.Add(-time.Minute))
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
	if got := providerDiscoveryRuntimeFactFromEvents(t, events).Addresses; len(got) != 0 {
		t.Fatalf("runtime addresses = %#v, want standby to clear stale provider discovery", got)
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
	recordProviderDiscoveryRuntime(t, store, "azure-router-b", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.13/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "standby-router-nic", SubnetRef: "subnet-b"}, now.Add(-time.Minute), 2*time.Minute),
		},
	}, now.Add(-time.Minute))
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
	recordProviderDiscoveryRuntime(t, store, "azure-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			NICRef:     "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
			SubnetRef:  "subnet-a",
			PrivateIPs: []string{"10.88.60.11/32"},
		},
		Placement: discoveryPlacementObservation{ActiveNode: "azure-router-a", Active: true},
	}, now)

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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "azure-router-b", spec.Members, map[string]string{
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
	if status["phase"] == "Degraded" {
		t.Fatalf("mobility status = %#v, want non-degraded plan after self NIC discovery", status)
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
	plannerNow := now.Add(10 * time.Second)
	planner := Controller{Router: router, Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return plannerNow }}
	if err := planner.Reconcile(context.Background()); err != nil {
		t.Fatalf("planner Reconcile after active marker loss: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(10 * time.Second) }
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "mobility-bgp", Kind: "BGPRouter"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	if err := controller.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if runner.calls != 2 {
		t.Fatalf("runner calls = %d, want BGP liveness loss to bypass scan interval", runner.calls)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	events, err := store.ListFederationEvents("cloudedge", false, now.Add(10*time.Second).Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if runtime := providerDiscoveryRuntimeFactFromEvents(t, events); runtime.Placement.Seize || status["discoveryPhase"] != "Standby" || status["bgpSeizeHoldDownActive"] != true {
		t.Fatalf("status = %#v, want hold-down standby after active marker loss", status)
	}
	plannerNow = now.Add(10*time.Second + bgpSeizeLivenessMissingHold + time.Second)
	if err := planner.Reconcile(context.Background()); err != nil {
		t.Fatalf("planner Reconcile after hold-down expiry: %v", err)
	}
	controller.Now = func() time.Time { return plannerNow }
	if err := controller.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent after hold-down: %v", err)
	}
	if runner.calls != 3 {
		t.Fatalf("runner calls = %d, want hold-down expiry to bypass scan interval", runner.calls)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	events, err = store.ListFederationEvents("cloudedge", false, now.Add(10*time.Second+bgpSeizeLivenessMissingHold+time.Second).Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if runtime := providerDiscoveryRuntimeFactFromEvents(t, events); !runtime.Placement.Seize || status["discoveryPhase"] != "Observed" || status["bgpSeizeHoldDownActive"] != false {
		t.Fatalf("status = %#v, want seized discovery after hold-down", status)
	}
}

func TestDiscoveryControllerStoresForwardingInRuntimeFact(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
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
		t.Fatalf("runner calls = %d, want initial scan", runner.calls)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if runtime := providerDiscoveryRuntimeFactFromEvents(t, events); runtime.Self.ForwardingEnabled == nil || *runtime.Self.ForwardingEnabled {
		t.Fatalf("runtime self fact = %#v, want forwarding=false", runtime.Self)
	}
}

func TestDiscoveryControllerStoresImplicitSelfNICInRuntimeFact(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	spec.Members[1].Capture.NICRef = ""
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
		t.Fatalf("runner calls = %d, want initial scan", runner.calls)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if runtime := providerDiscoveryRuntimeFactFromEvents(t, events); runtime.Self.NICRef != "router-nic" {
		t.Fatalf("runtime self fact = %#v, want self NIC populated", runtime.Self)
	}
}

func TestDiscoveryControllerDoesNotExpireProviderDiscoveryOnTransientActiveMiss(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordProviderDiscoveryRuntime(t, store, "azure-router-a", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic", SubnetRef: "subnet-a"}, now.Add(-90*time.Second), 2*time.Minute),
		},
	}, now.Add(-90*time.Second))
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
	runtime := providerDiscoveryRuntimeFactFromEvents(t, events)
	if len(runtime.Addresses) != 1 || runtime.Addresses[0].Address != "10.88.60.12/32" || !runtime.Addresses[0].MissingHoldUntil.After(now) {
		t.Fatalf("runtime fact = %#v, want active missing scan retained through missing hold", runtime)
	}
}

func TestDiscoveryControllerExpiresProviderDiscoveryAfterMissingInventoryHold(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordProviderDiscoveryRuntime(t, store, "azure-router-a", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic", SubnetRef: "subnet-a"}, now.Add(-3*time.Minute), 10*time.Minute),
		},
	}, now.Add(-3*time.Minute))
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
	if got := providerDiscoveryRuntimeFactFromEvents(t, events).Addresses; len(got) != 0 {
		t.Fatalf("runtime addresses = %#v, want missing inventory after hold to withdraw provider discovery", got)
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

func TestDiscoveryControllerExpiresProviderDiscoveryAddressExcludedBySelector(t *testing.T) {
	now := time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordProviderDiscoveryRuntime(t, store, "azure-router-a", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic", SubnetRef: "subnet-a"}, now.Add(-3*time.Minute), 5*time.Minute),
		},
	}, now.Add(-3*time.Minute))
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
	if got := providerDiscoveryRuntimeFactFromEvents(t, events).Addresses; len(got) != 0 {
		t.Fatalf("runtime addresses = %#v, want selector-excluded address withdrawn", got)
	}
}

func TestDiscoveryControllerExpiresProviderDiscoveryAddressScopedOutByProvider(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := discoveryPoolSpec()
	recordProviderDiscoveryRuntime(t, store, "azure-router-a", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "stale-foreign-nic", ProviderRef: "azure-provider", SubnetRef: "subnet-a"}, now.Add(-time.Minute), 5*time.Minute),
		},
	}, now.Add(-time.Minute))
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
	if got := providerDiscoveryRuntimeFactFromEvents(t, events).Addresses; len(got) != 0 {
		t.Fatalf("runtime owned addresses = %#v, want provider-scoped-out address excluded", got)
	}
}

func discoveryPoolSpec() testMobilityPoolSpec {
	spec := centralizedOwnershipPoolSpec()
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

func profileDiscoveryPoolSpecForNode(selfNode string) testMobilityPoolSpec {
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
				Type:        "provider-secondary-ip",
				ProviderRef: "azure-provider",
				NICRef:      spec.Values["azure.nic"],
				TargetFrom:  map[string]string{"ipConfigName": "azure.ipConfigName", "region": "azure.region"},
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
	spec.Members = []api.ResolvedMobilityPoolMember{
		spec.Members[0],
		{NodeRef: "azure-router-a", Site: "azure", Role: "cloud", ProfileRef: "azure-edge", Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10}},
		{NodeRef: "azure-router-b", Site: "azure", Role: "cloud", Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20}},
	}
	return spec
}

func TestDiscoveryRouterNICRefsIncludesCanonicalLocalCaptureNIC(t *testing.T) {
	members := plannerMembers([]api.ResolvedMobilityPoolMember{{
		NodeRef: "azure-leaf-b",
		Capture: api.MobilityMemberCapture{
			Type:   "provider-secondary-ip",
			NICRef: "/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/azure-leaf-bVMNic",
		},
	}})
	refs := discoveryRouterNICRefs(NormalizedMobilityPool{Self: members["azure-leaf-b"]})
	if !refs["/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/azure-leaf-bVMNic"] {
		t.Fatalf("refs = %#v, want capture.nicRef excluded as router NIC", refs)
	}
}

func reconcileDiscoveryRequest(t *testing.T, selfNode string, spec testMobilityPoolSpec, now time.Time) providerinventory.ObservePrivateIPsRequest {
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

func discoveryRouter(nodeName string, spec testMobilityPoolSpec) *api.Router {
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
