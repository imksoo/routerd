// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"gopkg.in/yaml.v3"
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

func TestNftablesFirewallZoneResolvesParsedVRFParent(t *testing.T) {
	var router api.Router
	err := yaml.Unmarshal([]byte(`
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: vrf-pve
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: lan}
      spec: {ifname: rd15-lan, managed: false, owner: external}
    - apiVersion: net.routerd.net/v1alpha1
      kind: VRF
      metadata: {name: tenant}
      spec: {ifname: vrf-pve, routeTable: 110, members: [lan]}
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata: {name: tenant}
      spec: {role: trust, interfaces: [tenant]}
`), &router)
	if err != nil {
		t.Fatal(err)
	}

	data, err := NftablesFirewall(&router, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `elements = { "vrf-pve" }`) {
		t.Fatalf("parsed VRF parent is absent from the firewall zone:\n%s", data)
	}
}
