// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func TestTailscaleFailedStartUserSystemdDisconnects(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_USER_SYSTEMD") != "1" {
		t.Skip("requires explicit ROUTERD_RUN_USER_SYSTEMD=1")
	}
	if os.Geteuid() == 0 {
		t.Fatal("requires ordinary user systemd, never system services")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	dir := t.TempDir()
	name := fmt.Sprintf("failed-%d-%d", os.Getpid(), time.Now().UnixNano())
	unit := render.TailscaleUnitName(name)
	binary := filepath.Join(dir, "tailscale")
	// Model up accepting persistent intent before its connection wait fails.
	script := "#!/bin/sh\ncase \"$1\" in\nup) touch '" + dir + "/requested' '" + dir + "/running'; exit 1;;\ndown) rm -f '" + dir + "/running';;\nesac\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	spec := render.TailscaleSystemdSpec(name, api.TailscaleNodeSpec{BinaryPath: binary})
	spec.Wants = nil
	spec.After = nil
	spec.WantedBy = nil
	spec.RuntimeDirectory = nil
	spec.CapabilityBoundingSet = nil
	spec.AmbientCapabilities = nil
	path := filepath.Join(dir, unit)
	if err := os.WriteFile(path, render.SystemdUnit(unit, spec), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	}
	t.Cleanup(func() {
		exec.Command("systemctl", "--user", "stop", unit).Run()
		exec.Command("systemctl", "--user", "disable", unit).Run()
		exec.Command("systemctl", "--user", "reset-failed", unit).Run()
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	})
	if out, err := run(ctx, "systemctl", "link", path); err != nil {
		t.Fatalf("link: %v %s", err, out)
	}
	if _, err := run(ctx, "systemctl", "start", unit); err == nil {
		t.Fatal("expected failed connection initialization")
	}
	if _, err := os.Stat(filepath.Join(dir, "requested")); err != nil {
		t.Fatal("up was not reached", err)
	}
	c := SystemdUnitController{Router: &api.Router{}, Store: mapStore{}, SystemdSystemDir: dir}
	if err := c.cleanupStaleTailscaleUnits(ctx, nil, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); !os.IsNotExist(err) {
		t.Fatalf("failed-start node still wants to run after withdrawal: %v", err)
	}
}
