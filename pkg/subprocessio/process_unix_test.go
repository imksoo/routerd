// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package subprocessio

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestRunCancellationClosesDescendantOutputPipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & wait")
	cmd.Stdout = NewCapture(100)
	cmd.Stderr = NewCapture(100)
	started := time.Now()
	if err := Run(cmd); err == nil {
		t.Fatal("cancelled invocation succeeded")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("descendant held invocation for %s", elapsed)
	}
}

func TestRunBoundsPipeInheritedAfterScriptExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & exit 0")
	cmd.Stdout = NewCapture(100)
	cmd.Stderr = NewCapture(100)
	started := time.Now()
	if err := Run(cmd); err == nil {
		t.Fatal("unclosed inherited pipe was reported as completed")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("inherited pipe held invocation for %s", elapsed)
	}
}
