// SPDX-License-Identifier: BSD-3-Clause

package liveiso_test

import (
	"os"
	"strings"
	"testing"
)

func TestLiveAutostartGuardsDuplicateServe(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"routerd_serve_running()",
		"pgrep -x routerd",
		"pidof routerd",
		"tr '\\000' ' ' < \"/proc/${pid}/cmdline\"",
		"if routerd_serve_running; then",
		"routerd serve already running; not starting a duplicate",
		"elif [ ! -S \"${socket}\" ]; then",
		"nohup \"${routerd}\" serve",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live autostart script missing %q", needle)
		}
	}
	if strings.Index(script, "if routerd_serve_running; then") > strings.Index(script, "nohup \"${routerd}\" serve") {
		t.Fatalf("duplicate serve guard must run before nohup routerd serve")
	}
}
