//go:build freebsd

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFreeBSDProviderLauncherTransfersExclusiveLeaseToOSSupervisor(t *testing.T) {
	directory := t.TempDir()
	pidfile := filepath.Join(directory, "provider.pid")
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidfile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGTERM)
			}
		}
	})
	if output, err := runIPsecCommand(t.Context(), "/usr/sbin/daemon", "-f", "-S", "-P", pidfile, "/bin/sleep", "60"); err != nil {
		t.Fatalf("provider launcher: %v %s", err, output)
	}
	if err := waitForFile(pidfile, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(pidfile, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("OS supervisor did not retain its exclusive provider lease: %v", err)
	}
	marker := filepath.Join(directory, "duplicate-started")
	_, err = runIPsecCommand(t.Context(), "/usr/sbin/daemon", "-f", "-S", "-P", pidfile, "/usr/bin/touch", marker)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 3 {
		t.Fatalf("duplicate provider did not lose pidfile admission: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate provider child ran: %v", err)
	}
}
