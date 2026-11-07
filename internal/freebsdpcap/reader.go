// SPDX-License-Identifier: BSD-3-Clause

// Package freebsdpcap reads native BPF capture records through the FreeBSD
// base-system tcpdump/libpcap implementation.
package freebsdpcap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Reader streams Ethernet frames from a base-system tcpdump process.
type Reader struct {
	stdout    *os.File
	order     binary.ByteOrder
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	exitErr   error
	closeOnce sync.Once
	closeErr  error
}

// Open starts an unbuffered pcap stream for ifname. FreeBSD ships tcpdump and
// libpcap in the base system; libpcap owns the platform BPF ABI details.
func Open(ctx context.Context, ifname string) (*Reader, error) {
	return openCommand(ctx, "tcpdump", []string{"-U", "-n", "-i", ifname, "-s", "65535", "-w", "-"})
}

func openCommand(ctx context.Context, command string, args []string) (*Reader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, command, args...)
	if err := bindCaptureChildLifetime(cmd); err != nil {
		cancel()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	// The reader owns this pipe independently of cmd.Wait. StdoutPipe instead
	// permits Wait to close a stream while its consumer is still reading it.
	stdout, childOutput, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("tcpdump stdout: %w", err)
	}
	cmd.Stdout = childOutput
	if err := cmd.Start(); err != nil {
		cancel()
		stdout.Close()
		childOutput.Close()
		return nil, fmt.Errorf("start tcpdump: %w", err)
	}
	_ = childOutput.Close()
	r := &Reader{stdout: stdout, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go func() { r.exitErr = cmd.Wait(); close(r.done) }()
	deadline := time.Now().Add(3 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := stdout.SetReadDeadline(deadline); err != nil {
		r.Close()
		return nil, err
	}
	header := make([]byte, 24)
	if _, err := io.ReadFull(stdout, header); err != nil {
		cause := ctx.Err()
		_ = r.Close()
		if cause != nil {
			return nil, cause
		}
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
	if err := stdout.SetReadDeadline(time.Time{}); err != nil {
		r.Close()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		r.Close()
		return nil, err
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
		// An already failed child remains an error; termination requested by
		// this owner is normal. done orders access to the sole Wait result.
		reportExit, exited := r.ctx.Err() == nil, false
		select {
		case <-r.done:
			exited = true
		default:
		}
		r.cancel()
		if err := r.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			r.closeErr = err
		}
		<-r.done
		if reportExit && exited {
			r.closeErr = errors.Join(r.closeErr, r.exitErr)
		}
	})
	return r.closeErr
}
