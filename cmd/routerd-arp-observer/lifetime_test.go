// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestARPEventLongPollStopsWithRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	d := &daemon{}
	d.cond = sync.NewCond(&d.mu)
	done := make(chan struct{})
	go func() {
		d.handleEvents(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/events?wait=1h", nil).WithContext(ctx))
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not release event waiter")
	}
}

func TestARPServeDoesNotDeleteNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "important")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleARPSocket(path); err == nil {
		t.Fatal("non-socket removed")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatalf("file changed: %q %v", b, err)
	}
}

func TestARPEventCursorSurvivesHelperRestartAndMissingRetainedCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	first := &daemon{opts: options{resource: "pool", eventFile: path}}
	first.cond = sync.NewCond(&first.mu)
	first.publishLocked("observed", "Info", "Seen", "first", nil)
	oldCursor := first.events[0].Cursor
	restarted := &daemon{opts: first.opts}
	restarted.cond = sync.NewCond(&restarted.mu)
	if err := restarted.restoreEvents(); err != nil {
		t.Fatal(err)
	}
	restarted.publishLocked("observed", "Info", "Seen", "second", nil)
	events, cursor := restarted.eventsSinceLocked(oldCursor, "", false)
	if len(events) != 1 || events[0].Message != "second" || cursor == oldCursor {
		t.Fatalf("restart lost current event: %+v %q", events, cursor)
	}
	events, _ = restarted.eventsSinceLocked("dropped-or-undurable-cursor", "", false)
	if len(events) != 2 {
		t.Fatalf("missing cursor suppressed bounded retained replay: %+v", events)
	}
}

func TestARPOutstandingWorkIsBoundedAndExpiredTargetsCanRetry(t *testing.T) {
	d := &daemon{opts: options{probeCooldown: time.Second, probeTimeout: time.Second}, lastProbeAt: map[string]time.Time{}, pendingProbe: map[string]time.Time{}}
	now := time.Now()
	for index := 0; index < maxActiveARPProbes; index++ {
		target := netip.AddrFrom4([4]byte{192, 0, 2, byte(index)})
		if !d.shouldProbe(target, now) {
			t.Fatalf("early bound at %d", index)
		}
	}
	target := netip.MustParseAddr("198.51.100.1")
	if d.shouldProbe(target, now) {
		t.Fatal("unbounded outstanding probe work accepted")
	}
	if !d.shouldProbe(target, now.Add(3*time.Second)) {
		t.Fatal("expired pending probes held capacity forever")
	}
}
