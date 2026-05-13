// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package healthcheck

import (
	"fmt"
	"net"
)

func configureDialerSocket(_ *net.Dialer, ifname string, fwmark int, _, _, _ string, _ bool) error {
	if fwmark != 0 {
		return fmt.Errorf("fwmark is not supported on this platform")
	}
	return fmt.Errorf("sourceInterface %q is only supported on Linux and FreeBSD", ifname)
}
