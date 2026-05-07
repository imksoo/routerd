package chain

import (
	"context"
	"reflect"
	"testing"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
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
