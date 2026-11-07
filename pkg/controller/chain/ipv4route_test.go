// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
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
