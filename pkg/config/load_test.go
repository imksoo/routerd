// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownResourceKind(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: TypoedKind
      metadata:
        name: bad
      spec: {}
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported resource kind TypoedKind") {
		t.Fatalf("error = %v, want unsupported kind", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/router.yaml"
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
