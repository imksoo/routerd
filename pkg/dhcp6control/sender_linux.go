//go:build linux

package dhcp6control

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

type AFPacketSender struct{}

func (AFPacketSender) SendFrame(ctx context.Context, ifname string, frame []byte) error {
	if ifname == "" {
		return fmt.Errorf("interface name is required")
	}
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(etherTypeIPv6)))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if deadline, ok := ctx.Deadline(); ok {
		tv := unix.NsecToTimeval(deadline.Sub(time.Now()).Nanoseconds())
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv)
	}
	addr := &unix.SockaddrLinklayer{
		Protocol: htons(etherTypeIPv6),
		Ifindex:  ifi.Index,
		Halen:    6,
	}
	copy(addr.Addr[:], frame[0:6])
	return unix.Sendto(fd, frame, 0, addr)
}

func htons(value uint16) uint16 {
	return (value << 8) | (value >> 8)
}
