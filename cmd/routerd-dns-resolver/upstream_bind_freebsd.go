//go:build freebsd

package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func bindToDeviceControl(name string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		if fib, ok, err := parseFreeBSDFIB(name); err != nil {
			return err
		} else if ok {
			var setErr error
			if err := c.Control(func(fd uintptr) {
				setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SETFIB, fib)
			}); err != nil {
				return err
			}
			return setErr
		}
		iface, err := net.InterfaceByName(name)
		if err != nil {
			return err
		}
		return fmt.Errorf("viaInterface %q resolved to ifindex %d, but direct ifname socket binding is not available on FreeBSD; use fib:<n> after assigning routes to a FIB", name, iface.Index)
	}
}

func parseFreeBSDFIB(name string) (int, bool, error) {
	value, ok := strings.CutPrefix(strings.TrimSpace(name), "fib:")
	if !ok {
		return 0, false, nil
	}
	fib, err := strconv.Atoi(value)
	if err != nil || fib < 0 {
		return 0, true, fmt.Errorf("invalid FreeBSD FIB selector %q", name)
	}
	return fib, true, nil
}
