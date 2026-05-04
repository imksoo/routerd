package chain

import (
	"strings"
	"testing"

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
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/hc-a": {"phase": "Healthy"},
		api.NetAPIVersion + "/HealthCheck/hc-b": {"phase": "Unhealthy"},
		api.NetAPIVersion + "/HealthCheck/hc-c": {"phase": "Failing"},
	}
	controller := IPv4PolicyRouteController{Router: router, Store: store}
	data, err := render.NftablesIPv4PolicyRoutes(controller.effectivePolicyRouteRouter())
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
