// SPDX-License-Identifier: BSD-3-Clause

package subprocessio

import (
	"errors"
	"testing"
)

func TestCaptureConsumesButDoesNotRetainPastLimit(t *testing.T) {
	c := NewCapture(4)
	n, err := c.Write([]byte("abcdefgh"))
	if err != nil || n != 8 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got := c.String(); got != "abcd" {
		t.Fatalf("captured = %q", got)
	}
	if !c.Exceeded() {
		t.Fatal("limit exceed was not recorded")
	}
}

func TestLimitErrorIsTyped(t *testing.T) {
	stdout := NewCapture(1)
	stderr := NewCapture(1)
	_, _ = stdout.Write([]byte("xx"))
	err := LimitError(stdout, stderr)
	var limitErr *OutputLimitError
	if !errors.As(err, &limitErr) || limitErr.Stream != "stdout" {
		t.Fatalf("error = %#v", err)
	}
}
