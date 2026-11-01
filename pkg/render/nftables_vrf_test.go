// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNftablesFirewallZoneResolvesVRFParent(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "tenant"}, Spec: api.VRFSpec{IfName: "vrf-pve", RouteTable: 110}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "tenant"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"tenant"}}},
	}}}

	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `elements = { "vrf-pve" }`) {
		t.Fatalf("VRF parent is absent from the firewall zone:\n%s", data)
	}
}
