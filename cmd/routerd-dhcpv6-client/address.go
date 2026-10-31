// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/pdclient"
)

func requestMode(value string) (pdclient.RequestMode, error) {
	switch value {
	case "", "pd":
		return pdclient.RequestPrefixDelegation, nil
	case "address":
		return pdclient.RequestAddresses, nil
	case "pd-address":
		return pdclient.RequestPrefixAndAddress, nil
	default:
		return 0, fmt.Errorf("unknown DHCPv6 request mode %q: use pd, address, or pd-address", value)
	}
}

func (d *dhcpv6Daemon) resourceKind() string {
	if d.client.Config.Mode == pdclient.RequestAddresses {
		return "DHCPv6Address"
	}
	return "DHCPv6PrefixDelegation"
}

func (d *dhcpv6Daemon) resourceName() string {
	if d.client.Config.Mode == pdclient.RequestAddresses && d.opts.addressResource != "" {
		return d.opts.addressResource
	}
	return d.opts.resource
}

func addressSnapshotsEqual(a, b []pdclient.AddressSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Address != b[i].Address || a[i].Preferred != b[i].Preferred || a[i].Valid != b[i].Valid || !a[i].AcquiredAt.Equal(b[i].AcquiredAt) {
			return false
		}
	}
	return true
}
