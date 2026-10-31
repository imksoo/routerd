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

	"golang.org/x/sys/unix"
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
	const attribute = "user.routerd-live-cache"
	wantAttribute := []byte("package metadata round trip")
	if err := unix.Setxattr(payload, attribute, wantAttribute, 0); err != nil {
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
		break
	}
	restoredWork := filepath.Join(dir, "restored")
	if err := os.Mkdir(restoredWork, 0755); err != nil {
		t.Fatal(err)
	}
	start = strings.Index(script, "if [ -n \"${rootfs_cache}\" ] && [ -f \"${rootfs_cache}\" ]; then\n")
	if start < 0 {
		t.Fatal("missing production cache reader")
	}
	end = strings.Index(script[start:], "\nelse\n")
	if end < 0 {
		t.Fatal("unterminated production cache reader")
	}
	body = "workdir=" + shellLiteral(restoredWork) + "\nrootfs=" + shellLiteral(filepath.Join(restoredWork, "rootfs")) + "\nrootfs_cache=" + shellLiteral(archive) + "\n" + script[start:start+end] + "\nfi\n"
	if out, err := runLiveShell(t, body); err != nil {
		t.Fatalf("actual cache reader: %v\n%s", err, out)
	}
	restoredPayload := filepath.Join(restoredWork, "rootfs", "owned-mode-fixture")
	gotAttribute := make([]byte, 256)
	n, err := unix.Getxattr(restoredPayload, attribute, gotAttribute)
	if err != nil {
		t.Fatalf("cache lost package extended attribute: %v", err)
	}
	if string(gotAttribute[:n]) != string(wantAttribute) {
		t.Fatalf("cache changed package extended attribute: got=%q want=%q", gotAttribute[:n], wantAttribute)
	}
}

func TestLiveRootfsCacheRejectsOlderFormats(t *testing.T) {
	script := liveISOScript(t)
	start := strings.Index(script, "if [ -n \"${rootfs_cache}\" ] && [ -f \"${rootfs_cache}\" ]; then\n")
	if start < 0 {
		t.Fatal("missing production cache reader")
	}
	end := strings.Index(script[start:], "\nelse\n")
	if end < 0 {
		t.Fatal("unterminated production cache reader")
	}
	for _, format := range []string{"routerd-live-rootfs-v1", "routerd-live-rootfs-v2"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			etc := filepath.Join(dir, "rootfs", "etc")
			if err := os.MkdirAll(etc, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(etc, "routerd-live-rootfs-format"), []byte(format+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(dir, "older.tar")
			if out, err := runLiveShell(t, "tar -cf "+shellLiteral(archive)+" -C "+shellLiteral(dir)+" rootfs"); err != nil {
				t.Fatalf("older cache fixture: %v\n%s", err, out)
			}
			restored := filepath.Join(dir, "restored")
			if err := os.Mkdir(restored, 0755); err != nil {
				t.Fatal(err)
			}
			body := "workdir=" + shellLiteral(restored) + "\nrootfs=" + shellLiteral(filepath.Join(restored, "rootfs")) + "\nrootfs_cache=" + shellLiteral(archive) + "\n" + script[start:start+end] + "\nfi\n"
			if out, err := runLiveShell(t, body); err == nil || !strings.Contains(out, "rebuild it") {
				t.Fatalf("older cache was not refused: %v\n%s", err, out)
			}
		})
	}
}
