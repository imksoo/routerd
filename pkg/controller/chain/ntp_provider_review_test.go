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
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNTPManagedProvidersDoNotRestartTheirConflictOnEveryTick(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, provider := range []string{"systemd-timesyncd", "chrony"} {
		for _, initiallyActive := range []bool{true, false} {
			name := provider + map[bool]string{true: "/active-to-backup", false: "/initial-backup"}[initiallyActive]
			t.Run(name, func(t *testing.T) {
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
				router := ntpRouter(api.NTPClientSpec{Provider: provider, Managed: true, Servers: []string{"192.0.2.123"}})
				router.Spec.Resources = append(router.Spec.Resources, ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.124"}, AllowCIDRs: []string{"192.0.2.0/24"}, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ntp-role": {Equals: "master"}}}}).Spec.Resources...)
				router.TypeMeta = api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}
				router.Metadata.Name = "ntp-provider-review"
				if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
					t.Fatal(err)
				}
				// Only actual OS observations/effects are replaced. Both real
				// callers own decisions, generated files, SQLite and statuses.
				active := map[string]bool{"systemd-timesyncd.service": true}
				var starts, stops, effects int
				command := func(_ context.Context, name string, args ...string) ([]byte, error) {
					if name != "systemctl" || len(args) < 2 {
						return nil, errors.New("unexpected service boundary command")
					}
					unit := args[len(args)-1]
					switch args[0] {
					case "is-active", "is-enabled":
						if active[unit] {
							return nil, nil
						}
						return nil, errors.New("test-owned inactive observation")
					case "restart", "enable":
						effects++
						if unit == "systemd-timesyncd.service" {
							starts++
						}
						active[unit] = true
					case "disable":
						effects++
						if unit == "systemd-timesyncd.service" {
							stops++
						}
						active[unit] = false
					default:
						return nil, errors.New("unexpected systemctl operation")
					}
					return nil, nil
				}
				clientPath, serverPath := filepath.Join(t.TempDir(), "client.conf"), filepath.Join(t.TempDir(), "server.conf")
				client := NTPClientController{DeclaredRouter: router, Store: store, ConfigPath: clientPath, Command: command}
				server := NTPServerController{DeclaredRouter: router, Store: store, ConfigPath: serverPath, Command: command}
				run := func() {
					t.Helper()
					client.Router = mustWhenFilteredRouter(t, false, router, store)
					server.Router = client.Router
					if err := client.Reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
					if err := server.Reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
					if err := client.Reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				// A declaration by itself cannot establish an applied result.
				client.Router = mustWhenFilteredRouter(t, false, router, store)
				if err := client.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				status, err := store.ReadObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
				if err != nil || status["phase"] != "Pending" || effects != 0 {
					t.Fatalf("unconfirmed delegation: status=%v effects=%d err=%v", status, effects, err)
				}
				run()
				run()
				if initiallyActive {
					if _, err := store.Set("ntp-role", "backup", "authoritative role transition"); err != nil {
						t.Fatal(err)
					}
					run()
				}
				if starts != 0 || stops != 1 || !active["chrony.service"] || active["systemd-timesyncd.service"] {
					t.Fatalf("shared service responsibility: starts=%d stops=%d active=%v", starts, stops, active)
				}
				status, err = store.ReadObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
				if err != nil || status["phase"] != "Applied" || status["provider"] != "chrony" || status["managedBy"] != "NTPServer" {
					t.Fatalf("client not applied through actual provider: %v err=%v", status, err)
				}
				if provider != "chrony" && status["requestedProvider"] != provider {
					t.Fatalf("requested provider missing: %v", status)
				}
				data, err := os.ReadFile(serverPath)
				if err != nil || !strings.Contains(string(data), "server 192.0.2.123 iburst\n") || strings.Contains(string(data), "server 192.0.2.124 iburst\n") || strings.Contains("\n"+string(data), "\nallow ") {
					t.Fatalf("inactive role did not preserve only client upstreams: %q err=%v", data, err)
				}
				if _, err := os.Stat(clientPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("client acquired a second fragment: %v", err)
				}
				clientRecords, err := (ntpConfigOwner{store: store, kind: "NTPClient"}).read()
				if err != nil || len(clientRecords) != 0 {
					t.Fatalf("client acquired shared ownership: %v %v", clientRecords, err)
				}
				// Delegated clients have no private file record. Public data
				// must still retire when their declaration is removed.
				router = &api.Router{}
				client.DeclaredRouter, server.DeclaredRouter = router, router
				run()
				for _, kind := range []string{"NTPClient", "NTPServer"} {
					resourceName := "system-time"
					if kind == "NTPServer" {
						resourceName = "lan-time"
					}
					status, err := store.ReadObjectStatus(api.SystemAPIVersion, kind, resourceName)
					if err != nil || status["phase"] != "Removed" || status["servers"] != nil || status["allowCIDRs"] != nil {
						t.Fatalf("withdrawn public %s: %v err=%v", kind, status, err)
					}
				}
			})
		}
	}
}
