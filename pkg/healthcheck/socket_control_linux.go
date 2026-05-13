// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package healthcheck

import (
	"net"
	"syscall"
)

func configureDialerSocket(dialer *net.Dialer, ifname string, fwmark int, _, _, _ string, _ bool) error {
	dialer.Control = func(_, _ string, conn syscall.RawConn) error {
		if fwmark != 0 {
			if err := setSocketMark(conn, fwmark); err != nil {
				return err
			}
		}
		if ifname != "" {
			return bindToDevice(conn, ifname)
		}
		return nil
	}
	return nil
}

func setSocketMark(conn syscall.RawConn, mark int) error {
	var markErr error
	if err := conn.Control(func(fd uintptr) {
		markErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
	}); err != nil {
		return err
	}
	return markErr
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
