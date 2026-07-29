// SPDX-License-Identifier: BSD-3-Clause

// Package subprocessio provides bounded process-output capture.
package subprocessio

import (
	"bytes"
	"fmt"
)

const (
	StdoutLimit = 4 << 20
	StderrLimit = 64 << 10
)

type Capture struct {
	limit    int
	buf      bytes.Buffer
	exceeded bool
}

func NewCapture(limit int) *Capture {
	return &Capture{limit: limit}
}

func (c *Capture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := c.limit - c.buf.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = c.buf.Write(p[:remaining])
	}
	if len(p) > remaining {
		c.exceeded = true
	}
	// Always consume the complete write so a noisy child cannot block on a
	// full pipe while it is being terminated or allowed to exit.
	return n, nil
}

func (c *Capture) Bytes() []byte  { return c.buf.Bytes() }
func (c *Capture) String() string { return c.buf.String() }
func (c *Capture) Exceeded() bool { return c.exceeded }

type OutputLimitError struct {
	Stream string
	Limit  int
}

func (e *OutputLimitError) Error() string {
	return fmt.Sprintf("%s output exceeded %d-byte limit", e.Stream, e.Limit)
}

func LimitError(stdout, stderr *Capture) error {
	if stdout.Exceeded() {
		return &OutputLimitError{Stream: "stdout", Limit: StdoutLimit}
	}
	if stderr.Exceeded() {
		return &OutputLimitError{Stream: "stderr", Limit: StderrLimit}
	}
	return nil
}
