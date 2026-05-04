package chain

import (
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/render"
)

func TestIPv4PolicyRouteSetFiltersUnhealthyTargets(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"}, Metadata: api.ObjectMeta{Name: "dslite"}, Spec: api.IPv4PolicyRouteSetSpec{
			Mode:             "hash",
			HashFields:       []string{"sourceAddress"},
			SourceCIDRs:      []string{"172.18.0.0/16"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
			Targets: []api.IPv4PolicyRouteTarget{
				{Name: "a", OutboundInterface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110, HealthCheck: "hc-a"},
				{Name: "b", OutboundInterface: "ds-lite-b", Table: 111, Priority: 10111, Mark: 0x111, HealthCheck: "hc-b"},
				{Name: "c", OutboundInterface: "ds-lite-c", Table: 112, Priority: 10112, Mark: 0x112, HealthCheck: "hc-c"},
			},
		}},
	}}}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/hc-a": {"phase": "Healthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/HealthCheck/hc-b": {"phase": "Unhealthy", "lastCheckedAt": now},
		api.NetAPIVersion + "/HealthCheck/hc-c": {"phase": "Failing", "lastCheckedAt": now},
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

func TestIPv4PolicyRouteSetReferencedByDefaultPolicyRendersOnlyWhenActive(t *testing.T) {
	routeSet := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"}, Metadata: api.ObjectMeta{Name: "dslite"}, Spec: api.IPv4PolicyRouteSetSpec{
		Mode:             "hash",
		HashFields:       []string{"sourceAddress"},
		SourceCIDRs:      []string{"172.18.0.0/16"},
		DestinationCIDRs: []string{"0.0.0.0/0"},
		Targets: []api.IPv4PolicyRouteTarget{
			{Name: "a", OutboundInterface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110},
		},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		routeSet,
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DefaultRoutePolicy"}, Metadata: api.ObjectMeta{Name: "lan-default"}, Spec: api.IPv4DefaultRoutePolicySpec{
			SourceCIDRs:      []string{"172.18.0.0/16"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
			Candidates: []api.IPv4DefaultRoutePolicyCandidate{
				{Name: "dslite", RouteSet: "dslite", Priority: 10},
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
		t.Fatalf("inactive referenced routeSet should not render marks:\n%s", inactive)
	}
	active, err := render.NftablesIPv4PolicyRoutes(controller.effectivePolicyRouteRouter(map[string]bool{"dslite": true}))
	if err != nil {
		t.Fatalf("render active policy routes: %v", err)
	}
	if !strings.Contains(string(active), "0x110") {
		t.Fatalf("active referenced routeSet should render marks:\n%s", active)
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
