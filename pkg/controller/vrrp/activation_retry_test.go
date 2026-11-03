// SPDX-License-Identifier: BSD-3-Clause
package vrrp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestPendingNativePromotionRetriesWithoutAuthorizingMasterConsumers(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "stopped"}[active], func(t *testing.T) {
			resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{
				Interface: "lan", Address: "198.18.30.1/32", Family: "ipv4", Mode: "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 130, GracefulActivation: &api.VirtualAddressVRRPGracefulActivationSpec{}, FailoverVMAC: &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:31"}},
			}}
			var action string
			c := &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, VRRPRuntimeDir: t.TempDir(), Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "systemctl" {
					if !active {
						return nil, errors.New("inactive")
					}
					return nil, nil
				}
				if strings.HasSuffix(name, "routerd-vrrp-vmac") {
					action = args[0]
					return nil, nil
				}
				t.Fatalf("unexpected native effect %s %v", name, args)
				return nil, nil
			}}
			path := gracefulElectionStatePath(c, "gateway")
			if err := os.WriteFile(filepath.Clean(path), []byte("activating\n"), 0600); err != nil {
				t.Fatal(err)
			}
			aliases := map[string]string{"lan": "lan0", "wan": "wan0"}
			roles := observeKeepalivedRoles(t.Context(), c, aliases)
			if roles["gateway"] == "master" {
				t.Fatal("incomplete activation authorized master consumers")
			}
			if err := syncFailoverVMACs(t.Context(), c, aliases, roles); err != nil {
				t.Fatal(err)
			}
			want := "deactivate"
			if active {
				want = "activate"
			}
			if action != want {
				t.Fatalf("action=%q want %q (roles=%v)", action, want, roles)
			}
		})
	}
}
