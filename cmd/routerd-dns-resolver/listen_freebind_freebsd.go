// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"fmt"
	"net"
	"syscall"
)

// FreeBSD has no Linux SO_FREEBIND equivalent.  Keep normal local and
// wildcard listeners working, but reject a non-local address explicitly
// instead of reporting a listener as configured and failing later at bind(2).
func dnsListenConfig() net.ListenConfig {
	return net.ListenConfig{Control: rejectFreeBSDNonLocalBind}
}

func rejectFreeBSDNonLocalBind(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host == "" || ip == nil || ip.IsUnspecified() {
		return nil
	}
	addrs, err := mustInterfaceAddrs()
	if err != nil {
		return fmt.Errorf("list FreeBSD interface addresses: %w", err)
	}
	for _, iface := range addrs {
		candidate, _, err := net.ParseCIDR(iface.String())
		if err == nil && candidate.Equal(ip) {
			return nil
		}
	}
	return fmt.Errorf("FreeBSD does not support Linux IP_FREEBIND; listener address %s is not assigned", host)
}

var mustInterfaceAddrs = net.InterfaceAddrs
