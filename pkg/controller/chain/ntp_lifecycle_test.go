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

// This fixture owns only SQLite, a private config path, and the existing service
// command boundary. All config ownership and recovery decisions run in Reconcile.
type ntpLifecycleFixture struct {
	kind       string
	db         string
	path       string
	store      *routerstate.SQLiteStore
	router     *api.Router
	commands   []string
	failReload bool
}

func newNTPLifecycleFixture(t *testing.T, kind string) *ntpLifecycleFixture {
	t.Helper()
	f := &ntpLifecycleFixture{kind: kind, db: filepath.Join(t.TempDir(), "state.db"), path: filepath.Join(t.TempDir(), "routerd.conf")}
	var err error
	f.store, err = routerstate.OpenSQLite(f.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.store.Close() })
	if kind == "NTPClient" {
		f.router = ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}})
	} else {
		f.router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, AllowCIDRs: []string{"192.0.2.0/24"}})
	}
	return f
}

func (f *ntpLifecycleFixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = routerstate.OpenSQLite(f.db)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *ntpLifecycleFixture) command(_ context.Context, name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, strings.Join(append([]string{name}, args...), " "))
	if name == "systemctl" && len(args) > 0 && (args[0] == "is-enabled" || args[0] == "is-active") {
		return nil, errors.New("test-owned inactive service observation")
	}
	if f.failReload && len(args) > 0 && args[0] == "restart" {
		return nil, errors.New("test-owned reload failure")
	}
	return nil, nil
}

func (f *ntpLifecycleFixture) reconcile(ctx context.Context) error {
	if f.kind == "NTPClient" {
		return (NTPClientController{Router: f.router, DeclaredRouter: f.router, Store: f.store, ConfigPath: f.path, Command: f.command}).Reconcile(ctx)
	}
	return (NTPServerController{Router: f.router, DeclaredRouter: f.router, Store: f.store, ConfigPath: f.path, Command: f.command}).Reconcile(ctx)
}

func TestNTPManagedAcquirePreservesForeignConfiguration(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			f := newNTPLifecycleFixture(t, kind)
			foreign := []byte("# maintained by the operator\nserver 192.0.2.200 iburst\n")
			if err := os.WriteFile(f.path, foreign, 0600); err != nil {
				t.Fatal(err)
			}
			if err := f.reconcile(t.Context()); err == nil {
				t.Fatal("foreign config was accepted as acquired")
			}
			if got, err := os.ReadFile(f.path); err != nil || string(got) != string(foreign) || len(f.commands) != 0 {
				t.Fatalf("foreign config=%q err=%v commands=%v", got, err, f.commands)
			}
		})
	}
}

func TestNTPAcquiredConfigReplacementSurvivesWithdrawal(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			f := newNTPLifecycleFixture(t, kind)
			if err := f.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			foreign := []byte("# replacement installed by the operator\nserver 192.0.2.200 iburst\n")
			if err := os.WriteFile(f.path, foreign, 0600); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			f.router = &api.Router{}
			f.commands = nil
			if err := f.reconcile(t.Context()); err == nil {
				t.Fatal("foreign replacement was treated as removed")
			}
			if got, err := os.ReadFile(f.path); err != nil || string(got) != string(foreign) || len(f.commands) != 0 {
				t.Fatalf("foreign config=%q err=%v commands=%v", got, err, f.commands)
			}
		})
	}
}

func TestNTPReloadFailureRemainsRetryableAfterSQLiteReopen(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			for _, operation := range []string{"apply", "withdraw"} {
				t.Run(operation, func(t *testing.T) {
					f := newNTPLifecycleFixture(t, kind)
					name := f.router.Spec.Resources[0].Metadata.Name
					if operation == "withdraw" {
						if err := f.reconcile(t.Context()); err != nil {
							t.Fatal(err)
						}
						f.router = &api.Router{}
					}
					f.failReload = true
					f.commands = nil
					if err := f.reconcile(t.Context()); err == nil {
						t.Fatal("failed service reload was reported as applied")
					}
					owner := ntpConfigOwner{store: f.store, kind: kind}
					record, err := owner.get(name)
					if err != nil || record == nil || operation == "apply" && !record.ReloadPending || operation == "withdraw" && !record.Withdrawing {
						t.Fatalf("unfinished receipt=%+v err=%v", record, err)
					}
					if operation == "withdraw" {
						if _, err := os.Stat(f.path); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("owned config removal was not recorded: %v", err)
						}
					}
					f.reopen(t)
					f.failReload = false
					f.commands = nil
					if err := f.reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(strings.Join(f.commands, "\n"), "systemctl restart chrony.service") {
						t.Fatalf("unfinished reload was not retried: %v", f.commands)
					}
					owner.store = f.store
					record, err = owner.get(name)
					if err != nil || operation == "withdraw" && record != nil || operation == "apply" && (record == nil || record.ReloadPending) {
						t.Fatalf("recovered receipt=%+v err=%v", record, err)
					}
				})
			}
		})
	}
}

func TestNTPClientProviderSwitchWithdrawsPreviousFragment(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	f := newNTPLifecycleFixture(t, "NTPClient")
	if err := f.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	previous := f.path
	f.path = filepath.Join(t.TempDir(), "timesyncd.conf")
	f.router = ntpRouter(api.NTPClientSpec{Provider: "systemd-timesyncd", Managed: true, Servers: []string{"192.0.2.124"}})
	f.reopen(t)
	if err := f.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(previous); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old provider fragment remains: %q %v", data, err)
	}
	if data, err := os.ReadFile(f.path); err != nil || !strings.Contains(string(data), "NTP=192.0.2.124\n") {
		t.Fatalf("new provider config=%q err=%v", data, err)
	}
}

func TestNTPCanceledReconcileDoesNotAcquireConfiguration(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			f := newNTPLifecycleFixture(t, kind)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := f.reconcile(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled reconcile=%v", err)
			}
			if _, err := os.Stat(f.path); !errors.Is(err, os.ErrNotExist) || len(f.commands) != 0 {
				t.Fatalf("cancelled owner acquired native effects: %v %v", err, f.commands)
			}
		})
	}
}
