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
		fmt.Fprintln(os.Stderr, "usage: dhcpv4-relay-render relay|withdrawn LEASE_FILE")
		os.Exit(2)
	}
	mode := os.Args[1]
	if mode != "relay" && mode != "withdrawn" {
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", mode)
		os.Exit(2)
	}

	resources := []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "lan"},
		Spec:     api.InterfaceSpec{IfName: "lan0"},
	}}
	resources = append(resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "lan-second"},
		Spec:     api.InterfaceSpec{IfName: "lan1"},
	})
	if mode == "relay" {
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "lan-second-address"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "lan-second", Address: "198.18.2.1/24"},
		})
		resources = append(resources,
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-address"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"},
			},
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"},
				Metadata: api.ObjectMeta{Name: "lan-relay"},
				Spec:     api.DHCPv4RelaySpec{Interfaces: []string{"lan", "lan-second"}, Upstream: "198.51.100.2"},
			},
		)
	}

	config, warnings, err := render.DnsmasqConfig(
		&api.Router{Spec: api.RouterSpec{Resources: resources}},
		render.DnsmasqRuntime{LeaseFile: os.Args[2]},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	if _, err := os.Stdout.Write(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
