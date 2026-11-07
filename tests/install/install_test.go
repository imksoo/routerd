// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapInstallerPassesShellcheck(t *testing.T) {
	root := repoRoot(t)
	bootstrap := filepath.Join(root, "packaging", "bootstrap.sh")
	if _, err := os.Stat(bootstrap); err != nil {
		t.Fatalf("bootstrap.sh not found: %v", err)
	}
	cmd := exec.Command("shellcheck", bootstrap)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shellcheck failed:\n%s", out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
