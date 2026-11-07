// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"encoding/json"
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
	for _, scenario := range []string{"replacement-cleanup", "unacquired-child", "source-expired", "source-expired-noop", "owned-gre-reconfigure"} {
		t.Run(scenario, func(t *testing.T) {
			run := func(args ...string) []byte {
				out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
				if err != nil {
					t.Fatalf("ip %v: %v: %s", args, err, out)
				}
				return out
			}
			defer func() { _, _ = exec.CommandContext(ctx, "ip", "link", "delete", "dev", "tnl-owned").CombinedOutput() }()
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "tnl-owned"}, Spec: api.TunnelInterfaceSpec{Mode: "ipip", Local: "198.18.0.1", Remote: "198.18.0.2", Address: "192.0.2.1/32", MTU: 1400, TrustedUnderlay: true}},
			}}}
			parent := TunnelInterfaceController{Router: router, Store: eventedStore{Store: store, Router: router}}
			if scenario == "owned-gre-reconfigure" {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Mode, spec.Key = "gre", 42
				router.Spec.Resources[0].Spec = spec
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				before, err := parent.tunnelNativeReceipts()
				if err != nil || len(before) != 1 {
					t.Fatalf("initial GRE receipt unavailable: %+v %v", before, err)
				}
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "tnl-owned", Address: "192.0.2.99/32"}})
				child := IPv4StaticAddressController{Router: router, Store: store}
				if err := child.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				spec.Key, spec.TTL, spec.MTU = 0, 32, 1380
				spec.Remote, spec.Address = "198.18.0.3", "192.0.2.2/32"
				router.Spec.Resources[0].Spec = spec
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				var links []struct {
					Index int    `json:"ifindex"`
					MTU   int    `json:"mtu"`
					Alias string `json:"ifalias"`
					Info  struct {
						Data struct {
							Remote string `json:"remote"`
							TTL    int    `json:"ttl"`
							IKey   string `json:"ikey"`
							OKey   string `json:"okey"`
						} `json:"info_data"`
					} `json:"linkinfo"`
				}
				if err := json.Unmarshal(run("-j", "-d", "link", "show", "dev", "tnl-owned"), &links); err != nil || len(links) != 1 {
					t.Fatalf("updated GRE native readback unavailable: %+v %v", links, err)
				}
				link := links[0]
				if link.Index != before[0].Index || link.Alias != "routerd-tunnel:"+before[0].Token || link.MTU != 1380 || link.Info.Data.Remote != spec.Remote || link.Info.Data.TTL != 32 || link.Info.Data.IKey != "" && link.Info.Data.IKey != "0.0.0.0" || link.Info.Data.OKey != "" && link.Info.Data.OKey != "0.0.0.0" {
					t.Fatalf("owned GRE was replaced or retained old native parameters: %+v", link)
				}
				var interfaces []struct {
					Addresses []struct {
						Local string `json:"local"`
					} `json:"addr_info"`
				}
				if err := json.Unmarshal(run("-j", "-4", "address", "show", "dev", "tnl-owned"), &interfaces); err != nil || len(interfaces) != 1 {
					t.Fatalf("updated GRE address readback unavailable: %+v %v", interfaces, err)
				}
				addresses := map[string]bool{}
				for _, address := range interfaces[0].Addresses {
					addresses[address.Local] = true
				}
				if addresses["192.0.2.1"] || !addresses["192.0.2.2"] || !addresses["192.0.2.99"] {
					t.Fatalf("GRE parent address update did not preserve its child: %+v", addresses)
				}
				return
			}
			if scenario == "source-expired" || scenario == "source-expired-noop" {
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
				expire := func() {
					// The DHCPv4 publisher clears currentAddress on real lease expiry.
					if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Expired", "currentAddress": ""}); err != nil {
						t.Fatal(err)
					}
				}
				observations := 0
				if scenario == "source-expired-noop" {
					parent.ObserveNative = func(ctx context.Context, name string) (tunnelNativeLink, error) {
						link, err := observeTunnelNativeLink(ctx, name)
						observations++
						if err == nil && link.Exists && observations == 2 {
							expire()
						}
						return link, err
					}
				} else {
					expire()
				}
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				if scenario == "source-expired-noop" && observations < 2 {
					t.Fatal("unchanged reconciliation did not cross its final native observation")
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
