// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func main() {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		interfaceResource("wan", "wan0"),
		interfaceResource("lan", "lan0"),
		interfaceResource("mgmt", "mgmt0"),
		firewallResource("FirewallZone", "wan", api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}),
		firewallResource("FirewallZone", "lan", api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}),
		firewallResource("FirewallZone", "mgmt", api.FirewallZoneSpec{Role: "mgmt", Interfaces: []string{"mgmt"}}),
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"},
			Metadata: api.ObjectMeta{Name: "management-web"},
			Spec:     api.IPAddressSetSpec{Addresses: []string{"10.78.0.88"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"},
			Metadata: api.ObjectMeta{Name: "allow-lan-management-web"},
			Spec: api.FirewallRuleSpec{
				FromZone: "lan", ToZone: "mgmt", Protocol: "tcp", DestinationPorts: []api.FirewallPort{"8080"},
				DestinationSetRefs: []string{"IPAddressSet/management-web"}, Action: "accept",
			},
		},
	}}}
	data, err := render.NftablesFirewall(router, nil)
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func interfaceResource(name, ifName string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.InterfaceSpec{IfName: ifName},
	}
}

func firewallResource(kind, name string, spec any) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: kind},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     spec,
	}
}
