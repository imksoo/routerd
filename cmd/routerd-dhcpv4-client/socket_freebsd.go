// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"golang.org/x/sys/unix"
)

const bpfWordAlignment = 8

type bpfPacketConn struct {
	fd       int
	ifi      *net.Interface
	buf      []byte
	pending  [][]byte
	deadline time.Time
	write    *net.UDPConn
}

func listenDHCPv4(ifname string) (dhcpv4PacketConn, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := openBPF()
	if err != nil {
		return nil, err
	}
	conn := &bpfPacketConn{fd: fd, ifi: ifi}
	if err := conn.configure(ifname); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var sockErr error
		if err := raw.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			if sockErr == nil {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			}
		}); err != nil {
			return err
		}
		return sockErr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	conn.write = pc.(*net.UDPConn)
	return conn, nil
}

func openBPF() (int, error) {
	for i := 0; i < 256; i++ {
		path := "/dev/bpf"
		if i > 0 {
			path = fmt.Sprintf("/dev/bpf%d", i)
		}
		fd, err := unix.Open(path, unix.O_RDWR, 0)
		if err == nil {
			return fd, nil
		}
		if !errors.Is(err, unix.EBUSY) && i > 0 {
			return -1, err
		}
	}
	return -1, fmt.Errorf("no free BPF device")
}

func (c *bpfPacketConn) configure(ifname string) error {
	var ifr [32]byte
	copy(ifr[:], ifname)
	if err := ioctlPtr(c.fd, unix.BIOCSETIF, unsafe.Pointer(&ifr[0])); err != nil {
		return fmt.Errorf("BIOCSETIF %s: %w", ifname, err)
	}
	if err := ioctlSetInt(c.fd, unix.BIOCIMMEDIATE, 1); err != nil {
		return fmt.Errorf("BIOCIMMEDIATE: %w", err)
	}
	if err := ioctlSetInt(c.fd, unix.BIOCSHDRCMPLT, 1); err != nil {
		return fmt.Errorf("BIOCSHDRCMPLT: %w", err)
	}
	if err := unix.SetNonblock(c.fd, true); err != nil {
		return err
	}
	size, err := ioctlGetInt(c.fd, unix.BIOCGBLEN)
	if err != nil || size <= 0 {
		size = 4096
	}
	c.buf = make([]byte, size)
	return nil
}

func (c *bpfPacketConn) ReadFromUDP(dst []byte) (int, *net.UDPAddr, error) {
	for {
		if len(c.pending) > 0 {
			packet := c.pending[0]
			c.pending = c.pending[1:]
			return copy(dst, packet), &net.UDPAddr{IP: net.IPv4bcast, Port: 67}, nil
		}
		timeout := -1
		if !c.deadline.IsZero() {
			remaining := time.Until(c.deadline)
			if remaining <= 0 {
				return 0, nil, os.ErrDeadlineExceeded
			}
			timeout = int(remaining / time.Millisecond)
			if timeout == 0 {
				timeout = 1
			}
		}
		pollfds := []unix.PollFd{{Fd: int32(c.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pollfds, timeout)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, nil, err
		}
		if n == 0 {
			return 0, nil, os.ErrDeadlineExceeded
		}
		readN, err := unix.Read(c.fd, c.buf)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				continue
			}
			return 0, nil, err
		}
		c.collectPackets(c.buf[:readN])
	}
}

func (c *bpfPacketConn) Send(ctx context.Context, packet dhcpv4client.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if packet.Destination == netip.MustParseAddr("255.255.255.255") {
		frame := ethernetBroadcast(c.ifi.HardwareAddr, packet)
		n, err := unix.Write(c.fd, frame)
		if err == nil && n != len(frame) {
			return fmt.Errorf("short DHCPv4 BPF write")
		}
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := c.write.SetWriteDeadline(deadline); err != nil {
		return err
	}
	oob := make([]byte, unix.CmsgSpace(4))
	header := (*unix.Cmsghdr)(unsafe.Pointer(&oob[0]))
	header.Level, header.Type = unix.IPPROTO_IP, unix.IP_SENDSRCADDR
	header.SetLen(unix.CmsgLen(4))
	copy(oob[unix.CmsgLen(0):], packet.Source.AsSlice())
	_, _, err := c.write.WriteMsgUDP(packet.Payload, oob, &net.UDPAddr{IP: net.IP(packet.Destination.AsSlice()), Port: 67})
	return err
}

func (c *bpfPacketConn) SetReadDeadline(t time.Time) error {
	c.deadline = t
	return nil
}

func (c *bpfPacketConn) Close() error {
	return errors.Join(unix.Close(c.fd), c.write.Close())
}

func (c *bpfPacketConn) collectPackets(data []byte) {
	for len(data) >= int(unsafe.Sizeof(unix.BpfHdr{})) {
		hdr := (*unix.BpfHdr)(unsafe.Pointer(&data[0]))
		hdrLen := int(hdr.Hdrlen)
		capLen := int(hdr.Caplen)
		if hdrLen <= 0 || capLen <= 0 || hdrLen+capLen > len(data) {
			return
		}
		frame := data[hdrLen : hdrLen+capLen]
		if payload, _ := dhcpv4client.EthernetReply(frame); len(payload) > 0 {
			c.pending = append(c.pending, append([]byte(nil), payload...))
		}
		next := bpfWordAlign(hdrLen + capLen)
		if next <= 0 || next > len(data) {
			return
		}
		data = data[next:]
	}
}

func bpfWordAlign(n int) int {
	return (n + (bpfWordAlignment - 1)) &^ (bpfWordAlignment - 1)
}

func ioctlGetInt(fd int, req uint) (int, error) {
	var value int32
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return 0, errno
	}
	return int(value), nil
}

func ioctlSetInt(fd int, req uint, value int) error {
	raw := int32(value)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&raw)))
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlPtr(fd int, req uint, ptr unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(ptr))
	if errno != 0 {
		return errno
	}
	return nil
}
