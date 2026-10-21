// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package dhcpeventqueue

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCorruptQueueIsAnExplicitFailureAndPreserved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pending.db")
	raw := []byte("not a SQLite database\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Store(context.Background(), path, lease("renewed"), time.Now()); err == nil {
		t.Fatal("corrupt queue acknowledged callback")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("corrupt evidence modified", err)
	}
}

func TestReadOnlyQueueCannotAcknowledgeCallback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX write permission fixture; run as unprivileged test user")
	}
	dir := filepath.Join(t.TempDir(), "events")
	path := filepath.Join(dir, "pending.db")
	q, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	q.Close()
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if err := Store(context.Background(), path, lease("renewed"), time.Now()); err == nil {
		t.Fatal("read-only queue acknowledged callback")
	}
}
