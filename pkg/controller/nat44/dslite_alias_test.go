// SPDX-License-Identifier: BSD-3-Clause
package nat44

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControllerDSLiteNativeAliasForNATArtifacts(t *testing.T) {
	for _, targetOS := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		for _, configured := range []string{"ds-transix", "gif41", ""} {
			t.Run(string(targetOS)+"/"+configured, func(t *testing.T) {
				router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
					{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds"}, Spec: api.DSLiteTunnelSpec{Interface: "underlay", TunnelName: configured}},
					{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "nat"}, Spec: api.NAT44RuleSpec{Type: "masquerade", EgressInterface: "ds", SourceRanges: []string{"192.0.2.0/24"}}},
				}}}
				store := &testStore{}
				path := filepath.Join(t.TempDir(), "nat44.nft")
				c := Controller{OS: targetOS, Router: router, Store: store, DryRun: true, NftablesPath: path}
				if err := c.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				if targetOS == platform.OSFreeBSD {
					path = strings.TrimSuffix(path, ".nft") + ".pf"
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				native := configured
				if native == "" {
					native = "underlay"
				}
				native = api.DSLiteRuntimeIfName(native, string(targetOS))
				want := `oifname "` + native + `" ip saddr 192.0.2.0/24 masquerade`
				if targetOS == platform.OSFreeBSD {
					want = `nat on ` + native + ` from 192.0.2.0/24 to any -> (` + native + `)`
				}
				if !strings.Contains(string(data), want) {
					t.Fatalf("runtime NAT artifact misses actual owner %q:\n%s", want, data)
				}
				if got := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "nat")["activeEgressInterface"]; got != native {
					t.Fatalf("status egress=%v, native=%s", got, native)
				}
			})
		}
	}
}
