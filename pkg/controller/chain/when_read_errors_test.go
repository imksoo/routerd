// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestConditionalAddressStopsBeforeCommandsOnFailedStateRead(t *testing.T) {
	for _, failure := range []string{"closed", "corrupt"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if _, err := store.Set("ha.role", "master", "test observation"); err != nil {
				t.Fatal(err)
			}
			if failure == "closed" {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec(`UPDATE objects SET status='{'`); err != nil {
					t.Fatal(err)
				}
			}
			commands := 0
			controller := IPv4StaticAddressController{
				Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
					Metadata: api.ObjectMeta{Name: "lan"},
					Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24", When: api.ResourceWhenSpec{
						State: map[string]api.StateMatchSpec{"ha.role": {MaxAge: "1m"}},
					}},
				}}}},
				Store: store,
				Command: func(context.Context, string, ...string) error {
					commands++
					return nil
				},
			}
			err = controller.Reconcile(context.Background())
			if err == nil || commands != 0 {
				t.Fatalf("read failure reached host effects: commands=%d err=%v", commands, err)
			}
			if failure == "closed" && !errors.Is(err, sql.ErrConnDone) {
				t.Fatalf("lost closed-store cause: %v", err)
			}
		})
	}
}

func mustWhenFilteredRouter(t *testing.T, resolved bool, router *api.Router, store resourcequery.StateStore) *api.Router {
	t.Helper()
	var result *api.Router
	var err error
	if resolved {
		result, err = resourcequery.FilterRouterByResolvedWhen(router, store)
	} else {
		result, err = resourcequery.FilterRouterByWhen(router, store)
	}
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDHCPWhenRouter(t *testing.T, controller DHCPv6ServerController) *api.Router {
	t.Helper()
	result, err := controller.effectiveRouter()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDaemonWhenRouter(t *testing.T, runner *Runner, store eventedStore) *api.Router {
	t.Helper()
	result, err := runner.daemonSupervisionRouter(store)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustVXLANGateState(t *testing.T, controller VXLANTunnelController, resource api.Resource) vxlanGateState {
	t.Helper()
	result, err := controller.gateState(resource)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
