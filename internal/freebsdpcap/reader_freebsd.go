// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

// Package freebsdpcap reads native BPF capture records through the FreeBSD
// base-system tcpdump/libpcap implementation.
package freebsdpcap

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Reader streams Ethernet frames from a base-system tcpdump process.
type Reader struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	order     binary.ByteOrder
	closeOnce sync.Once
	closeErr  error
}

// Open starts an unbuffered pcap stream for ifname. FreeBSD ships tcpdump and
// libpcap in the base system; libpcap owns the platform BPF ABI details.
func Open(ifname string) (*Reader, error) {
	cmd := exec.Command("tcpdump", "-U", "-n", "-i", ifname, "-s", "65535", "-w", "-")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("tcpdump stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start tcpdump: %w", err)
	}
	r := &Reader{cmd: cmd, stdout: stdout}
	header := make([]byte, 24)
	if _, err := io.ReadFull(stdout, header); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("read tcpdump pcap header: %w", err)
	}
	switch string(header[:4]) {
	case "\xd4\xc3\xb2\xa1", "\x4d\x3c\xb2\xa1":
		r.order = binary.LittleEndian
	case "\xa1\xb2\xc3\xd4", "\xa1\xb2\x3c\x4d":
		r.order = binary.BigEndian
	default:
		_ = r.Close()
		return nil, fmt.Errorf("unsupported tcpdump pcap magic %x", header[:4])
	}
	return r, nil
}

// Read copies the next captured Ethernet frame into frame.
func (r *Reader) Read(frame []byte) (int, error) {
	header := make([]byte, 16)
	if _, err := io.ReadFull(r.stdout, header); err != nil {
		return 0, err
	}
	captured := int(r.order.Uint32(header[8:12]))
	original := int(r.order.Uint32(header[12:16]))
	if captured < 0 || captured > original || captured > len(frame) {
		return 0, fmt.Errorf("invalid pcap record lengths captured=%d original=%d buffer=%d", captured, original, len(frame))
	}
	if _, err := io.ReadFull(r.stdout, frame[:captured]); err != nil {
		return 0, err
	}
	return captured, nil
}

// Close terminates the capture and reaps the child process.
func (r *Reader) Close() error {
	r.closeOnce.Do(func() {
		_ = r.stdout.Close()
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		r.closeErr = r.cmd.Wait()
	})
	return r.closeErr
}
