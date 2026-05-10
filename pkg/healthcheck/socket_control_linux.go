// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package healthcheck

import (
	"net"
	"syscall"
)

func bindDialerToDevice(dialer *net.Dialer, ifname, _, _, _ string, _ bool) error {
	dialer.Control = func(_, _ string, conn syscall.RawConn) error {
		return bindToDevice(conn, ifname)
	}
	return nil
}

func bindToDevice(conn syscall.RawConn, ifname string) error {
	var bindErr error
	if err := conn.Control(func(fd uintptr) {
		bindErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifname)
	}); err != nil {
		return err
	}
	return bindErr
}
