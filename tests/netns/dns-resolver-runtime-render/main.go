// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dnsresolver"
)

func main() {
	if len(os.Args) != 2 || net.ParseIP(os.Args[1]) == nil {
		fmt.Fprintln(os.Stderr, "usage: dns-resolver-runtime-render ZONE_IPV4")
		os.Exit(2)
	}

	config := dnsresolver.RuntimeConfig{
		Resource: "lan",
		Spec: api.DNSResolverSpec{
			Listen: []api.DNSResolverListenSpec{{
				Name:      "lan",
				Addresses: []string{"192.0.2.1"},
				Port:      53,
			}},
			Sources: []api.DNSResolverSourceSpec{
				{
					Name:    "home",
					Kind:    "zone",
					Match:   []string{"home.arpa"},
					ZoneRef: []string{"DNSZone/home"},
				},
				{
					Name:         "udp-forward",
					Kind:         "forward",
					Match:        []string{"udp.example"},
					Upstreams:    []string{"udp://198.51.100.53:53"},
					QueryTimeout: "2s",
					Healthcheck: api.DNSResolverHealthcheckSpec{
						Interval: "1s", Timeout: "500ms", FailThreshold: 1, PassThreshold: 1,
					},
				},
				{
					Name:         "tcp-forward",
					Kind:         "forward",
					Match:        []string{"tcp.example"},
					Upstreams:    []string{"tcp://198.51.100.53:53"},
					QueryTimeout: "2s",
					Healthcheck: api.DNSResolverHealthcheckSpec{
						Interval: "1s", Timeout: "500ms", FailThreshold: 1, PassThreshold: 1,
					},
				},
			},
		},
		Zones: []dnsresolver.RuntimeZone{{
			Name: "home",
			Spec: api.DNSZoneSpec{
				Zone: "home.arpa",
				TTL:  30,
				Records: []api.DNSZoneRecordSpec{{
					Hostname: "router",
					IPv4:     os.Args[1],
				}},
			},
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
