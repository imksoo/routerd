// SPDX-License-Identifier: BSD-3-Clause

package nativepackage_test

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	if info, err := os.Stat(filepath.Join(root, "usr/local/etc/rc.d/routerd")); err != nil || info.Mode().Perm() != 0555 {
		t.Errorf("native rc.d script must be executable and mode 0555: %v %v", info, err)
	}
	for _, path := range []string{"usr/local/etc/routerd/router.yaml", "var/db/routerd/routerd.db", "install.sh", "uninstall.sh"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("package must not ship %s: %v", path, err)
		}
	}
	if manifest.Scripts["pre-install"] != "" {
		t.Run("native hook stop and restore", func(t *testing.T) {
			base, bin, rc := t.TempDir(), t.TempDir(), t.TempDir()
			state := filepath.Join(base, "restart")
			running := filepath.Join(base, "running")
			if err := os.WriteFile(running, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(rc, "routerd"), []byte("#!/bin/sh\n"), 0755); err != nil {
				t.Fatal(err)
			}
			fake := "#!/bin/sh\ncase \"$2\" in\nonestatus) test -f \"$TEST_RUNNING\";;\nonestop) rm -f \"$TEST_RUNNING\";;\nonestart) touch \"$TEST_RUNNING\";;\n*) exit 91;;\nesac\n"
			if err := os.WriteFile(filepath.Join(bin, "service"), []byte(fake), 0755); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"pre-install", "post-install"} {
				// pkg raw manifests URL-encode script bytes; pkg decodes them before execution.
				body, err := url.PathUnescape(manifest.Scripts[phase])
				if err != nil {
					t.Fatal(err)
				}
				body = strings.ReplaceAll(body, "/usr/local/etc/rc.d", rc)
				body = strings.ReplaceAll(body, "/var/run/routerd-package", state)
				script := filepath.Join(base, phase)
				if err := os.WriteFile(script, []byte(body), 0755); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("sh", script)
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "TEST_RUNNING="+running, "POSIXLY_CORRECT=1")
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s with native BSD file utilities: %v %s", phase, err, output)
				}
				if phase == "pre-install" {
					if _, err := os.Stat(running); !os.IsNotExist(err) {
						t.Fatalf("old service remained: %v", err)
					}
					if intent, err := os.ReadFile(filepath.Join(state, "restart-units")); err != nil || string(intent) != "routerd\n" {
						t.Fatalf("missing recovery intent: %q %v", intent, err)
					}
				}
			}
			if _, err := os.Stat(running); err != nil {
				t.Fatal("prior active service was not restored:", err)
			}
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Fatalf("native hook left temporary state: %v", err)
			}
		})
	}
}
