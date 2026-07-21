// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// packetSocket uses a native BPF device.  BPF returns one or more records per
// read; this observer consumes the first complete Ethernet frame and the
// daemon's normal ARP parser filters non-ARP traffic.
type packetSocket struct {
	fd  int
	buf []byte
}

func openPacketSocket(ifname string) (*packetSocket, error) {
	fd, err := openObserverBPF(unix.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err := attachObserverBPF(fd, ifname); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.BIOCIMMEDIATE, 1); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCIMMEDIATE: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.BIOCSHDRCMPLT, 1); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCSHDRCMPLT: %w", err)
	}
	size, err := unix.IoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil || size <= 0 {
		size = 4096
	}
	return &packetSocket{fd: fd, buf: make([]byte, size)}, nil
}

func (s *packetSocket) read(frame []byte) (int, error) {
	n, err := unix.Read(s.fd, s.buf)
	if err != nil {
		return 0, err
	}
	if n < unix.SizeofBpfHdr {
		return 0, fmt.Errorf("short BPF record: %d bytes", n)
	}
	hdr := (*unix.BpfHdr)(unsafe.Pointer(&s.buf[0]))
	start, length := int(hdr.Hdrlen), int(hdr.Caplen)
	if start <= 0 || length < 0 || start+length > n {
		return 0, fmt.Errorf("invalid BPF record header")
	}
	return copy(frame, s.buf[start:start+length]), nil
}

func (s *packetSocket) write(frame []byte) (int, error) { return unix.Write(s.fd, frame) }
func (s *packetSocket) close() error                    { return unix.Close(s.fd) }

func openObserverBPF(flags int) (int, error) {
	if fd, err := unix.Open("/dev/bpf", flags, 0); err == nil {
		return fd, nil
	}
	var last error
	for i := 0; i < 256; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), flags, 0)
		if err == nil {
			return fd, nil
		}
		last = err
	}
	return -1, fmt.Errorf("open BPF device: %w", last)
}

func attachObserverBPF(fd int, ifname string) error {
	var req [32]byte
	if len(ifname) >= len(req) {
		return fmt.Errorf("interface name too long: %s", ifname)
	}
	copy(req[:], ifname)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BIOCSETIF), uintptr(unsafe.Pointer(&req[0])))
	if errno != 0 {
		return os.NewSyscallError("BIOCSETIF", errno)
	}
	return nil
}
