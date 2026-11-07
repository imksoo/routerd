// SPDX-License-Identifier: BSD-3-Clause

package ha

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireRejectsUnreadableExistingLeaseWithoutOverwriting(t *testing.T) {
	for _, contents := range []string{"", "{", "{}"} {
		t.Run(contents, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lease.json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			decision, err := Acquire(t.Context(), Config{Identity: "router-b", Peers: []string{"router-a"}, LeasePath: path, TTL: time.Minute})
			if err == nil || decision.Leader || decision.Lease != nil {
				t.Fatalf("corrupt existing lease granted ownership: decision=%+v err=%v", decision, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != contents {
				t.Fatalf("failed read replaced existing authority: %q err=%v", got, err)
			}
		})
	}
}

func TestCanceledAcquireDoesNotCreateLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	decision, err := Acquire(ctx, Config{Identity: "router-a", Peers: []string{"router-b"}, LeasePath: path})
	if !errors.Is(err, context.Canceled) || decision.Leader {
		t.Fatalf("canceled acquisition: decision=%+v err=%v", decision, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled acquisition created a lease: %v", err)
	}
}
