// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"reflect"
	"strings"
	"unicode/utf8"
)

const fileSyncDiagnosticLimit = 4096

// Keep the original command error and cancellation cause separate: an exec
// killed by a deadline usually returns a signal error, not DeadlineExceeded.
func fileSyncCommandFailure(status map[string]any, err, contextErr error) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status["exitCode"] = exitErr.ExitCode()
	}
	status["timedOut"] = errors.Is(contextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
	status["canceled"] = errors.Is(contextErr, context.Canceled) || errors.Is(err, context.Canceled)
}

func (c FileSyncController) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func sameFileSyncFailure(previous, next map[string]any) bool {
	if previous["phase"] != "Error" {
		return false
	}
	for _, key := range []string{"reason", "command", "source", "target", "output", "error", "exitCode", "timedOut", "canceled"} {
		if key == "exitCode" && fmt.Sprint(previous[key]) == fmt.Sprint(next[key]) {
			continue
		}
		if !reflect.DeepEqual(previous[key], next[key]) {
			return false
		}
	}
	return true
}

// Retain the tail, where rsync/SSH normally report their final error. Bound the
// decoded text and preserve valid UTF-8 even for arbitrary subprocess output.
func fileSyncDiagnosticText(text string) (string, bool) {
	truncated := len(text) > fileSyncDiagnosticLimit
	if truncated {
		text = text[len(text)-fileSyncDiagnosticLimit:]
		for len(text) > 0 && !utf8.RuneStart(text[0]) {
			text = text[1:]
		}
	}
	return strings.ToValidUTF8(text, "?"), truncated
}

func (c FileSyncController) logFileSyncFailure(apiVersion, kind, name string, status map[string]any) {
	args := []any{"apiVersion", apiVersion, "kind", kind, "name", name}
	// Deliberate allowlist: never serialize the whole job, SSH options, command
	// arguments, lease contents, or unrelated status fields.
	for _, key := range []string{"reason", "command", "source", "target", "error", "output", "exitCode", "timedOut", "canceled"} {
		value, exists := status[key]
		if !exists {
			continue
		}
		if text, ok := value.(string); ok {
			bounded, truncated := fileSyncDiagnosticText(text)
			args = append(args, key, bounded)
			if truncated {
				args = append(args, key+"Truncated", true)
			}
		} else {
			args = append(args, key, value)
		}
	}
	c.logger().Warn("DHCP lease synchronization failed", args...)
}
