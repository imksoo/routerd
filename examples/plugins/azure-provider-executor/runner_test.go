// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/subprocessio"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeRunnerBoundsOutputAndKillsTimedOutChildren(t *testing.T) {
	for _, mode := range []string{"output", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "az")
			body := "#!/bin/sh\nhead -c 5000000 /dev/zero\n"
			if mode == "timeout" {
				body = "#!/bin/sh\nsleep 10 &\nwait\n"
			}
			if err := os.WriteFile(path, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

			ctx := t.Context()
			cancel := func() {}
			if mode == "timeout" {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
			}
			defer cancel()
			started := time.Now()
			_, err := execRunner(ctx, "network", "nic", "update")
			if mode == "output" {
				var limit *subprocessio.OutputLimitError
				if !errors.As(err, &limit) {
					t.Fatalf("unbounded output accepted: %v", err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
				t.Fatalf("CLI/process children outlived cancellation: elapsed=%s err=%v", time.Since(started), err)
			}
		})
	}
}

func TestNativeRunnerMarksOnlyLocalLaunchFailureAsDefinitive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	_, err := execRunner(t.Context(), "network", "nic", "update")
	var local *commandNotStartedError
	if !errors.As(err, &local) {
		t.Fatalf("missing executable could not have mutated cloud: %v", err)
	}
}
