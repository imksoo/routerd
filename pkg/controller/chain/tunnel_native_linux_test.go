// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestTunnelNativeLinuxOwnershipBoundaries(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	hostNS := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(hostNS, "net:[") || currentNS == hostNS {
		t.Fatal("outer network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	run := func(args ...string) []byte {
		out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, out)
		}
		return out
	}
	run("link", "add", "name", "tnl-underlay", "type", "dummy")
	run("address", "add", "198.18.0.1/30", "dev", "tnl-underlay")
	run("link", "set", "dev", "tnl-underlay", "up")
	for _, scenario := range []string{"replacement-cleanup", "unacquired-child", "source-expired"} {
		t.Run(scenario, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "tnl-owned"}, Spec: api.TunnelInterfaceSpec{Mode: "ipip", Local: "198.18.0.1", Remote: "198.18.0.2", Address: "192.0.2.1/32", MTU: 1400, TrustedUnderlay: true}},
			}}}
			parent := TunnelInterfaceController{Router: router, Store: eventedStore{Store: store, Router: router}}
			if scenario == "source-expired" {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Local = ""
				spec.LocalFrom = api.StatusValueSourceSpec{Resource: "DHCPv4Client/underlay", Field: "currentAddress"}
				router.Spec.Resources[0].Spec = spec
				if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Bound", "currentAddress": "198.18.0.1/30"}); err != nil {
					t.Fatal(err)
				}
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				defer func() { _, _ = exec.CommandContext(ctx, "ip", "link", "delete", "dev", "tnl-owned").CombinedOutput() }()
				// The DHCPv4 publisher clears currentAddress on real lease expiry.
				if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Expired", "currentAddress": ""}); err != nil {
					t.Fatal(err)
				}
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.CommandContext(ctx, "ip", "-j", "link", "show", "dev", "tnl-owned").CombinedOutput(); err == nil {
					t.Fatalf("expired source retained its applied tunnel: %s", out)
				}
				return
			}
			if scenario == "replacement-cleanup" {
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				run("link", "delete", "dev", "tnl-owned")
			}
			// This object has no routerd cookie, and was created independently.
			run("link", "add", "name", "tnl-owned", "type", "ipip", "local", "198.18.0.1", "remote", "198.18.0.2")
			defer func() { _, _ = exec.CommandContext(ctx, "ip", "link", "delete", "dev", "tnl-owned").CombinedOutput() }()
			if scenario == "replacement-cleanup" {
				parent.Router = &api.Router{}
				err := parent.Reconcile(ctx)
				out, linkErr := exec.CommandContext(ctx, "ip", "-j", "link", "show", "dev", "tnl-owned").CombinedOutput()
				if linkErr != nil {
					t.Fatalf("stale display ownership deleted a same-name replacement: reconcile=%v native=%v %s", err, linkErr, out)
				}
				return
			}
			router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "tnl-owned", Address: "192.0.2.99/32"}})
			child := IPv4StaticAddressController{Router: router, Store: store}
			err = child.Reconcile(ctx)
			if out := run("-j", "-4", "address", "show", "dev", "tnl-owned"); strings.Contains(string(out), "192.0.2.99") {
				t.Fatalf("unacquired TunnelInterface admitted a child onto a same-name foreign link: err=%v native=%s", err, out)
			}
		})
	}
}
