// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExitCodeWithHardTimeout(t *testing.T) {
	block := make(chan struct{})
	var stderr bytes.Buffer
	code := exitCodeWithHardTimeout(func() error {
		<-block
		return nil
	}, 10*time.Millisecond, &stderr)
	close(block)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "dhcp-event-relay: hard timeout") {
		t.Fatalf("stderr missing hard timeout: %q", stderr.String())
	}
}

func TestExitCodeWithHardTimeoutReportsRunError(t *testing.T) {
	var stderr bytes.Buffer
	code := exitCodeWithHardTimeout(func() error {
		return errors.New("boom")
	}, time.Second, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr missing run error: %q", stderr.String())
	}
}

func TestExitCodeWithHardTimeoutReturnsZeroOnSuccess(t *testing.T) {
	var stderr bytes.Buffer
	code := exitCodeWithHardTimeout(func() error {
		return nil
	}, time.Second, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
