// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// FreeBSD's enum bpf_direction values are IN=0, INOUT=1, OUT=2.
const bpfDirectionInOut = 1

// packetSocket uses a native BPF device.  BPF returns one or more records per
// read; this observer consumes the first complete Ethernet frame and the
// daemon's normal ARP parser filters non-ARP traffic.
type packetSocket struct {
	fd      int
	buf     []byte
	pending []byte
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
	// Passive ARP observation must include frames that are not addressed to
	// the router itself. Attaching a BPF descriptor alone only guarantees the
	// host's normal receive path; promiscuous mode provides observer parity.
	if err := unix.IoctlSetInt(fd, unix.BIOCPROMISC, 0); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCPROMISC: %w", err)
	}
	if err := observerIoctlSetInt(fd, unix.BIOCSDIRECTION, bpfDirectionInOut); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCSDIRECTION: %w", err)
	}
	if err := installObserverFilter(fd); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := observerIoctlSetTimeval(fd, unix.BIOCSRTIMEOUT, unix.Timeval{Sec: 1}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCSRTIMEOUT: %w", err)
	}
	if err := observerIoctlSetInt(fd, unix.BIOCSHDRCMPLT, 1); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCSHDRCMPLT: %w", err)
	}
	size, err := observerIoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil || size <= 0 {
		size = 4096
	}
	return &packetSocket{fd: fd, buf: make([]byte, size)}, nil
}

// Install an explicit accept program so capture activation does not depend on
// an implicit descriptor filter. The ARP parser remains the protocol boundary.
func installObserverFilter(fd int) error {
	insns := []unix.BpfInsn{{Code: unix.BPF_RET | unix.BPF_K, K: 0xffff}}
	program := unix.BpfProgram{Len: uint32(len(insns)), Insns: &insns[0]}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BIOCSETF), uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return fmt.Errorf("BIOCSETF: %w", errno)
	}
	return nil
}

func (s *packetSocket) read(frame []byte) (int, error) {
	for {
		if len(s.pending) >= unix.SizeofBpfHdr {
			hdr := (*unix.BpfHdr)(unsafe.Pointer(&s.pending[0]))
			hdrLen, capLen, dataLen := int(hdr.Hdrlen), int(hdr.Caplen), int(hdr.Datalen)
			if hdrLen < unix.SizeofBpfHdr || capLen < 0 || dataLen < 0 || capLen > dataLen {
				return 0, fmt.Errorf("invalid BPF record header")
			}
			recordLen := bpfWordAlign(hdrLen + capLen)
			if recordLen < hdrLen || recordLen > len(s.pending) {
				return 0, fmt.Errorf("truncated BPF record")
			}
			if capLen > len(frame) {
				return 0, fmt.Errorf("BPF frame exceeds receive buffer: %d bytes", capLen)
			}
			n := copy(frame, s.pending[hdrLen:hdrLen+capLen])
			s.pending = s.pending[recordLen:]
			return n, nil
		}
		n, err := unix.Read(s.fd, s.buf)
		if err != nil {
			return 0, err
		}
		// A BPF read timeout is reported as a successful zero-byte read.
		// Keep the attached descriptor instead of treating idle periods as a
		// fatal short record and reopening it between traffic bursts.
		if n == 0 {
			continue
		}
		if n < unix.SizeofBpfHdr {
			return 0, fmt.Errorf("short BPF read: %d bytes", n)
		}
		s.pending = append(s.pending[:0], s.buf[:n]...)
	}
}

func observerIoctlGetInt(fd int, req uint) (int, error) {
	var value int32
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return 0, errno
	}
	return int(value), nil
}

func observerIoctlSetInt(fd int, req uint, value int) error {
	raw := int32(value)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&raw)))
	if errno != 0 {
		return errno
	}
	return nil
}

func observerIoctlSetTimeval(fd int, req uint, value unix.Timeval) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return errno
	}
	return nil
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

// BPF_WORDALIGN is ABI-sized, rather than a fixed four-byte alignment.  In
// particular, FreeBSD/amd64 BPF records are eight-byte aligned.
func bpfWordAlign(n int) int {
	alignment := int(unsafe.Sizeof(uintptr(0)))
	return (n + alignment - 1) &^ (alignment - 1)
}
