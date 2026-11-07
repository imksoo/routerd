// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

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
		if _, duplicate := byType[intent.SourceType]; duplicate {
			t.Fatalf("duplicate ARP source type %s", intent.SourceType)
		}
		byType[intent.SourceType] = intent
		if intent.IfName != "ens3" {
			t.Fatalf("%s IfName = %q, want ens3", intent.SourceType, intent.IfName)
		}
	}
	for _, kind := range []string{OnPremSourceARPObserver, OnPremSourceOnDemandARP, OnPremSourcePVESVNet} {
		if _, ok := byType[kind]; !ok {
			t.Fatalf("required ARP source %s absent: %#v", kind, byType)
		}
	}
	passive := byType[OnPremSourceARPObserver]
	if !passive.Observe || passive.OnDemand || passive.SourceType != OnPremSourceARPObserver {
		t.Fatalf("passive ARP intent = %#v", passive)
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
