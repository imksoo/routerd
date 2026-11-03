// SPDX-License-Identifier: BSD-3-Clause
package nativepackage_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This unit captures the manifest passed to pkg; it does not claim native
// package-format or ELF qualification. The FreeBSD test uses native tools.
func TestFreeBSDManifestRetainsNativeNDPIRequirements(t *testing.T) {
	payload, out, bin := fixture(t), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(payload, "share/doc/TARGET"), []byte("freebsd-amd64\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "bin/routerd-ndpi-agent"), []byte("fixture ELF supplied by native build"), 0755); err != nil {
		t.Fatal(err)
	}
	fake := `#!/usr/bin/env python3
import pathlib,shutil,sys
args=sys.argv[1:]
if args[0]=='query':
 print('dnsmasq\t2.93\tdns/dnsmasq\nca_root_nss\t3.132\tsecurity/ca_root_nss\nndpi\t5.0\tnet/ndpi\nlibgcrypt\t1.11\tsecurity/libgcrypt')
elif args[0]=='which':
 print('ndpi-5.0' if 'libndpi' in args[-1] else 'libgcrypt-1.11')
else:
 shutil.copyfile(args[args.index('-M')+1],pathlib.Path(args[args.index('-o')+1])/'manifest.json')
`
	if err := os.WriteFile(filepath.Join(bin, "pkg"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	readelf := `#!/bin/sh
kind='(NEEDED)'
[ "$TEST_NEEDED_STYLE" != freebsd ] || kind=NEEDED
for library in libndpi.so.5 libgcrypt.so.20 libc.so.7; do
 printf '0x01 %s Shared library: [%s]\n' "$kind" "$library"
done
`
	if err := os.WriteFile(filepath.Join(bin, "readelf"), []byte(readelf), 0755); err != nil {
		t.Fatal(err)
	}
	ldd := "#!/bin/sh\nprintf 'libndpi.so.5\t/usr/local/lib/libndpi.so.5\nlibgcrypt.so.20\t/usr/local/lib/libgcrypt.so.20\nlibc.so.7\t/lib/libc.so.7\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "ldd"), []byte(ldd), 0755); err != nil {
		t.Fatal(err)
	}
	for _, style := range []string{"gnu", "freebsd"} {
		t.Run(style, func(t *testing.T) {
			cmd := exec.Command("python3", "../../scripts/build-native-package.py", "--format", "pkg", "--payload", payload, "--output", out, "--version", "1.5.0.a20261103060000", "--arch", "amd64")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "TEST_NEEDED_STYLE="+style)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("manifest construction: %v %s", err, output)
			}
			data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				Deps map[string]struct {
					Version string `json:"version"`
					Origin  string `json:"origin"`
				} `json:"deps"`
				Libraries []string `json:"shlibs_required"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			dep, ok := manifest.Deps["ndpi"]
			if !ok || dep.Version != "5.0" || dep.Origin != "net/ndpi" {
				t.Errorf("missing native ndpi dependency: %+v", manifest.Deps)
			}
			if strings.Join(manifest.Libraries, ",") != "libgcrypt.so.20,libndpi.so.5" {
				t.Errorf("missing exact ELF library requirement: %+v", manifest.Libraries)
			}
			if _, ok := manifest.Deps["libgcrypt"]; !ok {
				t.Error("direct crypto library provider is not a package dependency")
			}
		})
	}
}

func TestNativeAgentBuildCheckRequiresSuccessfulSelftest(t *testing.T) {
	bin := t.TempDir()
	agent := filepath.Join(bin, "agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\necho '{\"libndpiLoaded\":true}'\nexit 42\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "file"), []byte("#!/bin/sh\necho 'ELF dynamically linked'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	tool := "make"
	if runtime.GOOS == "freebsd" {
		tool = "gmake"
	}
	cmd := exec.Command(tool, "-C", "../..", "check-ndpi-agent-libndpi", "ROUTERD_OS=linux", "ROUTERD_NDPI_AGENT_BIN="+agent)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("native build accepted a failed selftest: %s", output)
	}
}
