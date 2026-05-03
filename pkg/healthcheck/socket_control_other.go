//go:build !linux

package healthcheck

import "syscall"

func bindToDevice(conn syscall.RawConn, ifname string) error {
	return nil
}
