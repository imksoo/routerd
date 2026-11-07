// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiDSLiteRejectsCollisionsBeforeCreatingFixtures(t *testing.T) {
	for _, path := range []string{"/usr/sbin/routerd", "/etc/routerd/router.yaml", "/var/lib/routerd/routerd.db"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Skip("requires no installed routerd at " + path)
		}
	}
	for _, scenario := range []string{"namespace", "interface"} {
		t.Run(scenario, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			marker := filepath.Join(root, "unexpected-mutation")
			commands := map[string]string{
				"id":                  "echo 0\n",
				"systemd-detect-virt": "exit 0\n",
				"dpkg":                "exit 0\n",
				"dpkg-deb":            "if [ \"$3\" = Package ]; then echo routerd; elif [ \"${2##*/}\" = current.deb ]; then echo 2; else echo 1; fi\n",
				"mktemp":              "touch \"$GUARD_MARKER\"\nexit 77\n",
				"ip": `if [ "$1 $2" = 'netns list' ]; then
 [ "$GUARD_SCENARIO" != namespace ] || echo rd15-mclient
 exit 0
fi
if [ "$1 $2" = 'link show' ]; then
 if [ "$3" = eth0 ]; then exit 0; fi
 if [ "$GUARD_SCENARIO" = interface ] && [ "$3" = rd15-mwan ]; then exit 0; fi
 exit 1
fi
exit 90
`,
			}
			for _, name := range []string{"jq", "curl", "python3", "nft", "conntrack", "tcpdump", "apt-get", "timeout"} {
				commands[name] = "exit 90\n"
			}
			for name, body := range commands {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0755); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"../../scripts/linux-multi-dslite-smoke.sh", "--disposable-vm"}
			for _, name := range []string{"fixture.json", "prior.deb", "current.deb"} {
				path := filepath.Join(root, name)
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, path)
			}
			command := exec.Command("bash", append(args, "eth0")...)
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "GUARD_MARKER="+marker, "GUARD_SCENARIO="+scenario)
			out, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "existing "+scenario) {
				t.Fatalf("collision was not rejected before fixture creation: %v: %s", err, out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("fixture creation was attempted after a collision")
			}
		})
	}
}
