// SPDX-License-Identifier: BSD-3-Clause
package nativepackage_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNDPIAddonUsesNativeDiversionAndRestoresBaseFile(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("requires native Debian tools")
	}
	payload, out, bin := fixture(t), t.TempDir(), t.TempDir()
	native := "#!/bin/sh\nprintf '%s\\n' '{\"libndpiLoaded\":true}'\n"
	if err := os.WriteFile(filepath.Join(payload, "bin/routerd-ndpi-agent"), []byte(native), 0755); err != nil {
		t.Fatal(err)
	}
	// Metadata uses the native dependency scanner; format this test's shell
	// fixture as a scanner result. Real ELF dependency validation is a build gate.
	if err := os.WriteFile(filepath.Join(bin, "dpkg-shlibdeps"), []byte("#!/bin/sh\necho 'shlibs:Depends=libc6 (>= 2.34), libndpi4.2 (>= 4.2)'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "../../scripts/build-native-package.py", "--format", "deb", "--component", "routerd-ndpi-agent-libndpi", "--payload", payload, "--output", out, "--version", "1.5.0~20261103050000", "--arch", "amd64")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build addon: %v %s", err, output)
	}
	artifact := filepath.Join(out, "routerd-ndpi-agent-libndpi_1.5.0~20261103050000_amd64.deb")
	metadata, err := exec.Command("dpkg-deb", "-f", artifact, "Package", "Depends").CombinedOutput()
	if err != nil {
		t.Fatal(err, string(metadata))
	}
	for _, want := range []string{"routerd-ndpi-agent-libndpi", "routerd (= 1.5.0~20261103050000)", "libndpi4.2"} {
		if !strings.Contains(string(metadata), want) {
			t.Errorf("missing %q: %s", want, metadata)
		}
	}
	staged, control, root := t.TempDir(), t.TempDir(), t.TempDir()
	for _, args := range [][]string{{"-x", artifact, staged}, {"-e", artifact, control}} {
		if output, err := exec.Command("dpkg-deb", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(output))
		}
	}
	for _, name := range []string{"usr/sbin/routerd", "usr/lib/systemd/system/routerd.service", "etc/routerd/router.yaml"} {
		if _, err := os.Lstat(filepath.Join(staged, name)); !os.IsNotExist(err) {
			t.Errorf("addon must not own %s", name)
		}
	}
	agent := "/usr/sbin/routerd-ndpi-agent"
	target := root + agent
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var/lib/dpkg"), 0755); err != nil {
		t.Fatal(err)
	}
	base := []byte("#!/bin/sh\necho base-static-agent\n")
	if err := os.WriteFile(target, base, 0755); err != nil {
		t.Fatal(err)
	}
	run := func(phase, action string, wantSuccess bool) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(control, phase))
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ReplaceAll(string(body), "/run/systemd/system", filepath.Join(root, "no-systemd"))
		s = strings.ReplaceAll(s, `"$agent" selftest`, `"$DPKG_ROOT$agent" selftest`)
		script := filepath.Join(control, phase+".isolated")
		if err := os.WriteFile(script, []byte(s), 0755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", script, action)
		cmd.Env = append(os.Environ(), "DPKG_ROOT="+root)
		if output, err := cmd.CombinedOutput(); (err == nil) != wantSuccess {
			t.Fatalf("%s %s success=%v want=%v: %v %s", phase, action, err == nil, wantSuccess, err, output)
		}
	}
	foreign := func(action string) {
		t.Helper()
		cmd := exec.Command("dpkg-divert", "--root", root, "--package", "foreign-addon", "--divert", agent+".foreign", "--"+action, "--rename", agent)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("foreign diversion fixture: %v %s", err, output)
		}
	}
	foreign("add")
	run("preinst", "install", false)
	if contents, err := os.ReadFile(target + ".foreign"); err != nil || string(contents) != string(base) {
		t.Fatalf("foreign file changed: %q %v", contents, err)
	}
	foreign("remove")
	run("preinst", "install", true)
	diverted, err := os.ReadFile(target + ".routerd-static")
	if err != nil || string(diverted) != string(base) {
		t.Fatalf("base was not diverted: %q %v", diverted, err)
	}
	if err := os.WriteFile(target, []byte(native), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(native+"exit 42\n"), 0755); err != nil {
		t.Fatal(err)
	}
	run("postinst", "configure", false)
	if err := os.WriteFile(target, []byte(native), 0755); err != nil {
		t.Fatal(err)
	}
	run("postinst", "configure", true)
	run("preinst", "upgrade", true)
	// Native dpkg resolves a base package's logical path to this diverted name.
	cmd = exec.Command("dpkg-divert", "--root", root, "--truename", agent)
	destination, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(destination)) != agent+".routerd-static" {
		t.Fatalf("wrong base update destination: %s", destination)
	}
	base = []byte("#!/bin/sh\necho updated-base-static-agent\n")
	if err := os.WriteFile(target+".routerd-static", base, 0755); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(target); err != nil || string(contents) != native {
		t.Fatalf("base update overwrote native agent: %q %v", contents, err)
	}
	run("prerm", "remove", true)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	} // dpkg removes addon-owned file.
	run("postrm", "remove", true)
	run("postrm", "purge", true)
	foreign("add")
	run("postrm", "purge", false)
	if contents, err := os.ReadFile(target + ".foreign"); err != nil || string(contents) != string(base) {
		t.Fatalf("foreign diversion removed: %q %v", contents, err)
	}
	foreign("remove")
	restored, err := os.ReadFile(target)
	if err != nil || string(restored) != string(base) {
		t.Fatalf("base not restored: %q %v", restored, err)
	}
}
