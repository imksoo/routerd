// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dhcpeventqueue"
	"github.com/imksoo/routerd/pkg/state"
)

type dhcpQueueJournal struct {
	mu     sync.Mutex
	fail   bool
	events []daemonapi.DaemonEvent
}

func (s *dhcpQueueJournal) RecordBusEvent(_ context.Context, e daemonapi.DaemonEvent) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return "", &state.StorageWriteError{Err: errors.New("fixture journal failure")}
	}
	s.events = append(s.events, e)
	return "1", nil
}

func TestDHCPQueueWorkerStartsWithoutCreatingStateAndRecoversNewQueue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "dhcp-events", "pending.db")
	journal := &dhcpQueueJournal{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		runDHCPEventQueue(ctx, path, filepath.Join(root, "sticky.db"), filepath.Join(root, "leases"), func() *api.Router { return nil }, bus.NewWithStore(journal))
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("idle startup created queue", err)
	}
	if err := dhcpeventqueue.Store(ctx, path, controlapi.DHCPLeaseEventRequest{Action: "renewed", IP: "192.0.2.42"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		journal.mu.Lock()
		n := len(journal.events)
		journal.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not recover newly created queue")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not cancel")
	}
}

func TestDHCPQueueWorkerStorageFailureDoesNotExitRouterd(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pending.db")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runDHCPEventQueue(ctx, path, filepath.Join(root, "sticky.db"), filepath.Join(root, "leases"), func() *api.Router { return nil }, bus.New())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("worker exited on recoverable queue error")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not cancel")
	}
}
