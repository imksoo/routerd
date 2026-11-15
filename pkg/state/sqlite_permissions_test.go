// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHKeyHistoryProtectsExistingSQLiteFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "state-link.db")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSQLite(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	files := []string{path, path + "-wal", path + "-shm"}
	for _, name := range files {
		if err := os.Chmod(name, 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Ordinary history writes and read-only opens must not alter permissions.
	if _, _, err := store.BeginGenerationIfChanged("ordinary", "spec:\n  resources: []\n"); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("ordinary operation changed %s permissions: %v", filepath.Base(name), err)
		}
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	secret := "spec:\n  sshHostKeys:\n    - privateKey: |\n"
	for _, line := range strings.Split(strings.TrimSpace(string(pem.EncodeToMemory(block))), "\n") {
		secret += "        " + line + "\n"
	}
	secret += "  resources: []\n"
	// A failed permission change must leave the secret out of SQLite.
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.BeginGenerationIfChanged("secret", secret); err == nil {
		t.Fatal("missing database must prevent history write")
	}
	if err := os.Rename(path+".moved", path); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM generations WHERE config_hash = 'secret'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed protection still wrote a generation: %v", err)
	}
	generation, changed, err := store.BeginGenerationIfChanged("secret", secret)
	if err != nil || !changed {
		t.Fatalf("save generation: %v", err)
	}
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unprotected %s: %v", filepath.Base(name), err)
		}
	}
	got, ok, err := store.GenerationConfig(generation)
	if err != nil || !ok || got != secret {
		t.Fatal("saved history cannot be restored")
	}
	// The legacy update path must also protect existing permissive files.
	for _, name := range files {
		if err := os.Chmod(name, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordGenerationConfig(generation, secret); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("update left %s unprotected: %v", filepath.Base(name), err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("recreated %s is not protected: %v", filepath.Base(name), err)
		}
	}
}
