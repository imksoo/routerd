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
	"github.com/imksoo/routerd/pkg/render"
)

// Reproduces the native empty-config result: Healthy with a running orphaned
// routerd-conntrackd@ instance. Only the supervisor command is replaced locally.
func TestEmptyRouterStopsAndRemovesConntrackdInstance(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	dir := t.TempDir()
	unitName := "routerd-conntrackd@removed.service"
	path := filepath.Join(dir, unitName)
	if err := os.WriteFile(path, render.SystemdUnit(unitName, conntrackdSystemdSpec("removed", "/etc/conntrackd/routerd-removed.conf")), 0644); err != nil {
		t.Fatal(err)
	}
	// Never touch an existing development host's synchronization config.
	if _, err := os.Stat("/etc/conntrackd/routerd-removed.conf"); !os.IsNotExist(err) {
		t.Skip("host owns the fixture config path")
	}
	var commands []string
	c := SystemdUnitController{Router: &api.Router{Metadata: api.ObjectMeta{Name: "empty"}}, Store: mapStore{}, SystemdSystemDir: dir, StateDir: filepath.Join(dir, "state"), BGPStatePath: filepath.Join(dir, "bgp", "applied.json"), Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if name == "systemctl" && len(args) > 0 && args[0] == "is-active" {
			return nil, errors.New("inactive")
		}
		return []byte("ok"), nil
	}}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !commandLineContains(commands, "systemctl disable --now "+unitName) {
		t.Fatalf("removed sync daemon never stopped: %v", commands)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("removed sync unit retained: %v", err)
	}
}

type conntrackdRemovalStore struct {
	mapStore
	failPublication bool
}

func (s *conntrackdRemovalStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if s.failPublication && kind == "ServiceUnit" && status["phase"] == "Removed" {
		return errors.New("removal acknowledgement unavailable")
	}
	return s.mapStore.SaveObjectStatus(version, kind, name, status)
}

func TestConntrackdRemovalRetriesStopReloadAndAcknowledgement(t *testing.T) {
	for _, stage := range []string{"disable", "daemon-reload", "publication"} {
		t.Run(stage, func(t *testing.T) {
			dir, configs := t.TempDir(), t.TempDir()
			unit := conntrackdUnitName("removed")
			path := filepath.Join(dir, unit)
			config := filepath.Join(configs, "routerd-removed.conf")
			for _, p := range []string{path, config} {
				if err := os.WriteFile(p, []byte("# Managed by routerd. Do not edit by hand.\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			store := &conntrackdRemovalStore{mapStore: mapStore{api.NetAPIVersion + "/NAT44SessionSync/removed": {"phase": "Running"}}, failPublication: stage == "publication"}
			c := SystemdUnitController{Router: &api.Router{}, Store: store, SystemdSystemDir: dir}
			fail := true
			command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if fail && args[0] == stage {
					return nil, errors.New("supervisor acknowledgement failed")
				}
				return nil, nil
			}
			if err := c.cleanupStaleConntrackdSyncUnits(t.Context(), configs, command); err == nil {
				t.Fatal("failed removal reported success")
			}
			if _, err := os.Stat(path + activationReceiptSuffix); err != nil {
				t.Fatalf("retry receipt lost: %v", err)
			}
			if store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "removed")["phase"] != "Running" {
				t.Fatal("unconfirmed removal retired sync status")
			}
			fail = false
			store.failPublication = false
			// Only the durable receipt survives a controller restart after unlink.
			c = SystemdUnitController{Router: &api.Router{}, Store: store, SystemdSystemDir: dir}
			if err := c.cleanupStaleConntrackdSyncUnits(t.Context(), configs, command); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{path, config, path + activationReceiptSuffix} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Fatalf("artifact retained: %s %v", p, err)
				}
			}
			if _, ok := store.mapStore[api.NetAPIVersion+"/NAT44SessionSync/removed"]; ok {
				t.Fatal("removed sync status retained")
			}
		})
	}
}

func TestConntrackdRemovalPreservesForeignArtifactsAndDisabledDeclaration(t *testing.T) {
	for _, foreign := range []string{"unit", "config", "none"} {
		t.Run(foreign, func(t *testing.T) {
			dir, configs := t.TempDir(), t.TempDir()
			unit := conntrackdUnitName("disabled")
			path := filepath.Join(dir, unit)
			config := filepath.Join(configs, "routerd-disabled.conf")
			for which, p := range map[string]string{"unit": path, "config": config} {
				data := "# Managed by routerd. Do not edit by hand.\n"
				if which == foreign {
					data = "# locally managed\n"
				}
				if err := os.WriteFile(p, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			store := mapStore{api.NetAPIVersion + "/NAT44SessionSync/disabled": {"phase": "Pending", "reason": "WhenFalse"}}
			declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "disabled"}}}}}
			c := SystemdUnitController{Router: &api.Router{}, DeclaredRouter: declared, Store: store, SystemdSystemDir: dir}
			called := false
			err := c.cleanupStaleConntrackdSyncUnits(t.Context(), configs, func(context.Context, string, ...string) ([]byte, error) { called = true; return nil, nil })
			if foreign != "none" {
				if err == nil || called {
					t.Fatalf("foreign artifact authorized mutation: called=%v error=%v", called, err)
				}
				for _, p := range []string{path, config} {
					if _, err := os.Stat(p); err != nil {
						t.Fatal(err)
					}
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "disabled")["reason"] != "WhenFalse" {
				t.Fatal("inactive declaration status deleted")
			}
		})
	}
}
