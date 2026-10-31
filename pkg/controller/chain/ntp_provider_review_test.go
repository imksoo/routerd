// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
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
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := ntpRouter(api.NTPClientSpec{Provider: "systemd-timesyncd", Managed: true, Servers: []string{"192.0.2.123"}})
	router.Spec.Resources = append(router.Spec.Resources, ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.124"}, AllowCIDRs: []string{"192.0.2.0/24"}}).Spec.Resources...)
	router.TypeMeta = api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}
	router.Metadata.Name = "ntp-provider-review"
	if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
		t.Fatalf("fixture is not an accepted Linux declaration: %v", err)
	}
	// Only the observed systemd unit states are modeled. The real two callers
	// choose commands, config fragments, statuses, and conflict handling.
	active := map[string]bool{}
	var starts, stops int
	var commands []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if name == "timedatectl" && strings.Join(args, " ") == "set-ntp true" {
			return nil, nil
		}
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
			if unit == "systemd-timesyncd.service" && !active[unit] {
				starts++
			}
			active[unit] = true
		case "disable":
			if unit == "systemd-timesyncd.service" && active[unit] {
				stops++
			}
			active[unit] = false
		default:
			return nil, errors.New("unexpected systemctl operation")
		}
		return nil, nil
	}
	client := NTPClientController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: filepath.Join(t.TempDir(), "client.conf"), Command: command}
	server := NTPServerController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: filepath.Join(t.TempDir(), "server.conf"), Command: command}
	for i := 0; i < 2; i++ {
		if err := client.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := server.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if starts > 1 || stops > 1 {
		t.Fatalf("unchanged managed declarations reactivated and disabled the same provider repeatedly: starts=%d stops=%d commands=%v", starts, stops, commands)
	}
}
