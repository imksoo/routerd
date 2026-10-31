//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNAT44SessionSyncDerivedDaemonPackage(t *testing.T) {
	dpkg, err := exec.LookPath("dpkg-query")
	if err != nil {
		t.Skip("requires actual dpkg-query against a private database")
	}
	for _, mode := range []string{"conntrackd", "event-stream", ""} {
		label := mode
		if label == "" {
			label = "default"
		}
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			installed := false
			writePrivateDB := func() {
				t.Helper()
				names := []string{"iproute2", "nftables", "conntrack", "kmod"}
				if installed {
					names = append(names, "conntrackd")
				}
				var data strings.Builder
				for _, name := range names {
					data.WriteString("Package: " + name + "\nStatus: install ok installed\nArchitecture: all\nVersion: 1\nMaintainer: Routerd Test <test@example.invalid>\nDescription: private conntrackd dependency test\n\n")
				}
				if err := os.WriteFile(filepath.Join(dir, "status"), []byte(data.String()), 0600); err != nil {
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
			var queried []string
			spec := api.NAT44SessionSyncSpec{Mode: mode, NATRules: []string{"NAT44Rule/outbound"}}
			if mode == "conntrackd" {
				spec.Conntrackd = &api.ConntrackdSyncSpec{Interface: "lan", LocalAddress: "198.18.96.1", PeerAddress: "198.18.96.2", Port: 3780}
			} else {
				spec.Targets = []api.NAT44SessionSyncTargetSpec{{Name: "peer", Host: "198.18.96.2", User: "root"}}
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "outbound"}, Spec: api.NAT44RuleSpec{Type: "snat", EgressInterface: "lan", SourceRanges: []string{"198.18.97.0/24"}, SNATAddress: "198.18.96.1"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "sessions"}, Spec: spec},
			}}}
			c := PackageController{OSName: "ubuntu", Router: router, Store: store,
				Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name == "dpkg-query" {
						queried = append(queried, args[len(args)-1])
						return exec.CommandContext(ctx, dpkg, append([]string{"--admindir=" + dir}, args...)...).CombinedOutput()
					}
					if name == "sudo" && len(args) > 0 {
						name, args = args[0], args[1:]
					}
					if name != "apt-get" || strings.Join(args, " ") != "install -y conntrackd" {
						t.Fatalf("unexpected package effect: %s %v", name, args)
					}
					// Only the private dpkg inventory changes; never dispatch apt.
					attempts++
					installed = true
					writePrivateDB()
					return nil, nil
				},
			}
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "conntrackd" {
				want = 1
			}
			status, err := store.ReadObjectStatus(api.SystemAPIVersion, "Package", "router-runtime")
			if err != nil || status["phase"] != "Applied" || attempts != want || slices.Contains(queried, "conntrackd") != (want == 1) {
				t.Fatalf("mode=%q installed conntrack hid daemon requirement: attempts=%d queried=%v status=%+v err=%v", mode, attempts, queried, status, err)
			}
			if err := c.Reconcile(t.Context()); err != nil || attempts != want {
				t.Fatalf("converged dependency requested another effect: attempts=%d err=%v", attempts, err)
			}
		})
	}
}
