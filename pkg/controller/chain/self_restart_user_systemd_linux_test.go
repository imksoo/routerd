// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The real scheduler runs against uniquely named dummy services in the caller's
// unprivileged user manager. No system services or host networking are touched.
func TestSelfRestartUserSystemdLifecycle(t *testing.T) {
	if ready := os.Getenv("ROUTERD_USER_SYSTEMD_CHILD"); ready != "" {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("notify readiness barrier was not released")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if out, err := exec.Command("systemd-notify", "--ready").CombinedOutput(); err != nil {
			t.Fatalf("notify: %v: %s", err, out)
		}
		time.Sleep(120 * time.Second)
		return
	}
	if os.Getenv("ROUTERD_RUN_USER_SYSTEMD") != "1" {
		t.Skip("requires explicit ROUTERD_RUN_USER_SYSTEMD=1 and a running user systemd manager")
	}
	if os.Geteuid() == 0 {
		t.Fatal("run this test as an ordinary user, not against the system manager")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 55*time.Second)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	}
	property := func(unit, key string) string {
		out, _ := run("show", unit, "-p", key, "--value")
		return strings.TrimSpace(string(out))
	}
	for _, scenario := range []string{"stop", "restart", "activating"} {
		t.Run(scenario, func(t *testing.T) {
			parent := fmt.Sprintf("routerd-test-parent-%d-%d.service", os.Getpid(), time.Now().UnixNano())
			units := []string{parent}
			runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
			if runtimeDir == "" {
				runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
			}
			unitDir := filepath.Join(runtimeDir, "systemd/user")
			if err := os.MkdirAll(unitDir, 0700); err != nil {
				t.Fatal(err)
			}
			unitFile := filepath.Join(unitDir, parent)
			file, err := os.OpenFile(unitFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for i := len(units) - 1; i >= 0; i-- {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = exec.CommandContext(cleanupCtx, "systemctl", "--user", "stop", units[i]).Run()
					cleanupCancel()
				}
				_ = os.Remove(unitFile)
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = exec.CommandContext(cleanupCtx, "systemctl", "--user", "daemon-reload").Run()
				cleanupCancel()
			})
			ready := filepath.Join(t.TempDir(), "ready")
			// A persistent definition is essential: an unreferenced transient parent
			// disappears on stop, hiding the resurrection bug as "unit not found".
			body := "[Service]\nType=exec\nExecStart=/bin/sleep 120\n"
			if scenario == "activating" {
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				body = "[Service]\nType=notify\nNotifyAccess=all\nEnvironment=" + strconv.Quote("ROUTERD_USER_SYSTEMD_CHILD="+ready) + "\nExecStart=" + strconv.Quote(binary) + " -test.run=^TestSelfRestartUserSystemdLifecycle$\n"
			}
			if err := os.WriteFile(unitFile, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := run("daemon-reload"); err != nil {
				t.Fatalf("load dummy parent: %v: %s", err, out)
			}
			args := []string{"start", parent}
			if scenario == "activating" {
				args = append([]string{"--no-block"}, args...)
			}
			if out, err := run(args...); err != nil {
				t.Fatalf("start isolated user parent: %v: %s", err, out)
			}
			if scenario == "activating" && property(parent, "ActiveState") != "activating" {
				t.Fatal("notify parent must still be activating before scheduling")
			}
			before := property(parent, "MainPID")
			scheduleCtx, scheduleCancel := context.WithTimeout(ctx, 3*time.Second)
			defer scheduleCancel()
			command := func(commandCtx context.Context, name string, original ...string) ([]byte, error) {
				if name != "systemd-run" {
					t.Fatalf("unexpected scheduler command %s", name)
				}
				// Only the manager/parent names and timer accuracy change; the
				// production ten-second delay and dependency settings are exercised.
				remapped := []string{"--user", "--timer-property=AccuracySec=100ms"}
				for index, arg := range original {
					if index > 0 && original[index-1] == "--unit" {
						units = append(units, strings.TrimSuffix(arg, ".service")+".timer", arg)
					}
					if arg == "systemctl" {
						remapped = append(remapped, arg, "--user")
					} else {
						remapped = append(remapped, strings.ReplaceAll(arg, "routerd.service", parent))
					}
				}
				return exec.CommandContext(commandCtx, name, remapped...).CombinedOutput()
			}
			if err := scheduleRouterdServiceRestart(scheduleCtx, command); err != nil {
				t.Fatalf("enqueue while parent is %s: %v", scenario, err)
			}
			if scenario == "activating" {
				if err := os.WriteFile(ready, nil, 0600); err != nil {
					t.Fatal(err)
				}
				return
			}
			if scenario == "stop" {
				if out, err := run("stop", parent); err != nil {
					t.Fatalf("stop: %v: %s", err, out)
				}
			} else {
				deadline := time.Now().Add(13 * time.Second)
				for property(parent, "MainPID") == before && time.Now().Before(deadline) {
					time.Sleep(50 * time.Millisecond)
				}
				if pid := property(parent, "MainPID"); pid == before || pid == "0" || pid == "" {
					t.Fatalf("intended restart did not replace the active process: %q -> %q", before, pid)
				}
				before = property(parent, "MainPID")
			}
			deadline := time.Now().Add(12 * time.Second)
			for time.Now().Before(deadline) {
				if scenario == "stop" {
					if state := property(parent, "ActiveState"); state == "active" || state == "activating" {
						t.Fatalf("deferred restart resurrected explicitly stopped service: %s", state)
					}
				} else if pid := property(parent, "MainPID"); pid != before {
					t.Fatalf("restart timer caused another restart: %q -> %q", before, pid)
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
}
