// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const bpfWordAlignment = 8

type bpfPacketConn struct {
	fd       int
	ifi      *net.Interface
	buf      []byte
	pending  [][]byte
	deadline time.Time
}

func bindSocketToDevice(_ int, _ string) error {
	return nil
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

func (c *bpfPacketConn) WriteToUDP(packet []byte, _ *net.UDPAddr) (int, error) {
	frame := ethernetIPv4UDPBroadcast(c.ifi.HardwareAddr, packet)
	if _, err := unix.Write(c.fd, frame); err != nil {
		return 0, err
	}
	return len(packet), nil
}

func (c *bpfPacketConn) SetReadDeadline(t time.Time) error {
	c.deadline = t
	return nil
}

func (c *bpfPacketConn) Close() error {
	return unix.Close(c.fd)
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
		if payload, ok := dhcpv4PayloadFromEthernet(frame); ok {
			c.pending = append(c.pending, append([]byte(nil), payload...))
		}
		next := bpfWordAlign(hdrLen + capLen)
		if next <= 0 || next > len(data) {
			return
		}
		data = data[next:]
	}
}

func dhcpv4PayloadFromEthernet(frame []byte) ([]byte, bool) {
	if len(frame) < 14+20+8 || binary.BigEndian.Uint16(frame[12:14]) != 0x0800 {
		return nil, false
	}
	ip := frame[14:]
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl+8 || ip[9] != 17 {
		return nil, false
	}
	udp := ip[ihl:]
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	if !((srcPort == 67 && dstPort == 68) || (srcPort == 68 && dstPort == 67)) {
		return nil, false
	}
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < 8 || udpLen > len(udp) {
		return nil, false
	}
	return udp[8:udpLen], true
}

func ethernetIPv4UDPBroadcast(src net.HardwareAddr, payload []byte) []byte {
	ipLen := 20 + 8 + len(payload)
	frame := make([]byte, 14+ipLen)
	for i := 0; i < 6; i++ {
		frame[i] = 0xff
	}
	copy(frame[6:12], src)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	ip := frame[14:]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	ip[8] = 64
	ip[9] = 17
	copy(ip[12:16], net.IPv4zero.To4())
	copy(ip[16:20], net.IPv4bcast.To4())
	binary.BigEndian.PutUint16(ip[10:12], ipv4Checksum(ip[:20]))
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], 68)
	binary.BigEndian.PutUint16(udp[2:4], 67)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(payload)))
	copy(udp[8:], payload)
	return frame
}

func ipv4Checksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(header); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
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
