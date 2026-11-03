// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRejectsMissingPayloadBeforePackageOperations(t *testing.T) {
	for _, manager := range []string{"apt", "pkg"} {
		t.Run(manager, func(t *testing.T) {
			dir := t.TempDir()
			commands := filepath.Join(dir, "commands")
			marker := filepath.Join(dir, "package-calls")
			binary := manager
			if manager == "apt" {
				binary = "apt-get"
			}
			writeExecutable(t, filepath.Join(commands, binary), `#!/bin/sh
printf '%s\n' "$*" >> "$ROUTERD_TEST_PACKAGE_CALLS"
exit 71
`)
			out, err := runInstallWithEnv(t, dir, filepath.Join(dir, "prefix"), []string{
				"ROUTERD_INSTALL_PACKAGE_MANAGER=" + manager,
				"PATH=" + commands + string(os.PathListSeparator) + os.Getenv("PATH"),
				"ROUTERD_TEST_PACKAGE_CALLS=" + marker,
			})
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 {
				t.Errorf("missing payload must exit 2, got %v:\n%s", err, out)
			}
			if !strings.Contains(out, "required payload bin/routerd not found") {
				t.Errorf("missing payload diagnostic absent:\n%s", out)
			}
			if calls, err := os.ReadFile(marker); !os.IsNotExist(err) {
				t.Errorf("invalid payload reached package manager: calls=%q error=%v", calls, err)
			}
		})
	}
}

func TestInstallDependenciesOnlyDoesNotRequireReleasePayload(t *testing.T) {
	dir := t.TempDir()
	out, err := runInstallWithEnv(t, dir, filepath.Join(dir, "prefix"), []string{
		"ROUTERD_INSTALL_PACKAGE_MANAGER=apt",
	}, "--deps-only", "--dry-run")
	if err != nil || !strings.Contains(out, "dry-run: apt-get install -y") {
		t.Fatalf("standalone dependency bootstrap failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "required payload") {
		t.Fatalf("dependency bootstrap incorrectly requires a release payload:\n%s", out)
	}
}
