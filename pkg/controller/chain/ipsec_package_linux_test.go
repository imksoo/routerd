//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestIPsecDerivedPackageRequiresDaemonWhenSwanctlIsAlreadyInstalled(t *testing.T) {
	dpkg, err := exec.LookPath("dpkg-query")
	if err != nil {
		t.Skip("requires actual dpkg-query against a private database")
	}
	for _, osName := range []string{"debian", "ubuntu"} {
		t.Run(osName, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "status")
			installed := false
			writePrivateDB := func() {
				t.Helper()
				names := []string{"strongswan-swanctl"}
				if installed {
					names = append(names, "charon-systemd")
				}
				var data strings.Builder
				for _, name := range names {
					data.WriteString("Package: " + name + "\nStatus: install ok installed\nArchitecture: all\nVersion: 1\nMaintainer: Routerd Test <test@example.invalid>\nDescription: private IPsec dependency test\n\n")
				}
				if err := os.WriteFile(path, []byte(data.String()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writePrivateDB()
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			attempts := 0
			c := PackageController{OSName: osName, Store: store,
				Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"}, Metadata: api.ObjectMeta{Name: "site"},
					Spec: api.IPsecConnectionSpec{LocalAddress: "198.18.94.1", RemoteAddress: "198.18.94.2", LeftSubnet: "10.254.94.1/32", RightSubnet: "10.254.94.2/32", PreSharedKey: "disposable"},
				}}}},
				Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name == "dpkg-query" {
						return exec.CommandContext(ctx, dpkg, append([]string{"--admindir=" + directory}, args...)...).CombinedOutput()
					}
					if name == "sudo" && len(args) > 0 {
						name, args = args[0], args[1:]
					}
					if name != "apt-get" || strings.Join(args, " ") != "install -y charon-systemd" {
						t.Fatalf("unexpected dependency installation boundary: %s %v", name, args)
					}
					// Never dispatch apt. Acknowledge only in the private dpkg database.
					attempts++
					installed = true
					writePrivateDB()
					return nil, nil
				},
			}
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if attempts != 1 {
				t.Fatalf("installed swanctl hid the absent IKE daemon: attempts=%d status=%+v", attempts, store.ObjectStatus(api.SystemAPIVersion, "Package", "router-runtime"))
			}
			if err := c.Reconcile(t.Context()); err != nil || attempts != 1 {
				t.Fatalf("installed daemon was requested again: attempts=%d err=%v", attempts, err)
			}
		})
	}
}
