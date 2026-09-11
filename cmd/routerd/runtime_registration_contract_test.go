// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/lifecycle"
)

func TestContractRuntimeShapeKindsHaveLifecycleOrExplicitException(t *testing.T) {
	want := strings.Fields(`RouterdCluster DHCPv4Client DHCPv4Server DHCPv6Client DHCPv6Server
		DHCPv6PrefixDelegation PPPoESession HealthCheck DNSResolver DSLiteTunnel EventGroup
		EventSubscription WebConsole SAMTransportProfile MobilityPool ServiceUnit`)
	var got []string
	exceptions := map[string]string{
		"ServiceUnit":  "derived service units have runtime state, but are not declared config resources",
		"DHCPv6Client": "historical runtime-shape guard; this name is not an accepted config kind",
	}
	for kind, enabled := range runtimeShapeKinds {
		got = append(got, kind)
		if !enabled {
			t.Errorf("runtime shape %s has a disabled registry entry", kind)
		}
		if _, ok := lifecycle.Lookup(lifecycle.APIVersionForKind(kind), kind); !ok && exceptions[kind] == "" {
			t.Errorf("runtime shape %s has no lifecycle declaration or explicit exception", kind)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime shape kinds=%v, want %v; review reload ownership when changing this set", got, want)
	}
	for kind := range exceptions {
		if _, ok := lifecycle.Lookup(lifecycle.APIVersionForKind(kind), kind); ok {
			t.Errorf("stale runtime-shape exception for %s", kind)
		}
	}
}

func TestContractControllerDisplayKindsHaveLifecycleOrDerivedReason(t *testing.T) {
	// Display groups are intentionally not a one-to-one controller registry.
	// A kind such as IngressService is displayed by ingress, nat and firewall.
	derived := map[string]string{
		"ServiceUnit":     "service units are derived from declared resource intent",
		"KernelModule":    "required modules are derived; the former config kind is rejected",
		"NetworkAdoption": "adoption is derived from interfaces and service requirements",
		"IPv4StaticRoute": "route lowerings are generated from declared route resources",
		"IPv6StaticRoute": "route lowerings are generated from declared route resources",
	}
	known := map[string]bool{}
	for _, kind := range api.ConfigResourceKinds() {
		known[kind.Kind] = true
	}
	seen := map[string]bool{}
	for _, controller := range controllerDefaultStatuses() {
		if seen[controller.Name] {
			t.Errorf("duplicate display group %q", controller.Name)
		}
		seen[controller.Name] = true
		if len(controller.ResourceKinds) == 0 {
			t.Errorf("display group %q has no resource kinds", controller.Name)
		}
		for _, kind := range controller.ResourceKinds {
			if !known[kind] && derived[kind] == "" {
				t.Errorf("display group %s has undeclared kind %s without a reason", controller.Name, kind)
			}
			if known[kind] {
				if _, ok := lifecycle.Lookup(lifecycle.APIVersionForKind(kind), kind); !ok {
					t.Errorf("display kind %s has no lifecycle declaration", kind)
				}
			}
		}
	}
	for kind := range derived {
		if known[kind] {
			t.Errorf("stale derived-kind exception %s", kind)
		}
	}
	for _, alias := range []struct{ display, worker, kind string }{
		{"dhcpv4client", "dhcpv4-lease", "DHCPv4Client"},
		{"dhcpv6", "dhcpv6-server", "DHCPv6Server"},
		{"pppoesession", "pppoe-session", "PPPoESession"},
		{"nat", "nat44", "NAT44Rule"},
		{"ingress", "ingress-service", "IngressService"},
	} {
		if !slices.Contains(controllerResourceKinds(alias.display), alias.kind) {
			t.Errorf("display %s lost %s", alias.display, alias.kind)
		}
		if len(controllerResourceKinds(alias.worker)) != 0 {
			t.Errorf("worker %s acquired a display mapping; review the existing %s group explicitly", alias.worker, alias.display)
		}
	}
}
