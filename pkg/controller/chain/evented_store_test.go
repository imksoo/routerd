// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mergeTrackingMapStore struct {
	mapStore
	mergeCalls int
	saveCalls  int
}

func (s *mergeTrackingMapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s.saveCalls++
	return s.mapStore.SaveObjectStatus(apiVersion, kind, name, status)
}

func (s *mergeTrackingMapStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	s.mergeCalls++
	current := s.ObjectStatus(apiVersion, kind, name)
	next := copyStatusMap(current)
	for key, value := range updates {
		next[key] = value
	}
	return s.mapStore.SaveObjectStatus(apiVersion, kind, name, next)
}

func anyStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func TestStatusWithOwnershipAddsControllerMetadata(t *testing.T) {
	status := statusWithOwnership("net.routerd.net/v1alpha1", "EgressRoutePolicy", map[string]any{"phase": "Applied"})
	if status["owner"] != "route" {
		t.Fatalf("owner = %v, want route", status["owner"])
	}
	if status["managedBy"] != "routerd" || status["management"] != "managed" {
		t.Fatalf("management metadata = managedBy:%v management:%v, want routerd/managed", status["managedBy"], status["management"])
	}
}

func TestStatusWithOwnershipPreservesAdoptedManagedBy(t *testing.T) {
	status := statusWithOwnership("system.routerd.net/v1alpha1", "NetworkAdoption", map[string]any{
		"phase":     "Observed",
		"managed":   false,
		"managedBy": "systemd-networkd",
	})
	if status["owner"] != "network-adoption" {
		t.Fatalf("owner = %v, want network-adoption", status["owner"])
	}
	if status["managedBy"] != "systemd-networkd" || status["management"] != "adopted" {
		t.Fatalf("management metadata = managedBy:%v management:%v, want systemd-networkd/adopted", status["managedBy"], status["management"])
	}
}

func TestEventedStoreAddsLifecycleOwnerMetadata(t *testing.T) {
	base := mapStore{}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
		Metadata: api.ObjectMeta{Name: "fabric"},
		Spec:     api.BGPPeerSpec{RouterRef: "core", PeerASN: 64512, Peers: []string{"192.0.2.1"}},
	}}}}
	store := eventedStore{Store: base, Router: router}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", "fabric", map[string]any{"phase": "Established"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	status := base.ObjectStatus(api.NetAPIVersion, "BGPPeer", "fabric")
	if status["ownerKey"] != api.NetAPIVersion+"/BGPPeer/fabric" {
		t.Fatalf("ownerKey = %v", status["ownerKey"])
	}
	ownerRef, ok := status["ownerRef"].(map[string]any)
	if !ok || ownerRef["apiVersion"] != api.NetAPIVersion || ownerRef["kind"] != "BGPPeer" || ownerRef["name"] != "fabric" {
		t.Fatalf("ownerRef = %#v", status["ownerRef"])
	}
	if status["lifecycleClass"] != "controller" {
		t.Fatalf("lifecycleClass = %v, want controller", status["lifecycleClass"])
	}
}

func TestMobilityStoreForwardsRecordBusEventThroughProductionBus(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	raw, err := routerstate.Open(statePath)
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	rawStore, ok := raw.(*routerstate.SQLiteStore)
	if !ok {
		t.Fatalf("state store = %T, want *SQLiteStore", raw)
	}
	t.Cleanup(func() {
		_ = rawStore.Close()
	})
	eventBus := bus.NewWithStore(rawStore)
	store := mobilityStore{
		evented: eventedStore{Store: rawStore, Bus: eventBus},
		data:    rawStore,
	}
	recorder, ok := any(store).(interface {
		RecordBusEvent(context.Context, daemonapi.DaemonEvent) (string, error)
	})
	if !ok {
		t.Fatalf("mobilityStore does not expose RecordBusEvent")
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "mobility"}, "routerd.mobility.holder.transition", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	event.Attributes = map[string]string{
		"transitionKind": "seize-complete",
		"address":        "10.88.60.10/32",
	}
	if _, err := recorder.RecordBusEvent(context.Background(), event); err != nil {
		t.Fatalf("record bus event: %v", err)
	}
	events, err := rawStore.ListEvents(routerstate.EventQuery{Topic: "routerd.mobility.holder.transition", Limit: 10})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(events))
	}
	if got := events[0].Attributes["transitionKind"]; got != "seize-complete" {
		t.Fatalf("transitionKind = %q, want seize-complete", got)
	}
	if got := events[0].ResourceKind + "/" + events[0].ResourceName; got != "MobilityPool/cloudedge" {
		t.Fatalf("resource = %q, want MobilityPool/cloudedge", got)
	}
}

func TestDaemonStatusControllerMergesMobilityPoolStatus(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			http.NotFound(w, r)
			return
		}
		status := daemonapi.DaemonStatus{Resources: []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"},
			Phase:    "Observed",
			Health:   "OK",
			Observed: map[string]string{
				"sourceType": "arp-observer",
				"address":    "10.88.60.10/32",
			},
		}}}
		_ = json.NewEncoder(w).Encode(status)
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	base := &mergeTrackingMapStore{mapStore: mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"plannerPhase":            "BGPPlanned",
			"discoverySelfPrivateIPs": []string{"10.88.60.21"},
		},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"},
		Metadata: api.ObjectMeta{Name: "wan"},
	}}}}
	controller := DaemonStatusController{
		Router:        router,
		Store:         base,
		DaemonSockets: map[string]string{"wan": socket},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if base.mergeCalls != 1 || base.saveCalls != 0 {
		t.Fatalf("mergeCalls=%d saveCalls=%d, want MobilityPool partial merge", base.mergeCalls, base.saveCalls)
	}
	status := base.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "BGPPlanned" || status["discoverySelfPrivateIPs"] == nil {
		t.Fatalf("status fields were not preserved: %#v", status)
	}
	if status["phase"] != "Observed" || status["address"] != "10.88.60.10/32" {
		t.Fatalf("daemon observed fields missing: %#v", status)
	}
}

func TestDaemonStatusControllerKeepsDedicatedControllerStatusFields(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			http.NotFound(w, r)
			return
		}
		status := daemonapi.DaemonStatus{Resources: []daemonapi.ResourceStatus{
			{
				Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
				Phase:    daemonapi.ResourcePhaseBound,
				Health:   daemonapi.HealthOK,
				Observed: map[string]string{
					"interface":      "ens18",
					"currentAddress": "192.0.2.10",
					"prefixLength":   "24",
					"defaultGateway": "192.0.2.1",
					"dnsServers":     `["192.0.2.53","192.0.2.54"]`,
					"leaseTime":      "3600",
				},
			},
			{
				Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: "pppoe"},
				Phase:    "Connected",
				Health:   daemonapi.HealthOK,
				Observed: map[string]string{
					"ifname":         "ppp0",
					"currentAddress": "198.51.100.10",
					"peerAddress":    "198.51.100.1",
					"dnsServers":     `["203.0.113.53"]`,
					"bytesIn":        "100",
					"bytesOut":       "200",
				},
			},
			{
				Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation", Name: "wan-pd"},
				Phase:    daemonapi.ResourcePhaseBound,
				Health:   daemonapi.HealthOK,
				Observed: map[string]string{
					"interface":     "ens18",
					"currentPrefix": "2001:db8:1::/56",
					"dnsServers":    `["2001:db8::53"]`,
					"sntpServers":   `["2001:db8::123"]`,
					"domainSearch":  `["example.test"]`,
				},
			},
			{
				Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: "lan-dns"},
				Phase:    "Running",
				Health:   daemonapi.HealthOK,
				Observed: map[string]string{
					"listeners": "2",
					"zones":     "3",
				},
			},
			{
				Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "internet"},
				Phase:    "Healthy",
				Health:   daemonapi.HealthOK,
				Observed: map[string]string{
					"lastResult": "passed",
					"target":     "1.1.1.1",
				},
			},
		}}
		_ = json.NewEncoder(w).Encode(status)
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	base := mapStore{
		api.NetAPIVersion + "/DHCPv4Client/wan": {
			"phase":          daemonapi.ResourcePhaseBound,
			"currentAddress": "192.0.2.10",
			"prefixLength":   24,
			"dnsServers":     []string{"192.0.2.53"},
			"appliedAddress": "192.0.2.10/24",
			"addressPresent": true,
			"applyMode":      "dry-run",
			"ifname":         "ens18",
			"device":         "ens18",
			"dryRun":         true,
			"gateway":        "192.0.2.1",
		},
		api.NetAPIVersion + "/PPPoESession/pppoe": {
			"phase":          "Connected",
			"device":         "ppp0",
			"currentAddress": "198.51.100.10",
			"peerAddress":    "198.51.100.1",
			"gateway":        "198.51.100.1",
			"dnsServers":     []string{"203.0.113.53"},
			"dryRun":         true,
		},
		api.NetAPIVersion + "/DHCPv6PrefixDelegation/wan-pd": {
			"phase":         daemonapi.ResourcePhaseBound,
			"currentPrefix": "2001:db8:1::/56",
			"serverDUID":    "00010001",
			"dnsServers":    []string{"2001:db8::53"},
		},
		api.NetAPIVersion + "/DNSResolver/lan-dns": {
			"phase":           "Applied",
			"health":          "ControllerOK",
			"listeners":       1,
			"listenAddresses": []string{"127.0.0.1:53"},
			"sources":         4,
		},
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":        "Healthy",
			"lastResult":   "passed",
			"failureCount": 0,
		},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe"}, Spec: api.PPPoESessionSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-dns"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}},
	}}}
	controller := DaemonStatusController{
		Router:        router,
		Store:         base,
		DaemonSockets: map[string]string{"wan": socket, "pppoe": socket, "wan-pd": socket, "lan-dns": socket, "internet": socket},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	dhcp := base.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	for key, want := range map[string]any{"appliedAddress": "192.0.2.10/24", "addressPresent": true, "applyMode": "dry-run", "ifname": "ens18", "device": "ens18", "dryRun": true, "gateway": "192.0.2.1"} {
		if got := dhcp[key]; got != want {
			t.Fatalf("DHCPv4Client %s = %#v, want %#v in %#v", key, got, want, dhcp)
		}
	}
	if _, ok := dhcp["leaseTime"]; ok {
		t.Fatalf("daemon observed leaseTime leaked to top-level status: %#v", dhcp)
	}
	dhcpObserved, ok := dhcp["observed"].(map[string]any)
	if !ok {
		t.Fatalf("DHCPv4Client observed = %#v", dhcp["observed"])
	}
	if dhcpObserved["prefixLength"] != 24 || dhcpObserved["leaseTime"] != int64(3600) {
		t.Fatalf("DHCPv4Client normalized observed = %#v", dhcpObserved)
	}
	if got := anyStringSlice(dhcpObserved["dnsServers"]); strings.Join(got, ",") != "192.0.2.53,192.0.2.54" {
		t.Fatalf("DHCPv4Client observed dnsServers = %#v", dhcpObserved["dnsServers"])
	}

	pppoe := base.ObjectStatus(api.NetAPIVersion, "PPPoESession", "pppoe")
	if pppoe["device"] != "ppp0" || pppoe["dryRun"] != true || pppoe["gateway"] != "198.51.100.1" {
		t.Fatalf("PPPoESession controller fields were not preserved: %#v", pppoe)
	}
	pppoeObserved, ok := pppoe["observed"].(map[string]any)
	if !ok || pppoeObserved["bytesIn"] != uint64(100) || pppoeObserved["bytesOut"] != uint64(200) {
		t.Fatalf("PPPoESession normalized observed = %#v", pppoe["observed"])
	}
	if got := anyStringSlice(pppoeObserved["dnsServers"]); strings.Join(got, ",") != "203.0.113.53" {
		t.Fatalf("PPPoESession observed dnsServers = %#v", pppoeObserved["dnsServers"])
	}

	pd := base.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd")
	if pd["currentPrefix"] != "2001:db8:1::/56" || pd["serverDUID"] != "00010001" {
		t.Fatalf("DHCPv6PrefixDelegation controller fields were not preserved: %#v", pd)
	}
	pdObserved, ok := pd["observed"].(map[string]any)
	if !ok {
		t.Fatalf("DHCPv6PrefixDelegation observed = %#v", pd["observed"])
	}
	if got := anyStringSlice(pdObserved["dnsServers"]); strings.Join(got, ",") != "2001:db8::53" {
		t.Fatalf("DHCPv6PrefixDelegation observed dnsServers = %#v", pdObserved["dnsServers"])
	}
	if got := anyStringSlice(pdObserved["domainSearch"]); strings.Join(got, ",") != "example.test" {
		t.Fatalf("DHCPv6PrefixDelegation observed domainSearch = %#v", pdObserved["domainSearch"])
	}

	dns := base.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-dns")
	if dns["phase"] != "Applied" || dns["health"] != "ControllerOK" || dns["listeners"] != 1 || strings.Join(anyStringSlice(dns["listenAddresses"]), ",") != "127.0.0.1:53" || dns["sources"] != 4 {
		t.Fatalf("DNSResolver controller fields were not preserved: %#v", dns)
	}
	dnsObserved, ok := dns["observed"].(map[string]any)
	if !ok || dnsObserved["phase"] != "Running" || dnsObserved["health"] != daemonapi.HealthOK || dnsObserved["listeners"] != 2 || dnsObserved["zones"] != 3 {
		t.Fatalf("DNSResolver normalized observed = %#v", dns["observed"])
	}

	health := base.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if health["lastResult"] != "passed" || health["failureCount"] != 0 {
		t.Fatalf("HealthCheck controller fields were not preserved: %#v", health)
	}
	healthObserved, ok := health["observed"].(map[string]any)
	if !ok || healthObserved["target"] != "1.1.1.1" {
		t.Fatalf("HealthCheck observed = %#v", health["observed"])
	}
}

func TestStatusChangedForEventIgnoresVolatileObservedFields(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		kind       string
		current    map[string]any
		next       map[string]any
	}{
		{
			name:       "healthcheck success timestamp",
			apiVersion: api.NetAPIVersion,
			kind:       "HealthCheck",
			current:    map[string]any{"phase": "Healthy", "lastSuccessTime": "2026-07-06T12:00:00Z"},
			next:       map[string]any{"phase": "Healthy", "lastSuccessTime": "2026-07-06T12:00:30Z"},
		},
		{
			name:       "lease sync timestamp",
			apiVersion: api.NetAPIVersion,
			kind:       "DHCPv4ServerLeaseSync",
			current:    map[string]any{"phase": "Synced", "sources": []map[string]any{{"path": "/run/routerd/dnsmasq.leases", "size": 10}}, "targets": []map[string]any{{"host": "standby", "phase": "Synced"}}, "syncedAt": "2026-07-06T12:00:00Z"},
			next:       map[string]any{"phase": "Synced", "sources": []map[string]any{{"path": "/run/routerd/dnsmasq.leases", "size": 20}}, "targets": []map[string]any{{"host": "standby", "phase": "Synced"}}, "syncedAt": "2026-07-06T12:00:30Z"},
		},
		{
			name:       "ip address set refresh timestamp",
			apiVersion: api.NetAPIVersion,
			kind:       "IPAddressSet",
			current:    map[string]any{"phase": "Applied", "addresses": []any{"192.0.2.1"}, "resolvedAt": "2026-07-06T12:00:00Z", "nextRefreshAt": "2026-07-06T12:01:00Z"},
			next:       map[string]any{"phase": "Applied", "addresses": []any{"192.0.2.1"}, "resolvedAt": "2026-07-06T12:00:30Z", "nextRefreshAt": "2026-07-06T12:01:30Z"},
		},
		{
			name:       "nat session sync counters",
			apiVersion: api.NetAPIVersion,
			kind:       "NAT44SessionSync",
			current:    map[string]any{"phase": "Synced", "sessionCount": 10, "insertOK": 10, "scriptBytes": 100, "syncedAt": "2026-07-06T12:00:00Z"},
			next:       map[string]any{"phase": "Synced", "sessionCount": 12, "insertOK": 12, "scriptBytes": 120, "syncedAt": "2026-07-06T12:00:30Z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if statusChangedForEvent(tt.apiVersion, tt.kind, tt.current, tt.next) {
				t.Fatalf("%s volatile field change produced status event", tt.name)
			}
		})
	}
}

func TestStatusChangedForEventKeepsOperationalFields(t *testing.T) {
	if !statusChangedForEvent(api.NetAPIVersion, "HealthCheck",
		map[string]any{"phase": "Healthy", "lastSuccessTime": "2026-07-06T12:00:00Z"},
		map[string]any{"phase": "Unhealthy", "lastSuccessTime": "2026-07-06T12:00:30Z"},
	) {
		t.Fatal("HealthCheck phase change did not produce status event")
	}
	if !statusChangedForEvent(api.NetAPIVersion, "IPAddressSet",
		map[string]any{"phase": "Applied", "addresses": []any{"192.0.2.1"}, "resolvedAt": "2026-07-06T12:00:00Z"},
		map[string]any{"phase": "Applied", "addresses": []any{"192.0.2.2"}, "resolvedAt": "2026-07-06T12:00:30Z"},
	) {
		t.Fatal("IPAddressSet address change did not produce status event")
	}
}

func TestNormalizedDaemonObservedValueCoversDaemonBackedResources(t *testing.T) {
	if got := normalizedDaemonObservedValue("DNSResolver", "listeners", "2"); got != 2 {
		t.Fatalf("DNSResolver listeners = %#v, want 2", got)
	}
}

func TestEventedStoreAddsDerivedOwnerRefs(t *testing.T) {
	base := mapStore{}
	root := api.OwnerRef{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "fabric"}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "sam-core-a", OwnerRefs: []api.OwnerRef{root}},
		Spec:     api.TunnelInterfaceSpec{Mode: "ipip", Local: "10.99.0.1", Remote: "10.99.0.2"},
	}}}}
	store := eventedStore{Store: base, Router: router}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "sam-core-a", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	status := base.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "sam-core-a")
	if status["ownerKey"] != api.HybridAPIVersion+"/TunnelInterface/sam-core-a" {
		t.Fatalf("ownerKey = %v", status["ownerKey"])
	}
	refs, ok := status["ownerRefs"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("ownerRefs = %#v", status["ownerRefs"])
	}
	ref, ok := refs[0].(map[string]any)
	if !ok || ref["apiVersion"] != api.MobilityAPIVersion || ref["kind"] != "SAMTransportProfile" || ref["name"] != "fabric" {
		t.Fatalf("ownerRefs[0] = %#v", refs[0])
	}
	if status["lifecycleClass"] != "managed-host" {
		t.Fatalf("lifecycleClass = %v, want managed-host", status["lifecycleClass"])
	}
}

func TestEventedStoreSkipsLifecycleMetadataForSyntheticStatus(t *testing.T) {
	base := mapStore{}
	store := eventedStore{Store: base}
	if err := store.SaveObjectStatus(api.RouterAPIVersion, "Inventory", "host", map[string]any{"phase": "Observed"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	status := base.ObjectStatus(api.RouterAPIVersion, "Inventory", "host")
	for _, key := range []string{"ownerKey", "ownerRef", "ownerRefs", "lifecycleClass"} {
		if _, ok := status[key]; ok {
			t.Fatalf("synthetic status has %s: %#v", key, status)
		}
	}
}

func TestStatusChangedIgnoresObservedTrafficCounters(t *testing.T) {
	current := map[string]any{
		"phase":       "Observed",
		"path":        "/var/lib/routerd/traffic-flows.db",
		"source":      "conntrack",
		"activeFlows": 10,
		"count":       100,
		"observedAt":  "2026-05-05T00:00:00Z",
	}
	next := map[string]any{
		"phase":       "Observed",
		"path":        "/var/lib/routerd/traffic-flows.db",
		"source":      "conntrack",
		"activeFlows": 20,
		"count":       200,
		"observedAt":  "2026-05-05T00:00:30Z",
	}
	if statusChanged(current, next) {
		t.Fatalf("Observed counter-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresPathMTUObservationTimestamp(t *testing.T) {
	current := map[string]any{
		"phase":         "Applied",
		"mtu":           float64(1445),
		"mtuSource":     "probe",
		"mtuObservedAt": "2026-05-12T00:52:18Z",
	}
	next := map[string]any{
		"phase":         "Applied",
		"mtu":           1445,
		"mtuSource":     "probe",
		"mtuObservedAt": "2026-05-12T01:02:43Z",
	}
	if statusChanged(current, next) {
		t.Fatalf("path MTU observation timestamp-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}

	next["mtu"] = 1444
	fields := statusChangedFields(current, next)
	if len(fields) != 1 || fields[0] != "mtu" {
		t.Fatalf("changed fields = %v, want [mtu]", fields)
	}
}

func TestStatusChangedIgnoresLastTransitionAt(t *testing.T) {
	current := map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ds-lite-ra",
		"selectedDevice":    "ds-lite-ra",
		"lastTransitionAt":  "2026-05-20T10:00:00Z",
	}
	next := map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ds-lite-ra",
		"selectedDevice":    "ds-lite-ra",
		"lastTransitionAt":  "2026-05-20T10:00:30Z",
	}
	if statusChanged(current, next) {
		t.Fatalf("lastTransitionAt-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresDHCPv4ClientLeaseTimestamps(t *testing.T) {
	current := map[string]any{
		"phase":          "Bound",
		"currentAddress": "192.0.2.10",
		"prefixLength":   "24",
		"defaultGateway": "192.0.2.1",
		"appliedAddress": "192.0.2.10/24",
		"lastLeaseAt":    "2026-06-19T10:00:00Z",
		"lastRenewAt":    "2026-06-19T10:00:00Z",
		"lastAppliedAt":  "2026-06-19T10:00:00Z",
		"renewAt":        "2026-06-19T10:30:00Z",
		"rebindAt":       "2026-06-19T10:45:00Z",
		"expiresAt":      "2026-06-19T11:00:00Z",
		"observed": map[string]any{
			"leaseTime": "3600",
			"renewAt":   "2026-06-19T10:30:00Z",
			"expiresAt": "2026-06-19T11:00:00Z",
		},
	}
	next := map[string]any{}
	for key, value := range current {
		next[key] = value
	}
	next["lastLeaseAt"] = "2026-06-19T10:00:30Z"
	next["lastRenewAt"] = "2026-06-19T10:00:30Z"
	next["lastAppliedAt"] = "2026-06-19T10:00:30Z"
	next["renewAt"] = "2026-06-19T10:29:30Z"
	next["rebindAt"] = "2026-06-19T10:44:30Z"
	next["expiresAt"] = "2026-06-19T10:59:30Z"
	next["observed"] = map[string]any{
		"leaseTime": "3570",
		"renewAt":   "2026-06-19T10:29:30Z",
		"expiresAt": "2026-06-19T10:59:30Z",
	}
	if statusChangedForEvent(api.NetAPIVersion, "DHCPv4Client", current, next) {
		t.Fatal("DHCPv4Client lease timestamp-only refresh should not be event-significant")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}

	next["currentAddress"] = "192.0.2.11"
	if !statusChangedForEvent(api.NetAPIVersion, "DHCPv4Client", current, next) {
		t.Fatal("DHCPv4Client address change must remain event-significant")
	}
}

func TestStatusChangedEventSeverityDemotesChattyFields(t *testing.T) {
	current := map[string]any{
		"phase":                      "Ready",
		"observedClients":            []any{"192.0.2.10=02:00:00:00:00:10"},
		"ownershipResolverDecisions": map[string]any{"192.0.2.10/32": "node-a"},
	}
	next := map[string]any{
		"phase":                      "Ready",
		"observedClients":            []any{"192.0.2.10=02:00:00:00:00:10", "192.0.2.11=02:00:00:00:00:11"},
		"ownershipResolverDecisions": map[string]any{"192.0.2.10/32": "node-a"},
	}
	fields := statusChangedFieldsForEvent(api.MobilityAPIVersion, "MobilityPool", current, next)
	if got := statusChangedEventSeverity(api.MobilityAPIVersion, "MobilityPool", current, next, fields); got != daemonapi.SeverityDebug {
		t.Fatalf("severity = %s, want debug", got)
	}

	next["phase"] = "Degraded"
	fields = statusChangedFieldsForEvent(api.MobilityAPIVersion, "MobilityPool", current, next)
	if got := statusChangedEventSeverity(api.MobilityAPIVersion, "MobilityPool", current, next, fields); got != daemonapi.SeverityInfo {
		t.Fatalf("abnormal phase severity = %s, want info", got)
	}
}

func TestStatusChangedEventSeverityDemotesRoutinePhaseTransitions(t *testing.T) {
	current := map[string]any{"phase": "Watching"}
	next := map[string]any{"phase": "Ready"}
	fields := statusChangedFieldsForEvent(api.MobilityAPIVersion, "MobilityPool", current, next)
	if got := statusChangedEventSeverity(api.MobilityAPIVersion, "MobilityPool", current, next, fields); got != daemonapi.SeverityDebug {
		t.Fatalf("severity = %s, want debug", got)
	}
}

func TestStatusChangedEventSeverityKeepsDHCPv4LeaseValueChangeInfo(t *testing.T) {
	current := map[string]any{
		"phase":          daemonapi.ResourcePhaseBound,
		"currentAddress": "192.0.2.10",
		"defaultGateway": "192.0.2.1",
	}
	next := map[string]any{
		"phase":          daemonapi.ResourcePhaseBound,
		"currentAddress": "192.0.2.11",
		"defaultGateway": "192.0.2.1",
	}
	fields := statusChangedFieldsForEvent(api.NetAPIVersion, "DHCPv4Client", current, next)
	if got := statusChangedEventSeverity(api.NetAPIVersion, "DHCPv4Client", current, next, fields); got != daemonapi.SeverityInfo {
		t.Fatalf("DHCPv4 lease value severity = %s, want info", got)
	}
}

func TestEventedStoreDoesNotPublishTimestampOnlyStatusChange(t *testing.T) {
	base := mapStore{
		api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default": statusWithOwnership(api.NetAPIVersion, "EgressRoutePolicy", map[string]any{
			"phase":             "Applied",
			"selectedCandidate": "ds-lite-ra",
			"selectedDevice":    "ds-lite-ra",
			"lastTransitionAt":  "2026-05-20T10:00:00Z",
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy", Name: "ipv4-default"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ds-lite-ra",
		"selectedDevice":    "ds-lite-ra",
		"lastTransitionAt":  "2026-05-20T10:00:30Z",
	}); err != nil {
		t.Fatalf("save status: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}

	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ix2215",
		"selectedDevice":    "ix2215",
		"lastTransitionAt":  "2026-05-20T10:01:00Z",
	}); err != nil {
		t.Fatalf("save changed status: %v", err)
	}

	select {
	case event := <-ch:
		if fields := event.Attributes["changedFields"]; !strings.Contains(fields, "selectedCandidate") || strings.Contains(fields, "lastTransitionAt") {
			t.Fatalf("changedFields = %q, want selectedCandidate without lastTransitionAt", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for semantic status change event")
	}
}

func TestEventedStoreDoesNotPublishMobilityTimestampOnlyStatusRefresh(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"plannerPhase":        "Planned",
			"phase":               "Projected",
			"dynamicDigest":       "sha256:abc",
			"generatedClaims":     1,
			"generatedActions":    2,
			"placementActive":     false,
			"plannedAt":           "2026-06-01T10:00:00Z",
			"projectedAt":         "2026-06-01T10:00:00Z",
			"dynamicExpiresAt":    "2026-06-01T10:05:00Z",
			"discoveryLastScanAt": "2026-06-01T10:00:00Z",
			"lastEventAt":         "2026-06-01T10:00:00Z",
			"lastPacketAt":        "2026-06-01T10:00:00Z",
			"lastScanAt":          "2026-06-01T10:00:00Z",
			"packetsSeen":         10,
			"scanCount":           20,
			"probeCount":          30,
			"probeHitCount":       40,
			"proactiveCount":      50,
			"observedCount":       60,
			"operatorIntent":      "MobilityPool",
			"derivedConfigKinds":  []string{"AddressMobilityDomain", "RemoteAddressClaim"},
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":        "Planned",
		"phase":               "Projected",
		"dynamicDigest":       "sha256:abc",
		"generatedClaims":     1,
		"generatedActions":    2,
		"placementActive":     false,
		"plannedAt":           "2026-06-01T10:00:30Z",
		"projectedAt":         "2026-06-01T10:00:30Z",
		"dynamicExpiresAt":    "2026-06-01T10:05:30Z",
		"discoveryLastScanAt": "2026-06-01T10:00:30Z",
		"lastEventAt":         "2026-06-01T10:00:30Z",
		"lastPacketAt":        "2026-06-01T10:00:30Z",
		"lastScanAt":          "2026-06-01T10:00:30Z",
		"packetsSeen":         11,
		"scanCount":           21,
		"probeCount":          31,
		"probeHitCount":       41,
		"proactiveCount":      51,
		"observedCount":       61,
		"operatorIntent":      "MobilityPool",
		"derivedConfigKinds":  []string{"AddressMobilityDomain", "RemoteAddressClaim"},
	}); err != nil {
		t.Fatalf("save mobility status refresh: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected mobility timestamp-only event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	if got := base.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")["plannedAt"]; got != "2026-06-01T10:00:30Z" {
		t.Fatalf("plannedAt was not persisted: %v", got)
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":       "Planned",
		"phase":              "Projected",
		"dynamicDigest":      "sha256:def",
		"generatedClaims":    1,
		"generatedActions":   3,
		"placementActive":    false,
		"plannedAt":          "2026-06-01T10:01:00Z",
		"projectedAt":        "2026-06-01T10:01:00Z",
		"dynamicExpiresAt":   "2026-06-01T10:06:00Z",
		"operatorIntent":     "MobilityPool",
		"derivedConfigKinds": []string{"AddressMobilityDomain", "RemoteAddressClaim"},
	}); err != nil {
		t.Fatalf("save semantic mobility status: %v", err)
	}

	select {
	case event := <-ch:
		fields := event.Attributes["changedFields"]
		if !strings.Contains(fields, "dynamicDigest") || !strings.Contains(fields, "generatedActions") {
			t.Fatalf("changedFields = %q, want semantic fields", fields)
		}
		for _, volatile := range []string{"plannedAt", "projectedAt", "dynamicExpiresAt", "discoveryLastScanAt", "lastEventAt", "lastPacketAt", "lastScanAt", "packetsSeen", "scanCount", "probeCount", "probeHitCount", "proactiveCount", "observedCount"} {
			if strings.Contains(fields, volatile) {
				t.Fatalf("changedFields = %q, should omit volatile %s", fields, volatile)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for semantic mobility status event")
	}
}

func TestEventedStoreDoesNotPublishIPAddressSetTTLOnlyStatusRefresh(t *testing.T) {
	base := mapStore{
		api.NetAPIVersion + "/IPAddressSet/public-dns": statusWithOwnership(api.NetAPIVersion, "IPAddressSet", map[string]any{
			"phase":         "Applied",
			"addresses":     []any{"1.1.1.1", "8.8.8.8"},
			"minTTLSeconds": 300,
			"nextRefreshAt": "2026-07-08T00:05:00Z",
			"resolvedAt":    "2026-07-08T00:00:00Z",
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet", Name: "public-dns"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", "public-dns", map[string]any{
		"phase":         "Applied",
		"addresses":     []any{"1.1.1.1", "8.8.8.8"},
		"minTTLSeconds": 120,
		"nextRefreshAt": "2026-07-08T00:06:00Z",
		"resolvedAt":    "2026-07-08T00:01:00Z",
	}); err != nil {
		t.Fatalf("save ttl-only ip address set status: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected IPAddressSet TTL-only event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	if got := base.ObjectStatus(api.NetAPIVersion, "IPAddressSet", "public-dns")["minTTLSeconds"]; got != 120 {
		t.Fatalf("minTTLSeconds was not persisted: %v", got)
	}

	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", "public-dns", map[string]any{
		"phase":         "Applied",
		"addresses":     []any{"1.1.1.1", "8.8.8.8", "9.9.9.9"},
		"minTTLSeconds": 120,
		"nextRefreshAt": "2026-07-08T00:07:00Z",
		"resolvedAt":    "2026-07-08T00:02:00Z",
	}); err != nil {
		t.Fatalf("save address change: %v", err)
	}

	select {
	case event := <-ch:
		fields := event.Attributes["changedFields"]
		if !strings.Contains(fields, "addresses") {
			t.Fatalf("changedFields = %q, want addresses", fields)
		}
		if strings.Contains(fields, "minTTLSeconds") || strings.Contains(fields, "nextRefreshAt") || strings.Contains(fields, "resolvedAt") {
			t.Fatalf("changedFields = %q, should omit volatile TTL fields", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IPAddressSet address change event")
	}
}

func TestMobilityStatusEventComparisonKeepsBehavioralFields(t *testing.T) {
	current := map[string]any{
		"phase":               "Projected",
		"plannerPhase":        "Planned",
		"owner":               "mobility",
		"holder":              "azure-router-a",
		"captureStatus":       "Captured",
		"captureActive":       true,
		"allowReassignment":   false,
		"deliveryRoute":       "Installed",
		"generatedActions":    1,
		"plannedAt":           "2026-06-01T10:00:00Z",
		"projectedAt":         "2026-06-01T10:00:00Z",
		"dynamicExpiresAt":    "2026-06-01T10:05:00Z",
		"streamMaxObservedAt": "2026-06-01T10:00:00Z",
	}
	for _, field := range []string{
		"phase",
		"plannerPhase",
		"owner",
		"holder",
		"captureStatus",
		"captureActive",
		"allowReassignment",
		"deliveryRoute",
		"generatedActions",
		"streamMaxObservedAt",
	} {
		next := map[string]any{}
		for key, value := range current {
			next[key] = value
		}
		next[field] = changedMobilityStatusValue(current[field])
		if !statusChangedForEvent(api.MobilityAPIVersion, "MobilityPool", current, next) {
			t.Fatalf("mobility status field %s must remain event-significant", field)
		}
	}

	next := map[string]any{}
	for key, value := range current {
		next[key] = value
	}
	next["plannedAt"] = "2026-06-01T10:00:30Z"
	next["projectedAt"] = "2026-06-01T10:00:30Z"
	next["dynamicExpiresAt"] = "2026-06-01T10:05:30Z"
	next["discoveryLastScanAt"] = "2026-06-01T10:00:30Z"
	next["lastEventAt"] = "2026-06-01T10:00:30Z"
	next["lastPacketAt"] = "2026-06-01T10:00:30Z"
	next["lastScanAt"] = "2026-06-01T10:00:30Z"
	next["packetsSeen"] = 11
	next["scanCount"] = 21
	next["probeCount"] = 31
	next["probeHitCount"] = 41
	next["proactiveCount"] = 51
	next["observedCount"] = 61
	if statusChangedForEvent(api.MobilityAPIVersion, "MobilityPool", current, next) {
		t.Fatalf("mobility scan-only refresh should not be event-significant")
	}
}

func TestMobilityPoolObservationRefreshDoesNotPublishStatusEvent(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"phase":                         "Ready",
			"observedClients":               `[{"ip":"192.168.123.129","mac":"02:00:00:00:01:29"}]`,
			"observedClientsBySource":       map[string]any{"pve-svnet": map[string]any{"observedClients": `[{"ip":"192.168.123.129"}]`}},
			"ownershipResolverAddressCount": 1,
			"ownershipResolverDecisions":    []map[string]any{{"address": "192.168.123.129/32", "owner": "pve-rt08"}},
			"bgpCaptureObservedAt":          "2026-07-07T00:48:00Z",
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.MergeObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                   "Watching",
		"observedClients":         `[{"ip":"192.168.123.129","mac":"02:00:00:00:01:29"},{"ip":"192.168.123.132","mac":"02:00:00:00:01:32"}]`,
		"observedClientsBySource": map[string]any{"pve-svnet": map[string]any{"observedClients": `[{"ip":"192.168.123.129"},{"ip":"192.168.123.132"}]`}},
		"bgpCaptureObservedAt":    "2026-07-07T00:49:00Z",
	}); err != nil {
		t.Fatalf("merge observation refresh: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected observation refresh event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	status := base.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Watching" || !strings.Contains(fmt.Sprint(status["observedClients"]), "192.168.123.132") {
		t.Fatalf("status was not persisted: %#v", status)
	}
}

func TestMobilityPoolPendingWatchingObservationRefreshDoesNotPublishStatusEvent(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"phase":                         "Pending",
			"observedClients":               `[]`,
			"observedClientsBySource":       map[string]any{},
			"sourceType":                    "onprem-l2",
			"ownershipResolverAddressCount": 1,
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.MergeObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                   "Watching",
		"observedClients":         `[{"ip":"192.168.123.1","mac":"02:00:00:00:00:01"}]`,
		"observedClientsBySource": map[string]string{"pve-svnet": `[{"ip":"192.168.123.1"}]`},
		"sourceType":              "pve-svnet",
	}); err != nil {
		t.Fatalf("merge pending/watching observation refresh: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected pending/watching observation refresh event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMobilityPoolOwnershipResolverChangePublishesStatusEvent(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"phase":                         "Ready",
			"ownershipResolverAddressCount": 1,
			"ownershipResolverDecisions":    []map[string]any{{"address": "192.168.123.129/32", "owner": "pve-rt08"}},
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.MergeObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                         "Ready",
		"ownershipResolverAddressCount": 2,
		"ownershipResolverDecisions": []map[string]any{
			{"address": "192.168.123.129/32", "owner": "pve-rt08"},
			{"address": "192.168.123.132/32", "owner": "pve-rt07"},
		},
	}); err != nil {
		t.Fatalf("merge ownership resolver status: %v", err)
	}

	select {
	case event := <-ch:
		fields := event.Attributes["changedFields"]
		if !strings.Contains(fields, "ownershipResolverAddressCount") || !strings.Contains(fields, "ownershipResolverDecisions") {
			t.Fatalf("changedFields = %q, want ownership resolver fields", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ownership resolver status event")
	}
}

func TestMobilityPoolPendingWatchingSemanticStatusPublishesEvent(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"phase":        "Pending",
			"plannerPhase": "Planned",
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.MergeObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":         "Watching",
		"plannerPhase":  "Degraded",
		"plannerReason": "ownership resolver conflict",
	}); err != nil {
		t.Fatalf("merge pending/watching semantic status: %v", err)
	}

	select {
	case event := <-ch:
		fields := event.Attributes["changedFields"]
		if !strings.Contains(fields, "plannerReason") {
			t.Fatalf("changedFields = %q, want plannerReason", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pending/watching semantic status event")
	}
}

func TestMobilityPoolLeaseRefreshAndNestedMapTypesDoNotPublishStatusEvent(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"phase": "Ready",
			"addresses": map[string]any{
				"192.168.123.129/32": map[string]any{
					"phase":            "Ready",
					"conditions":       map[string]string{"OwnershipResolved": "True"},
					"conditionReasons": map[string]string{"OwnershipResolved": "LocalHomeOwned"},
				},
			},
			"bgpCaptureClaim": map[string]any{
				"phase":         "Active",
				"generation":    "cloudedge/1",
				"desiredHolder": "pve-rt08",
				"renewedAt":     "2026-07-07T01:13:07Z",
				"leaseUntil":    "2026-07-07T01:18:07Z",
			},
			"observedSelfStaleCaptures": map[string]string{},
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.MergeObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase": "Ready",
		"addresses": map[string]any{
			"192.168.123.129/32": map[string]any{
				"phase":            "Ready",
				"conditions":       map[string]any{"OwnershipResolved": "True"},
				"conditionReasons": map[string]any{"OwnershipResolved": "LocalHomeOwned"},
			},
		},
		"bgpCaptureClaim": map[string]any{
			"phase":         "Active",
			"generation":    "cloudedge/1",
			"desiredHolder": "pve-rt08",
			"renewedAt":     "2026-07-07T01:13:09Z",
			"leaseUntil":    "2026-07-07T01:18:09Z",
		},
		"observedSelfStaleCaptures": map[string]any{},
	}); err != nil {
		t.Fatalf("merge lease refresh status: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected lease refresh event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMobilityPoolSemanticStatusStillPublishesEvent(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"phase":                         "Ready",
			"plannerPhase":                  "Planned",
			"ownershipResolverAddressCount": 1,
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.MergeObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                         "Degraded",
		"plannerPhase":                  "Degraded",
		"plannerReason":                 "ownership resolver conflict",
		"ownershipResolverAddressCount": 1,
	}); err != nil {
		t.Fatalf("merge semantic status: %v", err)
	}

	select {
	case event := <-ch:
		fields := event.Attributes["changedFields"]
		if !strings.Contains(fields, "plannerPhase") || !strings.Contains(fields, "plannerReason") {
			t.Fatalf("changedFields = %q, want planner semantic fields", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for semantic status event")
	}
}

func changedMobilityStatusValue(value any) any {
	switch typed := value.(type) {
	case bool:
		return !typed
	case int:
		return typed + 1
	case string:
		return typed + "-changed"
	default:
		return "changed"
	}
}

func TestStatusChangedIgnoresRuntimeTelemetry(t *testing.T) {
	current := map[string]any{
		"phase":               "Connected",
		"publicKey":           "peer-key",
		"allowedIPs":          []any{"10.99.0.2/32"},
		"latestHandshake":     "2026-05-13T06:30:00Z",
		"handshakeAgeSeconds": float64(12),
		"transferRxBytes":     float64(1000),
		"transferTxBytes":     float64(2000),
	}
	next := map[string]any{
		"phase":               "Connected",
		"publicKey":           "peer-key",
		"allowedIPs":          []string{"10.99.0.2/32"},
		"latestHandshake":     "2026-05-13T06:31:00Z",
		"handshakeAgeSeconds": 1,
		"transferRxBytes":     uint64(3000),
		"transferTxBytes":     uint64(4000),
	}
	if statusChanged(current, next) {
		t.Fatalf("runtime telemetry-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedTreatsNilStringSlicesAsNull(t *testing.T) {
	current := map[string]any{
		"phase":            "Active",
		"destinationCIDRs": nil,
		"skipped":          nil,
	}
	next := map[string]any{
		"phase":            "Active",
		"destinationCIDRs": []string(nil),
		"skipped":          []string(nil),
	}
	if statusChanged(current, next) {
		t.Fatalf("nil string slice should be stable against stored null")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresRenderDiagnostics(t *testing.T) {
	current := map[string]any{
		"phase":         "Applied",
		"backend":       "nftables",
		"internalHoles": float64(10),
	}
	next := map[string]any{
		"phase":         "Applied",
		"backend":       "nftables",
		"internalHoles": 11,
	}
	if statusChanged(current, next) {
		t.Fatalf("render diagnostic-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresNestedTrackCounters(t *testing.T) {
	current := map[string]any{
		"phase": "Applied",
		"track": []any{map[string]any{
			"resource":       "BGPRouter/lab",
			"healthy":        true,
			"healthyCount":   float64(10),
			"unhealthyCount": float64(0),
		}},
	}
	next := map[string]any{
		"phase": "Applied",
		"track": []map[string]any{{
			"resource":       "BGPRouter/lab",
			"healthy":        true,
			"healthyCount":   11,
			"unhealthyCount": 0,
		}},
	}
	if statusChanged(current, next) {
		t.Fatalf("nested track counter-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedNormalizesStructSlices(t *testing.T) {
	type backend struct {
		Name            string `json:"name"`
		ResolvedAddress string `json:"resolvedAddress"`
		Port            int    `json:"port"`
		Healthy         bool   `json:"healthy"`
	}
	current := map[string]any{
		"phase": "Active",
		"backends": []any{map[string]any{
			"name":            "router06-ssh",
			"resolvedAddress": "192.168.123.111",
			"port":            float64(22),
			"healthy":         true,
		}},
	}
	next := map[string]any{
		"phase": "Active",
		"backends": []backend{{
			Name:            "router06-ssh",
			ResolvedAddress: "192.168.123.111",
			Port:            22,
			Healthy:         true,
		}},
	}
	if statusChanged(current, next) {
		t.Fatalf("struct slice equivalent to stored map slice should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresPeerDetailTelemetry(t *testing.T) {
	current := map[string]any{
		"phase":           "Running",
		"backendState":    "Running",
		"onlinePeerCount": 2,
		"peers": []map[string]any{{
			"id":       "peer-a",
			"online":   true,
			"lastSeen": "2026-05-13T06:30:00Z",
			"rxBytes":  100,
			"txBytes":  200,
		}},
	}
	next := map[string]any{
		"phase":           "Running",
		"backendState":    "Running",
		"onlinePeerCount": 2,
		"peers": []map[string]any{{
			"id":       "peer-a",
			"online":   true,
			"lastSeen": "2026-05-13T06:31:00Z",
			"rxBytes":  300,
			"txBytes":  400,
		}},
	}
	if statusChanged(current, next) {
		t.Fatalf("peer detail telemetry-only update should not be a resource status change")
	}
	next["onlinePeerCount"] = 1
	fields := statusChangedFields(current, next)
	if len(fields) != 1 || fields[0] != "onlinePeerCount" {
		t.Fatalf("changed fields = %v, want [onlinePeerCount]", fields)
	}
}

func TestStatusChangedFieldsReportsMeaningfulChanges(t *testing.T) {
	current := map[string]any{
		"phase":             "Applied",
		"selectedDevice":    "ds-lite",
		"previousNoise":     "same",
		"updatedAt":         "2026-05-05T00:00:00Z",
		"consecutivePassed": 1,
	}
	next := map[string]any{
		"phase":             "Applied",
		"selectedDevice":    "ix2215",
		"previousNoise":     "same",
		"updatedAt":         "2026-05-05T00:00:30Z",
		"consecutivePassed": 2,
	}
	fields := statusChangedFields(current, next)
	if len(fields) != 1 || fields[0] != "selectedDevice" {
		t.Fatalf("changed fields = %v, want [selectedDevice]", fields)
	}
}
