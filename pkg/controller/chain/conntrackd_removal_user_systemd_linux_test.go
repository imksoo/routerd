// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func TestConntrackdRemovalUserSystemdRetriesAfterUnitDisappears(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_USER_SYSTEMD") != "1" {
		t.Skip("requires explicit ROUTERD_RUN_USER_SYSTEMD=1")
	}
	if os.Geteuid() == 0 {
		t.Fatal("requires ordinary user systemd, never system services")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	name := fmt.Sprintf("fixture-%d-%d", os.Getpid(), time.Now().UnixNano())
	unit := conntrackdUnitName(name)
	dir, configs := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, unit)
	run := func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	}
	t.Cleanup(func() {
		exec.Command("systemctl", "--user", "stop", unit).Run()
		exec.Command("systemctl", "--user", "disable", unit).Run()
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	})
	if err := os.WriteFile(path, render.SystemdUnit(unit, api.SystemdUnitSpec{ExecStart: []string{"/bin/sleep", "300"}}), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(ctx, "systemctl", "link", path); err != nil {
		t.Fatalf("link: %v %s", err, out)
	}
	if out, err := run(ctx, "systemctl", "start", unit); err != nil {
		t.Fatalf("start: %v %s", err, out)
	}
	store := &conntrackdRemovalStore{mapStore: mapStore{api.NetAPIVersion + "/NAT44SessionSync/" + name: {"phase": "Running"}}, failPublication: true}
	c := SystemdUnitController{Router: &api.Router{}, Store: store, SystemdSystemDir: dir}
	if err := c.cleanupStaleConntrackdSyncUnits(ctx, configs, run); err == nil {
		t.Fatal("failed acknowledgement hidden")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("fixture did not reach post-unlink retry: %v", err)
	}
	out, err := run(ctx, "systemctl", "show", unit, "--property=LoadState,ActiveState")
	if err != nil || !strings.Contains(string(out), "LoadState=not-found") {
		t.Fatalf("native missing-unit path not exercised: %s %v", out, err)
	}
	store.failPublication = false
	if err := c.cleanupStaleConntrackdSyncUnits(ctx, configs, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + activationReceiptSuffix); !os.IsNotExist(err) {
		t.Fatalf("pending receipt not completed: %v", err)
	}
	if _, ok := store.mapStore[api.NetAPIVersion+"/NAT44SessionSync/"+name]; ok {
		t.Fatal("unlinked instance retained public status")
	}
}
