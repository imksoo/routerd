// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "nat" && os.Args[1] != "firewall") {
		fmt.Fprintln(os.Stderr, "usage: portforward-render {nat|firewall}")
		os.Exit(2)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "wan0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "lan0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "PortForward"},
			Metadata: api.ObjectMeta{Name: "https"},
			Spec: api.PortForwardSpec{
				Listen:  api.IngressListenSpec{Interface: "wan", Address: "198.18.77.1", Protocol: "tcp", Port: 8443},
				Target:  api.IngressTargetSpec{Address: "10.77.0.88", Port: 443},
				Hairpin: api.IngressHairpinSpec{Enabled: true, Interfaces: []string{"lan"}},
			},
		},
	}}}

	var (
		data []byte
		err  error
	)
	if os.Args[1] == "nat" {
		data, err = render.NftablesNAT44RulesForRouter(router, nil)
	} else {
		data, err = render.NftablesFirewall(router, render.InternalFirewallHoles(router))
	}
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		panic(err)
	}
}
