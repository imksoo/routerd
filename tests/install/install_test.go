// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

func TestInstallWaitsForConcurrentAptAndDpkgOwners(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, want := range []string{
		"apt-get -o DPkg::Lock::Timeout=300 update",
		"apt-get -o DPkg::Lock::Timeout=300 install -y ${packages}",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("install.sh must tolerate bounded apt/dpkg lock contention via %q", want)
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

func TestInstallConfigureKeepsOneConfigBackupGeneration(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if strings.Contains(text, `backup.${timestamp}`) {
		t.Fatalf("install.sh still creates timestamped config backups")
	}
	for _, want := range []string{
		"backup_config_one_generation()",
		`backup="${target}.backup"`,
		`find "${dir}" -maxdepth 1 -type f -name "${base}.backup.*" -exec rm -f {} +`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("install.sh missing one-generation backup contract %q", want)
		}
	}
}

func TestUninstallCoversShippedBinaryAndPluginPayload(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	uninstaller, err := os.ReadFile(filepath.Join(repoRoot(t), "packaging", "uninstall.sh"))
	if err != nil {
		t.Fatal(err)
	}

	// Makefile's dist recipe is the release payload manifest. install.sh
	// installs every bin/* file and every supported libexec plugin file, so
	// every such shipped path must have an exact uninstall counterpart.
	distPathRE := regexp.MustCompile(`\$\(DISTROOT\)/(bin/[A-Za-z0-9._-]+|libexec/routerd/plugins/[A-Za-z0-9._/-]+)`)
	matches := distPathRE.FindAllSubmatch(makefile, -1)
	if len(matches) == 0 {
		t.Fatal("no shipped binary/plugin paths found in dist recipe")
	}
	for _, match := range matches {
		shipped := string(match[1])
		var uninstallPath string
		if strings.HasPrefix(shipped, "bin/") {
			uninstallPath = strings.TrimPrefix(shipped, "bin/")
		} else {
			uninstallPath = shipped
		}
		if !strings.Contains(string(uninstaller), uninstallPath) {
			t.Errorf("shipped payload %q has no exact uninstall entry", shipped)
		}
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
