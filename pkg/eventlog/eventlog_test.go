// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"strings"
	"testing"
)

func TestFormatEvent(t *testing.T) {
	line := formatEvent(Event{
		Level:   LevelWarning,
		Message: "changed",
		Router:  "r1",
		Command: "apply",
		Fields:  map[string]string{"phase": "Drifted"},
	})
	for _, want := range []string{"warning", "changed", "router=r1", "command=apply", "phase=Drifted"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain %q", line, want)
		}
	}
}
