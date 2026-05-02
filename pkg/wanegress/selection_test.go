package wanegress

import (
	"context"
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
		Router: routerWithPolicy(api.WANEgressPolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.WANEgressPolicyCandidate{
				{Name: "fallback", Source: "Link/fallback", Device: "${Link/fallback.status.ifname}", Weight: 50},
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", Device: "${DSLiteTunnel/ds-lite.status.interface}", Weight: 80},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WANEgressPolicy", "ipv4-default")
	if status["phase"] != PhaseApplied {
		t.Fatalf("phase = %v", status["phase"])
	}
	if status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("selectedCandidate = %v", status["selectedCandidate"])
	}
	if status["selectedDevice"] != "ds-routerd-test" {
		t.Fatalf("selectedDevice = %v", status["selectedDevice"])
	}
}

func TestControllerKeepsReadyCurrentDuringHysteresis(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 10, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":         {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/PPPoESession/wan-pppoe":       {"phase": "Up", "interface": "ppp0"},
		api.NetAPIVersion + "/WANEgressPolicy/ipv4-default": {"phase": PhaseApplied, "selectedCandidate": "ds-lite", "lastTransitionAt": now.Add(-10 * time.Second).Format(time.RFC3339Nano)},
	}
	controller := Controller{
		Router: routerWithPolicy(api.WANEgressPolicySpec{
			Selection:  SelectionHighestWeightReady,
			Hysteresis: "30s",
			Candidates: []api.WANEgressPolicyCandidate{
				{Name: "wan-pppoe", Source: "PPPoESession/wan-pppoe", Device: "${PPPoESession/wan-pppoe.status.interface}", Weight: 100},
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", Device: "${DSLiteTunnel/ds-lite.status.interface}", Weight: 80},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WANEgressPolicy", "ipv4-default")
	if status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("selectedCandidate = %v", status["selectedCandidate"])
	}
}

func TestControllerReportsUnsupportedSelection(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: routerWithPolicy(api.WANEgressPolicySpec{
			Selection:  SelectionWeightedECMP,
			Candidates: []api.WANEgressPolicyCandidate{{Name: "a", Weight: 1}},
		}),
		Bus:   bus.New(),
		Store: store,
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WANEgressPolicy", "ipv4-default")
	if status["phase"] != PhasePending || status["reason"] != ReasonUnsupported {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerRequiresHealthyHealthCheck(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite":        {"phase": "Up", "interface": "ds-routerd-test"},
		api.NetAPIVersion + "/HealthCheck/internet-tcp443": {"phase": "Unhealthy"},
	}
	controller := Controller{
		Router: routerWithPolicy(api.WANEgressPolicySpec{
			Selection: SelectionHighestWeightReady,
			Candidates: []api.WANEgressPolicyCandidate{
				{Name: "ds-lite", Source: "DSLiteTunnel/ds-lite", Device: "${DSLiteTunnel/ds-lite.status.interface}", Weight: 80, HealthCheck: "internet-tcp443"},
			},
		}),
		Bus:   bus.New(),
		Store: store,
		Now:   func() time.Time { return now },
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WANEgressPolicy", "ipv4-default")
	if status["phase"] != PhasePending || status["reason"] != ReasonNoReadyCandidates {
		t.Fatalf("status = %#v", status)
	}
	store[api.NetAPIVersion+"/HealthCheck/internet-tcp443"]["phase"] = "Healthy"
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "WANEgressPolicy", "ipv4-default")
	if status["phase"] != PhaseApplied || status["selectedCandidate"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}

func routerWithPolicy(spec api.WANEgressPolicySpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WANEgressPolicy"},
			Metadata: api.ObjectMeta{Name: "ipv4-default"},
			Spec:     spec,
		},
	}}}
}
