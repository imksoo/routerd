// SPDX-License-Identifier: BSD-3-Clause

package ingressservice

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type failedSourceStore struct {
	*routerstate.SQLiteStore
	failure error
	saves   int
}

func (s *failedSourceStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == "VirtualAddress" {
		return nil, s.failure
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

func (s *failedSourceStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	s.saves++
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestSourceReadFailurePreservesSelectedIngressBackend(t *testing.T) {
	for _, field := range []string{"listen", "backend"} {
		t.Run(field, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			previous := map[string]any{"phase": "Active", "listenAddress": "192.0.2.2", "activeBackend": map[string]any{"name": "api", "address": "192.0.2.10", "port": float64(443)}}
			if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "service", previous); err != nil {
				t.Fatal(err)
			}
			spec := api.IngressServiceSpec{Backends: []api.IngressBackendSpec{{Name: "api", Address: "192.0.2.10", Port: 443}}}
			source := api.StatusValueSourceSpec{Resource: "VirtualAddress/uplink", Field: "address", Optional: true}
			if field == "listen" {
				spec.Listen.AddressFrom = source
			} else {
				spec.Backends[0].Address = ""
				spec.Backends[0].AddressFrom = source
			}
			failure := errors.New("source query failed")
			fault := &failedSourceStore{SQLiteStore: store, failure: failure}
			checks := 0
			controller := Controller{Store: fault, Check: func(context.Context, string, int, time.Duration) error {
				checks++
				return nil
			}}
			if err := controller.reconcileResource(t.Context(), "service", spec); !errors.Is(err, failure) || checks != 0 || fault.saves != 0 {
				t.Fatalf("failed optional source changed ingress selection: checks=%d saves=%d err=%v", checks, fault.saves, err)
			}
			current, err := store.ReadObjectStatus(api.FirewallAPIVersion, "IngressService", "service")
			if err != nil || !reflect.DeepEqual(current, previous) {
				t.Fatalf("failed observation replaced selected backend: %v err=%v", current, err)
			}
		})
	}
}
