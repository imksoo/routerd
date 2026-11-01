// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"fmt"
	"os"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: dhcpv6-stateful-render LEASE_FILE stateful|both")
		os.Exit(2)
	}
	mode := os.Args[2]
	if mode != "stateful" && mode != "both" {
		fmt.Fprintln(os.Stderr, "mode must be stateful or both")
		os.Exit(2)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "lan0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
			Metadata: api.ObjectMeta{Name: "lan-v6"},
			Spec: api.DHCPv6ServerSpec{
				Interface: "lan",
				Mode:      mode,
				AddressPool: api.DHCPAddressPoolSpec{
					Start:     "fd00:1234::100",
					End:       "fd00:1234::110",
					LeaseTime: "2m",
				},
				DNSSource:  "static",
				DNSServers: []string{"fd00:1234::1"},
			},
		},
	}}}
	config, _, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{LeaseFile: os.Args[1]})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
