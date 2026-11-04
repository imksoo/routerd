// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdRemovedTailscaleRetainsUnitUntilStopped(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	dir := t.TempDir()
	unit := render.TailscaleUnitName("removed")
	path := filepath.Join(dir, unit)
	if err := os.WriteFile(path, render.SystemdUnit(unit, render.TailscaleSystemdSpec("removed", api.TailscaleNodeSpec{})), 0644); err != nil {
		t.Fatal(err)
	}
	store := mapStore{api.NetAPIVersion + "/TailscaleNode/removed": {"phase": "Running"}}
	failed := true
	stops := 0
	c := SystemdUnitController{Router: &api.Router{}, DeclaredRouter: &api.Router{}, Store: store, SystemdSystemDir: dir, BGPStatePath: filepath.Join(dir, "bgp", "applied.json"), Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" && len(args) == 3 && args[0] == "disable" && args[2] == unit {
			stops++
			if failed {
				return nil, errors.New("stop unconfirmed")
			}
		}
		return nil, nil
	}}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("removed Tailscale unit was not stopped or stop failure was hidden")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("unit removed before stop acknowledgement", err)
	}
	failed = false
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if stops != 2 {
		t.Fatalf("stop attempts=%d", stops)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit remains: %v", err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "TailscaleNode", "removed"); len(got) != 0 {
		t.Fatalf("removed node status remains: %v", got)
	}
}
