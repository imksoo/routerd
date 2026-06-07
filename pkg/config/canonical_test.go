// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalYAMLPreservesCommentsAndOrder(t *testing.T) {
	input := []byte(`# router owner note
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  # stable router identity
  name: comment-router
spec:
  resources:
    # resources stay where the operator put them
    []
`)
	got, err := CanonicalYAML(input)
	if err != nil {
		t.Fatalf("canonical yaml: %v", err)
	}
	out := string(got)
	for _, want := range []string{
		"# router owner note",
		"# stable router identity",
		"# resources stay where the operator put them",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("canonical output lost comment %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "apiVersion:") > strings.Index(out, "kind:") {
		t.Fatalf("canonical output reordered top-level fields:\n%s", out)
	}
}

func TestAtomicWriteFileReplacesContentAndKeepsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := AtomicWriteFile(path, []byte("new\n")); err != nil {
		t.Fatalf("atomic write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("file data = %q, want new", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %v, want 0600", got)
	}
}
