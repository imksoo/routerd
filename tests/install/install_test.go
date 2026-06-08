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

func TestInstallReplacesLegacyOpenRCInitScript(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	initDir := filepath.Join(dir, "init.d")
	binDir := filepath.Join(dir, "bin")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "rc-service"), `#!/bin/sh
if [ "$1" = "routerd" ] && [ "$2" = "status" ]; then exit 1; fi
exit 0
`)
	if err := os.MkdirAll(filepath.Join(pkg, "openrc"), 0o755); err != nil {
		t.Fatal(err)
	}
	newScript := `#!/sbin/openrc-run
name="routerd"
command="/usr/local/sbin/routerd"
command_args="serve --config /usr/local/etc/routerd/router.yaml --socket /run/routerd/routerd.sock --status-socket /run/routerd/routerd-status.sock --apply-interval 60s"
`
	if err := os.WriteFile(filepath.Join(pkg, "openrc", "routerd"), []byte(newScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyScript := `#!/sbin/openrc-run
name="routerd"
start_pre() {
    /usr/local/sbin/routerd check
}
`
	if err := os.WriteFile(filepath.Join(initDir, "routerd"), []byte(legacyScript), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runInstallWithEnv(t, pkg, prefix, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
		"ROUTERD_INSTALL_OPENRC_INIT_DIR=" + initDir,
	}, "--no-install-deps", "--no-config-update", "--no-restart")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "replacing legacy routerd OpenRC init script with removed routerd check") {
		t.Fatalf("missing legacy OpenRC replacement warning:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(initDir, "routerd"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "routerd check") {
		t.Fatalf("legacy OpenRC check was preserved:\n%s", got)
	}
	if !strings.Contains(got, "routerd") || !strings.Contains(got, "serve --config") {
		t.Fatalf("new OpenRC script was not installed:\n%s", got)
	}
}

func TestInstallOpenRCRestartUsesNodeps(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	initDir := filepath.Join(dir, "init.d")
	binDir := filepath.Join(dir, "bin")
	commandLog := filepath.Join(dir, "commands.log")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "rc-service"), fmt.Sprintf(`#!/bin/sh
echo "rc-service $@" >> %q
if [ "$1" = "routerd" ] && [ "$2" = "status" ]; then exit 0; fi
if [ "$1" = "--nodeps" ] && [ "$2" = "routerd" ] && [ "$3" = "restart" ]; then exit 0; fi
echo "unexpected rc-service call: $@" >&2
exit 1
`, commandLog))
	writeExecutable(t, filepath.Join(binDir, "rc-update"), fmt.Sprintf(`#!/bin/sh
echo "rc-update $@" >> %q
exit 0
`, commandLog))
	if err := os.MkdirAll(filepath.Join(pkg, "openrc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "openrc", "routerd"), []byte(`#!/sbin/openrc-run
name="routerd"
command="/usr/local/sbin/routerd"
command_args="serve"
`), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runInstallWithEnv(t, pkg, prefix, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
		"ROUTERD_INSTALL_OPENRC_INIT_DIR=" + initDir,
	}, "--no-install-deps", "--no-config-update")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(commandLog)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if !strings.Contains(log, "rc-service --nodeps routerd restart") {
		t.Fatalf("OpenRC restart did not use --nodeps:\n%s", log)
	}
	if strings.Contains(log, "rc-service routerd restart") {
		t.Fatalf("OpenRC restart used dependency-resolving form:\n%s", log)
	}
}

func TestInstallOpenRCDryRunDetectsStaleDeletedHelpers(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	initDir := filepath.Join(dir, "init.d")
	binDir := filepath.Join(dir, "bin")
	procDir := filepath.Join(dir, "proc")
	commandLog := filepath.Join(dir, "commands.log")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-new; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd-bgp"), `#!/bin/sh
exit 0
`)
	writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-old; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "rc-service"), fmt.Sprintf(`#!/bin/sh
echo "rc-service $@" >> %q
if [ "$1" = "routerd" ] && [ "$2" = "status" ]; then exit 0; fi
if [ "$1" = "--nodeps" ] && [ "$2" = "routerd" ] && [ "$3" = "restart" ]; then exit 0; fi
exit 0
`, commandLog))
	if err := os.MkdirAll(filepath.Join(pkg, "openrc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "openrc", "routerd"), []byte(`#!/sbin/openrc-run
name="routerd"
command="/usr/local/sbin/routerd"
command_args="serve"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	for pid, target := range map[string]string{
		"1234": filepath.Join(prefix, "sbin", "routerd-bgp") + " (deleted)",
		"2345": filepath.Join(prefix, "sbin", "routerd") + " (deleted)",
		"3456": "/usr/local/bin/routerd-bgp (deleted)",
	} {
		pidDir := filepath.Join(procDir, pid)
		if err := os.MkdirAll(pidDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(pidDir, "exe")); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runInstallWithEnv(t, pkg, prefix, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
		"ROUTERD_INSTALL_OPENRC_INIT_DIR=" + initDir,
		"ROUTERD_INSTALL_PROC_DIR=" + procDir,
	}, "--no-install-deps", "--no-config-update", "--dry-run")
	if err != nil {
		t.Fatalf("install dry-run failed: %v\n%s", err, out)
	}
	want := "dry-run: kill stale OpenRC routerd helper pid 1234 (" + filepath.Join(prefix, "sbin", "routerd-bgp") + ")"
	if !strings.Contains(out, want) {
		t.Fatalf("stale helper was not detected, want %q:\n%s", want, out)
	}
	if strings.Contains(out, "pid 2345") || strings.Contains(out, "pid 3456") {
		t.Fatalf("unexpected process selected:\n%s", out)
	}
}

func TestInstallDryRunCreatesRouterdGroupBeforeSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	binDir := filepath.Join(dir, "bin")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "getent"), `#!/bin/sh
if [ "$1" = "group" ] && [ "$2" = "routerd" ]; then exit 2; fi
exit 2
`)
	writeExecutable(t, filepath.Join(binDir, "groupadd"), `#!/bin/sh
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "systemctl"), `#!/bin/sh
if [ "$1" = "is-active" ]; then exit 1; fi
exit 0
`)
	if err := os.MkdirAll(filepath.Join(pkg, "systemd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "systemd", "routerd.service"), []byte("[Service]\nGroup=routerd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runInstallRawWithEnv(t, pkg, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}, "--dry-run", "--no-install-deps", "--no-config-update", "--no-restart")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	groupIdx := strings.Index(out, "dry-run: groupadd -r routerd")
	unitIdx := strings.Index(out, "dry-run: install -m 0644 systemd/routerd.service /etc/systemd/system/routerd.service")
	if groupIdx < 0 {
		t.Fatalf("missing routerd group creation:\n%s", out)
	}
	if unitIdx < 0 {
		t.Fatalf("missing systemd unit install:\n%s", out)
	}
	if groupIdx > unitIdx {
		t.Fatalf("routerd group creation happened after unit install:\n%s", out)
	}
}

func TestInstallFailsBeforeReplacingWhenRollbackSafeSpaceIsInsufficient(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-new; exit 0; fi
echo new-routerd
`)
	writeExecutable(t, filepath.Join(pkg, "bin", "routerctl"), `#!/bin/sh
echo new-routerctl
`)
	writeExecutable(t, filepath.Join(prefix, "sbin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-old; exit 0; fi
echo old-routerd
`)
	writeExecutable(t, filepath.Join(prefix, "sbin", "routerctl"), `#!/bin/sh
echo old-routerctl
`)

	out, err := runInstallWithEnv(t, pkg, prefix, []string{
		"ROUTERD_INSTALL_AVAILABLE_KB_OVERRIDE=1",
	}, "--no-install-deps", "--no-config-update", "--no-restart")
	if err == nil {
		t.Fatalf("install succeeded unexpectedly:\n%s", out)
	}
	if !strings.Contains(out, "insufficient free space for rollback-safe install") {
		t.Fatalf("missing capacity diagnostic:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(prefix, "sbin", "routerctl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "old-routerctl") {
		t.Fatalf("routerctl was replaced despite preflight failure:\n%s", string(data))
	}
	if matches, err := filepath.Glob(filepath.Join(prefix, "sbin", "*.backup.*")); err != nil || len(matches) != 0 {
		t.Fatalf("persistent backup files = %v, err=%v; want none before preflight failure", matches, err)
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

func runInstallRawWithEnv(t *testing.T, pkg string, env []string, args ...string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "packaging", "install.sh")
	cmd := exec.Command(script, args...)
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
