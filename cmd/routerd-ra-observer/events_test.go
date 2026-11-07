// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Done is evaluated only after the handler captured an empty event batch and
// its notification channel. This barrier proves the test exercises a blocked
// long-poll rather than merely replaying an event published before entry.
type waitingContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *waitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestObserverLongPollWakesOnActualPublication(t *testing.T) {
	d := newDaemon(options{})
	_, cursor := d.eventsSinceLocked("", "", false)
	ctx := &waitingContext{Context: t.Context(), entered: make(chan struct{})}
	req := httptest.NewRequest("GET", "/v1/events?since="+cursor+"&wait=30s", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); d.handleEvents(response, req) }()
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not enter actual long-poll wait")
	}
	d.mu.Lock()
	d.publishLocked("detected", "warning", "Detected", "observed", nil)
	d.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publication failed to wake an existing long-poll")
	}
	var events eventsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 || events.Events[0].Type != "detected" || events.Cursor == cursor {
		t.Fatalf("publication returned a stale snapshot: %+v", events)
	}
}

func TestObserverLongPollCancellationAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "daemon"}[shutdown], func(t *testing.T) {
			d := newDaemon(options{})
			_, cursor := d.eventsSinceLocked("", "", false)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := &waitingContext{Context: parent, entered: make(chan struct{})}
			req := httptest.NewRequest("GET", "/v1/events?since="+cursor+"&wait=30s", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); d.handleEvents(response, req) }()
			select {
			case <-ctx.entered:
			case <-time.After(time.Second):
				t.Fatal("handler did not wait")
			}
			if shutdown {
				close(d.stopping)
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("handler outlived cancellation")
			}
			if response.Body.Len() != 0 {
				t.Fatal("canceled observation published a response")
			}
		})
	}
}

func TestObserverEventDeadlineFilterTailAndRestartCursor(t *testing.T) {
	d := newDaemon(options{})
	_, cursor := d.eventsSinceLocked("", "", false)
	response := httptest.NewRecorder()
	start := time.Now()
	d.handleEvents(response, httptest.NewRequest("GET", "/v1/events?since="+cursor+"&wait=20ms", nil))
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond || elapsed > time.Second {
		t.Fatalf("wait deadline was not respected: %s", elapsed)
	}
	for _, topic := range []string{"wanted", "ignored", "wanted"} {
		d.publishLocked(topic, "info", "", "", nil)
	}
	filtered, next := d.eventsSinceLocked(cursor, "missing", false)
	if len(filtered) != 0 || next == cursor {
		t.Fatal("filtered event failed to advance the source cursor")
	}
	tail, tailCursor := d.eventsSinceLocked("", "wanted", true)
	if len(tail) != 1 || tail[0].Cursor != next || tailCursor != next {
		t.Fatalf("tail did not return the latest matching event: %+v", tail)
	}
	restarted := newDaemon(options{})
	restarted.publishLocked("new", "info", "", "", nil)
	events, restartCursor := restarted.eventsSinceLocked(next, "", false)
	if restartCursor == next || len(events) != 1 || events[0].Type != "new" {
		t.Fatal("previous process cursor concealed a fresh process's first event")
	}
	for _, wait := range []string{"-1s", "invalid"} {
		response := httptest.NewRecorder()
		d.handleEvents(response, httptest.NewRequest("GET", "/v1/events?wait="+wait, nil))
		if response.Code != 400 {
			t.Fatalf("invalid wait %q accepted", wait)
		}
	}
}
