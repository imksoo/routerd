// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNTPWithdrawalUsesAcquiredConfigurationAfterSQLiteReopen(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(db)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "routerd.conf")
			command := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "systemctl" && len(args) > 0 && (args[0] == "is-enabled" || args[0] == "is-active") {
					return nil, errors.New("inactive")
				}
				return nil, nil
			}
			var router *api.Router
			reconcile := func() error {
				if kind == "NTPClient" {
					return (NTPClientController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: path, Command: command}).Reconcile(t.Context())
				}
				return (NTPServerController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: path, Command: command}).Reconcile(t.Context())
			}
			if kind == "NTPClient" {
				router = ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}})
			} else {
				router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, AllowCIDRs: []string{"192.0.2.0/24"}})
			}
			if err := reconcile(); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "192.0.2.123") {
				t.Fatalf("initial configuration=%q err=%v", data, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = routerstate.OpenSQLite(db)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			router = &api.Router{}
			if err := reconcile(); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("removed %s declaration retained active configuration %q err=%v", kind, data, err)
			}
		})
	}
}

func TestNTPWhenFalseCannotRemoveUnacquiredForeignConfiguration(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.Set("ntp-role", "backup", "test-owned observation"); err != nil {
				t.Fatal(err)
			}
			when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ntp-role": {Equals: "master"}}}
			path := filepath.Join(t.TempDir(), "operator.conf")
			foreign := []byte("# operator-owned configuration\nserver 192.0.2.200 iburst\n")
			if err := os.WriteFile(path, foreign, 0600); err != nil {
				t.Fatal(err)
			}
			effects := 0
			command := func(context.Context, string, ...string) ([]byte, error) { effects++; return nil, nil }
			if kind == "NTPClient" {
				router := ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, When: when})
				_ = (NTPClientController{Router: &api.Router{}, DeclaredRouter: router, Store: store, ConfigPath: path, Command: command}).Reconcile(t.Context())
			} else {
				router := ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, When: when})
				_ = (NTPServerController{Router: &api.Router{}, DeclaredRouter: router, Store: store, ConfigPath: path, Command: command}).Reconcile(t.Context())
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != string(foreign) || effects != 0 {
				t.Fatalf("WhenFalse changed unacquired foreign config=%q err=%v serviceEffects=%d", data, err, effects)
			}
		})
	}
}
