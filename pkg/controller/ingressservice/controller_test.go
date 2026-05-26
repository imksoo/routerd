// SPDX-License-Identifier: BSD-3-Clause

package ingressservice

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/ingressdrain"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mapStore map[string]map[string]any

var testNow = time.Date(2026, 5, 19, 1, 30, 0, 0, time.UTC)

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func (s mapStore) Get(name string) routerstate.Value {
	status := s[api.RouterAPIVersion+"/State/"+name]
	value, _ := status["value"].(routerstate.Value)
	if value.Status == "" {
		return routerstate.Value{Status: routerstate.StatusUnknown, Since: testNow, UpdatedAt: testNow}
	}
	return value
}

func (s mapStore) Set(name, value, reason string) routerstate.Value {
	next := routerstate.Value{Status: routerstate.StatusSet, Value: value, Reason: reason, Since: testNow, UpdatedAt: testNow}
	s[api.RouterAPIVersion+"/State/"+name] = map[string]any{"value": next}
	return next
}

func (s mapStore) Unset(name, reason string) routerstate.Value {
	next := routerstate.Value{Status: routerstate.StatusUnset, Reason: reason, Since: testNow, UpdatedAt: testNow}
	s[api.RouterAPIVersion+"/State/"+name] = map[string]any{"value": next}
	return next
}

func (s mapStore) Forget(name, reason string) routerstate.Value {
	next := routerstate.Value{Status: routerstate.StatusUnknown, Reason: reason, Since: testNow, UpdatedAt: testNow}
	s[api.RouterAPIVersion+"/State/"+name] = map[string]any{"value": next}
	return next
}

func (s mapStore) Delete(name string) {
	delete(s, api.RouterAPIVersion+"/State/"+name)
}

func (s mapStore) Age(name string) time.Duration {
	return s.Now().Sub(s.Get(name).Since)
}

func (s mapStore) Now() time.Time { return testNow }

func (s mapStore) Save(string) error { return nil }

func (s mapStore) Variables() map[string]routerstate.Value {
	return nil
}

func TestReconcileIngressServiceSelectsHealthyFailoverBackend(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: ingressRouter(),
		Store:  store,
		Check: func(_ context.Context, address string, _ int, _ time.Duration) error {
			if address == "10.0.0.11" {
				return errors.New("down")
			}
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	active, _ := status["activeBackend"].(map[string]any)
	if active["name"] != "cp-02" || active["address"] != "10.0.0.12" {
		t.Fatalf("active backend = %#v, status=%#v", active, status)
	}
	if status["hostname"] != "k8s-api.lain.local" {
		t.Fatalf("hostname status = %#v", status)
	}
	if status["lastActiveBackendTransitionAt"] == "" {
		t.Fatalf("missing active backend transition timestamp: %#v", status)
	}
	if status["phase"] != "Degraded" {
		t.Fatalf("phase = %#v, status=%#v", status["phase"], status)
	}
}

func TestReconcileIngressServiceDrainsBackend(t *testing.T) {
	store := mapStore{}
	if _, err := ingressdrain.Drain(store, "api", "cp-01", 10*time.Minute); err != nil {
		t.Fatalf("drain: %v", err)
	}
	controller := Controller{
		Router: ingressRouter(),
		Store:  store,
		Check: func(_ context.Context, _ string, _ int, _ time.Duration) error {
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	active, _ := status["activeBackend"].(map[string]any)
	if active["name"] != "cp-02" {
		t.Fatalf("active backend = %#v, status=%#v", active, status)
	}
	if status["drainedBackends"] != 1 {
		t.Fatalf("drainedBackends = %#v, status=%#v", status["drainedBackends"], status)
	}
	backends := status["backends"].([]backendStatus)
	if !backends[0].Drained || backends[0].Healthy || backends[0].Reason != "Drained" || backends[0].DrainedUntil == "" {
		t.Fatalf("drained backend = %#v", backends[0])
	}
}

func TestReconcileIngressServiceUndrainRestoresBackend(t *testing.T) {
	store := mapStore{}
	if _, err := ingressdrain.Drain(store, "api", "cp-01", 0); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if err := ingressdrain.Undrain(store, "api", "cp-01"); err != nil {
		t.Fatalf("undrain: %v", err)
	}
	controller := Controller{
		Router: ingressRouter(),
		Store:  store,
		Check: func(_ context.Context, _ string, _ int, _ time.Duration) error {
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["phase"] != "Active" || status["drainedBackends"] != 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestReconcileIngressServiceExpiresDrain(t *testing.T) {
	store := mapStore{}
	state := ingressdrain.State{
		Service:      "api",
		Backend:      "cp-01",
		DrainedAt:    testNow.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		DrainedUntil: testNow.Add(-time.Minute).Format(time.RFC3339Nano),
	}
	data, _ := json.Marshal(state)
	store.Set(ingressdrain.Key("api", "cp-01"), string(data), "test")
	controller := Controller{
		Router: ingressRouter(),
		Store:  store,
		Check: func(_ context.Context, _ string, _ int, _ time.Duration) error {
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["phase"] != "Active" || status["drainedBackends"] != 0 {
		t.Fatalf("status = %#v", status)
	}
	if current := store.Get(ingressdrain.Key("api", "cp-01")); current.Status == routerstate.StatusSet {
		t.Fatalf("expired drain was not deleted: %#v", current)
	}
}

func TestReconcileIngressServiceSourceHashDistributesHealthyBackends(t *testing.T) {
	store := mapStore{}
	router := ingressRouter()
	spec := router.Spec.Resources[0].Spec.(api.IngressServiceSpec)
	spec.Policy.Selection = "sourceHash"
	router.Spec.Resources[0].Spec = spec
	controller := Controller{
		Router: router,
		Store:  store,
		Check: func(_ context.Context, _ string, _ int, _ time.Duration) error {
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["selection"] != "sourceHash" || status["effectiveSelection"] != "sourceHash" {
		t.Fatalf("selection status = %#v", status)
	}
	activeBackends, ok := status["activeBackends"].([]map[string]any)
	if !ok || len(activeBackends) != 2 {
		t.Fatalf("activeBackends = %#v, status=%#v", status["activeBackends"], status)
	}
}

func TestReconcileIngressServiceSourceHashFallsBackWhenOnlyOneBackendHealthy(t *testing.T) {
	store := mapStore{}
	router := ingressRouter()
	spec := router.Spec.Resources[0].Spec.(api.IngressServiceSpec)
	spec.Policy.Selection = "sourceHash"
	router.Spec.Resources[0].Spec = spec
	controller := Controller{
		Router: router,
		Store:  store,
		Check: func(_ context.Context, address string, _ int, _ time.Duration) error {
			if address == "10.0.0.11" {
				return errors.New("down")
			}
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["selection"] != "sourceHash" || status["effectiveSelection"] != "failover" {
		t.Fatalf("selection status = %#v", status)
	}
	active, _ := status["activeBackend"].(map[string]any)
	if active["name"] != "cp-02" || active["address"] != "10.0.0.12" {
		t.Fatalf("active backend = %#v, status=%#v", active, status)
	}
	activeBackends, ok := status["activeBackends"].([]map[string]any)
	if !ok || len(activeBackends) != 1 || activeBackends[0]["name"] != "cp-02" {
		t.Fatalf("activeBackends = %#v, status=%#v", status["activeBackends"], status)
	}
}

func TestReconcileIngressServiceResolvesBackendAddressFromStatus(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/IPv4StaticAddress/cp-01": {"address": "10.0.0.11/32"},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
			Metadata: api.ObjectMeta{Name: "api"},
			Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
				Backends: []api.IngressBackendSpec{{Name: "cp-01", AddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/cp-01", Field: "address"}, Port: 6443}},
			},
		},
	}}}
	controller := Controller{Router: router, Store: store, DryRun: true}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	active, _ := status["activeBackend"].(map[string]any)
	if active["address"] != "10.0.0.11" {
		t.Fatalf("active backend = %#v, status=%#v", active, status)
	}
}

func TestReconcileIngressServiceChecksHTTPReadyz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" || r.Host != "k8s-api.lain.local" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := netSplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
			Metadata: api.ObjectMeta{Name: "api"},
			Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
				Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: host, Port: port}},
				HealthCheck: api.IngressHealthCheckSpec{
					Protocol:       "http",
					Path:           "/readyz",
					Host:           "k8s-api.lain.local",
					ExpectedStatus: []int{http.StatusOK},
					ExpectedBody:   "ok",
				},
			},
		},
	}}}
	controller := Controller{Router: router, Store: store}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["phase"] != "Active" {
		t.Fatalf("status = %#v", status)
	}
	backends, ok := status["backends"].([]backendStatus)
	if !ok || len(backends) != 1 || !backends[0].Healthy || backends[0].LastHealthyAt == "" {
		t.Fatalf("backend status = %#v", status["backends"])
	}
}

func TestReconcileIngressServiceHonorsHealthThresholds(t *testing.T) {
	store := mapStore{}
	router := ingressRouter()
	spec := router.Spec.Resources[0].Spec.(api.IngressServiceSpec)
	spec.HealthCheck.HealthyThreshold = 2
	router.Spec.Resources[0].Spec = spec
	controller := Controller{
		Router: router,
		Store:  store,
		Check: func(_ context.Context, _ string, _ int, _ time.Duration) error {
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["phase"] != "NoHealthyBackends" {
		t.Fatalf("first status = %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	status = store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "api")
	if status["phase"] != "Active" {
		t.Fatalf("second status = %#v", status)
	}
}

func netSplitHostPort(value string) (string, string, error) {
	host, port, err := net.SplitHostPort(value)
	return host, port, err
}

func ingressRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
			Metadata: api.ObjectMeta{Name: "api"},
			Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
				Hostname: "k8s-api.lain.local",
				Backends: []api.IngressBackendSpec{
					{Name: "cp-01", Address: "10.0.0.11", Port: 6443},
					{Name: "cp-02", Address: "10.0.0.12", Port: 6443},
				},
				HealthCheck: api.IngressHealthCheckSpec{Protocol: "tcp", Timeout: "50ms"},
				Policy:      api.IngressServicePolicySpec{Selection: "failover"},
			},
		},
	}}}
}
