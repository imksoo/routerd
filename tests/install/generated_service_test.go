// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRefreshesGeneratedServiceBeforeStart(t *testing.T) {
	requireLinuxSystemdFixture(t)
	for _, renderFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("renderFails=%t", renderFails), func(t *testing.T) {
			dir := t.TempDir()
			pkg, prefix := filepath.Join(dir, "package"), filepath.Join(dir, "prefix")
			systemdDir, binDir := filepath.Join(dir, "systemd"), filepath.Join(dir, "bin")
			configPath := filepath.Join(prefix, "etc", "routerd", "router.yaml")
			unitPath := filepath.Join(systemdDir, "routerd.service")
			commandLog := filepath.Join(dir, "commands.log")
			for _, path := range []string{systemdDir, filepath.Join(pkg, "systemd"), filepath.Dir(configPath)} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			oldUnit := "# routerd-managed-service: v1\n# Managed by routerd. Do not edit by hand.\n[Service]\nCapabilityBoundingSet=CAP_NET_ADMIN\n"
			newUnit := "# routerd-managed-service: v1\n# Managed by routerd. Do not edit by hand.\n[Service]\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_SYS_PTRACE CAP_KILL CAP_DAC_OVERRIDE\nEnvironment=OTEL_SERVICE_NAME=configured-router\n"
			for path, data := range map[string]string{
				unitPath:   oldUnit,
				configPath: "configured-router\n",
				filepath.Join(pkg, "systemd", "routerd.service"): "# routerd-managed-service: v1\n[Service]\nType=notify\n",
			} {
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			oldBinary := "#!/bin/sh\necho old-version\n"
			writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), oldBinary)
			failure := ""
			if renderFails {
				failure = "echo 'invalid current config' >&2; exit 1"
			}
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), fmt.Sprintf(`#!/bin/sh
if [ "$1" = --version ]; then echo new-version; exit 0; fi
[ "$#" -eq 4 ] && [ "$1" = render ] && [ "$2" = systemd-service ] && [ "$3" = --config ] && [ "$4" = %q ] || exit 2
[ "$(cat "$4")" = configured-router ] || exit 3
%s
cat <<'UNIT'
%sUNIT
`, configPath, failure, newUnit))
			writeExecutable(t, filepath.Join(binDir, "systemctl"), fmt.Sprintf(`#!/bin/sh
echo "$*" >> %q
if [ "$1" = is-active ]; then exit 1; fi
exit 0
`, commandLog))
			out, err := runInstallWithEnv(t, pkg, prefix, []string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
				"ROUTERD_INSTALL_SYSTEMD_SYSTEM_DIR=" + systemdDir,
			}, "--no-install-deps", "--no-config-update", "--no-restart")
			wantUnit := newUnit
			if renderFails {
				wantUnit = oldUnit
				if err == nil || !strings.Contains(out, "invalid current config") {
					t.Fatalf("failed render must abort installation: %v\n%s", err, out)
				}
				gotBinary, readErr := os.ReadFile(filepath.Join(prefix, "sbin", "routerd"))
				if readErr != nil || string(gotBinary) != oldBinary {
					t.Fatalf("failed render did not restore old binary: %s, %v", gotBinary, readErr)
				}
			} else if err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			got, err := os.ReadFile(unitPath)
			if err != nil || string(got) != wantUnit {
				t.Fatalf("installed unit: %s, %v; want %s", got, err, wantUnit)
			}
			commands, err := os.ReadFile(commandLog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(commands), "restart ") || strings.Contains(string(commands), "start ") {
				t.Fatalf("--no-restart started a service: %s", commands)
			}
		})
	}
}
