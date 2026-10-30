// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

type packetSocket struct {
	file *os.File
}

func openPacketSocket(ifname string) (*packetSocket, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, int(htons(unix.ETH_P_IPV6)))
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_IPV6), Ifindex: iface.Index}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind %s: %w", ifname, err)
	}
	return &packetSocket{file: os.NewFile(uintptr(fd), "ra-observer-"+ifname)}, nil
}

func (s *packetSocket) read(frame []byte) (int, error) {
	return s.file.Read(frame)
}

func (s *packetSocket) close() error {
	return s.file.Close()
}

func htons(value uint16) uint16 {
	return value<<8&0xff00 | value>>8
}
