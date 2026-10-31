//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package liveiso_test

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveRootfsCacheStoresPortableNumericOwnership(t *testing.T) {
	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs")
	if err := os.MkdirAll(filepath.Join(rootfs, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(rootfs, "owned-mode-fixture")
	if err := os.WriteFile(payload, []byte("data only; never executed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(payload, 0750|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	script := liveISOScript(t)
	start := strings.Index(script, "if [ -n \"${rootfs_cache}\" ]; then\n")
	if start < 0 {
		t.Fatal("missing production cache writer")
	}
	end := strings.Index(script[start:], "\nfi")
	if end < 0 {
		t.Fatal("unterminated production cache writer")
	}
	archive := filepath.Join(dir, "rootfs.tar")
	body := "workdir=" + shellLiteral(dir) + "\nrootfs=" + shellLiteral(rootfs) + "\nrootfs_cache=" + shellLiteral(archive) + "\n" + script[start:start+end+3] + "\n"
	if out, err := runLiveShell(t, body); err != nil {
		t.Fatalf("actual cache writer: %v\n%s", err, out)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			t.Fatal("cache omitted the owned file")
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != "rootfs/owned-mode-fixture" {
			continue
		}
		if header.Uid != os.Getuid() || header.Gid != os.Getgid() || header.Mode != 04750 {
			t.Fatalf("package identity/mode changed in cache: uid=%d gid=%d mode=%o", header.Uid, header.Gid, header.Mode)
		}
		if header.Uname != "" || header.Gname != "" {
			t.Fatalf("cache depends on the receiving build host's account names: user=%q group=%q", header.Uname, header.Gname)
		}
		return
	}
}
