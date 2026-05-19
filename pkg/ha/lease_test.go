// SPDX-License-Identifier: BSD-3-Clause

package ha

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireLeaseElectsSingleLeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ha-lease")
	first, err := Acquire(context.Background(), Config{Identity: "router-a", Peers: []string{"router-a", "router-b"}, LeasePath: path, TTL: time.Minute})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Lease.Close()
	if !first.Leader || first.Holder != "router-a" {
		t.Fatalf("first decision = %#v", first)
	}
	second, err := Acquire(context.Background(), Config{Identity: "router-b", Peers: []string{"router-a", "router-b"}, LeasePath: path, TTL: time.Minute})
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if second.Leader || second.Holder != "router-a" {
		t.Fatalf("second decision = %#v", second)
	}
}

func TestAcquireLeaseTakesExpiredLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ha-lease")
	first, err := Acquire(context.Background(), Config{Identity: "router-a", Peers: []string{"router-a", "router-b"}, LeasePath: path, TTL: time.Millisecond})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := first.Lease.Close(); err != nil {
		t.Fatalf("close first lease: %v", err)
	}
	time.Sleep(3 * time.Millisecond)
	second, err := Acquire(context.Background(), Config{Identity: "router-b", Peers: []string{"router-a", "router-b"}, LeasePath: path, TTL: time.Minute})
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	defer second.Lease.Close()
	if !second.Leader || second.Holder != "router-b" {
		t.Fatalf("second decision = %#v", second)
	}
}
