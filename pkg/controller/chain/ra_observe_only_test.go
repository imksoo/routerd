// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestRAObserveOnlyDoesNotConfigureLinuxHost(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, tc := range []struct {
		name    string
		managed *bool
		pd      bool
		writes  bool
	}{
		{name: "observe only", managed: boolPtr(false)},
		{name: "default managed", writes: true},
		{name: "explicit managed", managed: boolPtr(true), writes: true},
		{name: "observe only with independent PD owner", managed: boolPtr(false), pd: true, writes: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "observe-wan"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RAAddress"}, Metadata: api.ObjectMeta{Name: "observed"}, Spec: api.IPv6RAAddressSpec{Interface: "wan", Managed: tc.managed}},
			}}}
			if tc.pd {
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}})
			}
			for _, actor := range []string{"sysctl", "network-adoption"} {
				t.Run(actor, func(t *testing.T) {
					dir := t.TempDir()
					store := mapStore{}
					var commands []string
					command := func(_ context.Context, name string, args ...string) ([]byte, error) {
						commands = append(commands, strings.Join(append([]string{name}, args...), " "))
						return []byte("0\n"), nil
					}
					var err error
					if actor == "sysctl" {
						err = (SysctlController{Router: router, Store: store, BaseDir: dir, Command: command}).Reconcile(t.Context())
					} else {
						err = (NetworkAdoptionController{Router: router, Store: store, NetworkdDropinBase: filepath.Join(dir, "network"), ResolvedDropinDir: filepath.Join(dir, "resolved"), Command: command}).Reconcile(t.Context())
					}
					if err != nil {
						t.Fatal(err)
					}
					files, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					if !tc.writes {
						if len(commands) != 0 || len(files) != 0 || len(store) != 0 {
							t.Fatalf("observe-only intent changed host configuration: commands=%v files=%v status=%v", commands, files, store)
						}
					} else {
						want := "sysctl -w net.ipv6.conf.observe-wan.accept_ra=2"
						if actor == "network-adoption" {
							want = "networkctl reconfigure observe-wan"
						}
						if !strings.Contains(strings.Join(commands, "\n"), want) || len(files) == 0 {
							t.Fatalf("managed owner lost its host effects: commands=%v files=%v", commands, files)
						}
					}
				})
			}
		})
	}
}
