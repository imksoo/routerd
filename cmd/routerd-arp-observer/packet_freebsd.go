// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/imksoo/routerd/internal/freebsdpcap"
	"golang.org/x/sys/unix"
)

// packetSocket uses FreeBSD base libpcap for capture and keeps a separate
// native BPF descriptor for proactive ARP writes.
type packetSocket struct {
	capture   *freebsdpcap.Reader
	writerFD  int
	closeOnce sync.Once
	closeErr  error
}

func openPacketSocket(ifname string) (*packetSocket, error) {
	capture, err := freebsdpcap.Open(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := openObserverBPF(unix.O_RDWR)
	if err != nil {
		_ = capture.Close()
		return nil, err
	}
	if err := attachObserverBPF(fd, ifname); err != nil {
		_ = unix.Close(fd)
		_ = capture.Close()
		return nil, err
	}
	if err := unix.IoctlSetPointerInt(fd, unix.BIOCSHDRCMPLT, 1); err != nil {
		_ = unix.Close(fd)
		_ = capture.Close()
		return nil, fmt.Errorf("BIOCSHDRCMPLT: %w", err)
	}
	return &packetSocket{capture: capture, writerFD: fd}, nil
}

func (s *packetSocket) read(frame []byte) (int, error)  { return s.capture.Read(frame) }
func (s *packetSocket) write(frame []byte) (int, error) { return unix.Write(s.writerFD, frame) }

func (s *packetSocket) closeSocket() error {
	s.closeOnce.Do(func() {
		captureErr := s.capture.Close()
		writerErr := unix.Close(s.writerFD)
		if captureErr != nil {
			s.closeErr = captureErr
		} else {
			s.closeErr = writerErr
		}
	})
	return s.closeErr
}

func (s *packetSocket) close() error { return s.closeSocket() }

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
