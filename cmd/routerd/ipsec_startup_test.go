// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
)

func TestIPsecCommandCancellationBoundsInheritedChildPipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runIPsecCommand(ctx, "sh", "-c", "sleep 30 & wait")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("IPsec command did not join its canceled child group: %v elapsed=%s", err, time.Since(started))
	}
}

func TestFreeBSDUnknownStrongSwanStatusPreservesExistingOwnerEndpoints(t *testing.T) {
	for _, endpoint := range []string{"charon.pid", "charon.vici"} {
		t.Run(endpoint, func(t *testing.T) {
			directory := t.TempDir()
			runtimeDir := filepath.Join(directory, "run")
			binDir := filepath.Join(directory, "bin")
			if err := os.MkdirAll(runtimeDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(runtimeDir, endpoint)
			original := []byte(strconv.Itoa(os.Getpid()) + "\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(binDir, "kldload"), "#!/bin/sh\nexit 0\n")
			writeExecutable(t, filepath.Join(binDir, "service"), "#!/bin/sh\necho 'VICI status unavailable' >&2\nexit 1\n")
			sysrcCalled := filepath.Join(directory, "sysrc-called")
			writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf("#!/bin/sh\ntouch %q\n", sysrcCalled))
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			previous := platformDefaults
			platformDefaults.OS = platform.OSFreeBSD
			platformDefaults.RuntimeDir = filepath.Join(runtimeDir, "routerd")
			t.Cleanup(func() { platformDefaults = previous })
			err := ensureFreeBSDStrongSwan(t.Context())
			if err == nil || !strings.Contains(err.Error(), "status is unknown") {
				t.Fatalf("unknown service status allowed startup: %v", err)
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil || string(contents) != string(original) {
				t.Fatalf("unowned endpoint changed: %v", readErr)
			}
			if _, err := os.Stat(sysrcCalled); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unknown provider caused boot configuration mutation: %v", err)
			}
		})
	}
}

func TestFreeBSDStrongSwanStatusTimeoutCannotStartProvider(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, filepath.Join(directory, "kldload"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(directory, "service"), "#!/bin/sh\nsleep 30 &\nwait\n")
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	previous := platformDefaults
	platformDefaults.OS = platform.OSFreeBSD
	platformDefaults.RuntimeDir = filepath.Join(directory, "run", "routerd")
	t.Cleanup(func() { platformDefaults = previous })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := ensureFreeBSDStrongSwan(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("unknown/timed-out status spawned or hung provider: %v elapsed=%s", err, time.Since(started))
	}
}
