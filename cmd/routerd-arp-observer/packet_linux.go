// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

type packetSocket struct {
	fd int
}

func openPacketSocket(ifname string) (*packetSocket, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ARP)))
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_ARP), Ifindex: iface.Index}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind %s: %w", ifname, err)
	}
	return &packetSocket{fd: fd}, nil
}

func (s *packetSocket) read(frame []byte) (int, error) {
	return unix.Read(s.fd, frame)
}

func (s *packetSocket) write(frame []byte) (int, error) {
	return unix.Write(s.fd, frame)
}

func (s *packetSocket) close() error {
	return unix.Close(s.fd)
}

func htons(value uint16) uint16 {
	return value<<8&0xff00 | value>>8
}
