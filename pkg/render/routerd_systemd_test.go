// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackagedRouterdSystemdUnitMatchesRuntimeRenderer(t *testing.T) {
	want := string(SystemdUnit(RouterdUnitName, RouterdServiceSystemdSpec()))
	path := filepath.Join("..", "..", "contrib", "systemd", "routerd.service")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read packaged routerd.service: %v", err)
	}
	if string(got) != want {
		t.Fatalf("packaged routerd.service differs from the runtime renderer:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
