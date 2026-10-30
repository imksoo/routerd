// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

type packetSocket struct {
	fd     int
	mu     sync.Mutex
	closed bool
}

func openPacketSocket(ctx context.Context, ifname string) (*packetSocket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, int(htons(unix.ETH_P_ARP)))
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
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return 0, os.ErrClosed
		}
		n, err := unix.Read(s.fd, frame)
		s.mu.Unlock()
		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return n, err
		}
		_, err = unix.Poll([]unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}, 500)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return 0, err
		}
	}
}

func (s *packetSocket) write(frame []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	return unix.Write(s.fd, frame)
}

func (s *packetSocket) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return unix.Close(s.fd)
}

func htons(value uint16) uint16 {
	return value<<8&0xff00 | value>>8
}
