// SPDX-License-Identifier: BSD-3-Clause

package ingressservice

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestChangedBackendTargetDoesNotInheritPreviousHealth(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "ingress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := Controller{Store: store, Check: func(context.Context, string, int, time.Duration) error { return nil }}
	spec := api.IngressServiceSpec{Backends: []api.IngressBackendSpec{{Name: "api", Address: "192.0.2.10", Port: 443}}, HealthCheck: api.IngressHealthCheckSpec{UnhealthyThreshold: 2}}
	if err := controller.reconcileResource(t.Context(), "service", spec); err != nil {
		t.Fatal(err)
	}
	controller.Check = func(context.Context, string, int, time.Duration) error { return errors.New("new target unavailable") }
	spec.Backends[0].Address = "192.0.2.11"
	if err := controller.reconcileResource(t.Context(), "service", spec); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "service")
	if status["phase"] != "NoHealthyBackends" {
		t.Fatalf("new failing target inherited old healthy state: %+v", status)
	}
}

func TestChangedBackendNameCannotReuseOldResolvedAddressOnDNSFailure(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "ingress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "service", map[string]any{"backends": []backendStatus{{Name: "api", Address: "old.invalid", Port: 443, ResolvedAddress: "192.0.2.10", Healthy: true, HealthyCount: 3}}}); err != nil {
		t.Fatal(err)
	}
	checks := 0
	controller := Controller{Store: store, Resolver: &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("DNS unavailable") }}, Check: func(context.Context, string, int, time.Duration) error { checks++; return nil }}
	spec := api.IngressServiceSpec{Backends: []api.IngressBackendSpec{{Name: "api", Address: "new.invalid", Port: 443}}}
	if err := controller.reconcileResource(t.Context(), "service", spec); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "IngressService", "service")
	if checks != 0 || status["phase"] != "NoHealthyBackends" {
		t.Fatalf("changed target used old IP: checks=%d status=%+v", checks, status)
	}
}
