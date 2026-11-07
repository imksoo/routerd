// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dhcpv4CallbackStatusStore struct {
	*routerstate.SQLiteStore
	writes int
}

func (s *dhcpv4CallbackStatusStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if kind == "DHCPv4Client" {
		s.writes++
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestPreparedDHCPv4LeaseCallbacksUseCurrentDeclarationAndStore(t *testing.T) {
	for _, periodic := range []bool{false, true} {
		name := "event"
		if periodic {
			name = "periodic"
		}
		t.Run(name, func(t *testing.T) {
			sqlite, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer sqlite.Close()
			store := &dhcpv4CallbackStatusStore{SQLiteStore: sqlite}
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "accepted"}, Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "uplink"}, Spec: api.InterfaceSpec{IfName: "old0"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan", OwnerRefs: []api.OwnerRef{{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "old-owner"}}}, Spec: api.DHCPv4ClientSpec{Interface: "uplink"}},
			}}}
			if err := config.Validate(router); err != nil {
				t.Fatal(err)
			}
			runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"dhcpv4-lease"}, DryRunDHCPv4Client: true, DaemonSockets: map[string]string{"wan": filepath.Join(t.TempDir(), "unavailable.sock")}}}
			controller := preparedCallback(t, runner)
			reconcile := func() {
				t.Helper()
				before := store.writes
				if periodic {
					if _, err := controller.PeriodicReconcile(context.Background()); err != nil {
						t.Fatal(err)
					}
				} else if err := controller.Reconcile(context.Background(), daemonapi.DaemonEvent{}); err != nil {
					t.Fatal(err)
				}
				if store.writes != before+1 {
					t.Fatalf("one lease callback wrote %d statuses, want one", store.writes-before)
				}
			}
			reconcile()
			next := router.Clone()
			next.Spec.Resources[0].Spec = api.InterfaceSpec{IfName: "new0"}
			next.Spec.Resources[1].Metadata.OwnerRefs[0].Name = "new-owner"
			runner.runtimeSnapshot = RuntimeSnapshot{Known: true, Available: true, Router: router}
			if err := runner.UpdateRuntimeRouter(&next); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				reconcile()
				status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
				if err != nil {
					t.Fatal(err)
				}
				if want := lifecycle.OwnerRefsStatusList(next.Spec.Resources[1].Metadata.OwnerRefs); !reflect.DeepEqual(status["ownerRefs"], want) {
					t.Fatalf("existing lease actor persisted stale ownership: %+v, want %+v", status["ownerRefs"], want)
				}
			}
		})
	}
}
