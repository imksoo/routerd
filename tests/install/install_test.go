// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPreservesNativeNDPIAgent(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd-ndpi-agent"), `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":false,"reason":"static fallback"}'; exit 0; fi
echo static-agent
`)
	writeExecutable(t, filepath.Join(prefix, "sbin", "routerd-ndpi-agent"), `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":true,"libndpiVersion":"4.2.0"}'; exit 0; fi
echo native-agent
`)

	out, err := runInstall(t, pkg, prefix, "--no-install-deps", "--no-config-update", "--no-restart")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "preserving existing native libndpi routerd-ndpi-agent") {
		t.Fatalf("missing preserve log:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(prefix, "sbin", "routerd-ndpi-agent"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "native-agent") || strings.Contains(string(data), "static-agent") {
		t.Fatalf("agent was not preserved:\n%s", string(data))
	}
}

func TestInstallWithNDPIRejectsStaticAgent(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd-ndpi-agent"), `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":false,"reason":"static fallback"}'; exit 0; fi
echo static-agent
`)

	out, err := runInstall(t, pkg, prefix, "--no-install-deps", "--no-config-update", "--no-restart", "--with-ndpi")
	if err == nil {
		t.Fatalf("install succeeded unexpectedly:\n%s", out)
	}
	for _, want := range []string{
		"--with-ndpi was requested",
		"routerd-ndpi-agent-libndpi-linux-amd64.tar.gz",
		"static fallback agent",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(prefix, "sbin", "routerd-ndpi-agent")); !os.IsNotExist(err) {
		t.Fatalf("failed install should roll back static agent, stat err=%v", err)
	}
}

func runInstall(t *testing.T, pkg, prefix string, args ...string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "packaging", "install.sh")
	fullArgs := append([]string{"--prefix", prefix}, args...)
	cmd := exec.Command(script, fullArgs...)
	cmd.Dir = pkg
	cmd.Env = append(os.Environ(), "ROUTERD_INSTALL_PACKAGE_MANAGER=none")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
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
