// SPDX-License-Identifier: BSD-3-Clause

package firewall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

// Run the compiled test with explicit sudo/unshare; ordinary go test cannot
// change networking. The namespace must differ from the host namespace.
func TestFirewallNativeWithdrawal(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated netns execution")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Readlink("/proc/1/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	if current == host {
		t.Fatal("refusing host network namespace")
	}
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("nft", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("nft %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	for _, check := range []string{"kernel-owner", "withdraw"} {
		t.Run(check, func(t *testing.T) {
			t.Cleanup(func() { _ = exec.Command("nft", "delete", "table", "inet", "routerd_filter").Run() })
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lo"}}},
			}}}
			c := Controller{Router: router, Store: firewallMapStore{}, NftablesPath: filepath.Join(t.TempDir(), "firewall.nft")}
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if check == "kernel-owner" {
				if got := run("-j", "list", "table", "inet", "routerd_filter"); !strings.Contains(got, render.NftablesRouterdOwnerMarker) {
					t.Fatalf("kernel table has no owner marker: %s", got)
				}
				return
			}
			// A fresh controller models config reload/restart: no in-memory history.
			c = Controller{Router: &api.Router{}, Store: c.Store, NftablesPath: c.NftablesPath}
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := run("-j", "list", "tables"); strings.Contains(got, "routerd_filter") {
				t.Fatalf("removed configuration left live filter: %s", got)
			}
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatalf("second withdrawal: %v", err)
			}
		})
	}
}
