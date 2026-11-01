// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"fmt"
	"os"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: dhcpv4-reservation-render LEASE_FILE")
		os.Exit(2)
	}

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "lan0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "lan-address"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
			Metadata: api.ObjectMeta{Name: "lan-v4"},
			Spec: api.DHCPv4ServerSpec{
				Interface:     "lan",
				AddressPool:   api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110", LeaseTime: "40s"},
				Authoritative: true,
				RouterSource:  "interfaceAddress",
				DNSSource:     "static",
				DNSServers:    []string{"192.0.2.1"},
				LogDHCP:       true,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"},
			Metadata: api.ObjectMeta{Name: "reserved-client"},
			Spec: api.DHCPv4ReservationSpec{
				Server:     "lan-v4",
				MACAddress: "02:00:00:00:00:50",
				Hostname:   "reserved-client",
				IPAddress:  "192.0.2.50",
				LeaseTime:  "40s",
			},
		},
	}}}

	config, warnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{LeaseFile: os.Args[1]})
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
