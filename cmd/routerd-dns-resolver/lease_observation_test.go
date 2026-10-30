// SPDX-License-Identifier: BSD-3-Clause
package main

import "time"

func observedLeaseForTest(lease dhcpLeaseEvent) dhcpLeaseEvent {
	if lease.Source == "" {
		lease.Source = "DHCPv4Server/dhcpv4"
	}
	if lease.MAC == "" {
		lease.MAC = "02:00:00:00:00:55"
	}
	lease.ObservedAt = time.Now().UTC()
	lease.Infinite = true
	return lease
}
