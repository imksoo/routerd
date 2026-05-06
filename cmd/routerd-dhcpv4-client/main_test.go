package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreLeaseIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(path, []byte(" \n\t"), 0644); err != nil {
		t.Fatalf("write lease: %v", err)
	}
	daemon := &dhcpv4Daemon{opts: options{leaseFile: path}}
	if err := daemon.restoreLease(); err != nil {
		t.Fatalf("restore empty lease: %v", err)
	}
}
