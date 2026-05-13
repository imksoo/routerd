// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
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
