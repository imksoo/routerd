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
	"github.com/imksoo/routerd/pkg/logstore"
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

func TestDHCPQueueRetryPreservesOriginalExpiryAndReacquisitionOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "queue", "pending.db")
	stickyPath := filepath.Join(root, "sticky.db")
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 1, 2, 3, 456, time.UTC)
	for i, a := range []string{"removed", "renewed"} {
		if err := dhcpeventqueue.Store(ctx, path, controlapi.DHCPLeaseEventRequest{Action: a, MAC: "02:00:00:00:00:42", IP: "192.0.2.42"}, at.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	q, err := dhcpeventqueue.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	journal := &dhcpQueueJournal{fail: true}
	eventBus := bus.NewWithStore(journal)
	process := func(ctx context.Context, e dhcpeventqueue.Event) error {
		return processDHCPLeaseEvent(ctx, e, stickyPath, 3, eventBus)
	}
	for i := 0; i < 2; i++ {
		if ok, err := q.ProcessOne(ctx, process); ok || err == nil {
			t.Fatal("failed publish acknowledged", ok, err)
		}
	}
	sticky, err := logstore.OpenDHCPStickyLog(stickyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sticky.Close()
	rows, err := sticky.List(ctx, logstore.DHCPStickyFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if !rows[0].StickyUntil.Equal(at.Add(72*time.Hour)) || !rows[0].ReleasedAt.Equal(at) {
		t.Fatalf("retry extended hold: %+v", rows[0])
	}
	journal.fail = false
	for i := 0; i < 2; i++ {
		if ok, err := q.ProcessOne(ctx, process); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	rows, err = sticky.List(ctx, logstore.DHCPStickyFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if !rows[0].StickyUntil.IsZero() || !rows[0].LastRequestAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("old deletion overrode reacquisition: %+v", rows[0])
	}
	if len(journal.events) != 2 || journal.events[0].Type != daemonapi.EventDHCPLeaseRemoved || journal.events[1].Type != daemonapi.EventDHCPLeaseRenewed {
		t.Fatal(journal.events)
	}
	if !journal.events[0].Time.Equal(at) || !journal.events[1].Time.Equal(at.Add(time.Hour)) {
		t.Fatal("history timestamp changed")
	}
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

func TestSupersededDHCPEventIsJournaledWithoutLiveDelivery(t *testing.T) {
	for _, actions := range [][2]string{{"removed", "renewed"}, {"renewed", "removed"}} {
		t.Run(actions[0], func(t *testing.T) {
			ctx := context.Background()
			stickyPath := filepath.Join(t.TempDir(), "sticky.db")
			at := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
			sticky, err := logstore.OpenDHCPStickyLog(stickyPath)
			if err != nil {
				t.Fatal(err)
			}
			defer sticky.Close()
			if err := sticky.RecordLeaseEvent(ctx, actions[1], "02:00:00:00:00:42", "192.0.2.42", "newer", 3, at.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			journal := &dhcpQueueJournal{}
			eventBus := bus.NewWithStore(journal)
			ch, cancel := eventBus.Subscribe(ctx, bus.Subscription{}, 1)
			defer cancel()
			pending := dhcpeventqueue.Event{ObservedAt: at, Lease: controlapi.DHCPLeaseEventRequest{Action: actions[0], MAC: "02:00:00:00:00:42", IP: "192.0.2.42", Hostname: "older"}}
			if err := processDHCPLeaseEvent(ctx, pending, stickyPath, 3, eventBus); err != nil {
				t.Fatal(err)
			}
			if len(journal.events) != 1 || journal.events[0].Attributes["replayDisposition"] != "superseded" || !journal.events[0].Time.Equal(at) {
				t.Fatal(journal.events)
			}
			select {
			case e := <-ch:
				t.Fatalf("stale event reached live DNS consumers: %+v", e)
			default:
			}
			rows, err := sticky.List(ctx, logstore.DHCPStickyFilter{})
			if err != nil || len(rows) != 1 || rows[0].Hostname != "newer" {
				t.Fatal(rows, err)
			}
			if actions[1] == "removed" && !rows[0].StickyUntil.Equal(at.Add(73*time.Hour)) {
				t.Fatal(rows[0])
			}
			if actions[1] == "renewed" && !rows[0].StickyUntil.IsZero() {
				t.Fatal(rows[0])
			}
		})
	}
}
