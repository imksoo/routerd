// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallFreeBSDStartFailurePreservesPublishedFiles(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			dir := t.TempDir()
			pkg, prefix, binDir := filepath.Join(dir, "package"), filepath.Join(dir, "prefix"), filepath.Join(dir, "bin")
			writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), "#!/bin/sh\necho old-routerd\n")
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), "#!/bin/sh\necho new-routerd\n")
			writeExecutable(t, filepath.Join(pkg, "rc.d", "routerd"), "#!/bin/sh\n# routerd-managed-service: v1\n")
			writeExecutable(t, filepath.Join(binDir, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo FreeBSD;; -m) echo amd64;; *) exit 1;; esac\n")
			status := 1
			if active {
				status = 0
			}
			writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf("#!/bin/sh\ncase \"$*\" in 'routerd status') exit %d;; 'routerd restart'|'routerd onestart') exit 7;; *) exit 99;; esac\n", status))
			out, err := runInstallWithEnv(t, pkg, prefix, []string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
				"ROUTERD_INSTALL_STATUS_SOCKET=" + filepath.Join(dir, "missing.sock"),
			}, "--start-service", "--no-install-deps", "--no-config-update")
			if err == nil || !strings.Contains(out, "service recovery failed") || strings.Contains(out, "restoring previous files") {
				t.Fatalf("failed native start did not retain published files: %v\n%s", err, out)
			}
			installed, err := os.ReadFile(filepath.Join(prefix, "sbin", "routerd"))
			if err != nil || !strings.Contains(string(installed), "new-routerd") {
				t.Fatalf("published binary lost after start failure: %v\n%s", err, out)
			}
		})
	}
}

func TestInstallCopyFailureRollsBackBeforeServiceRecovery(t *testing.T) {
	for _, failRestore := range []bool{false, true} {
		t.Run(fmt.Sprint(failRestore), func(t *testing.T) {
			dir := t.TempDir()
			pkg, prefix, binDir, tmpDir := filepath.Join(dir, "package"), filepath.Join(dir, "prefix"), filepath.Join(dir, "bin"), filepath.Join(dir, "tmp")
			if err := os.MkdirAll(tmpDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), "#!/bin/sh\necho old-routerd\n")
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), "#!/bin/sh\necho new-routerd\n")
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd-zaux"), "#!/bin/sh\nexit 0\n")
			installBin, err := exec.LookPath("install")
			if err != nil {
				t.Fatal(err)
			}
			mvBin, err := exec.LookPath("mv")
			if err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(binDir, "install"), fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *'bin/routerd-zaux '*) exit 19;; esac\nexec '%s' \"$@\"\n", installBin))
			if failRestore {
				writeExecutable(t, filepath.Join(binDir, "mv"), fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *'.restore.'*) exit 23;; esac\nexec '%s' \"$@\"\n", mvBin))
			}
			out, err := runInstallWithEnv(t, pkg, prefix, []string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR=" + tmpDir,
			}, "--no-install-deps", "--no-config-update")
			if err == nil || !strings.Contains(out, "restoring previous files") {
				t.Fatalf("copy failure did not attempt rollback: %v\n%s", err, out)
			}
			installed, err := os.ReadFile(filepath.Join(prefix, "sbin", "routerd"))
			if err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(filepath.Join(tmpDir, "routerd-install.*", "restore.list"))
			if err != nil {
				t.Fatal(err)
			}
			if failRestore {
				if !strings.Contains(out, "rollback failed; retaining recovery files") || len(backups) != 1 || !strings.Contains(string(installed), "new-routerd") {
					t.Fatalf("failed rollback lost its recovery receipt: backups=%v\n%s", backups, out)
				}
			} else if len(backups) != 0 || !strings.Contains(string(installed), "old-routerd") {
				t.Fatalf("successful rollback did not restore old binary: backups=%v\n%s", backups, out)
			}
		})
	}
}

func TestInstallInterruptedServiceRecoveryPreservesPublishedFiles(t *testing.T) {
	requireLinuxSystemdFixture(t)
	dir := t.TempDir()
	pkg, prefix, binDir := filepath.Join(dir, "package"), filepath.Join(dir, "prefix"), filepath.Join(dir, "bin")
	writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), "#!/bin/sh\necho old-routerd\n")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), "#!/bin/sh\necho new-routerd\n")
	writeExecutable(t, filepath.Join(pkg, "systemd", "routerd.service"), "# routerd-managed-service: v1\n")
	writeExecutable(t, filepath.Join(binDir, "systemctl"), `#!/bin/sh
case "$*" in
  'restart routerd.service') kill -TERM "$PPID"; exit 0 ;;
  *) exit 0 ;;
esac
`)
	out, err := runInstallWithEnv(t, pkg, prefix, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
		"ROUTERD_INSTALL_SYSTEMD_SYSTEM_DIR=" + filepath.Join(dir, "systemd"),
	}, "--no-install-deps", "--no-config-update")
	if err == nil || !strings.Contains(out, "interrupted after service recovery began") || strings.Contains(out, "restoring previous files") {
		t.Fatalf("interruption crossed the rollback boundary: %v\n%s", err, out)
	}
	installed, readErr := os.ReadFile(filepath.Join(prefix, "sbin", "routerd"))
	if readErr != nil || !strings.Contains(string(installed), "new-routerd") {
		t.Fatalf("interruption discarded published binary: %v\n%s", readErr, out)
	}
}

func TestInstallServiceObservationFailureBeforeStartRollsBack(t *testing.T) {
	requireLinuxSystemdFixture(t)
	for _, scenario := range []string{"initial-reload", "helper-reload", "helper-list"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			pkg, prefix, binDir := filepath.Join(dir, "package"), filepath.Join(dir, "prefix"), filepath.Join(dir, "bin")
			writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), "#!/bin/sh\necho old-routerd\n")
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), "#!/bin/sh\necho new-routerd\n")
			writeExecutable(t, filepath.Join(pkg, "systemd", "routerd.service"), "# routerd-managed-service: v1\n")
			writeExecutable(t, filepath.Join(binDir, "systemctl"), `#!/bin/sh
case "$*" in
  'is-active --quiet routerd.service') exit 1 ;;
  daemon-reload)
    [ "$INSTALL_OBSERVATION_FAILURE" != initial-reload ] || exit 3
    if [ "$INSTALL_OBSERVATION_FAILURE" = helper-reload ] && [ -f "$INSTALL_RELOAD_MARKER" ]; then exit 4; fi
    touch "$INSTALL_RELOAD_MARKER" ;;
  'list-units --type=service --state=running --plain --no-legend routerd*.service') exit 5 ;;
  *) echo unexpected-service-command >&2; exit 99 ;;
esac
`)
			out, err := runInstallWithEnv(t, pkg, prefix, []string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
				"ROUTERD_INSTALL_SYSTEMD_SYSTEM_DIR=" + filepath.Join(dir, "systemd"),
				"INSTALL_OBSERVATION_FAILURE=" + scenario, "INSTALL_RELOAD_MARKER=" + filepath.Join(dir, "reload.marker"),
			}, "--no-install-deps", "--no-config-update")
			if err == nil || !strings.Contains(out, "restoring previous files") || strings.Contains(out, "unexpected-service-command") {
				t.Fatalf("pre-start failure did not roll back: %v\n%s", err, out)
			}
			installed, readErr := os.ReadFile(filepath.Join(prefix, "sbin", "routerd"))
			if readErr != nil || !strings.Contains(string(installed), "old-routerd") {
				t.Fatalf("pre-start failure retained new binary: %v\n%s", readErr, out)
			}
		})
	}
}
