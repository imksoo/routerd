//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package liveiso_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveISOChecksumManifestExcludesItsTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"payload", "md5sum.txt.new"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("staged content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var command string
	for _, line := range strings.Split(liveISOScript(t), "\n") {
		if strings.HasPrefix(line, `(cd "${iso_root}" && find `) {
			command = line
			break
		}
	}
	if command == "" {
		t.Fatal("missing production ISO checksum writer")
	}
	body := "iso_root=" + shellLiteral(dir) + "\n" + command + "\n(cd " + shellLiteral(dir) + " && md5sum --check md5sum.txt)\n"
	if out, err := runLiveShell(t, body); err != nil {
		t.Fatalf("embedded ISO checksums must name only completed payload files: %v\n%s", err, out)
	}
}
