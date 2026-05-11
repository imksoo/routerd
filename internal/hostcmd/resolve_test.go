// SPDX-License-Identifier: BSD-3-Clause

package hostcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveKeepsAbsolutePath(t *testing.T) {
	path := "/custom/bin/conntrack"
	if got := Resolve(path); got != path {
		t.Fatalf("Resolve(%q) = %q, want %q", path, got, path)
	}
}

func TestResolveFindsExecutableInExtraDir(t *testing.T) {
	dir := t.TempDir()
	name := "routerd-test-command"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(name, dir); got != path {
		t.Fatalf("Resolve(%q, %q) = %q, want %q", name, dir, got, path)
	}
}

func TestResolveConntrackDefault(t *testing.T) {
	if got := ResolveConntrack(""); got == "" {
		t.Fatal("ResolveConntrack returned empty path")
	}
}
