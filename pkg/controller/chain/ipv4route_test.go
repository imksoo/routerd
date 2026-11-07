// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestIPv4RouteControllerRendersBlackholeRouteInDryRun(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: "private-10-blackhole",
			},
			Spec: api.IPv4RouteSpec{
				Type:        "blackhole",
				Destination: "10.0.0.0/8",
				Metric:      20,
			},
		},
	}}}
	store := mapStore{}
	controller := IPv4RouteController{Router: router, Store: store, DryRun: true}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "private-10-blackhole")
	if status["phase"] != "Rendered" || status["routeOwner"] != nil || status["installedAt"] != nil {
		t.Fatalf("dry-run claimed an installed route: %#v", status)
	}
	if status["type"] != "blackhole" {
		t.Fatalf("type = %v, want blackhole", status["type"])
	}
	if status["device"] != "" {
		t.Fatalf("blackhole route should not have device, got %v", status["device"])
	}
	if status["destination"] != "10.0.0.0/8" {
		t.Fatalf("destination = %v", status["destination"])
	}
}

func TestIPv4RouteControllerRendersIPv6StaticRouteInDryRun(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6StaticRouteSpec{
			Interface: "lan", Destination: "2001:db8:10::/64", Via: "fe80::1", Metric: 42,
		}},
	}}}
	store := mapStore{}
	controller := IPv4RouteController{Router: router, Store: store, DryRun: true}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6StaticRoute", "lan-v6")
	if status["phase"] != "Rendered" || status["dryRun"] != true || status["routeOwner"] != nil || status["destination"] != "2001:db8:10::/64" || status["gateway"] != "fe80::1" || status["interface"] != "lan" || status["metric"] != 42 {
		t.Fatalf("IPv6StaticRoute was not rendered through the route controller: %#v", status)
	}
}

func TestIPv4RouteControllerMarksDisabledDependencyNotApplicable(t *testing.T) {
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoESessionSpec{
			Interface: "wan",
			IfName:    "ppp-flets",
			Enabled:   &enabled,
			Username:  "user",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "pppoe-dependent"}, Spec: api.IPv4RouteSpec{
			Destination: "203.0.113.10/32",
			Device:      "ppp-flets",
			DependsOn: []api.ResourceDependencySpec{{
				Resource: "PPPoESession/pppoe-flets",
				Phase:    "Connected",
			}},
		}},
	}}}
	store := mapStore{}
	controller := IPv4RouteController{Router: router, Store: store, DryRun: true}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "pppoe-dependent")
	if status["phase"] != PhaseNotApplicable {
		t.Fatalf("phase = %v, want %s: %#v", status["phase"], PhaseNotApplicable, status)
	}
}

func TestIPv4RouteControllerMarksDisabledHealthcheckRouteStandby(t *testing.T) {
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoESessionSpec{
			Interface: "wan",
			IfName:    "ppp-flets",
			Enabled:   &enabled,
			Username:  "user",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "pppoe-healthcheck"}, Spec: api.IPv4RouteSpec{
			Destination: "208.67.222.222/32",
			Device:      "ppp-flets",
			DependsOn: []api.ResourceDependencySpec{{
				Resource: "PPPoESession/pppoe-flets",
				Phase:    "Connected",
			}},
		}},
	}}}
	store := mapStore{}
	controller := IPv4RouteController{Router: router, Store: store, DryRun: true}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "pppoe-healthcheck")
	if status["phase"] != PhaseStandby {
		t.Fatalf("phase = %v, want %s: %#v", status["phase"], PhaseStandby, status)
	}
}

func TestIPv4RouteControllerDoesNotRefreshInstalledAtWhenUnchanged(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: "static",
			},
			Spec: api.IPv4RouteSpec{
				Destination: "192.168.0.0/16",
				Device:      "ens18",
				Metric:      20,
			},
		},
	}}}
	store := mapStore{}
	installed := false
	controller := IPv4RouteController{Router: router, Store: store,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) > 4 && args[0] == "-j" {
				if installed {
					return []byte(`[{"dst":"192.168.0.0/16","dev":"ens18","metric":20,"protocol":"boot"}]`), nil
				}
				return []byte(`[]`), nil
			}
			installed = true
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	first := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "static")["installedAt"]
	if first == nil {
		t.Fatal("first reconcile did not record installedAt")
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	second := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "static")["installedAt"]
	if second != first {
		t.Fatalf("installedAt changed for unchanged route: first=%v second=%v", first, second)
	}
}

func TestIPv4RouteControllerSkipsUnchangedKernelRoute(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: "default",
			},
			Spec: api.IPv4RouteSpec{
				Destination: "0.0.0.0/0",
				Device:      "ens18",
			},
		},
	}}}
	store := mapStore{}
	var commands [][]string
	installed := false
	controller := IPv4RouteController{
		Router:       router,
		Store:        store,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "-j", "-d", "-4", "route", "show", "default"}) {
				if installed {
					return []byte(`[{"dst":"default","dev":"ens18","protocol":"boot"}]`), nil
				}
				return []byte(`[]`), nil
			}
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "route", "add", "0.0.0.0/0", "dev", "ens18", "proto", "boot"}) {
				installed = true
			}
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	want := [][]string{
		{"ip", "route", "add", "0.0.0.0/0", "dev", "ens18", "proto", "boot"},
	}
	if !reflect.DeepEqual(ordinaryMutationCommands(commands), want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "default")
	if changed := status["changed"]; changed != false {
		t.Fatalf("second reconcile should preserve unchanged status, changed=%#v", changed)
	}
	if status["kernelRouteAlreadyCurrent"] != true {
		t.Fatalf("second reconcile should skip route replace, status=%#v", status)
	}
}

func TestIPv4RouteControllerInstallsPreferredSource(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: "delivery",
			},
			Spec: api.IPv4RouteSpec{
				Destination:     "10.77.60.11/32",
				Device:          "wg-hybrid",
				PreferredSource: "10.77.60.10",
				Metric:          120,
			},
		},
	}}}
	store := mapStore{}
	var commands [][]string
	installed := false
	controller := IPv4RouteController{
		Router:       router,
		Store:        store,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			call := append([]string{name}, args...)
			if reflect.DeepEqual(call, []string{"ip", "-j", "-4", "addr", "show"}) {
				return []byte(`[{"addr_info":[{"family":"inet","local":"10.77.60.10"}]}]`), nil
			}
			if reflect.DeepEqual(call, []string{"ip", "-j", "-d", "-4", "route", "show", "10.77.60.11/32"}) {
				if installed {
					return []byte(`[{"dst":"10.77.60.11","dev":"wg-hybrid","prefsrc":"10.77.60.10","metric":120,"protocol":"boot"}]`), nil
				}
				return []byte(`[]`), nil
			}
			if reflect.DeepEqual(call, []string{"ip", "route", "add", "10.77.60.11/32", "dev", "wg-hybrid", "src", "10.77.60.10", "metric", "120", "proto", "boot"}) {
				installed = true
			}
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	want := [][]string{
		{"ip", "route", "add", "10.77.60.11/32", "dev", "wg-hybrid", "src", "10.77.60.10", "metric", "120", "proto", "boot"},
	}
	if !reflect.DeepEqual(ordinaryMutationCommands(commands), want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "delivery")
	if status["preferredSource"] != "10.77.60.10" || status["kernelRouteAlreadyCurrent"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4RouteControllerSkipsNonLocalPreferredSource(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: "delivery",
			},
			Spec: api.IPv4RouteSpec{
				Destination:     "10.77.60.11/32",
				Device:          "wg-hybrid",
				PreferredSource: "10.77.60.12",
				Metric:          120,
			},
		},
	}}}
	store := mapStore{}
	var commands [][]string
	installed := false
	controller := IPv4RouteController{
		Router:       router,
		Store:        store,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if reflect.DeepEqual(args, []string{"-j", "-4", "addr", "show"}) {
				return []byte(`[{"addr_info":[{"family":"inet","local":"10.77.60.10"}]}]`), nil
			}
			if len(args) > 1 && args[0] == "route" && args[1] == "add" {
				installed = true
				return nil, nil
			}
			if len(args) > 4 && args[0] == "-j" && installed {
				return []byte(`[{"dst":"10.77.60.11","dev":"wg-hybrid","metric":120,"protocol":"boot"}]`), nil
			}
			return []byte(`[]`), nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := [][]string{
		{"ip", "route", "add", "10.77.60.11/32", "dev", "wg-hybrid", "metric", "120", "proto", "boot"},
	}
	if !reflect.DeepEqual(ordinaryMutationCommands(commands), want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "delivery")
	if status["preferredSourceSkipped"] != true || status["preferredSourceSkipReason"] != "LocalAddressMissing" {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4RouteControllerWaitsForDeviceBeforeApply(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: "default",
			},
			Spec: api.IPv4RouteSpec{
				Destination: "0.0.0.0/0",
				Device:      "ds-routerd-test",
			},
		},
	}}}
	store := mapStore{}
	var called bool
	controller := IPv4RouteController{
		Router:       router,
		Store:        store,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return 0, false, nil },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			called = true
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if called {
		t.Fatal("route command should not run before the referenced device exists")
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "default")
	if status["phase"] != "Pending" || status["reason"] != "DeviceNotReady" {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4RouteControllerDeletesRemovedRoute(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store := &routeCleanupStore{statuses: []routerstate.ObjectStatus{
		{
			APIVersion: api.NetAPIVersion,
			Kind:       "IPv4Route",
			Name:       "dslite-a-healthcheck",
			Status: ownedRouteStatusForTest(map[string]any{
				"phase":       "Installed",
				"type":        "unicast",
				"destination": "1.1.1.1/32",
				"device":      "ds-lite-a",
				"metric":      10,
			}),
		},
	}}
	var commands [][]string
	deleted := false
	controller := IPv4RouteController{
		Router:       &api.Router{Spec: api.RouterSpec{Resources: nil}},
		Store:        store,
		NativeScope:  ordinaryFixtureScope,
		DeviceLookup: ordinaryFixtureDevice,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if len(args) > 1 && args[0] == "route" && args[1] == "del" {
				deleted = true
				return nil, nil
			}
			if reflect.DeepEqual(args, []string{"-j", "-d", "-4", "route", "show", "1.1.1.1/32"}) {
				if deleted {
					return []byte(`[]`), nil
				}
				return []byte(`[{"dst":"1.1.1.1","dev":"ds-lite-a","metric":10,"protocol":"boot"}]`), nil
			}
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	want := [][]string{
		{"ip", "route", "del", "1.1.1.1/32", "dev", "ds-lite-a", "metric", "10", "proto", "boot"},
	}
	if !reflect.DeepEqual(ordinaryMutationCommands(commands), want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	if !store.deleted["net.routerd.net/v1alpha1/IPv4Route/dslite-a-healthcheck"] {
		t.Fatalf("removed route status was not deleted")
	}
}

func TestIPv4RouteControllerDoesNotDeleteRemovedRouteWhenKernelRouteIsBGP(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store := &routeCleanupStore{statuses: []routerstate.ObjectStatus{
		{
			APIVersion: api.NetAPIVersion,
			Kind:       "IPv4Route",
			Name:       "stale-sam-delivery",
			Status: ownedRouteStatusForTest(map[string]any{
				"phase":       "Installed",
				"type":        "unicast",
				"destination": "10.77.70.5/32",
				"device":      "samt2d56b60b72f",
				"metric":      200,
			}),
		},
	}}
	var commands [][]string
	controller := IPv4RouteController{
		Router:       &api.Router{Spec: api.RouterSpec{Resources: nil}},
		Store:        store,
		NativeScope:  ordinaryFixtureScope,
		DeviceLookup: ordinaryFixtureDevice,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			command := append([]string{name}, args...)
			commands = append(commands, command)
			if reflect.DeepEqual(command, []string{"ip", "-j", "-d", "-4", "route", "show", "10.77.70.5/32"}) {
				return []byte(`[{"dst":"10.77.70.5","gateway":"10.255.70.112","dev":"samt2d56b60b72f","protocol":"bgp","prefsrc":"10.77.70.50","metric":200}]`), nil
			}
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(commands) != 1 || !reflect.DeepEqual(commands[0], []string{"ip", "-j", "-d", "-4", "route", "show", "10.77.70.5/32"}) {
		t.Fatalf("commands = %#v, want only route show", commands)
	}
	if !store.deleted["net.routerd.net/v1alpha1/IPv4Route/stale-sam-delivery"] {
		t.Fatalf("removed route status was not deleted")
	}
}

func TestIPv4RouteControllerDeletesRemovedLinkRouteWhenBGPRouteSharesDestination(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store := &routeCleanupStore{statuses: []routerstate.ObjectStatus{
		{
			APIVersion: api.NetAPIVersion,
			Kind:       "IPv4Route",
			Name:       "sam-cloudedge-local-10-77-60-12",
			Status: ownedRouteStatusForTest(map[string]any{
				"phase":       "Installed",
				"type":        "unicast",
				"destination": "10.77.60.12/32",
				"device":      "ens5",
				"metric":      1,
			}),
		},
	}}
	var commands [][]string
	deleted := false
	controller := IPv4RouteController{
		Router:       &api.Router{Spec: api.RouterSpec{Resources: nil}},
		Store:        store,
		NativeScope:  ordinaryFixtureScope,
		DeviceLookup: ordinaryFixtureDevice,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			command := append([]string{name}, args...)
			commands = append(commands, command)
			if len(args) > 1 && args[0] == "route" && args[1] == "del" {
				deleted = true
				return nil, nil
			}
			if reflect.DeepEqual(command, []string{"ip", "-j", "-d", "-4", "route", "show", "10.77.60.12/32"}) {
				if deleted {
					return []byte(`[{"dst":"10.77.60.12","gateway":"10.255.70.5","dev":"samt54927d44912","protocol":"bgp","metric":200}]`), nil
				}
				return []byte(`[{"dst":"10.77.60.12","dev":"ens5","protocol":"boot","metric":1},{"dst":"10.77.60.12","gateway":"10.255.70.5","dev":"samt54927d44912","protocol":"bgp","metric":200}]`), nil
			}
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	want := [][]string{
		{"ip", "route", "del", "10.77.60.12/32", "dev", "ens5", "metric", "1", "proto", "boot"},
	}
	if !reflect.DeepEqual(ordinaryMutationCommands(commands), want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	if !store.deleted["net.routerd.net/v1alpha1/IPv4Route/sam-cloudedge-local-10-77-60-12"] {
		t.Fatalf("removed route status was not deleted")
	}
}

func TestFreeBSDIPv4RouteHostCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "1.1.1.1/32", "gif41", "", "")
	want := []string{"-n", "change", "-host", "1.1.1.1", "-interface", "gif41"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

func TestFreeBSDIPv4RouteDefaultDSLiteCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "0.0.0.0/0", "gif41", "", "")
	want := []string{"-n", "change", "default", "-interface", "gif41"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

func TestFreeBSDIPv4RoutePreferredSourceCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "10.77.60.11/32", "wg0", "", "10.77.60.10")
	want := []string{"-n", "change", "-host", "10.77.60.11", "-interface", "wg0", "-ifa", "10.77.60.10"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

func TestFreeBSDIPv4RouteRemovalDeleteCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteRemovalDeleteCommand(ipv4RouteRemoval{Destination: "10.77.60.11/32"})
	want := []string{"-n", "delete", "-host", "10.77.60.11"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

type routeCleanupStore struct {
	statuses []routerstate.ObjectStatus
	deleted  map[string]bool
}

func ownedRouteStatusForTest(status map[string]any) map[string]any {
	status["routeOwner"] = ipv4RouteReceipt{Route: ipv4RouteRemovalFromStatus(status), Scope: "linux:fixture:1", IfIndex: 17}
	status["managedBy"] = "routerd"
	return status
}

func (s *routeCleanupStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	return nil
}

func (s *routeCleanupStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return map[string]any{}
}

func (s *routeCleanupStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}

func (s *routeCleanupStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	return s.statuses, nil
}

func (s *routeCleanupStore) DeleteObject(apiVersion, kind, name string) error {
	if s.deleted == nil {
		s.deleted = map[string]bool{}
	}
	s.deleted[apiVersion+"/"+kind+"/"+name] = true
	return nil
}

func ordinaryMutationCommands(commands [][]string) [][]string {
	var writes [][]string
	for _, command := range commands {
		if len(command) > 2 && command[1] == "route" && (command[2] == "add" || command[2] == "del") {
			writes = append(writes, command)
		}
	}
	return writes
}

func TestIPv4RouteControllerAppliesAndWithdrawsIPv6StaticRoute(t *testing.T) {
	for _, tc := range []struct {
		name, configured, observed string
		metric                     int
	}{
		{"prefix", "2001:db8:10::/64", "2001:db8:10::/64", 42},
		{"host", "2001:db8:10::99/128", "2001:db8:10::99", 42},
		{"default", "::/0", "default", 42},
		{"default metric", "2001:db8:10::/64", "2001:db8:10::/64", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testIPv6StaticRouteLifecycle(t, tc.configured, tc.observed, tc.metric)
		})
	}
}

func testIPv6StaticRouteLifecycle(t *testing.T, configured, observed string, metric int) {
	t.Helper()
	kernelMetric := metric
	if kernelMetric == 0 {
		kernelMetric = 1024
	}
	route := ipv6StaticRoute{Destination: configured, Gateway: "fe80::1", Device: "lan0", Metric: strconv.Itoa(kernelMetric)}
	installed := false
	var commands [][]string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		if name != "ip" {
			t.Fatalf("command = %s %v", name, args)
		}
		if reflect.DeepEqual(args, []string{"-j", "-d", "-6", "route", "show", route.Destination}) {
			if !installed {
				return []byte("[]"), nil
			}
			return []byte(fmt.Sprintf(`[{"dst":%q,"gateway":"fe80::1","dev":"lan0","metric":%d,"protocol":"boot"}]`, observed, kernelMetric)), nil
		}
		if reflect.DeepEqual(args, []string{"-6", "route", "add", route.Destination, "via", route.Gateway, "dev", route.Device, "metric", route.Metric, "proto", "boot"}) || (metric == 0 && reflect.DeepEqual(args, []string{"-6", "route", "add", route.Destination, "via", route.Gateway, "dev", route.Device, "proto", "boot"})) {
			installed = true
			return nil, nil
		}
		if reflect.DeepEqual(args, []string{"-6", "route", "del", route.Destination, "via", route.Gateway, "dev", route.Device, "metric", route.Metric, "proto", "boot"}) {
			installed = false
			return nil, nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return nil, nil
	}
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6StaticRouteSpec{Interface: "lan", Destination: route.Destination, Via: route.Gateway, Metric: metric}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: route.Device}}, resource,
	}}}
	store := mapStore{}
	controller := IPv4RouteController{
		Router: router, Store: store, Command: command,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return 7, true, nil },
		NativeScope:  func(context.Context) (string, error) { return "test-scope", nil },
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("install reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6StaticRoute", "lan-v6")
	if status["phase"] != "Installed" || status["device"] != route.Device || status["gateway"] != route.Gateway || status["routeOwner"] == nil {
		t.Fatalf("installed status = %#v", status)
	}
	if !installed {
		t.Fatal("route was not installed")
	}

	controller.Router = &api.Router{}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("withdraw reconcile: %v", err)
	}
	if installed {
		t.Fatal("route was not withdrawn")
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "IPv6StaticRoute", "lan-v6"); len(status) != 0 {
		t.Fatalf("removed route status = %#v", status)
	}
	if len(commands) < 5 {
		t.Fatalf("commands = %#v, want observation, add, readback, delete, readback", commands)
	}
}
