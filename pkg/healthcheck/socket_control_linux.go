//go:build linux

package healthcheck

import "syscall"

func bindToDevice(conn syscall.RawConn, ifname string) error {
	var bindErr error
	if err := conn.Control(func(fd uintptr) {
		bindErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifname)
	}); err != nil {
		return err
	}
	return bindErr
}
