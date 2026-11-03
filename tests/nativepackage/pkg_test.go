// SPDX-License-Identifier: BSD-3-Clause

package nativepackage_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFreeBSDNativePackageOwnershipAndUpgradeHooks(t *testing.T) {
	if runtime.GOOS != "freebsd" {
		t.Skip("requires native FreeBSD pkg")
	}
	payload, out := fixture(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(payload, "share/doc/TARGET"), []byte("freebsd-amd64\n"), 0644); err != nil {
		t.Fatal(err)
	}
	version := "1.5.0.a202611030100"
	cmd := exec.Command("python3", "../../scripts/build-native-package.py", "--format", "pkg", "--payload", payload, "--output", out, "--version", version, "--arch", "amd64")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v %s", err, output)
	}
	file := filepath.Join(out, "routerd-"+version+".pkg")
	metadata, err := exec.Command("pkg", "info", "-R", "--raw-format=json", "-F", file).CombinedOutput()
	if err != nil {
		t.Fatalf("native manifest: %v %s", err, metadata)
	}
	var manifest struct {
		Name    string                     `json:"name"`
		ABI     string                     `json:"abi"`
		Deps    map[string]json.RawMessage `json:"deps"`
		Scripts map[string]string          `json:"scripts"`
	}
	if err := json.Unmarshal(metadata, &manifest); err != nil {
		t.Fatalf("manifest: %v %s", err, metadata)
	}
	if manifest.Name != "routerd" || manifest.ABI != "FreeBSD:14:amd64" {
		t.Fatalf("incorrect native metadata: %+v", manifest)
	}
	if _, ok := manifest.Deps["dnsmasq"]; !ok {
		t.Error("missing native dnsmasq dependency")
	}
	for _, phase := range []string{"pre-install", "post-install", "pre-deinstall", "post-deinstall"} {
		if manifest.Scripts[phase] == "" {
			t.Errorf("missing %s hook", phase)
		}
	}
	root := t.TempDir()
	if output, err := exec.Command("tar", "-xf", file, "-C", root).CombinedOutput(); err != nil {
		t.Fatalf("native extraction: %v %s", err, output)
	}
	for _, path := range []string{"usr/local/sbin/routerd", "usr/local/bin/routerctl", "usr/local/libexec/routerd/dhcp-event-relay", "usr/local/etc/rc.d/routerd"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("missing native path %s: %v", path, err)
		}
	}
	for _, path := range []string{"usr/local/etc/routerd/router.yaml", "var/db/routerd/routerd.db", "install.sh", "uninstall.sh"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("package must not ship %s: %v", path, err)
		}
	}
}
