// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestIPv4RouteControllerInstallsBlackholeRouteInDryRun(t *testing.T) {
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
	if status["phase"] != "Installed" {
		t.Fatalf("phase = %v, want Installed", status["phase"])
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
	controller := IPv4RouteController{Router: router, Store: store, DryRun: true}

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
		Router: router,
		Store:  store,
		DevicePresent: func(context.Context, string) bool {
			return true
		},
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "route", "show", "default"}) {
				if installed {
					return []byte("default dev ens18\n"), nil
				}
				return nil, nil
			}
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "route", "replace", "0.0.0.0/0", "dev", "ens18"}) {
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
		{"ip", "route", "show", "default"},
		{"ip", "route", "replace", "0.0.0.0/0", "dev", "ens18"},
		{"ip", "route", "show", "default"},
	}
	if !reflect.DeepEqual(commands, want) {
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
		Router: router,
		Store:  store,
		DevicePresent: func(context.Context, string) bool {
			return true
		},
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			call := append([]string{name}, args...)
			if reflect.DeepEqual(call, []string{"ip", "route", "show", "10.77.60.11/32"}) {
				if installed {
					return []byte("10.77.60.11/32 dev wg-hybrid src 10.77.60.10 metric 120\n"), nil
				}
				return nil, nil
			}
			if reflect.DeepEqual(call, []string{"ip", "route", "replace", "10.77.60.11/32", "dev", "wg-hybrid", "src", "10.77.60.10", "metric", "120"}) {
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
		{"ip", "route", "show", "10.77.60.11/32"},
		{"ip", "route", "replace", "10.77.60.11/32", "dev", "wg-hybrid", "src", "10.77.60.10", "metric", "120"},
		{"ip", "route", "show", "10.77.60.11/32"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "delivery")
	if status["preferredSource"] != "10.77.60.10" || status["kernelRouteAlreadyCurrent"] != true {
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
		Router: router,
		Store:  store,
		DevicePresent: func(context.Context, string) bool {
			return false
		},
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
	store := &routeCleanupStore{statuses: []routerstate.ObjectStatus{
		{
			APIVersion: api.NetAPIVersion,
			Kind:       "IPv4Route",
			Name:       "dslite-a-healthcheck",
			Status: map[string]any{
				"phase":       "Installed",
				"type":        "unicast",
				"destination": "1.1.1.1/32",
				"device":      "ds-lite-a",
				"metric":      10,
			},
		},
	}}
	var commands [][]string
	controller := IPv4RouteController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: nil}},
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return nil, nil
		},
	}

	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	want := []string{"ip", "route", "del", "1.1.1.1/32", "dev", "ds-lite-a", "metric", "10"}
	if len(commands) != 1 || !reflect.DeepEqual(commands[0], want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	if !store.deleted["net.routerd.net/v1alpha1/IPv4Route/dslite-a-healthcheck"] {
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

type routeCleanupStore struct {
	statuses []routerstate.ObjectStatus
	deleted  map[string]bool
}

func (s *routeCleanupStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	return nil
}

func (s *routeCleanupStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return map[string]any{}
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
