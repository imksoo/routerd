// SPDX-License-Identifier: BSD-3-Clause

package ingressservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"routerd/pkg/api"
)

type mapStore map[string]map[string]any

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
	if status["phase"] != "Degraded" {
		t.Fatalf("phase = %#v, status=%#v", status["phase"], status)
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

func ingressRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
			Metadata: api.ObjectMeta{Name: "api"},
			Spec: api.IngressServiceSpec{
				Listen: api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
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
