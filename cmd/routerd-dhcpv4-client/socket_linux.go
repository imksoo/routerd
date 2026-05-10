// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	etherTypeIPv4 = 0x0800
	ethHeaderLen  = 14
)

func bindSocketToDevice(fd int, ifname string) error {
	return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname)
}

func listenDHCPv4(ifname string) (dhcpv4PacketConn, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	rawFD, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(etherTypeIPv4)))
	if err != nil {
		return nil, err
	}
	if err := attachDHCPv4Filter(rawFD); err != nil {
		_ = unix.Close(rawFD)
		return nil, err
	}
	if err := unix.Bind(rawFD, &unix.SockaddrLinklayer{Protocol: htons(etherTypeIPv4), Ifindex: ifi.Index}); err != nil {
		_ = unix.Close(rawFD)
		return nil, err
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var sockErr error
		if err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			if sockErr == nil {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
			}
			if sockErr == nil && ifname != "" {
				sockErr = bindSocketToDevice(int(fd), ifname)
			}
		}); err != nil && sockErr == nil {
			sockErr = err
		}
		return sockErr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		_ = unix.Close(rawFD)
		return nil, err
	}
	return &linuxDHCPv4PacketConn{rawFD: rawFD, write: pc.(*net.UDPConn), hwaddr: append(net.HardwareAddr(nil), ifi.HardwareAddr...)}, nil
}

func attachDHCPv4Filter(fd int) error {
	filter := []unix.SockFilter{
		bpfStmt(unix.BPF_LD|unix.BPF_H|unix.BPF_ABS, 12),
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, etherTypeIPv4, 0, 13),
		bpfStmt(unix.BPF_LD|unix.BPF_B|unix.BPF_ABS, 23),
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, unix.IPPROTO_UDP, 0, 11),
		bpfStmt(unix.BPF_LD|unix.BPF_H|unix.BPF_ABS, 20),
		bpfJump(unix.BPF_JMP|unix.BPF_JSET|unix.BPF_K, 0x1fff, 9, 0),
		bpfStmt(unix.BPF_LD|unix.BPF_B|unix.BPF_ABS, 14),
		bpfStmt(unix.BPF_ALU|unix.BPF_AND|unix.BPF_K, 0x0f),
		bpfStmt(unix.BPF_ALU|unix.BPF_LSH|unix.BPF_K, 2),
		bpfStmt(unix.BPF_MISC|unix.BPF_TAX, 0),
		bpfStmt(unix.BPF_LD|unix.BPF_H|unix.BPF_IND, 14),
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, 67, 0, 3),
		bpfStmt(unix.BPF_LD|unix.BPF_H|unix.BPF_IND, 16),
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, 68, 0, 1),
		bpfStmt(unix.BPF_RET|unix.BPF_K, 0xffff),
		bpfStmt(unix.BPF_RET|unix.BPF_K, 0),
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	return unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &program)
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}

type linuxDHCPv4PacketConn struct {
	rawFD  int
	write  *net.UDPConn
	hwaddr net.HardwareAddr
}

func (c *linuxDHCPv4PacketConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	frame := make([]byte, 2048)
	for {
		n, _, err := unix.Recvfrom(c.rawFD, frame, 0)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				return 0, nil, linuxTimeoutError{err: err}
			}
			return 0, nil, os.NewSyscallError("recvfrom", err)
		}
		payload, src := c.dhcpPayload(frame[:n])
		if len(payload) == 0 {
			continue
		}
		copied := copy(buf, payload)
		return copied, &net.UDPAddr{IP: src, Port: 67}, nil
	}
}

type linuxTimeoutError struct {
	err error
}

func (e linuxTimeoutError) Error() string {
	return e.err.Error()
}

func (e linuxTimeoutError) Timeout() bool {
	return true
}

func (e linuxTimeoutError) Temporary() bool {
	return true
}

func (e linuxTimeoutError) Unwrap() error {
	return e.err
}

func (c *linuxDHCPv4PacketConn) dhcpPayload(frame []byte) ([]byte, net.IP) {
	if len(frame) < ethHeaderLen+20 {
		return nil, nil
	}
	if len(c.hwaddr) > 0 && len(frame) >= 12 && net.HardwareAddr(frame[6:12]).String() == c.hwaddr.String() {
		return nil, nil
	}
	if binary.BigEndian.Uint16(frame[12:14]) != etherTypeIPv4 {
		return nil, nil
	}
	ip := frame[ethHeaderLen:]
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl+8 || ip[9] != unix.IPPROTO_UDP {
		return nil, nil
	}
	total := int(binary.BigEndian.Uint16(ip[2:4]))
	if total <= 0 || total > len(ip) {
		total = len(ip)
	}
	ip = ip[:total]
	udp := ip[ihl:]
	if len(udp) < 8 {
		return nil, nil
	}
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	if srcPort != 67 || dstPort != 68 {
		return nil, nil
	}
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < 8 || udpLen > len(udp) {
		return nil, nil
	}
	return udp[8:udpLen], net.IPv4(ip[12], ip[13], ip[14], ip[15])
}

func (c *linuxDHCPv4PacketConn) WriteToUDP(buf []byte, addr *net.UDPAddr) (int, error) {
	return c.write.WriteToUDP(buf, addr)
}

func (c *linuxDHCPv4PacketConn) SetReadDeadline(deadline time.Time) error {
	var timeout time.Duration
	if deadline.IsZero() {
		timeout = 0
	} else {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			timeout = time.Microsecond
		}
	}
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	return unix.SetsockoptTimeval(c.rawFD, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
}

func (c *linuxDHCPv4PacketConn) Close() error {
	err1 := unix.Close(c.rawFD)
	err2 := c.write.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | v>>8
}
