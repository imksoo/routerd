// SPDX-License-Identifier: BSD-3-Clause
package firewallbackend

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func TestBackendDSLiteServiceHoleUsesTargetNativeName(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds"}, Spec: api.DSLiteTunnelSpec{Interface: "uplink", TunnelName: "ds-transix"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"DSLiteTunnel/ds"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "bgp"}, Spec: api.BGPRouterSpec{}},
	}}}
	rules, err := (PF{}).Render(router, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	native := api.DSLiteRuntimeIfName("ds-transix", "freebsd")
	if want := `pass in quick on ` + native + ` proto tcp to any port 179`; !strings.Contains(string(rules.Data), want) {
		t.Fatalf("PF service hole missed native %q:\n%s", want, rules.Data)
	}
	rules, err = (Nftables{}).Render(router, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rules.Data), `iifname "ds-transix" tcp dport 179`) {
		t.Fatalf("Linux service hole used another target's name:\n%s", rules.Data)
	}
}
