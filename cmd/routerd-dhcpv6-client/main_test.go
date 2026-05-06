package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreLeaseIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("write lease: %v", err)
	}
	daemon := &dhcpv6Daemon{opts: options{leaseFile: path}}
	if err := daemon.restoreLease(context.Background()); err != nil {
		t.Fatalf("restore empty lease: %v", err)
	}
}
