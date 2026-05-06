//go:build freebsd

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"routerd/pkg/logstore"
)

const maxBPFDevices = 256

type bpfIfReq struct {
	Name [16]byte
	_    [16]byte
}

func runPflogDaemon(ctx context.Context, opts options, log *logstore.FirewallLog) error {
	fd, err := openBPF()
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := attachBPFInterface(fd, opts.pflogInterface); err != nil {
		return err
	}
	_ = unix.IoctlSetInt(fd, unix.BIOCIMMEDIATE, 1)
	bufLen, err := unix.IoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil || bufLen <= 0 {
		bufLen = 4096
	}
	buf := make([]byte, bufLen)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		for _, packet := range bpfPackets(buf[:n]) {
			entry, ok := firewallLogEntryFromPflogPacket(packet)
			if !ok {
				continue
			}
			if err := log.Record(ctx, entry); err != nil {
				return err
			}
		}
	}
}

func openBPF() (int, error) {
	if fd, err := unix.Open("/dev/bpf", unix.O_RDONLY, 0); err == nil {
		return fd, nil
	}
	var last error
	for i := 0; i < maxBPFDevices; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDONLY, 0)
		if err == nil {
			return fd, nil
		}
		last = err
	}
	return -1, fmt.Errorf("open bpf device: %w", last)
}

func attachBPFInterface(fd int, name string) error {
	var req bpfIfReq
	if len(name) >= len(req.Name) {
		return fmt.Errorf("pflog interface name too long: %s", name)
	}
	copy(req.Name[:], name)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BIOCSETIF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return os.NewSyscallError("BIOCSETIF", errno)
	}
	return nil
}

func bpfPackets(buf []byte) [][]byte {
	var out [][]byte
	for len(buf) >= unix.SizeofBpfHdr {
		hdr := (*unix.BpfHdr)(unsafe.Pointer(&buf[0]))
		hdrLen := int(hdr.Hdrlen)
		capLen := int(hdr.Caplen)
		if hdrLen <= 0 || capLen < 0 || len(buf) < hdrLen+capLen {
			break
		}
		out = append(out, buf[hdrLen:hdrLen+capLen])
		next := bpfWordAlign(hdrLen + capLen)
		if next <= 0 || next > len(buf) {
			break
		}
		buf = buf[next:]
	}
	return out
}

func bpfWordAlign(n int) int {
	return (n + 3) &^ 3
}

func firewallLogEntryFromPflogPacket(packet []byte) (logstore.FirewallLogEntry, bool) {
	if len(packet) < 1 {
		return logstore.FirewallLogEntry{}, false
	}
	headerLen := int(packet[0])
	if headerLen <= 0 || headerLen > len(packet) {
		return logstore.FirewallLogEntry{}, false
	}
	entry, ok := firewallLogEntryFromIPPacket(time.Now().UTC(), packet[headerLen:], "pflog-bpf")
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	if headerLen >= 6 {
		entry.Action = pflogAction(packet[2])
		entry.RuleName = fmt.Sprintf("pf-rule-%d", binary.BigEndian.Uint16(packet[4:6]))
	}
	return entry, true
}

func pflogAction(action byte) string {
	switch action {
	case 0:
		return "pass"
	case 1:
		return "drop"
	case 2:
		return "scrub"
	default:
		return "drop"
	}
}
