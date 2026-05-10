// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package healthcheck

import (
	"fmt"
	"net"
)

func bindDialerToDevice(_ *net.Dialer, ifname, _, _, _ string, _ bool) error {
	return fmt.Errorf("sourceInterface %q is only supported on Linux and FreeBSD", ifname)
}
