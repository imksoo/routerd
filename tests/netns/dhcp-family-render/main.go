// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
	"os"
)

func main() {
	r := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan-parent"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.InterfaceSpec{IfName: "lan-vrrp"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "v4"}, Spec: api.DHCPv4ServerSpec{Interface: "lan", AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.DHCPv6ServerSpec{Interface: "v6", Mode: "stateless"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "v6"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan", "v6"}}},
	}}}
	data, err := render.NftablesFirewall(r, render.InternalFirewallHoles(r))
	if err != nil {
		panic(err)
	}
	if _, err = os.Stdout.Write(data); err != nil {
		panic(err)
	}
}
