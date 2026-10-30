// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

// A single coalesced notification wakes all current waiters. There is no
// polling goroutine or per-request background owner; request cancellation and
// shutdown release the handler even when no advertisement ever arrives.
func (d *daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	since, topic := r.URL.Query().Get("since"), r.URL.Query().Get("topic")
	tail := r.URL.Query().Get("tail") == "true"
	var wait time.Duration
	if raw := r.URL.Query().Get("wait"); raw != "" {
		var err error
		wait, err = time.ParseDuration(raw)
		if err != nil || wait < 0 {
			http.Error(w, "invalid wait", http.StatusBadRequest)
			return
		}
		wait = min(wait, 30*time.Second)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		d.mu.Lock()
		events, cursor := d.eventsSinceLocked(since, topic, tail)
		changed := d.changed
		d.mu.Unlock()
		if len(events) > 0 || cursor != since || wait == 0 || tail {
			writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
			return
		}
		select {
		case <-changed:
			continue
		case <-timer.C:
			// Publication can race the deadline. Return the latest complete
			// batch rather than the empty snapshot captured before waiting.
			d.mu.Lock()
			events, cursor = d.eventsSinceLocked(since, topic, tail)
			d.mu.Unlock()
			writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
			return
		case <-r.Context().Done():
			return
		case <-d.stopping:
			return
		}
	}
}

func (d *daemon) eventsSinceLocked(since, topic string, tail bool) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	prefix := d.cursorEpoch + ":"
	sinceID := uint64(0)
	if strings.HasPrefix(since, prefix) {
		sinceID, _ = strconv.ParseUint(strings.TrimPrefix(since, prefix), 10, 64)
	}
	cursor := prefix + strconv.FormatUint(d.nextCursor, 10)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(strings.TrimPrefix(event.Cursor, prefix), 10, 64)
		if id <= sinceID || topic != "" && event.Type != topic {
			continue
		}
		if tail {
			out = []daemonapi.DaemonEvent{event}
		} else {
			out = append(out, event)
		}
	}
	return out, cursor
}
