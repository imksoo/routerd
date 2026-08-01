// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
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
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resource"
)

func TestIPv6HostPolicyUsesStateAndNeverMarksNft(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	router := hostPolicyRouter(true)
	store := mapStore{}
	var calls []string
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0    inet6 2001:db8::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte(""), nil
		default:
			return nil, nil
		}
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::2",
		"ip -6 rule add priority 10120 iif lo lookup 120",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if data, err := render.NftablesIPv4PolicyRoutes(router); err != nil || strings.Contains(string(data), "120") {
		t.Fatalf("host policy must not enter IPv4 nft: %q, %v", data, err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.Join(calls, "\n"), "route replace default"); got != 2 {
		t.Fatalf("state must not suppress route drift repair, route replace count=%d", got)
	}
}

func TestIPv6HostPolicyRepairsMissingRuleWithMatchingState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte("0: from all lookup local\n"), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::2",
		"ip -6 rule add priority 10120 iif lo lookup 120",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("matching state did not repair %q:\n%s", want, joined)
		}
	}
}

func TestIPv6HostPolicyKeepsNormalLocalTrafficOnPhysicalWANWhenPDUsesVMAC(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	router := hostPolicyRouter(true)
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-vmac"}, Spec: api.InterfaceSpec{IfName: "wan-vmac"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "lan-gw"}, Spec: api.VirtualAddressSpec{
			Family: "ipv4", Interface: "lan", Address: "192.0.2.1/32", Mode: "vrrp",
			VRRP: api.VirtualAddressVRRPSpec{
				VirtualRouterID: 18,
				FailoverVMAC:    &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13"},
			},
		}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan-vmac"}},
	)
	var calls []string
	controller := IPv4PolicyRouteController{Router: router, Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8:1200::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte(""), nil
		default:
			return nil, nil
		}
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "addr show dev wan-vmac") || strings.Contains(joined, "dev wan-vmac table 120") {
		t.Fatalf("normal host policy must not use the DS-Lite VMAC:\n%s", joined)
	}
	for _, want := range []string{
		"ip -6 -o addr show dev wan0 scope global",
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8:1200::2",
		"ip -6 rule add priority 10120 iif lo lookup 120",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestIPv6HostPolicyRoutesOnlyDSLiteOuterSourcesThroughVMACMainTable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	router := hostPolicyRouter(true)
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan-vmac", LocalAddressSource: "delegatedAddress"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-ra"}, Spec: api.DSLiteTunnelSpec{Interface: "wan-vmac", LocalAddressSource: "delegatedAddress"}},
	)
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-a":  {"localIPv6": "2001:db8:1200::21"},
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite-ra": {"localIPv6": "2001:db8:1200::23"},
	}
	var calls []string
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8:1200::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte(""), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip -6 rule add priority 10100 from 2001:db8:1200::21/128 lookup main",
		"ip -6 rule add priority 10101 from 2001:db8:1200::23/128 lookup main",
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8:1200::2",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "route replace default via fe80::1 dev wan-vmac") {
		t.Fatalf("DS-Lite source rules must use the existing VMAC main route, not replace a host route:\n%s", joined)
	}
}

func TestIPv6HostPolicyKeepsPhysicalSLAACDSLiteSourceInHostPolicyTable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	physical := "2001:db8:1200::2"
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{
		{Name: "host-ipv6-ra", Owner: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: physical, Metric: 50},
		// This is the erroneous rule produced when physical-SLAAC tunnel
		// endpoints were first introduced.  Reconcile must remove it.
		{Name: "dslite-source-ds-lite-ra", Owner: "host-ipv6-ra", Priority: 10100, Lookup: "main", Source: physical, RuleFrom: true},
	}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	router := hostPolicyRouter(true)
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-ra"}, Spec: api.DSLiteTunnelSpec{Interface: "wan"}},
	)
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite-ra": {"localIPv6": physical}}
	var calls []string
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 " + physical + "/64 scope global dynamic\n"), nil
		case "ip -6 rule show":
			return []byte("10100: from " + physical + " lookup main\n10120: from all iif lo lookup 120\n"), nil
		default:
			return nil, nil
		}
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	wantDelete := "ip -6 rule del priority 10100 from " + physical + "/128 lookup main"
	if !strings.Contains(joined, wantDelete) {
		t.Fatalf("physical-SLAAC source rule was not removed; missing %q in:\n%s", wantDelete, joined)
	}
	if strings.Contains(joined, "ip -6 rule add priority 10100") {
		t.Fatalf("physical-SLAAC DS-Lite endpoint must use iif lo/table 120, not main:\n%s", joined)
	}
	state, err := loadIPv6HostPolicyState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range state.Policies {
		if policy.isSourceRule() {
			t.Fatalf("physical-SLAAC source rule remained in state: %#v", state)
		}
	}
}

func TestIPv6HostPolicyMigratesVMACCatchAllRuleToPhysicalWANAndCleansOldState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Owner: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan-vmac", Source: "2001:db8:1200::23", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	router := hostPolicyRouter(true)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-ra"}, Spec: api.DSLiteTunnelSpec{Interface: "wan-vmac", LocalAddressSource: "delegatedAddress"}})
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite-ra": {"localIPv6": "2001:db8:1200::23"}}
	var calls []string
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8:1200::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte(""), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip -6 rule del priority 10120 iif lo lookup 120",
		"ip -6 route del default via fe80::1 dev wan-vmac table 120 metric 50 src 2001:db8:1200::23",
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8:1200::2",
		"ip -6 rule add priority 10100 from 2001:db8:1200::23/128 lookup main",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestIPv6HostPolicyRecognizesKernelHostPrefixDisplay(t *testing.T) {
	var calls []string
	controller := IPv4PolicyRouteController{CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if call == "ip -6 rule show" {
			return []byte("10100: from 2001:db8:1200::23 lookup main\n"), nil
		}
		return nil, nil
	}}
	transient, err := controller.ensureIPv6HostRule(t.Context(), ipv6HostPolicy{Priority: 10100, Lookup: "main", Source: "2001:db8:1200::23", RuleFrom: true})
	if err != nil || transient {
		t.Fatalf("ensure existing source rule = transient:%t err:%v", transient, err)
	}
	if strings.Contains(strings.Join(calls, "\n"), "rule add") {
		t.Fatalf("kernel's bare /128 display must not add a duplicate rule: %s", strings.Join(calls, "\n"))
	}
}

func TestIPv6HostPolicyMigratesLegacySourceRuleStateWithoutDeletingRoute(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{
		{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50},
		// ruleFrom was absent in the short-lived initial state format.
		{Name: "dslite-source-ds-lite-ra", Priority: 10100, Lookup: "main", Source: "2001:db8:1200::23"},
	}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	router := hostPolicyRouter(true)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-ra"}, Spec: api.DSLiteTunnelSpec{Interface: "wan-vmac", LocalAddressSource: "delegatedAddress"}})
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite-ra": {"localIPv6": "2001:db8:1200::23"}}
	var calls []string
	controller := IPv4PolicyRouteController{Router: router, Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte("10100: from 2001:db8:1200::23 lookup main\n10120: from all iif lo lookup 120\n"), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "route del default via  dev  table 0") {
		t.Fatalf("legacy source rule must not be treated as a route policy:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -6 rule del priority 10100 from 2001:db8:1200::23/128 lookup main") {
		t.Fatalf("legacy source rule was not deleted as a source rule:\n%s", joined)
	}
}

func TestIPv6HostPolicyInitialSourceUnavailableDoesNotApply(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	store := mapStore{}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "link show") {
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		}
		if strings.Contains(strings.Join(args, " "), "addr show") {
			return nil, errors.New("no global address")
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("initial unavailable source must not fail policy reconcile: %v", err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "host-ipv6-ra"); got["phase"] != "Pending" || got["reason"] != "PreferredSourceUnavailable" {
		t.Fatalf("status = %#v", got)
	}
	for _, call := range calls {
		if strings.Contains(call, "route replace") || strings.Contains(call, "rule add") {
			t.Fatalf("initial unavailable source must not apply host policy: %s", call)
		}
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("initial unavailable source must not create state: %v", err)
	}
}

func TestIPv6HostPolicyRetainsStateWhenPreferredSourceUnavailable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: store, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "link show") {
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		}
		if strings.Contains(strings.Join(args, " "), "addr show") {
			return nil, errors.New("no global address")
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("host source loss must not fail IPv4 reconciliation: %v", err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "host-ipv6-ra"); got["reason"] != "PreferredSourceUnavailable" || got["phase"] != "Pending" {
		t.Fatalf("unexpected status: %#v", got)
	}
	for _, call := range calls {
		if strings.Contains(call, "route del") || strings.Contains(call, "rule del") || strings.Contains(call, "route replace") || strings.Contains(call, "rule add") {
			t.Fatalf("must retain previous host state: %s", call)
		}
	}
}

func TestIPv6HostPolicyChangesAndRemovesOnlyStatefulRules(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if strings.Contains(call, "link show") {
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		}
		if strings.Contains(call, "addr show") {
			return []byte("2: wan0 inet6 2001:db8::9/64 scope global\n"), nil
		}
		if strings.Contains(call, "rule show") {
			return nil, nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip -6 rule del priority 10120 iif lo lookup 120",
		"ip -6 route del default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::2",
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::9",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	calls = nil
	controller.Router = hostPolicyRouter(false)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(calls, "\n")
	if !strings.Contains(joined, "ip -6 route del default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::9") {
		t.Fatalf("config removal must clean current route:\n%s", joined)
	}
	state, err := loadIPv6HostPolicyState(statePath)
	if err != nil || len(state.Policies) != 0 {
		t.Fatalf("state after removal = %#v, %v", state, err)
	}
}

func TestIPv6HostPolicyChangeIgnoresAlreadyMissingPreviousRule(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case call == "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case call == "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8::9/64 scope global\n"), nil
		case call == "ip -6 rule del priority 10120 iif lo lookup 120":
			return []byte("RTNETLINK answers: No such file or directory\n"), errors.New("exit status 2")
		case call == "ip -6 rule show":
			return []byte("0: from all lookup local\n"), nil
		default:
			return nil, nil
		}
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip -6 rule del priority 10120 iif lo lookup 120",
		"ip -6 route del default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::2",
		"ip -6 route replace default via fe80::1 dev wan0 table 120 metric 50 src 2001:db8::9",
		"ip -6 rule add priority 10120 iif lo lookup 120",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestIPv6HostPolicyRetainsStateWhenDeviceIsDown(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,MULTICAST> mtu 1500 state DOWN mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8::2/64 scope global\n"), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(calls, "\n"), "route replace") {
		t.Fatalf("device down must not install a host policy:\n%s", strings.Join(calls, "\n"))
	}
	got, err := loadIPv6HostPolicyState(statePath)
	if err != nil || len(got.Policies) != 1 || got.Policies[0] != previous.Policies[0] {
		t.Fatalf("state = %#v, %v", got, err)
	}
}

func TestIPv6HostPolicyRetainsStateWhenOnlyAddressIsTentative(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::2", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8::9/64 scope global tentative\n"), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(calls, "\n"), "route replace") {
		t.Fatalf("tentative address must not install a host policy:\n%s", strings.Join(calls, "\n"))
	}
	got, err := loadIPv6HostPolicyState(statePath)
	if err != nil || len(got.Policies) != 1 || got.Policies[0] != previous.Policies[0] {
		t.Fatalf("state = %#v, %v", got, err)
	}
}

func TestIPv6HostPolicyRetriesTransientRouteReplaceWithoutRecordingState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "ipv6-host-policy.json")
	previous := ipv6HostPolicyState{Policies: []ipv6HostPolicy{{Name: "host-ipv6-ra", Priority: 10120, Table: 120, Gateway: "fe80::1", Interface: "wan0", Source: "2001:db8::9", Metric: 50}}}
	if err := writeIPv6HostPolicyState(statePath, previous); err != nil {
		t.Fatal(err)
	}
	var routeReplaceCalls int
	controller := IPv4PolicyRouteController{Router: hostPolicyRouter(true), Store: mapStore{}, OperatingSystem: platform.OSLinux, HostPolicyStatePath: statePath, CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "ip -o link show dev wan0":
			return []byte("2: wan0: <BROADCAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default\n"), nil
		case "ip -6 -o addr show dev wan0 scope global":
			return []byte("2: wan0 inet6 2001:db8::2/64 scope global\n"), nil
		case "ip -6 rule show":
			return []byte(""), nil
		}
		if strings.Contains(call, "route replace") {
			routeReplaceCalls++
			return []byte("RTNETLINK answers: Nexthop device is not up\n"), errors.New("exit status 2")
		}
		return nil, nil
	}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("transient route failure must not fail reconcile: %v", err)
	}
	state, err := loadIPv6HostPolicyState(statePath)
	if err != nil || len(state.Policies) != 0 {
		t.Fatalf("state after transient failure = %#v, %v", state, err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("transient route failure must remain retryable: %v", err)
	}
	if routeReplaceCalls != 2 {
		t.Fatalf("route replace calls = %d, want 2", routeReplaceCalls)
	}
}

func hostPolicyRouter(includeHost bool) *api.Router {
	resources := []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}}}
	if includeHost {
		resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "host-ipv6-ra"}, Spec: api.EgressRoutePolicySpec{Family: "ipv6", Mode: "priority", HostTraffic: true, Candidates: []api.EgressRoutePolicyCandidate{{Name: "physical-ra", Interface: "wan", GatewaySource: "static", Gateway: "fe80::1", PreferredSource: "interface", Table: 120, Priority: 10120, Metric: 50}}}})
	}
	return &api.Router{Spec: api.RouterSpec{Resources: resources}}
}

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
	if len(spec.Candidates) != 1 || len(spec.Candidates[0].Targets) != 1 || spec.Candidates[0].Targets[0].Name != "up" {
		t.Fatalf("effective targets = %#v, want only ready target", spec.Candidates)
	}
	original, err := router.Spec.Resources[2].EgressRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Candidates[0].Targets) != 2 {
		t.Fatalf("effective filtering mutated declared targets: %#v", original.Candidates[0].Targets)
	}
}

func TestIPv4PolicyRouteInstallsFwmarkBootstrapRouteForHealthCheck(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
	case <-time.After(40 * time.Millisecond):
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
	case <-time.After(40 * time.Millisecond):
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

func TestIPv4PolicyRouteApplyNftTableReloadsUnchangedStaleTable(t *testing.T) {
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
		"-c -f " + tablePath,
		"-f " + tablePath,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nft command log missing %q:\n%s", want, got)
		}
	}
}

func TestIPv4PolicyRouteApplyNftTableSkipsRecentlyVerifiedExistingTable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	nftPath := filepath.Join(dir, "nft")
	tablePath := filepath.Join(dir, "policy.nft")
	data := []byte("table ip routerd_policy {}\n")
	if err := os.WriteFile(tablePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := nftstate.MarkVerified(tablePath, time.Now().UTC()); err != nil {
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
	if !strings.Contains(got, "list table ip routerd_policy") {
		t.Fatalf("nft command log missing list:\n%s", got)
	}
	for _, unwanted := range []string{"-c -f " + tablePath, "-f " + tablePath} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("recently verified existing table should not run %q:\n%s", unwanted, got)
		}
	}
}

func TestIPv4PolicyRouteApplyNftTableReloadsMissingRecentlyVerifiedTable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	nftPath := filepath.Join(dir, "nft")
	tablePath := filepath.Join(dir, "policy.nft")
	data := []byte("table ip routerd_policy {}\n")
	if err := os.WriteFile(tablePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := nftstate.MarkVerified(tablePath, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + testShellQuote(logPath) + "\n" +
		"if [ \"$1 $2 $3 $4\" = \"list table ip routerd_policy\" ]; then exit 1; fi\n" +
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
	for _, want := range []string{"list table ip routerd_policy", "-c -f " + tablePath, "-f " + tablePath} {
		if !strings.Contains(got, want) {
			t.Fatalf("nft command log missing %q:\n%s", want, got)
		}
	}
}

func TestIPv4PolicyRouteApplyNftTableDeletesExistingTableWhenDesiredEmptyDespiteRecentVerification(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	nftPath := filepath.Join(dir, "nft")
	tablePath := filepath.Join(dir, "policy.nft")
	if err := nftstate.MarkVerified(tablePath, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + testShellQuote(logPath) + "\n" +
		"exit 0\n"
	if err := os.WriteFile(nftPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	controller := IPv4PolicyRouteController{}
	if err := controller.applyNftTable(context.Background(), nftPath, tablePath, "ip", "routerd_policy", nil); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logData)
	for _, want := range []string{"list table ip routerd_policy", "delete table ip routerd_policy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("nft command log missing %q:\n%s", want, got)
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
