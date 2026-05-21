// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestInstallWithNDPIArchiveInstallsNativeAgent(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	archive := filepath.Join(dir, "routerd-ndpi-agent-libndpi-linux-amd64.tar.gz")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd-ndpi-agent"), `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":false,"reason":"static fallback"}'; exit 0; fi
echo static-agent
`)
	writeNDPIArchive(t, archive, "linux-amd64", `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":true,"libndpiVersion":"4.2.0"}'; exit 0; fi
echo native-agent
`)

	out, err := runInstall(t, pkg, prefix, "--no-install-deps", "--no-config-update", "--no-restart", "--with-ndpi", "--with-ndpi-archive", archive)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installing native libndpi routerd-ndpi-agent") {
		t.Fatalf("missing native archive install log:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(prefix, "sbin", "routerd-ndpi-agent"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "native-agent") || strings.Contains(string(data), "static-agent") {
		t.Fatalf("native archive was not installed:\n%s", string(data))
	}
}

func TestInstallWithNDPIArchiveRejectsStaticArchive(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	archive := filepath.Join(dir, "routerd-ndpi-agent-libndpi-linux-amd64.tar.gz")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd-ndpi-agent"), `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":false,"reason":"static fallback"}'; exit 0; fi
echo static-agent
`)
	writeNDPIArchive(t, archive, "linux-amd64", `#!/bin/sh
if [ "$1" = "selftest" ]; then echo '{"libndpiLoaded":false,"reason":"static archive"}'; exit 0; fi
echo static-archive-agent
`)

	out, err := runInstall(t, pkg, prefix, "--no-install-deps", "--no-config-update", "--no-restart", "--with-ndpi", "--with-ndpi-archive", archive)
	if err == nil {
		t.Fatalf("install succeeded unexpectedly:\n%s", out)
	}
	for _, want := range []string{
		"archive selftest did not report libndpiLoaded=true",
		"install failed; restoring previous files",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(prefix, "sbin", "routerd-ndpi-agent")); !os.IsNotExist(err) {
		t.Fatalf("failed install should roll back static agent, stat err=%v", err)
	}
}

func TestInstallMigratesLegacySystemdUnitConfig(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	if err := os.MkdirAll(filepath.Join(pkg, "etc", "routerd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "etc", "routerd", "router.yaml.sample"), []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: sample\nspec: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(prefix, "etc", "routerd", "router.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: legacy
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: SystemdUnit
      metadata:
        name: routerd.service
      spec:
        execStart: /usr/local/sbin/routerd serve --controller-chain
    - apiVersion: system.routerd.net/v1alpha1
      kind: Hostname
      metadata:
        name: router
      spec:
        hostname: router01
`
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runInstall(t, pkg, prefix, "--no-install-deps", "--no-config-update", "--no-restart")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "removing legacy SystemdUnit resources") {
		t.Fatalf("missing migration warning:\n%s", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "SystemdUnit") || strings.Contains(got, "--controller-chain") {
		t.Fatalf("legacy SystemdUnit was not removed:\n%s", got)
	}
	if !strings.Contains(got, "kind: Hostname") {
		t.Fatalf("non-legacy resource was not preserved:\n%s", got)
	}
}

func TestInstallReplacesLegacyRouterdServiceBeforeRestart(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	systemdDir := filepath.Join(dir, "systemd")
	binDir := filepath.Join(dir, "bin")
	commandLog := filepath.Join(dir, "commands.log")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "systemctl"), fmt.Sprintf(`#!/bin/sh
echo "systemctl $@" >> %q
if [ "$1" = "is-active" ]; then exit 1; fi
exit 0
`, commandLog))
	if err := os.MkdirAll(filepath.Join(pkg, "systemd"), 0o755); err != nil {
		t.Fatal(err)
	}
	newUnit := `[Service]
ExecStart=/usr/local/sbin/routerd serve --config /usr/local/etc/routerd/router.yaml
`
	if err := os.WriteFile(filepath.Join(pkg, "systemd", "routerd.service"), []byte(newUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyUnit := `# Managed by routerd.
[Service]
ExecStart=/usr/local/sbin/routerd serve --controller-chain --controller-chain-dry-run-route=false
`
	if err := os.WriteFile(filepath.Join(systemdDir, "routerd.service"), []byte(legacyUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runInstallWithEnv(t, pkg, prefix, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
		"ROUTERD_INSTALL_SYSTEMD_SYSTEM_DIR=" + systemdDir,
	}, "--no-install-deps", "--no-config-update", "--no-restart")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "replacing legacy routerd.service") {
		t.Fatalf("missing legacy replacement warning:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(systemdDir, "routerd.service"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "--controller-chain") || strings.Contains(got, "# Managed by routerd.") {
		t.Fatalf("legacy unit was preserved:\n%s", got)
	}
	if !strings.Contains(got, "routerd serve --config") {
		t.Fatalf("new unit was not installed:\n%s", got)
	}
}

func runInstall(t *testing.T, pkg, prefix string, args ...string) (string, error) {
	t.Helper()
	return runInstallWithEnv(t, pkg, prefix, nil, args...)
}

func runInstallWithEnv(t *testing.T, pkg, prefix string, env []string, args ...string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "packaging", "install.sh")
	fullArgs := append([]string{"--prefix", prefix}, args...)
	cmd := exec.Command(script, fullArgs...)
	cmd.Dir = pkg
	cmd.Env = append(os.Environ(), "ROUTERD_INSTALL_PACKAGE_MANAGER=none")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeNDPIArchive(t *testing.T, path, target, agentScript string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	addTarFile(t, tw, "bin/routerd-ndpi-agent", 0o755, agentScript)
	addTarFile(t, tw, "share/doc/TARGET", 0o644, target+"\n")
}

func addTarFile(t *testing.T, tw *tar.Writer, name string, mode int64, content string) {
	t.Helper()
	header := &tar.Header{
		Name:    name,
		Mode:    mode,
		Size:    int64(len(content)),
		ModTime: time.Unix(0, 0),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, content); err != nil {
		t.Fatal(err)
	}
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
