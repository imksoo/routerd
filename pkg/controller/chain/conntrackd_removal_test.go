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
