// SPDX-License-Identifier: BSD-3-Clause
package vrrp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestInitialRolesUseLiveOwnershipBeforePersistedMaster(t *testing.T) {
	for _, scenario := range []string{"inactive", "backup", "live-master", "foreign-config", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keepalived.conf")
			data := keepalivedConfigHeader
			if scenario == "foreign-config" {
				data = "# locally managed\n"
			}
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "198.18.30.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 130}}},
			}}}
			store := mapStore{api.NetAPIVersion + "/VirtualAddress/gateway": {"role": "master", "phase": "Applied", "vipAdvertised": true, "vipPublicationConfirmed": true, "lastRoleTransitionAt": "old-transition"}}
			c := Controller{Router: router, Store: store, OperatingSystem: platform.OSLinux, ConfigPath: path, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ip" && args[1] == "addr" && args[2] == "show" {
					if scenario == "live-master" {
						return []byte("inet 198.18.30.1/32 scope global lan0\n"), nil
					}
					return nil, nil
				}
				if name == "systemctl" && args[0] == "is-active" {
					if scenario == "inactive" {
						return nil, errors.New("inactive")
					}
					return nil, nil
				}
				t.Fatalf("startup observation attempted a native mutation: %s %v", name, args)
				return nil, nil
			}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			err := c.RefreshInitialRoles(ctx)
			got := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway")
			if scenario == "foreign-config" || scenario == "cancelled" {
				if err == nil || got["role"] != "master" {
					t.Fatalf("unconfirmed observation changed persisted history: %v %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := scenario
			if scenario == "live-master" {
				want = "master"
			}
			if got["role"] != want {
				t.Fatalf("role=%v want %s", got, want)
			}
			if scenario == "live-master" {
				if got["vipPublicationConfirmed"] != true || got["lastRoleTransitionAt"] != "old-transition" {
					t.Fatalf("live ownership/history discarded: %v", got)
				}
			} else if got["vipPublicationConfirmed"] != false || got["vipAdvertised"] != false {
				t.Fatalf("stale publication kept: %v", got)
			}
		})
	}
}
