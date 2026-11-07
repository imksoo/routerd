// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestWhenReadFailuresAreErrorsInsteadOfUnknownObservations(t *testing.T) {
	for _, ref := range []string{"ha.role", "VirtualAddress/vip.status.role"} {
		for _, failure := range []string{"closed", "corrupt"} {
			t.Run(ref+"/"+failure, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "state.db")
				store, err := routerstate.OpenSQLite(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Close() })
				if ref == "ha.role" {
					if _, err := store.Set(ref, "master", "test observation"); err != nil {
						t.Fatal(err)
					}
				} else if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip", map[string]any{"role": "master"}); err != nil {
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
				for _, match := range []api.StateMatchSpec{{Status: routerstate.StatusUnknown}, {MaxAge: "1m"}} {
					when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{ref: match}}
					if matches, err := ResourceWhenMatches(when, store); err == nil || matches {
						t.Fatalf("failed read became a match: matches=%t err=%v", matches, err)
					} else if failure == "closed" && !errors.Is(err, sql.ErrConnDone) {
						t.Fatalf("lost closed-store cause: %v", err)
					}
					router := whenTestRouter(when)
					for _, filter := range []func(*api.Router, StateStore) (*api.Router, error){FilterRouterByWhen, FilterRouterByResolvedWhen} {
						if filtered, err := filter(router, store); err == nil || filtered != nil {
							t.Fatalf("failed read produced an effective router: router=%+v err=%v", filtered, err)
						}
					}
				}
			})
		}
	}
}

func TestWhenMissingObservationRemainsUsableAndUnrelatedFailuresAreNarrow(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, ref := range []string{"ha.role", "VirtualAddress/vip.status.role"} {
		when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{ref: {Status: routerstate.StatusUnknown}}}
		filtered, err := FilterRouterByWhen(whenTestRouter(when), store)
		if err != nil || len(filtered.Spec.Resources) != 1 {
			t.Fatalf("missing observation: filtered=%+v err=%v", filtered, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	filtered, err := FilterRouterByWhen(whenTestRouter(api.ResourceWhenSpec{}), store)
	if err != nil || len(filtered.Spec.Resources) != 1 {
		t.Fatalf("unconditional resource required a state read: filtered=%+v err=%v", filtered, err)
	}
}

func whenTestRouter(when api.ResourceWhenSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
		Metadata: api.ObjectMeta{Name: "lan"},
		Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24", When: when},
	}}}}
}

func mustWhenFilteredRouter(t *testing.T, resolved bool, router *api.Router, store StateStore) *api.Router {
	t.Helper()
	var result *api.Router
	var err error
	if resolved {
		result, err = FilterRouterByResolvedWhen(router, store)
	} else {
		result, err = FilterRouterByWhen(router, store)
	}
	if err != nil {
		t.Fatal(err)
	}
	return result
}
