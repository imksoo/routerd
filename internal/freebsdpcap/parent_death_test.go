// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package freebsdpcap

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestCaptureParentFixture(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "--capture-parent" { return }
	pidPath, readyPath := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	runtime.LockOSThread() // Linux's equivalent notification is thread-scoped.
	reader, err := openCommand(context.Background(), os.Args[0], []string{"-test.run=^TestQuietCaptureFixture$", "--", "--quiet-capture", pidPath})
	if err != nil { t.Fatal(err) }
	defer reader.Close()
	if err := os.WriteFile(readyPath, []byte("ready"), 0600); err != nil { t.Fatal(err) }
	for { time.Sleep(time.Second) }
}

// The private lock is released by kernel process exit, even if the test host's
// init has not reaped an orphan zombie. No packet or broken-pipe write wakes it.
func TestQuietCaptureFixture(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--quiet-capture" { return }
	file, err := os.OpenFile(os.Args[len(os.Args)-1], os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil { t.Fatal(err) }
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil { t.Fatal(err) }
	// Write the PID only after acquiring its lifetime witness.
	if _, err := file.WriteString(strconv.Itoa(os.Getpid())); err != nil { t.Fatal(err) }
	if _, err := os.Stdout.Write(captureHeader()); err != nil { t.Fatal(err) }
	for { time.Sleep(time.Second) }
}

func TestCaptureAbruptParentDeathStopsQuietActualChild(t *testing.T) {
	dir := t.TempDir()
	pidPath, readyPath := filepath.Join(dir, "capture.pid"), filepath.Join(dir, "parent.ready")
	exe, err := os.Executable()
	if err != nil { t.Fatal(err) }
	parent := exec.Command(exe, "-test.run=^TestCaptureParentFixture$", "--", "--capture-parent", pidPath, readyPath)
	parent.Stderr = os.Stderr
	if err := parent.Start(); err != nil { t.Fatal(err) }
	parentJoined := false
	defer func() { if !parentJoined { _ = parent.Process.Kill(); _ = parent.Wait() } }()
	pid := waitChildPID(t, pidPath)
	child, err := os.FindProcess(pid)
	if err != nil { t.Fatal(err) }
	defer child.Release()
	childExited := false
	defer func() { if !childExited { _ = child.Kill() } }()
	for deadline := time.Now().Add(3*time.Second); ; {
		if _, err := os.Stat(readyPath); err == nil { break }
		if time.Now().After(deadline) { t.Fatal("actual parent did not finish capture startup") }
		time.Sleep(time.Millisecond)
	}
	lock, err := os.OpenFile(pidPath, os.O_RDWR, 0)
	if err != nil { t.Fatal(err) }
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) { t.Fatalf("capture lacked lifetime lock before parent death: %v", err) }
	if err := parent.Process.Kill(); err != nil { t.Fatal(err) }
	_ = parent.Wait()
	parentJoined = true
	for deadline := time.Now().Add(2*time.Second); ; {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			childExited = true
			return
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) { t.Fatalf("quiet capture survived abrupt parent death: %v", err) }
		time.Sleep(time.Millisecond)
	}
}
