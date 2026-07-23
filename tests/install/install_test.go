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
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireLinuxSystemdFixture(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux systemd installer fixture")
	}
}

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

func TestInstallDoesNotExcludeBGPOrDNSResolverHelpersFromStaleRestart(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, forbidden := range []string{
		"not auto-restarting",
		"routerd-bgp.service|routerd-bgp@*.service|routerd-dns-resolver.service|routerd-dns-resolver@*.service",
		"restart it deliberately when ready",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("install.sh still excludes stale BGP/DNS resolver helpers from automatic restart via %q", forbidden)
		}
	}
}

func TestInstallWaitsForJSONApplyStateAfterServiceRestart(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if !strings.Contains(text, `routerctl" get status -o json --socket`) {
		t.Fatalf("install.sh should query JSON status before checking lastApplyTime")
	}
	if strings.Contains(text, `routerctl" get status --socket "${socket}"`) {
		t.Fatalf("install.sh still checks human status output for lastApplyTime")
	}
	if !strings.Contains(text, `wait_for_routerd_status_socket "${status_socket}" || true`) {
		t.Fatalf("install.sh should wait for the status socket before final post-upgrade status output")
	}
}

func TestInstallConfigureUsesCurrentRouterdServeCLI(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, forbidden := range []string{
		`"${routerd_bin}" validate --config`,
		`"${routerd_bin}" plan --config`,
		`"${routerd_bin}" apply --config`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("install.sh still calls removed routerd CLI %q", forbidden)
		}
	}
	for _, want := range []string{
		`"${routerd_bin}" serve --sandbox --root "${sandbox_root}" --config "${final_config}" --once`,
		`"${routerd_bin}" serve --config "${final_config}" --once`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("install.sh missing current serve CLI %q", want)
		}
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

func TestInstallInstallsLibexecPluginPayload(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package")
	prefix := filepath.Join(dir, "prefix")
	writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo routerd-test; exit 0; fi
exit 0
`)
	writeExecutable(t, filepath.Join(pkg, "libexec", "routerd", "plugins", "provider-private-ip-inventory"), `#!/bin/sh
echo inventory-plugin
`)
	if err := os.MkdirAll(filepath.Join(pkg, "libexec", "routerd", "plugins", "azure-provider-executor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "libexec", "routerd", "plugins", "azure-provider-executor", "plugin.yaml"), []byte("name: azure-provider-executor\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runInstall(t, pkg, prefix, "--no-install-deps", "--no-config-update", "--no-restart")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	pluginPath := filepath.Join(prefix, "libexec", "routerd", "plugins", "provider-private-ip-inventory")
	info, err := os.Stat(pluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("plugin mode = %o, want 0755", got)
	}
	manifestPath := filepath.Join(prefix, "libexec", "routerd", "plugins", "azure-provider-executor", "plugin.yaml")
	info, err = os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("manifest mode = %o, want 0644", got)
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
	requireLinuxSystemdFixture(t)
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

func TestInstallPreservesForeignCanonicalServiceArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		osName   string
		payload  string
		target   string
		envKey   string
		fakeTool string
	}{
		{
			name:     "linux-systemd",
			osName:   "Linux",
			payload:  "systemd/routerd.service",
			target:   "routerd.service",
			envKey:   "ROUTERD_INSTALL_SYSTEMD_SYSTEM_DIR",
			fakeTool: "systemctl",
		},
		{
			name:     "freebsd-rcd",
			osName:   "FreeBSD",
			payload:  "rc.d/routerd",
			target:   "routerd",
			envKey:   "ROUTERD_INSTALL_RCD_DIR",
			fakeTool: "service",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pkg := filepath.Join(dir, "package")
			prefix := filepath.Join(dir, "prefix")
			serviceDir := filepath.Join(dir, "service")
			binDir := filepath.Join(dir, "bin")
			commandLog := filepath.Join(dir, "service-manager.log")
			writeExecutable(t, filepath.Join(pkg, "bin", "routerd"), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo routerd-test; fi\n")
			if err := os.MkdirAll(filepath.Join(pkg, filepath.Dir(tc.payload)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pkg, tc.payload), []byte("# routerd-managed-service: v1\nrouterd-owned\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(serviceDir, filepath.Dir(tc.target)), 0o755); err != nil {
				t.Fatal(err)
			}
			foreignPath := filepath.Join(serviceDir, tc.target)
			foreign := "# administrator-owned canonical service\nforeign-content\n"
			if err := os.WriteFile(foreignPath, []byte(foreign), 0o755); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(binDir, "uname"), fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"-s\" ]; then echo %s; else echo x86_64; fi\n", tc.osName))
			writeExecutable(t, filepath.Join(binDir, tc.fakeTool), fmt.Sprintf("#!/bin/sh\necho %s \"$@\" >> %q\nexit 0\n", tc.fakeTool, commandLog))
			if tc.osName == "FreeBSD" {
				writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf("#!/bin/sh\necho sysrc \"$@\" >> %q\nexit 0\n", commandLog))
			}

			out, err := runInstallWithEnv(t, pkg, prefix, []string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1",
				tc.envKey + "=" + serviceDir,
			}, "--no-install-deps", "--no-config-update", "--no-restart")
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "refusing to mutate foreign") {
				t.Fatalf("missing foreign-service refusal:\n%s", out)
			}
			got, err := os.ReadFile(foreignPath)
			if err != nil || string(got) != foreign {
				t.Fatalf("foreign service mutated: %q, err=%v", got, err)
			}
			if data, err := os.ReadFile(commandLog); err == nil && len(data) != 0 {
				t.Fatalf("foreign service invoked service manager:\n%s", data)
			}
		})
	}
}

func TestUninstallPreservesForeignCanonicalServiceArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		osName     string
		serviceRel string
		envKey     string
		fakeTools  []string
	}{
		{name: "linux-systemd", osName: "Linux", serviceRel: "routerd.service", envKey: "ROUTERD_UNINSTALL_SYSTEMD_SYSTEM_DIR", fakeTools: []string{"systemctl"}},
		{name: "freebsd-rcd", osName: "FreeBSD", serviceRel: "routerd", envKey: "ROUTERD_UNINSTALL_RCD_DIR", fakeTools: []string{"service", "sysrc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			prefix := filepath.Join(dir, "prefix")
			serviceDir := filepath.Join(dir, "service")
			binDir := filepath.Join(dir, "bin")
			commandLog := filepath.Join(dir, "service-manager.log")
			if err := os.MkdirAll(filepath.Join(serviceDir, filepath.Dir(tc.serviceRel)), 0o755); err != nil {
				t.Fatal(err)
			}
			foreignPath := filepath.Join(serviceDir, tc.serviceRel)
			foreign := "# administrator-owned canonical service\nforeign-content\n"
			if err := os.WriteFile(foreignPath, []byte(foreign), 0o755); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(binDir, "uname"), fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"-s\" ]; then echo %s; else echo x86_64; fi\n", tc.osName))
			for _, tool := range tc.fakeTools {
				writeExecutable(t, filepath.Join(binDir, tool), fmt.Sprintf("#!/bin/sh\necho %s \"$@\" >> %q\nexit 0\n", tool, commandLog))
			}
			writeExecutable(t, filepath.Join(binDir, "rm"), `#!/bin/sh
for arg in "$@"; do
  case "$arg" in /run/routerd|/var/run/routerd) exit 0 ;; esac
done
exec /bin/rm "$@"
`)
			script := filepath.Join(repoRoot(t), "packaging", "uninstall.sh")
			cmd := exec.Command(script, "--prefix", prefix, "--yes")
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ROUTERD_UNINSTALL_FORCE_SERVICE_MANAGER=1",
				tc.envKey+"="+serviceDir,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("uninstall failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "preserving foreign") {
				t.Fatalf("missing foreign-service preservation:\n%s", out)
			}
			got, err := os.ReadFile(foreignPath)
			if err != nil || string(got) != foreign {
				t.Fatalf("foreign service mutated: %q, err=%v", got, err)
			}
			if data, err := os.ReadFile(commandLog); err == nil && len(data) != 0 {
				t.Fatalf("foreign service invoked service manager:\n%s", data)
			}
		})
	}
}

func TestUninstallRemovesOwnedCanonicalServiceArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		osName     string
		serviceRel string
		envKey     string
		fakeTools  []string
	}{
		{name: "linux-systemd", osName: "Linux", serviceRel: "routerd.service", envKey: "ROUTERD_UNINSTALL_SYSTEMD_SYSTEM_DIR", fakeTools: []string{"systemctl"}},
		{name: "freebsd-rcd", osName: "FreeBSD", serviceRel: "routerd", envKey: "ROUTERD_UNINSTALL_RCD_DIR", fakeTools: []string{"service", "sysrc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			prefix := filepath.Join(dir, "prefix")
			serviceDir := filepath.Join(dir, "service")
			binDir := filepath.Join(dir, "bin")
			commandLog := filepath.Join(dir, "service-manager.log")
			if err := os.MkdirAll(serviceDir, 0o755); err != nil {
				t.Fatal(err)
			}
			ownedPath := filepath.Join(serviceDir, tc.serviceRel)
			if err := os.WriteFile(ownedPath, []byte("# routerd-managed-service: v1\nrouterd-owned\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(binDir, "uname"), fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"-s\" ]; then echo %s; else echo x86_64; fi\n", tc.osName))
			for _, tool := range tc.fakeTools {
				writeExecutable(t, filepath.Join(binDir, tool), fmt.Sprintf("#!/bin/sh\necho %s \"$@\" >> %q\nexit 0\n", tool, commandLog))
			}
			writeExecutable(t, filepath.Join(binDir, "rm"), `#!/bin/sh
for arg in "$@"; do
  case "$arg" in /run/routerd|/var/run/routerd) exit 0 ;; esac
done
exec /bin/rm "$@"
`)
			script := filepath.Join(repoRoot(t), "packaging", "uninstall.sh")
			cmd := exec.Command(script, "--prefix", prefix, "--yes")
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ROUTERD_UNINSTALL_FORCE_SERVICE_MANAGER=1",
				tc.envKey+"="+serviceDir,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("uninstall failed: %v\n%s", err, out)
			}
			if _, err := os.Stat(ownedPath); !os.IsNotExist(err) {
				t.Fatalf("owned service artifact remains, stat err=%v", err)
			}
			data, err := os.ReadFile(commandLog)
			if err != nil || len(data) == 0 {
				t.Fatalf("owned service did not use service manager: %q, err=%v", data, err)
			}
		})
	}
}

func TestInstallDryRunCreatesRouterdGroupBeforeSystemdUnit(t *testing.T) {
	requireLinuxSystemdFixture(t)
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

func TestBootstrapInstallerIsPublishedByReleaseWorkflow(t *testing.T) {
	root := repoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(workflow)
	for _, needle := range []string{
		"packaging/bootstrap.sh",
		"install.sh",
	} {
		if !strings.Contains(content, needle) {
			t.Fatalf("release workflow missing bootstrap installer reference %q", needle)
		}
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
