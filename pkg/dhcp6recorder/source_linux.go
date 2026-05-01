//go:build linux

package dhcp6recorder

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

type AFPacketSource struct {
	fd     int
	ifname string
}

func NewAFPacketSource(ifname string) (*AFPacketSource, error) {
	if ifname == "" {
		return nil, fmt.Errorf("interface name is required")
	}
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(etherTypeIPv6)))
	if err != nil {
		return nil, err
	}
	addr := &unix.SockaddrLinklayer{Protocol: htons(etherTypeIPv6), Ifindex: ifi.Index}
	if err := unix.Bind(fd, addr); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	tv := unix.NsecToTimeval(time.Second.Nanoseconds())
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	return &AFPacketSource{fd: fd, ifname: ifname}, nil
}

func (s *AFPacketSource) ReadFrame(ctx context.Context) ([]byte, error) {
	buf := make([]byte, 65535)
	for {
		n, _, err := unix.Recvfrom(s.fd, buf, 0)
		if err == nil {
			return append([]byte(nil), buf[:n]...), nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
			continue
		}
		return nil, err
	}
}

func (s *AFPacketSource) Close() error {
	if s == nil || s.fd < 0 {
		return nil
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return err
}

func htons(value uint16) uint16 {
	return (value << 8) | (value >> 8)
}
