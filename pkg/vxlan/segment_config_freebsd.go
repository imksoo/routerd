// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package vxlan

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func readFreeBSDSegmentConfig(ctx context.Context, name string, index int) (bsdSegmentConfig, error) {
	if err := ctx.Err(); err != nil {
		return bsdSegmentConfig{}, err
	}
	if len(name) >= unix.IFNAMSIZ {
		return bsdSegmentConfig{}, fmt.Errorf("VXLAN interface name is too long")
	}
	before, err := net.InterfaceByName(name)
	if err != nil {
		return bsdSegmentConfig{}, err
	}
	if before.Index != index {
		return bsdSegmentConfig{}, fmt.Errorf("VXLAN identity changed before driver observation: expected %d, observed %d", index, before.Index)
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return bsdSegmentConfig{}, err
	}
	defer unix.Close(fd)
	var config bsdVXLANDriverConfig
	// Native struct ifdrv. uintptr matches unsigned long and size_t on both
	// supported FreeBSD word sizes; the request owns this pointer for ioctl.
	request := struct {
		Name [unix.IFNAMSIZ]byte
		Cmd  uintptr
		Len  uintptr
		Data unsafe.Pointer
	}{Cmd: 0 /* VXLAN_CMD_GET_CONFIG */, Len: unsafe.Sizeof(config), Data: unsafe.Pointer(&config)}
	copy(request.Name[:], name)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.SIOCGDRVSPEC), uintptr(unsafe.Pointer(&request)))
	runtime.KeepAlive(&config)
	runtime.KeepAlive(&request)
	if errno != 0 {
		return bsdSegmentConfig{}, fmt.Errorf("read VXLAN driver configuration: %w", errno)
	}
	after, err := net.InterfaceByName(name)
	if err != nil {
		return bsdSegmentConfig{}, err
	}
	if after.Index != index {
		return bsdSegmentConfig{}, fmt.Errorf("VXLAN identity changed during driver observation: expected %d, observed %d", index, after.Index)
	}
	if err := ctx.Err(); err != nil {
		return bsdSegmentConfig{}, err
	}
	return decodeBSDSegmentConfig(config)
}
