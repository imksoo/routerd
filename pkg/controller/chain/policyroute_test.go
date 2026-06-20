// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resource"
)

func TestEgressRoutePolicyFiltersUnhealthyTargets(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "dslite"}, Spec: api.EgressRoutePolicySpec{
			Mode:             "hash",
			HashFields:       []string{"sourceAddress"},
			SourceCIDRs:      []string{"172.18.0.0/16"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name: "dslite",
				Targets: []api.EgressRoutePolicyTarget{
					{Name: "a", Interface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110, HealthCheck: "hc-a"},
					{Name: "b", Interface: "ds-lite-b", Table: 111, Priority: 10111, Mark: 0x111, HealthCheck: "hc-b"},
					{Name: "c", Interface: "ds-lite-c", Table: 112, Priority: 10112, Mark: 0x112, HealthCheck: "hc-c"},
				},
			}},
		}},
	}}}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/hc-a": {"phase": "Healthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/HealthCheck/hc-b": {"phase": "Unhealthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/HealthCheck/hc-c": {"phase": "Unhealthy", "lastCheckedAt": now},
	}
	controller := IPv4PolicyRouteController{Router: router, Store: store}
	data, err := render.NftablesIPv4PolicyRoutes(controller.effectivePolicyRouteRouter(map[string]bool{}))
	if err != nil {
		t.Fatalf("render policy routes: %v", err)
	}
	got := string(data)
	for _, want := range []string{"mod 1 map { 0 : 0x110 }", "ct mark 0x0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"0x00000111", "0x00000112"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("nftables output contains unhealthy mark %q:\n%s", notWant, got)
		}
	}
}

func TestIPv4PolicyRouteKeepsCandidateDuringTransientFailing(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.HealthCheckSpec{Interval: "30s", Timeout: "3s", UnhealthyThreshold: 3}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":             "Failing",
			"consecutiveFailed": 1,
			"lastCheckedAt":     time.Now().UTC().Format(time.RFC3339Nano),
		},
	}}
	if !controller.targetHealthy("internet") {
		t.Fatal("single transient failing probe should not remove a policy route candidate")
	}
	controller.Store = mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":             "Failing",
			"consecutiveFailed": 3,
			"lastCheckedAt":     time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if controller.targetHealthy("internet") {
		t.Fatal("failing probe at unhealthy threshold should remove a policy route candidate")
	}
	controller.Store = mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":         "Unhealthy",
			"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if controller.targetHealthy("internet") {
		t.Fatal("unhealthy healthcheck should remove a policy route candidate")
	}
}

func TestIPv4PolicyRouteUsesObservedHealthCheckStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.HealthCheckSpec{Interval: "30s", Timeout: "3s"}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":         "Healthy",
			"lastCheckedAt": time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano),
			"observed": map[string]any{
				"phase":         "Healthy",
				"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
			},
		},
	}}
	if !controller.targetHealthy("internet") {
		t.Fatal("fresh observed healthcheck status should keep a policy route candidate ready")
	}
}

func TestIPv4PolicyRouteInstallsFwmarkBootstrapRouteForHealthCheck(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-via-hgw": {
			"phase":         "Unhealthy",
			"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet-via-hgw"}, Spec: api.HealthCheckSpec{
			Target: "1.1.1.1",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name:          "hgw",
				Interface:     "wan",
				GatewaySource: "static",
				Gateway:       "192.168.1.1",
				Table:         116,
				Priority:      40,
				Mark:          0x116,
				HealthCheck:   "internet-via-hgw",
			}},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: store, DryRun: true}
	if err := controller.applyRouteTables(t.Context(), map[string]string{"wan": "lo"}); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "hgw"); len(status) != 0 {
		t.Fatalf("route target should not create phantom EgressRoutePolicy status: %#v", status)
	}

	enabled := false
	router.Spec.Resources[0].Spec = api.HealthCheckSpec{Target: "1.1.1.1", Enabled: &enabled}
	store = mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-via-hgw": {
			"phase":         "Disabled",
			"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	controller = IPv4PolicyRouteController{Router: router, Store: store, DryRun: true}
	if err := controller.applyRouteTables(t.Context(), map[string]string{"wan": "lo"}); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "hgw"); len(status) != 0 {
		t.Fatalf("disabled healthcheck should not bootstrap route: %#v", status)
	}
}

func TestEgressRoutePolicyTargetCandidateRendersOnlyWhenActive(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "lan-default"}, Spec: api.EgressRoutePolicySpec{
			Mode:             "priority",
			HashFields:       []string{"sourceAddress"},
			SourceCIDRs:      []string{"172.18.0.0/16"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "dslite", Priority: 10, Targets: []api.EgressRoutePolicyTarget{
					{Name: "a", Interface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110},
				}},
				{Name: "fallback", Interface: "lan", Priority: 20, Mark: 0x114},
			},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: mapStore{}}
	inactive, err := render.NftablesIPv4PolicyRoutes(controller.effectivePolicyRouteRouter(map[string]bool{}))
	if err != nil {
		t.Fatalf("render inactive policy routes: %v", err)
	}
	if strings.Contains(string(inactive), "0x110") {
		t.Fatalf("inactive target candidate should not render marks:\n%s", inactive)
	}
	active, err := render.NftablesIPv4PolicyRoutes(controller.effectivePolicyRouteRouter(map[string]bool{"lan-default/dslite": true}))
	if err != nil {
		t.Fatalf("render active policy routes: %v", err)
	}
	if !strings.Contains(string(active), "0x110") {
		t.Fatalf("active target candidate should render marks:\n%s", active)
	}
}

func TestIPv4PolicyRouteSkipsSelectionOnlyPolicy(t *testing.T) {
	store := mapStore{}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name:      "wan",
				Interface: "wan",
				Priority:  10,
				Mark:      0x110,
				Table:     110,
			}},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: store, DryRun: true}

	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default"); len(status) != 0 {
		t.Fatalf("mode-omitted policy should be owned by egressroute controller, got status %#v", status)
	}
}

func TestIPv4PolicyRouteOwnsPriorityPolicyWithoutChurn(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	base := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-a": {"phase": "Healthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/HealthCheck/internet-b": {"phase": "Healthy", "lastCheckedAt": now},
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy", Name: "ipv4-default"}
	statusCh, cancelStatus := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 4)
	defer cancelStatus()
	routeCh, cancelRoute := eventBus.Subscribe(context.Background(), bus.Subscription{Topics: []string{"routerd.lan.route.changed"}}, 1)
	defer cancelRoute()

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode:        "priority",
			HashFields:  []string{"sourceAddress"},
			SourceCIDRs: []string{"192.0.2.0/24"},
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "dslite-pd-balanced", Priority: 10, HealthCheck: "internet-a", Targets: []api.EgressRoutePolicyTarget{
					{Name: "ds-lite-a", Interface: "wan-a", Priority: 10110, Mark: 0x110, Table: 110},
					{Name: "ds-lite-b", Interface: "wan-b", Priority: 10111, Mark: 0x111, Table: 111},
				}},
				{Name: "ds-lite-ra", Interface: "wan-a", Priority: 20, Mark: 0x112, Table: 112, HealthCheck: "internet-b"},
			},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: eventedStore{Store: base, Bus: eventBus}, Bus: eventBus, DryRun: true}

	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := base.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != "Applied" || status["selectedCandidate"] != "dslite-pd-balanced" || status["dryRun"] != true {
		t.Fatalf("priority policy status = %#v", status)
	}
	drainEvents(statusCh)

	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-statusCh:
		t.Fatalf("unchanged priority policy should not publish status churn: %#v", event)
	case event := <-routeCh:
		t.Fatalf("priority policy should not publish legacy route changed event: %#v", event)
	case <-time.After(40 * time.Millisecond):
	}
}

func TestIPv4PolicyRoutePriorityDryRunDoesNotChurnUnchangedFallback(t *testing.T) {
	eventBus := bus.New()
	base := mapStore{
		api.NetAPIVersion + "/Interface/ix2215": {"phase": "Up", "ifname": "lo"},
	}
	resource := daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy", Name: "ipv4-default"}
	statusCh, cancelStatus := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 4)
	defer cancelStatus()
	routeCh, cancelRoute := eventBus.Subscribe(context.Background(), bus.Subscription{Topics: []string{"routerd.lan.route.changed"}}, 1)
	defer cancelRoute()

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ix2215"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name:          "ix2215-fallback",
				DeviceFrom:    api.StatusValueSourceSpec{Resource: "Interface/ix2215", Field: "ifname"},
				GatewaySource: "static",
				Gateway:       "192.168.1.1",
				Table:         116,
				Metric:        50,
				Priority:      10116,
				Mark:          0x116,
				Weight:        10,
			}},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: eventedStore{Store: base, Bus: eventBus}, Bus: eventBus, DryRun: true}

	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := base.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "ix2215-fallback" || status["selectedDevice"] != "lo" || status["selectedGateway"] != "192.168.1.1" || status["dryRun"] != true {
		t.Fatalf("priority fallback status = %#v", status)
	}
	drainEvents(statusCh)

	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-statusCh:
		t.Fatalf("unchanged priority dry-run policy should not publish status churn: %#v", event)
	case event := <-routeCh:
		t.Fatalf("priority dry-run policy should not publish legacy route changed event: %#v", event)
	case <-time.After(40 * time.Millisecond):
	}
}

func TestIPv4PolicyRoutePrioritySelectionUsesWeightThenPriority(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/primary":  {"phase": "Healthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/HealthCheck/fallback": {"phase": "Healthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/Interface/wan-b":      {"phase": "Up", "ifname": "lo"},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode:      "priority",
			Selection: "highest-weight-ready",
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "primary", Interface: "wan-a", Priority: 10, Mark: 0x110, Table: 110, Weight: 100, HealthCheck: "primary"},
				{Name: "fallback", DeviceFrom: api.StatusValueSourceSpec{Resource: "Interface/wan-b", Field: "ifname"}, Priority: 20, Mark: 0x111, Table: 111, Weight: 200, HealthCheck: "fallback", GatewaySource: "static", Gateway: "192.0.2.1"},
			},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: store, DryRun: true}
	if err := controller.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "default.nft")); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "fallback" || status["selectedDevice"] != "lo" || status["selectedGateway"] != "192.0.2.1" || status["selectedWeight"] != 200 {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4PolicyRoutePrioritySelectionSkipsDisabled(t *testing.T) {
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "disabled", Interface: "wan-a", Priority: 10, Mark: 0x110, Table: 110, Weight: 300, Enabled: &enabled},
				{Name: "enabled", Interface: "wan-b", Priority: 20, Mark: 0x111, Table: 111, Weight: 100},
			},
		}},
	}}}
	store := mapStore{}
	controller := IPv4PolicyRouteController{Router: router, Store: store, DryRun: true}
	if err := controller.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "default.nft")); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "enabled" {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4PolicyRoutePriorityReportsUnsupportedSelection(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode:      "priority",
			Selection: "weighted-ecmp",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name: "wan", Weight: 1,
			}},
		}},
	}}}
	store := mapStore{}
	controller := IPv4PolicyRouteController{Router: router, Store: store, DryRun: true}
	if err := controller.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "default.nft")); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != "Pending" || status["reason"] != "UnsupportedSelection" {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4PolicyRouteCleansOnlyLedgerOwnedStaleRulesAndTables(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "artifacts.json")
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{
		{
			Kind:  "linux.ipv4.fwmarkRule",
			Name:  "priority=10110,mark=0x110,table=110",
			Owner: api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default",
			Attributes: map[string]string{
				"priority": "10110",
				"mark":     "0x110",
				"table":    "110",
			},
		},
		{
			Kind:       "linux.ipv4.routeTable",
			Name:       "table=110",
			Owner:      api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default",
			Attributes: map[string]string{"table": "110"},
		},
		{
			Kind:  "linux.ipv4.fwmarkRule",
			Name:  "priority=10111,mark=0x111,table=111",
			Owner: api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default",
			Attributes: map[string]string{
				"priority": "10111",
				"mark":     "0x111",
				"table":    "111",
			},
		},
		{
			Kind:       "linux.ipv4.routeTable",
			Name:       "table=111",
			Owner:      api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default",
			Attributes: map[string]string{"table": "111"},
		},
	})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name: "dslite",
				Targets: []api.EgressRoutePolicyTarget{{
					Name: "ds-lite-b", Interface: "wan-b", Priority: 10111, Mark: 0x111, Table: 111,
				}},
			}},
		}},
	}}}
	var commands []string
	controller := IPv4PolicyRouteController{
		Router:     router,
		Store:      mapStore{},
		LedgerPath: ledgerPath,
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			switch command {
			case "ip -4 rule show":
				return []byte("10110: from all fwmark 0x110 lookup 110\n10111: from all fwmark 0x111 lookup 111\n100: from all fwmark 0x999 lookup 999\n"), nil
			case "ip -4 route show table all":
				return []byte("default dev old table 110\ndefault dev lo table 111\ndefault dev manual table 999\n"), nil
			default:
				return []byte(""), nil
			}
		},
	}
	if err := controller.cleanupLedgerOwnedPolicyRoutes(t.Context(), map[string]string{"wan-b": "lo"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		"ip -4 rule del priority 10110 fwmark 0x110 table 110",
		"ip -4 route flush table 110",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commands =\n%s\nwant %s", joined, want)
		}
	}
	for _, notWant := range []string{
		"priority 10111",
		"table 111",
		"0x999",
		"table 999",
	} {
		if strings.Contains(joined, notWant) {
			t.Fatalf("commands =\n%s\nshould not contain %s", joined, notWant)
		}
	}
	loaded, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loaded.Close() }()
	if loaded.Owns(resource.Artifact{Kind: "linux.ipv4.fwmarkRule", Name: "priority=10110,mark=0x110,table=110"}) {
		t.Fatalf("stale rule remained in ledger: %+v", loaded.All())
	}
	if !loaded.Owns(resource.Artifact{Kind: "linux.ipv4.fwmarkRule", Name: "priority=10111,mark=0x111,table=111"}) {
		t.Fatalf("desired rule missing from ledger: %+v", loaded.All())
	}
}

func drainEvents(ch <-chan daemonapi.DaemonEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func TestIPv4PolicyRouteHealthCheckRequiresFreshStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.HealthCheckSpec{Interval: "30s", Timeout: "3s"}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":         "Healthy",
			"lastCheckedAt": time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano),
		},
	}}
	if controller.targetHealthy("internet") {
		t.Fatal("stale healthcheck status should not be treated as healthy")
	}
	controller.Store = mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":         "Healthy",
			"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if !controller.targetHealthy("internet") {
		t.Fatal("fresh healthy status should be treated as healthy")
	}
}

func TestIPv4PolicyRouteApplyNftTableSkipsUnchangedExistingTable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	nftPath := filepath.Join(dir, "nft")
	tablePath := filepath.Join(dir, "policy.nft")
	data := []byte("table ip routerd_policy {}\n")
	if err := os.WriteFile(tablePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + testShellQuote(logPath) + "\n" +
		"exit 0\n"
	if err := os.WriteFile(nftPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	controller := IPv4PolicyRouteController{}
	if err := controller.applyNftTable(context.Background(), nftPath, tablePath, "ip", "routerd_policy", data); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logData)
	for _, want := range []string{
		"list table ip routerd_policy",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nft command log missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"-c -f " + tablePath,
		"-f " + tablePath,
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unchanged existing table should not run %q:\n%s", unwanted, got)
		}
	}
}

func testShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestIPv4PolicyRouteGatewayResolution(t *testing.T) {
	controller := IPv4PolicyRouteController{}
	ctx := context.Background()
	if gateway, err := controller.routeGateway(ctx, "wan0", "none", ""); err != nil || gateway != "" {
		t.Fatalf("none gateway = %q err=%v, want empty nil", gateway, err)
	}
	if gateway, err := controller.routeGateway(ctx, "wan0", "static", "192.0.2.1"); err != nil || gateway != "192.0.2.1" {
		t.Fatalf("static gateway = %q err=%v", gateway, err)
	}
	if gateway, err := controller.routeGateway(ctx, "wan0", "dhcpv4", "192.0.2.1"); err != nil || gateway != "192.0.2.1" {
		t.Fatalf("dhcpv4 pre-resolved gateway = %q err=%v", gateway, err)
	}
	if _, err := controller.routeGateway(ctx, "wan0", "static", ""); err == nil {
		t.Fatal("empty static gateway should be rejected")
	}
}

// TestCleanupLedgerOwnedPolicyRoutesDoesNotLeakFDs is the controller-level
// regression test for issue #39. routerd serve drives the cleanup at 30s
// reconcile, so a leaked *sql.DB per call would accumulate hundreds of
// fds/day against routerd.db. Linux-only because it inspects /proc/self/fd.
func TestCleanupLedgerOwnedPolicyRoutesDoesNotLeakFDs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/self/fd only available on linux")
	}
	dir := t.TempDir()
	// Use a routerd.db path so LoadLedger picks the SQLite backend (the
	// JSON backend has no fd lifetime concern).
	ledgerPath := filepath.Join(dir, "routerd.db")
	seed, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name: "dslite",
				Targets: []api.EgressRoutePolicyTarget{{
					Name: "ds-lite-b", Interface: "wan-b", Priority: 10111, Mark: 0x111, Table: 111,
				}},
			}},
		}},
	}}}
	controller := IPv4PolicyRouteController{
		Router:     router,
		Store:      mapStore{},
		LedgerPath: ledgerPath,
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch name + " " + strings.Join(args, " ") {
			case "ip -4 rule show":
				return []byte(""), nil
			case "ip -4 route show table all":
				return []byte(""), nil
			default:
				return []byte(""), nil
			}
		},
	}
	// Warm up: settle any first-call fd churn (e.g. table creation).
	if err := controller.cleanupLedgerOwnedPolicyRoutes(t.Context(), map[string]string{"wan-b": "lo"}); err != nil {
		t.Fatalf("warmup cleanup: %v", err)
	}
	base := countLedgerFDs(t, ledgerPath)
	for i := 0; i < 10; i++ {
		if err := controller.cleanupLedgerOwnedPolicyRoutes(t.Context(), map[string]string{"wan-b": "lo"}); err != nil {
			t.Fatalf("iter %d cleanup: %v", i, err)
		}
	}
	after := countLedgerFDs(t, ledgerPath)
	if after > base {
		t.Fatalf("fd leak across 10 cleanup reconciles: before=%d after=%d", base, after)
	}
}

func countLedgerFDs(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	suffixes := []string{"", "-journal", "-wal", "-shm"}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%s", entry.Name()))
		if err != nil {
			continue
		}
		for _, suffix := range suffixes {
			if strings.HasSuffix(target, path+suffix) {
				count++
				break
			}
		}
	}
	return count
}
