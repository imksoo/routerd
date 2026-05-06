//go:build !linux && !freebsd

package main

import (
	"fmt"
	"syscall"
)

func bindToDeviceControl(name string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("viaInterface is only supported on Linux")
	}
}
