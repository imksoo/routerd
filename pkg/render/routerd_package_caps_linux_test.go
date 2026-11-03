// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in root test: only temporary files and a child process capability set
// change. No packages, services, network state or parent capabilities change.
func TestRouterdPackageCachePermissionsNative(t *testing.T) {
	if directory := os.Getenv("ROUTERD_PACKAGE_CAP_CHILD"); directory != "" {
		partial := filepath.Join(directory, "partial")
		switch os.Getenv("ROUTERD_PACKAGE_CAP_ACTION") {
		case "chmod":
			if err := os.Chmod(partial, 0700); err != nil {
				t.Fatalf("apt cannot set mode on its downloader-owned partial directory: %v", err)
			}
		case "rename":
			if err := os.Rename(filepath.Join(partial, "download.deb"), filepath.Join(directory, "ready.deb")); err != nil {
				t.Fatalf("apt cannot publish from its downloader-owned partial directory: %v", err)
			}
		default:
			t.Fatal("unknown child action")
		}
		return
	}
	if os.Getenv("ROUTERD_RUN_PACKAGE_CAPS") != "1" {
		t.Skip("explicit ROUTERD_RUN_PACKAGE_CAPS=1 and root required; temporary filesystem effects only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("native permission test requires root")
	}
	tool, err := exec.LookPath("setpriv")
	if err != nil {
		t.Fatal(err)
	}
	var capabilities []string
	for _, capability := range RouterdServiceSystemdSpec().CapabilityBoundingSet {
		capabilities = append(capabilities, "+"+strings.ToLower(strings.TrimPrefix(capability, "CAP_")))
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"chmod", "rename"} {
		t.Run(action, func(t *testing.T) {
			directory := t.TempDir()
			partial := filepath.Join(directory, "partial")
			if err := os.Mkdir(partial, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(partial, "download.deb"), []byte("test download"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(partial, 65534, 65534); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(tool, "--bounding-set=-all,"+strings.Join(capabilities, ","),
				"--inh-caps=-all", "--ambient-caps=-all", binary, "-test.run=^TestRouterdPackageCachePermissionsNative$", "-test.v")
			command.Env = append(os.Environ(), "ROUTERD_PACKAGE_CAP_CHILD="+directory, "ROUTERD_PACKAGE_CAP_ACTION="+action)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("packaged main-service capabilities prevent apt %s: %v\n%s", action, err, output)
			}
		})
	}
}
