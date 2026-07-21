// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

type packetSocket struct {
	fd      int
	buf     []byte
	pending []byte
}

func openPacketSocket(ifname string) (*packetSocket, error) {
	fd, err := openRABPF()
	if err != nil {
		return nil, err
	}
	if err := attachRABPF(fd, ifname); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	// Rogue RA detection must observe advertisements independently of the
	// host's multicast memberships and normal receive filtering.
	if err := unix.IoctlSetInt(fd, unix.BIOCPROMISC, 0); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCPROMISC: %w", err)
	}
	if err := observerIoctlSetInt(fd, unix.BIOCIMMEDIATE, 1); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("BIOCIMMEDIATE: %w", err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("set BPF nonblocking: %w", err)
	}
	size, err := observerIoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil || size <= 0 {
		size = 4096
	}
	return &packetSocket{fd: fd, buf: make([]byte, size)}, nil
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
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				if _, pollErr := unix.Poll([]unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}, -1); pollErr != nil {
					return 0, pollErr
				}
				continue
			}
			return 0, err
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

func (s *packetSocket) close() error { return unix.Close(s.fd) }

func openRABPF() (int, error) {
	if fd, err := unix.Open("/dev/bpf", unix.O_RDONLY, 0); err == nil {
		return fd, nil
	}
	var last error
	for i := 0; i < 256; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDONLY, 0)
		if err == nil {
			return fd, nil
		}
		last = err
	}
	return -1, fmt.Errorf("open BPF device: %w", last)
}

func attachRABPF(fd int, ifname string) error {
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
