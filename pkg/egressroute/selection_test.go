// SPDX-License-Identifier: BSD-3-Clause

package egressroute

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestControllerSelectsHighestWeightReady(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/Link/fallback":        {"phase": "Up", "ifname": "wan0"},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "fallback", Source: "Link/fallback", DeviceFrom: api.StatusValueSourceSpec{Resource: "Link/fallback", Field: "ifname"}, Weight: 50},
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"}, Weight: 80},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied {
		t.Fatalf("phase = %v", status["phase"])
	}
	if status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("selectedCandidate = %v", status["selectedCandidate"])
	}
	if status["selectedDevice"] != "ds-routerd-test" {
		t.Fatalf("selectedDevice = %v", status["selectedDevice"])
	}
	if got := fmt.Sprint(status["destinationCIDRs"]); !strings.Contains(got, "0.0.0.0/0") {
		t.Fatalf("destinationCIDRs = %v", status["destinationCIDRs"])
	}
}

func TestControllerSkipsPolicyRouteModes(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up", "interface": "ds-routerd-test"},
	}
	b := bus.New()
	ch, cancel := b.Subscribe(context.Background(), bus.Subscription{Topics: []string{EventRouteChanged}}, 1)
	defer cancel()
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Mode:      "priority",
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name:       "ds-lite",
				Source:     "DSLiteTunnel/ds-lite",
				DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"},
				Weight:     80,
			}},
		}),
		Bus:   b,
		Store: store,
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default"); len(status) != 0 {
		t.Fatalf("mode:priority policy should be owned by policyroute controller, got status %#v", status)
	}
	select {
	case event := <-ch:
		t.Fatalf("mode:priority policy should not publish route changed event: %#v", event)
	default:
	}
}

func TestControllerReportsSelectedGateway(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/Link/ix2215": {"phase": "Up", "ifname": "ens19"},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			DestinationCIDRs: []string{"203.0.113.0/24"},
			Selection:        SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "ix2215", Source: "Link/ix2215", DeviceFrom: api.StatusValueSourceSpec{Resource: "Link/ix2215", Field: "ifname"}, GatewaySource: "static", Gateway: "172.17.0.1", Weight: 60},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedGateway"] != "172.17.0.1" || status["selectedGatewaySource"] != "static" {
		t.Fatalf("gateway status = %#v", status)
	}
}

func TestControllerKeepsReadyCurrentDuringHysteresis(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 10, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":           {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/PPPoESession/wan-pppoe":         {"phase": "Up", "interface": "ppp0"},
		api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default": {"phase": PhaseApplied, "selectedCandidate": "ds-lite", "lastTransitionAt": now.Add(-10 * time.Second).Format(time.RFC3339Nano)},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection:  SelectionHighestWeightReady,
			Hysteresis: "30s",
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "wan-pppoe", Source: "PPPoESession/wan-pppoe", DeviceFrom: api.StatusValueSourceSpec{Resource: "PPPoESession/wan-pppoe", Field: "interface"}, Weight: 100},
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"}, Weight: 80},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("selectedCandidate = %v", status["selectedCandidate"])
	}
}

func TestControllerRequiresResolvedOutputWithReadyDependency(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up"},
		api.NetAPIVersion + "/Link/fallback":        {"phase": "Up", "ifname": "wan0"},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection:  SelectionHighestWeightReady,
			Hysteresis: "0s",
			Candidates: []api.EgressRoutePolicyCandidate{
				{
					Name:       "ds-lite",
					Source:     "DSLiteTunnel/ds-lite",
					DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"},
					Weight:     80,
					DependsOn: []api.ResourceDependencySpec{{
						Resource: "DSLiteTunnel/ds-lite",
						Phase:    "Up",
					}},
				},
				{Name: "fallback", Source: "Link/fallback", DeviceFrom: api.StatusValueSourceSpec{Resource: "Link/fallback", Field: "ifname"}, Weight: 50},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "fallback" {
		t.Fatalf("status = %#v", status)
	}

	store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["interface"] = "ds-routerd-test"
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" || status["selectedDevice"] != "ds-routerd-test" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerReportsUnsupportedSelection(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection:  SelectionWeightedECMP,
			Candidates: []api.EgressRoutePolicyCandidate{{Name: "a", Weight: 1}},
		}),
		Bus:   bus.New(),
		Store: store,
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhasePending || status["reason"] != ReasonUnsupported {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerRequiresHealthyHealthCheck(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":        {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/HealthCheck/internet-tcp443": {"phase": "Unhealthy", "lastCheckedAt": now.Format(time.RFC3339Nano)},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"}, Weight: 80, HealthCheck: "internet-tcp443"},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhasePending || status["reason"] != ReasonNoReadyCandidates {
		t.Fatalf("status = %#v", status)
	}
	store[api.NetAPIVersion+"/HealthCheck/internet-tcp443"]["phase"] = "Healthy"
	store[api.NetAPIVersion+"/HealthCheck/internet-tcp443"]["lastCheckedAt"] = now.Format(time.RFC3339Nano)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerKeepsCandidateReadyDuringHealthCheckGraceFailures(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":        {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/Link/fallback":               {"phase": "Up", "ifname": "wan0"},
		api.NetAPIVersion + "/HealthCheck/internet-tcp443": {"phase": "Failing", "consecutiveFailed": 1, "lastCheckedAt": now.Format(time.RFC3339Nano)},
	}
	controller := Controller{
		Router: routerWithResources(
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
				Metadata: api.ObjectMeta{Name: "internet-tcp443"},
				Spec:     api.HealthCheckSpec{UnhealthyThreshold: 3},
			},
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "ipv4-default"},
				Spec: api.EgressRoutePolicySpec{
					Selection: SelectionHighestWeightReady,
					Candidates: []api.EgressRoutePolicyCandidate{
						{Name: "fallback", Source: "Link/fallback", DeviceFrom: api.StatusValueSourceSpec{Resource: "Link/fallback", Field: "ifname"}, Weight: 50},
						{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"}, Weight: 80, HealthCheck: "internet-tcp443"},
					},
				},
			},
		),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}

	store[api.NetAPIVersion+"/HealthCheck/internet-tcp443"]["consecutiveFailed"] = 3
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "fallback" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerRejectsStaleHealthCheckStatus(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":        {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/HealthCheck/internet-tcp443": {"phase": "Healthy", "lastCheckedAt": now.Add(-10 * time.Minute).Format(time.RFC3339Nano)},
	}
	controller := Controller{
		Router: routerWithResources(
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
				Metadata: api.ObjectMeta{Name: "internet-tcp443"},
				Spec:     api.HealthCheckSpec{Interval: "30s", Timeout: "3s"},
			},
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "ipv4-default"},
				Spec: api.EgressRoutePolicySpec{
					Selection: SelectionHighestWeightReady,
					Candidates: []api.EgressRoutePolicyCandidate{
						{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"}, Weight: 80, HealthCheck: "internet-tcp443"},
					},
				},
			},
		),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhasePending || status["reason"] != ReasonNoReadyCandidates {
		t.Fatalf("status = %#v", status)
	}

	store[api.NetAPIVersion+"/HealthCheck/internet-tcp443"]["lastCheckedAt"] = now.Format(time.RFC3339Nano)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerUsesHealthyOutputWhenSourceHasNoStatus(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-via-pppoe": {"phase": "Healthy", "lastCheckedAt": now.Format(time.RFC3339Nano)},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{
					Name:        "pppoe-flets",
					Source:      "PPPoESession/pppoe-flets",
					Device:      "ppp-flets",
					Weight:      60,
					HealthCheck: "internet-via-pppoe",
				},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "pppoe-flets" {
		t.Fatalf("status = %#v", status)
	}
	if status["selectedDevice"] != "ppp-flets" {
		t.Fatalf("selectedDevice = %v", status["selectedDevice"])
	}
}

func TestControllerSkipsDisabledCandidate(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-via-pppoe": {"phase": "Healthy", "lastCheckedAt": now.Format(time.RFC3339Nano)},
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":           {"phase": "Up", "device": "ds-lite"},
	}
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "pppoe-flets", Disabled: true, Source: "PPPoESession/pppoe-flets", Device: "ppp-flets", Weight: 120, HealthCheck: "internet-via-pppoe"},
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "device"}, Weight: 80},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerSkipsDisabledPPPoESource(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-via-pppoe": {"phase": "Healthy", "lastCheckedAt": now.Format(time.RFC3339Nano)},
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":           {"phase": "Up", "device": "ds-lite"},
	}
	controller := Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
				Metadata: api.ObjectMeta{Name: "pppoe-flets"},
				Spec:     api.PPPoESessionSpec{Interface: "wan", IfName: "ppp-flets", Disabled: true, Username: "open@open.ad.jp", Password: "open"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "ipv4-default"},
				Spec: api.EgressRoutePolicySpec{
					Selection: SelectionHighestWeightReady,
					Candidates: []api.EgressRoutePolicyCandidate{
						{Name: "pppoe-flets", Source: "PPPoESession/pppoe-flets", Device: "ppp-flets", Weight: 120, HealthCheck: "internet-via-pppoe"},
						{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "device"}, Weight: 80},
					},
				},
			},
		}}},
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerRepublishesWhenSelectedOutputChanges(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up", "interface": "ds-routerd-new"},
		api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default": {
			"phase":             PhaseApplied,
			"selectedCandidate": "ds-lite",
			"selectedDevice":    "ds-routerd-old",
			"lastTransitionAt":  now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	}
	b := bus.New()
	ch, cancel := b.Subscribe(context.Background(), bus.Subscription{Topics: []string{EventRouteChanged}}, 1)
	defer cancel()
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", DeviceFrom: api.StatusValueSourceSpec{Resource: "DSLiteTunnel/ds-lite", Field: "interface"}, Weight: 80},
			},
		}),
		Bus:   b,
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedDevice"] != "ds-routerd-new" {
		t.Fatalf("selectedDevice = %v", status["selectedDevice"])
	}
	select {
	case event := <-ch:
		if event.Attributes["selectedDevice"] != "ds-routerd-new" {
			t.Fatalf("event attributes = %#v", event.Attributes)
		}
	default:
		t.Fatal("expected route changed event")
	}
}

func TestControllerDoesNotRepublishWhenSelectionUnchanged(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/Link/ix2215": {"phase": "Up", "ifname": "ens19"},
	}
	b := bus.New()
	ch, cancel := b.Subscribe(context.Background(), bus.Subscription{Topics: []string{EventRouteChanged}}, 2)
	defer cancel()
	controller := Controller{
		Router: routerWithPolicy(api.EgressRoutePolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.EgressRoutePolicyCandidate{
				{
					Name:          "ix2215-fallback",
					Source:        "Link/ix2215",
					DeviceFrom:    api.StatusValueSourceSpec{Resource: "Link/ix2215", Field: "ifname"},
					GatewaySource: "static",
					Gateway:       "192.168.1.1",
					Table:         116,
					Metric:        50,
					Weight:        10,
				},
			},
		}),
		Bus:   b,
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-ch:
		if event.Attributes["selectedCandidate"] != "ix2215-fallback" {
			t.Fatalf("first event attributes = %#v", event.Attributes)
		}
	default:
		t.Fatal("expected initial route changed event")
	}

	now = now.Add(30 * time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if status["selectedCandidate"] != "ix2215-fallback" || status["selectedDevice"] != "ens19" || status["selectedGateway"] != "192.168.1.1" || statusInt(status["selectedRouteTable"]) != 116 {
		t.Fatalf("status = %#v", status)
	}
	select {
	case event := <-ch:
		t.Fatalf("unchanged selection should not publish route changed event: %#v", event)
	case <-time.After(40 * time.Millisecond):
	}
}

func routerWithPolicy(spec api.EgressRoutePolicySpec) *api.Router {
	return routerWithResources(api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
		Metadata: api.ObjectMeta{Name: "ipv4-default"},
		Spec:     spec,
	})
}

func routerWithResources(resources ...api.Resource) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: resources}}
}
