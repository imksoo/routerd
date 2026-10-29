// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenUnixSocketPreservesForeignPaths(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "control.sock")
			switch kind {
			case "file":
				if err := os.WriteFile(path, []byte("operator data"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("operator-target", path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := listenUnixSocket(path, 0660)
			if listener != nil {
				_ = listener.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "non-socket") {
				t.Fatalf("foreign %s: err=%v", kind, err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("foreign %s was replaced: info=%v err=%v", kind, after, err)
			}
			parent, err := os.Stat(dir)
			if err != nil || parent.Mode().Perm() != 0700 {
				t.Fatalf("parent permissions changed: info=%v err=%v", parent, err)
			}
		})
	}
}

func TestListenUnixSocketReclaimsOwnedStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	old, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("Unix socket fixture: %v", err)
	}
	old.SetUnlinkOnClose(false)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := listenUnixSocket(path, 0660)
	if err != nil {
		t.Fatalf("stale socket recovery: %v", err)
	}
	defer listener.Close()
}

func TestListenUnixSocketFailedBindPreservesParentPermissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := listenUnixSocket(filepath.Join(dir, strings.Repeat("s", 200)), 0660)
	if listener != nil {
		_ = listener.Close()
	}
	if err == nil {
		t.Fatal("oversized Unix socket path unexpectedly bound")
	}
	parent, err := os.Stat(dir)
	if err != nil || parent.Mode().Perm() != 0700 {
		t.Fatalf("failed bind changed parent permissions: info=%v err=%v", parent, err)
	}
}

func TestListenUnixSocketRejectsActiveListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := listenUnixSocket(path, 0660)
	if err != nil {
		t.Fatalf("Unix socket fixture: %v", err)
	}
	defer listener.Close()
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := listenUnixSocket(path, 0660)
	if second != nil {
		_ = second.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("active listener replaced: %v", err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("active socket path changed: info=%v err=%v", after, err)
	}
}
