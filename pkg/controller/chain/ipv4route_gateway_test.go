// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestOrdinaryRouteRequiredGatewayLifecycle(t *testing.T) {
	c, store, native, _ := newOrdinaryRouteFixture(t)
	router, err := config.LoadBytes([]byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: gateway-lifecycle}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: uplink}
      spec: {ifname: old0, managed: false}
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata: {name: wan}
      spec: {interface: uplink, useRoutes: false}
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4Route
      metadata: {name: route}
      spec:
        destination: 192.0.2.0/24
        device: old0
        metric: 10
        gatewayFrom: {resource: DHCPv4Client/wan, field: gateway}
`), "gateway-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	reconcile := func() {
		t.Helper()
		observed, err := ObserveRouterConfiguration(router, store, time.Now(), platform.OSLinux)
		if err != nil {
			t.Fatal(err)
		}
		c.Router = observed.RouteRouter
		if err := c.reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	pending := func() {
		t.Helper()
		status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "route")
		if status["phase"] != "Pending" || status["reason"] != "GatewayMissing" || readOrdinaryRouteReceipt(t, store) != nil || native.output != `[]` {
			t.Fatalf("unresolved required gateway retained a route: status=%v commands=%v", status, native.calls)
		}
	}
	reconcile()
	pending()
	if strings.Contains(strings.Join(native.calls, "\n"), "ip route add") {
		t.Fatalf("cold missing gateway added route: %v", native.calls)
	}
	native.afterApply = `[{"dst":"192.0.2.0/24","dev":"old0","gateway":"198.51.100.1","metric":10,"protocol":"boot"}]`
	for i := 0; i < 2; i++ {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan", map[string]any{"gateway": "198.51.100.1"}); err != nil {
			t.Fatal(err)
		}
		reconcile()
		receipt := readOrdinaryRouteReceipt(t, store)
		if receipt == nil || receipt.Pending || receipt.Route.Gateway != "198.51.100.1" {
			t.Fatalf("acquired gateway not installed: %v", receipt)
		}
		if err := store.DeleteObject(api.NetAPIVersion, "DHCPv4Client", "wan"); err != nil {
			t.Fatal(err)
		}
		native.calls = nil
		reconcile()
		pending()
		if !strings.Contains(strings.Join(native.calls, "\n"), "ip route del") || strings.Contains(strings.Join(native.calls, "\n"), "ip route add") {
			t.Fatalf("gateway loss must withdraw without direct-route replacement: %v", native.calls)
		}
	}
}

func TestOrdinaryRouteGatewayFallbackAndDirectIntent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  api.StatusValueSourceSpec
		gateway string
	}{
		{name: "direct"},
		{name: "optional", source: api.StatusValueSourceSpec{Resource: "DHCPv4Client/wan", Field: "gateway", Optional: true}},
		{name: "static fallback", source: api.StatusValueSourceSpec{Resource: "DHCPv4Client/wan", Field: "gateway"}, gateway: "198.51.100.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := newOrdinaryRouteFixture(t)
			premise, err := c.ordinaryRoutePremise(t.Context(), api.IPv4RouteSpec{Device: "old0", Gateway: tc.gateway, GatewayFrom: tc.source})
			if err != nil || !premise.Active || premise.Route.Gateway != tc.gateway {
				t.Fatalf("explicit fallback/direct intent lost: %+v, %v", premise, err)
			}
		})
	}
}
