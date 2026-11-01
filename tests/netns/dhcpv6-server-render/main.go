// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"fmt"
	"os"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func main() {
	mode := "stateful"
	if len(os.Args) == 2 {
		mode = os.Args[1]
	}
	if mode != "stateful" && mode != "both" {
		fmt.Fprintf(os.Stderr, "usage: %s [stateful|both]\n", os.Args[0])
		os.Exit(2)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcpv6"}, Spec: api.DHCPv6ServerSpec{
			Interface: "lan", Mode: mode,
			AddressPool:  api.DHCPAddressPoolSpec{Start: "fd00:1234::100", End: "fd00:1234::10f", LeaseTime: "60s"},
			DNSSource:    "static",
			DNSServers:   []string{"fd00:1234::1"},
			SNTPServers:  []string{"fd00:1234::123"},
			DomainSearch: []string{"home.arpa"},
		}},
	}}}
	data, _, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{})
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		panic(err)
	}
}
