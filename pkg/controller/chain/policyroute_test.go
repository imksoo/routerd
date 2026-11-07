// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
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
	for _, want := range []string{"mod 256 seed 0x73d6bcca map { 0 : 0x110", "ct mark 0x0"} {
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

func TestIPv4PolicyRouteRejectsDSLiteTargetUntilTunnelIsUp(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: mapStore{}}
	if controller.dsliteResourceReady("ds-lite-a") {
		t.Fatal("DS-Lite target without an Up status must not be used")
	}
	controller.Store = mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-a": {"phase": "Up"},
	}
	if !controller.dsliteResourceReady("DSLiteTunnel/ds-lite-a") {
		t.Fatal("DS-Lite target with an Up status must be usable")
	}
	controller.Store = mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-a": {
			"phase":  "Disabled",
			"reason": "WhenFalse",
			"observed": map[string]any{
				"phase": "Up",
			},
		},
	}
	if controller.dsliteResourceReady("DSLiteTunnel/ds-lite-a") {
		t.Fatal("WhenFalse DS-Lite target must not inherit a stale observed Up phase")
	}
}

func TestIPv4PolicyRouteSkipsDSLiteCandidateDuringMasterStartup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	const missing = "routerd-test-missing-dslite"
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-ra": {
			"phase":  "Disabled",
			"reason": "WhenFalse",
			"observed": map[string]any{
				"phase":  "Up",
				"device": missing,
			},
		},
		api.NetAPIVersion + "/HealthCheck/internet-via-dslite-ra": {
			"phase":         "Healthy",
			"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-ra"}, Spec: api.DSLiteTunnelSpec{TunnelName: missing}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet-via-dslite-ra"}, Spec: api.HealthCheckSpec{Interval: "3s", Timeout: "2s"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name:        "ds-lite-ra",
				Source:      "DSLiteTunnel/ds-lite-ra",
				Interface:   missing,
				Table:       113,
				Priority:    10113,
				Mark:        275,
				HealthCheck: "internet-via-dslite-ra",
				DependsOn: []api.ResourceDependencySpec{{
					Resource: "DSLiteTunnel/ds-lite-ra",
					Phase:    "Up",
				}},
			}},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, DryRun: true}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("normal DS-Lite startup gap must be transient: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != "Pending" || status["reason"] != "NoReadyCandidates" {
		t.Fatalf("policy status = %#v, want Pending/NoReadyCandidates", status)
	}
}

func TestEffectivePolicyRouteExcludesWhenFalseDSLiteTargetWithoutMutatingSpec(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	const missing = "routerd-test-stale-dslite"
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-up"}, Spec: api.DSLiteTunnelSpec{TunnelName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-stale"}, Spec: api.DSLiteTunnelSpec{TunnelName: missing}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name: "dslite-balanced",
				Targets: []api.EgressRoutePolicyTarget{
					{Name: "up", Interface: "ds-lite-up", Table: 110, Mark: 272},
					{Name: "stale", Interface: "ds-lite-stale", Table: 111, Mark: 273},
				},
			}},
		}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-up": {"phase": "Up"},
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-stale": {
			"phase":    "Disabled",
			"observed": map[string]any{"phase": "Up"},
		},
	}
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux}
	effective := controller.effectivePolicyRouteRouter(map[string]bool{"ipv4-default/dslite-balanced": true})
	spec, err := effective.Spec.Resources[2].EgressRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Candidates) != 1 || len(spec.Candidates[0].Targets) != 2 || spec.Candidates[0].Targets[0].RuntimeReady == nil || !*spec.Candidates[0].Targets[0].RuntimeReady || spec.Candidates[0].Targets[1].RuntimeReady == nil || *spec.Candidates[0].Targets[1].RuntimeReady {
		t.Fatalf("effective targets = %#v, want declared targets with runtime readiness", spec.Candidates)
	}
	original, err := router.Spec.Resources[2].EgressRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Candidates[0].Targets) != 2 {
		t.Fatalf("effective filtering mutated declared targets: %#v", original.Candidates[0].Targets)
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
	requireLinuxRuntimeFixture(t)
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
	default:
	}
}

func TestIPv4PolicyRoutePriorityDryRunDoesNotChurnUnchangedFallback(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
	default:
	}
}

func TestIPv4PolicyRoutePublishesHADatapathConvergence(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	now := time.Now().UTC()
	transitionAt := now.Add(-2500 * time.Millisecond).Format(time.RFC3339Nano)
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-gw":  {"phase": "Applied", "role": "master", "lastRoleTransitionAt": transitionAt},
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-a": {"phase": "Up"},
		api.NetAPIVersion + "/HealthCheck/internet-a": {"phase": "Unhealthy", "lastCheckedAt": now.Format(time.RFC3339Nano)},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "lan-gw"}, Spec: api.VirtualAddressSpec{Mode: "vrrp"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet-a"}, Spec: api.HealthCheckSpec{Interval: "3s", Timeout: "2s", UnhealthyThreshold: 1}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority", HashFields: []string{"sourceAddress"}, Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "dslite", Weight: 120, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/lan-gw.role": {Equals: "master"}}}, Targets: []api.EgressRoutePolicyTarget{{Name: "a", Interface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110, HealthCheck: "internet-a"}}},
				{Name: "fallback", Weight: 60, Interface: "lo", GatewaySource: "none", Table: 114, Priority: 10114, Mark: 0x114},
			},
		}},
	}}}
	controller := IPv4PolicyRouteController{Router: router, DeclaredRouter: router, Store: store, DryRun: true, OperatingSystem: platform.OSLinux, Now: func() time.Time { return now }}
	if err := controller.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "default.nft")); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "fallback" || status["datapathReady"] != false || status["datapathState"] != "Converging" || status["newFlowBehavior"] != "fallback" || status["datapathFallbackBehavior"] != "select-next-ready-candidate" {
		t.Fatalf("converging datapath status = %#v", status)
	}

	store[api.NetAPIVersion+"/HealthCheck/internet-a"] = map[string]any{"phase": "Healthy", "lastCheckedAt": now.Format(time.RFC3339Nano)}
	if err := controller.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "ready.nft")); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "dslite" || status["datapathReady"] != true || status["datapathState"] != "Ready" || status["datapathReadyDurationMillis"] != int64(2500) || status["datapathRoleResource"] != "VirtualAddress/lan-gw" {
		t.Fatalf("ready datapath status = %#v", status)
	}
	readyAt := status["datapathReadyAt"]
	controller.Now = func() time.Time { return now.Add(5 * time.Second) }
	if err := controller.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "stable.nft")); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["datapathReadyAt"] != readyAt || status["datapathReadyDurationMillis"] != int64(2500) {
		t.Fatalf("stable reconcile changed first-ready measurement: %#v", status)
	}
}

func TestIPv4PolicyRoutePrioritySelectionUsesWeightThenPriority(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
	requireLinuxRuntimeFixture(t)
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
