// SPDX-License-Identifier: BSD-3-Clause

package nativepackage_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{
		"bin/routerd":                                 "#!/bin/sh\necho routerd-test\n",
		"bin/routerctl":                               "#!/bin/sh\necho routerctl-test\n",
		"bin/routerd-dhcp-event-relay":                "#!/bin/sh\nexit 0\n",
		"libexec/routerd/plugins/example/plugin.yaml": "name: example\n",
		"share/doc/LICENSE":                           "test fixture license\n",
		"share/doc/TARGET":                            "linux-amd64\n",
		"etc/routerd/router.yaml.sample":              "apiVersion: routerd.net/v1alpha1\nkind: Router\n",
	} {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func build(t *testing.T, payload, out, arch string) ([]byte, error) {
	t.Helper()
	script := filepath.Join("..", "..", "scripts", "build-native-package.py")
	return exec.Command("python3", script, "--format", "deb", "--payload", payload, "--output", out, "--version", "1.5.0~202611030100", "--arch", arch).CombinedOutput()
}

func TestDebOwnsNativeFilesAndLeavesOperatorConfigurationAbsent(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("requires native dpkg-deb")
	}
	payload, out := fixture(t), t.TempDir()
	if err := os.Chmod(filepath.Join(payload, "share/doc/LICENSE"), 0666); err != nil {
		t.Fatal(err)
	}
	if output, err := build(t, payload, out, "amd64"); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	packageFile := filepath.Join(out, "routerd_1.5.0~202611030100_amd64.deb")
	metadata, err := exec.Command("dpkg-deb", "-f", packageFile, "Package", "Version", "Architecture", "Depends").CombinedOutput()
	if err != nil {
		t.Fatalf("metadata: %v\n%s", err, metadata)
	}
	for _, want := range []string{"Package: routerd", "Version: 1.5.0~202611030100", "Architecture: amd64", "Depends: systemd (>= 249)"} {
		if !strings.Contains(string(metadata), want) {
			t.Errorf("missing %q in %s", want, metadata)
		}
	}
	root := t.TempDir()
	if output, err := exec.Command("dpkg-deb", "-x", packageFile, root).CombinedOutput(); err != nil {
		t.Fatalf("extract: %v %s", err, output)
	}
	for _, path := range []string{"usr/sbin/routerd", "usr/bin/routerctl", "usr/libexec/routerd/dhcp-event-relay", "usr/libexec/routerd/plugins/example/plugin.yaml", "usr/share/doc/routerd/LICENSE", "usr/lib/systemd/system/routerd.service"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("missing packaged path %s: %v", path, err)
		}
	}
	for _, path := range []string{"usr/local", "etc/routerd/router.yaml", "var/lib/routerd/routerd.db", "install.sh", "uninstall.sh"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("package must not ship %s: %v", path, err)
		}
	}
	for _, path := range []string{"usr", "usr/bin", "usr/lib", "usr/lib/systemd/system", "usr/share/doc/routerd/examples"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0755 {
			t.Errorf("native directory %s has mode %o, want 755 independent of build umask", path, info.Mode().Perm())
		}
	}
	for _, path := range []string{"usr/lib/systemd/system/routerd.service", "usr/share/doc/routerd/LICENSE"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0022 != 0 {
			t.Errorf("native package file %s is group/world writable: %o", path, info.Mode().Perm())
		}
	}
}

func TestDebRejectsWrongPayloadArchitecture(t *testing.T) {
	payload, out := fixture(t), t.TempDir()
	output, err := build(t, payload, out, "arm64")
	if err == nil || !strings.Contains(string(output), "payload target") {
		t.Fatalf("wrong target accepted: %v %s", err, output)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid input created artifacts: %v", entries)
	}
}

func TestFailedNativeBuildDoesNotPublishPartialPackage(t *testing.T) {
	payload, out, bin := fixture(t), t.TempDir(), t.TempDir()
	fake := "#!/bin/sh\nfor target; do :; done\nprintf partial > \"$target\"\nexit 42\n"
	if err := os.WriteFile(filepath.Join(bin, "dpkg-deb"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "../../scripts/build-native-package.py", "--format", "deb", "--payload", payload, "--output", out, "--version", "1.5.0~202611030100", "--arch", "amd64")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("native tool failure must propagate: %s", output)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed build exposed partial artifacts: %v %v", entries, err)
	}
}

// Execute the package's actual maintainer script with isolated state and fake
// service commands. No package installation or host service operation occurs.
func TestDebUpgradeRefusesReplacementWhenOldServiceDoesNotStop(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("requires native dpkg-deb")
	}
	payload, out := fixture(t), t.TempDir()
	if output, err := build(t, payload, out, "amd64"); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	control := t.TempDir()
	file := filepath.Join(out, "routerd_1.5.0~202611030100_amd64.deb")
	if output, err := exec.Command("dpkg-deb", "-e", file, control).CombinedOutput(); err != nil {
		t.Fatalf("control: %v %s", err, output)
	}
	prerm := filepath.Join(control, "prerm")
	script, err := os.ReadFile(prerm)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime := t.TempDir()
	script = []byte(strings.ReplaceAll(string(script), "/run/routerd-package", state))
	script = []byte(strings.ReplaceAll(string(script), "/run/systemd/system", runtime))
	if err := os.WriteFile(prerm, script, 0755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	fake := "#!/bin/sh\ncase \"$1\" in\nlist-units) echo 'routerd.service loaded active running routerd';;\nshow) echo active;;\nstop) exit 0;;\n*) exit 91;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", prerm, "upgrade", "1.5.0~202611030200")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "still active") {
		t.Fatalf("upgrade must refuse an acknowledged but ineffective stop: %v %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(state, "restart-units")); err != nil {
		t.Fatalf("failed stop must retain recovery intent: %v", err)
	}
}

func TestDebUpgradeRestoresActiveServicesWithoutEnablingFreshInstall(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("requires native dpkg-deb")
	}
	out, control, runtime, state := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if output, err := build(t, fixture(t), out, "amd64"); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	file := filepath.Join(out, "routerd_1.5.0~202611030100_amd64.deb")
	if output, err := exec.Command("dpkg-deb", "-e", file, control).CombinedOutput(); err != nil {
		t.Fatalf("control: %v %s", err, output)
	}
	for _, phase := range []string{"prerm", "postinst"} {
		path := filepath.Join(control, phase)
		script, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		isolated := strings.ReplaceAll(string(script), "/run/routerd-package", filepath.Join(state, "restart"))
		isolated = strings.ReplaceAll(isolated, "/run/systemd/system", runtime)
		if err := os.WriteFile(path, []byte(isolated), 0755); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	fake := `#!/bin/sh
set -eu
for unit; do :; done
case "$1" in
list-units)
  for file in "$TEST_STATE"/active/*; do
    [ -f "$file" ] || continue
    echo "${file##*/} loaded active running routerd"
  done;;
show)
  if [ "$2" = --property=FragmentPath ]; then
    if [ "$unit" = routerd-unrelated.service ]; then echo "$TEST_STATE/unrelated-unit"; else echo "$TEST_STATE/owned-unit"; fi
  elif [ -f "$TEST_STATE/active/$unit" ]; then echo active; else echo inactive; fi;;
stop)
  echo "stop $unit" >> "$TEST_STATE/actions"
  rm -f "$TEST_STATE/active/$unit"
  # Reproduce a helper appearing during the daemon's shutdown.
  if [ "$unit" = routerd.service ]; then touch "$TEST_STATE/active/routerd-late.service"; fi;;
start)
  echo "start $unit" >> "$TEST_STATE/actions"
  touch "$TEST_STATE/active/$unit";;
daemon-reload) :;;
*) echo "unexpected systemctl $*" >&2; exit 91;;
esac
`
	for name, body := range map[string]string{"systemctl": fake, "policy-rc.d": "#!/bin/sh\nexit 0\n"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(state, "active"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"owned-unit": "# Managed by routerd. Do not edit by hand.\n", "unrelated-unit": "# Image setup or an operator service\n"} {
		if err := os.WriteFile(filepath.Join(state, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(phase, arg string) {
		t.Helper()
		cmd := exec.Command("sh", filepath.Join(control, phase), arg)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "TEST_STATE="+state)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v %s", phase, arg, err, output)
		}
	}
	run("postinst", "configure")
	if _, err := os.Stat(filepath.Join(state, "actions")); !os.IsNotExist(err) {
		t.Fatalf("fresh install must leave network services inactive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state, "active/routerd.service"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "active/routerd-unrelated.service"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	run("prerm", "upgrade")
	entries, err := os.ReadDir(filepath.Join(state, "active"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "routerd-unrelated.service" {
		t.Fatalf("stop owned services and late helpers, preserving unrelated services: %v %v", entries, err)
	}
	run("postinst", "configure")
	actions, err := os.ReadFile(filepath.Join(state, "actions"))
	if err != nil {
		t.Fatal(err)
	}
	want := "stop routerd.service\nstop routerd-late.service\nstart routerd.service\nstart routerd-late.service\n"
	if string(actions) != want {
		t.Fatalf("restart must preserve prior activation, including disabled active units: %q", actions)
	}
	if _, err := os.Stat(filepath.Join(state, "restart")); !os.IsNotExist(err) {
		t.Fatalf("successful update must consume restart state: %v", err)
	}
}
