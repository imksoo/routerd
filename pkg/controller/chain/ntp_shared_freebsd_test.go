//go:build freebsd

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNTPSharedNTPDWhenFalseRetainsOneConfigOwnerAndClientUpstreams(t *testing.T) {
	for _, initiallyActive := range []bool{true, false} {
		t.Run(map[bool]string{true: "active-to-backup", false: "initial-backup"}[initiallyActive], func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			role := "backup"
			if initiallyActive {
				role = "master"
			}
			if _, err := store.Set("ntp-role", role, "test-owned role observation"); err != nil {
				t.Fatal(err)
			}
			declared := ntpRouter(api.NTPClientSpec{Provider: "ntpd", Managed: true, Servers: []string{"192.0.2.123"}})
			serverRouter := ntpServerRouter(api.NTPServerSpec{Provider: "ntpd", Managed: true, Servers: []string{"192.0.2.200"}, ListenAddresses: []string{"198.51.100.1"}, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ntp-role": {Equals: "master"}}}})
			declared.Spec.Resources = append(declared.Spec.Resources, serverRouter.Spec.Resources...)
			path := filepath.Join(t.TempDir(), "shared-ntp.conf")
			command := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
			run := func() {
				t.Helper()
				effective := mustWhenFilteredRouter(t, false, declared, store)
				client := NTPClientController{Router: effective, DeclaredRouter: declared, Store: store, ConfigPath: path, Command: command}
				server := NTPServerController{Router: effective, DeclaredRouter: declared, Store: store, ConfigPath: path, Command: command}
				if err := client.Reconcile(t.Context()); err != nil {
					t.Fatalf("client before shared owner: %v", err)
				}
				if err := server.Reconcile(t.Context()); err != nil {
					t.Fatalf("shared owner: %v", err)
				}
				if err := client.Reconcile(t.Context()); err != nil {
					t.Fatalf("client after shared owner: %v", err)
				}
			}
			if initiallyActive {
				run()
				if _, err := store.Set("ntp-role", "backup", "authoritative role transition"); err != nil {
					t.Fatal(err)
				}
			}
			run()
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), "server 192.0.2.123 iburst\n") || strings.Contains(string(data), "server 192.0.2.200 iburst\n") || strings.Contains(string(data), "interface listen 198.51.100.1\n") {
				t.Fatalf("inactive server did not retain active client config=%q err=%v", data, err)
			}
			clientStatus, err := store.ReadObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
			if err != nil {
				t.Fatal(err)
			}
			if clientStatus["phase"] != "Applied" || clientStatus["managedBy"] != "NTPServer" {
				t.Fatalf("client not confirmed through shared owner: %+v", clientStatus)
			}
			clientRecords, err := (ntpConfigOwner{store: store, kind: "NTPClient"}).read()
			if err != nil {
				t.Fatal(err)
			}
			serverRecords, err := (ntpConfigOwner{store: store, kind: "NTPServer"}).read()
			if err != nil {
				t.Fatal(err)
			}
			if len(clientRecords) != 0 || len(serverRecords) != 1 || serverRecords[0].ReloadPending {
				t.Fatalf("shared writer was not singular/confirmed: clients=%+v servers=%+v", clientRecords, serverRecords)
			}
		})
	}
}

func TestNTPSharedNTPDDelegationCannotClaimUnappliedServer(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := ntpRouter(api.NTPClientSpec{Provider: "ntpd", Managed: true, Servers: []string{"192.0.2.123"}})
	router.Spec.Resources = append(router.Spec.Resources, ntpServerRouter(api.NTPServerSpec{Provider: "ntpd", Managed: true, Servers: []string{"192.0.2.200"}}).Spec.Resources...)
	effects := 0
	controller := NTPClientController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: filepath.Join(t.TempDir(), "shared.conf"), Command: func(context.Context, string, ...string) ([]byte, error) { effects++; return nil, nil }}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
	if err != nil {
		t.Fatal(err)
	}
	if status["phase"] == "Applied" || effects != 0 {
		t.Fatalf("unapplied shared server granted client success: status=%+v effects=%d", status, effects)
	}
}
