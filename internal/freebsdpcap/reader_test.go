// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package freebsdpcap

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func captureHeader() []byte {
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(header[4:], 2)
	binary.LittleEndian.PutUint16(header[6:], 4)
	binary.LittleEndian.PutUint32(header[16:], 65535)
	binary.LittleEndian.PutUint32(header[20:], 1)
	return header
}

func TestCaptureChildFixture(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "--capture-child" { return }
	mode, pidPath := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil { t.Fatal(err) }
	header := captureHeader()
	switch mode {
	case "partial":
		header = header[:7]
	case "silent":
		header = nil
	case "bad":
		header[0] = 0
	}
	if len(header) != 0 {
		if _, err := os.Stdout.Write(header); err != nil { t.Fatal(err) }
	}
	for { time.Sleep(time.Second) }
}

func waitChildPID(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(3*time.Second); time.Now().Before(deadline); {
		data, _ := os.ReadFile(path)
		if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 { return pid }
		time.Sleep(time.Millisecond)
	}
	t.Fatal("actual child did not announce its private PID")
	return 0
}

func TestCaptureCancellationJoinsActualChildDuringStartupAndRead(t *testing.T) {
	exe, err := os.Executable()
	if err != nil { t.Fatal(err) }
	for _, mode := range []string{"silent", "partial", "header"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			path := filepath.Join(t.TempDir(), "capture.pid")
			type outcome struct { reader *Reader; err error }
			opened := make(chan outcome, 1)
			go func() { r, err := openCommand(ctx, exe, []string{"-test.run=^TestCaptureChildFixture$", "--", "--capture-child", mode, path}); opened <- outcome{r, err} }()
			pid := waitChildPID(t, path)
			if mode != "header" { cancel() }
			var result outcome
			select {
			case result = <-opened:
			case <-time.After(time.Second): t.Fatal("startup cancellation did not join actual child")
			}
			if mode != "header" {
				if !errors.Is(result.err, context.Canceled) { t.Fatalf("canceled startup accepted: %v", result.err) }
			} else {
				if result.err != nil { t.Fatal(result.err) }
				read := make(chan error, 1)
				go func() { _, err := result.reader.Read(make([]byte, 64)); read <- err }()
				cancel()
				if err := result.reader.Close(); err != nil { t.Fatal(err) }
				select {
				case err := <-read: if err == nil { t.Fatal("canceled capture read succeeded") }
				case <-time.After(time.Second): t.Fatal("actual capture read did not join")
				}
				select {
				case <-result.reader.done:
				default: t.Fatal("Close acknowledged an unreaped child")
				}
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) { t.Fatalf("joined child%d remained: %v", pid, err) }
		})
	}
}

func TestCaptureBadStartupDoesNotBecomeCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil { t.Fatal(err) }
	_, err = openCommand(t.Context(), exe, []string{"-test.run=^TestCaptureChildFixture$", "--", "--capture-child", "bad", filepath.Join(t.TempDir(), "capture.pid")})
	if err == nil || errors.Is(err, context.Canceled) { t.Fatalf("invalid header error lost its cause: %v", err) }
}

func TestCaptureCanceledStartupCreatesNoChild(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := openCommand(ctx, "must-not-execute", nil); !errors.Is(err, context.Canceled) { t.Fatalf("already canceled startup executed: %v", err) }
}
