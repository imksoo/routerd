// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
)

func TestWebhookSinkWritesEventJSON(t *testing.T) {
	events := make(chan Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		events <- event
	}))
	defer server.Close()
	sink, err := NewSink(api.LogSinkSpec{
		Type:     "webhook",
		MinLevel: "debug",
		Webhook:  api.LogSinkWebhookSpec{URL: server.URL, Timeout: "2s"},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}

	err = sink.Emit(Event{
		Timestamp: time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Level:     LevelInfo,
		Message:   "test event",
		Router:    "test-router",
		Command:   "apply",
		Fields:    map[string]string{"phase": "Healthy"},
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	event := <-events
	if event.Message != "test event" || event.Router != "test-router" || event.Fields["phase"] != "Healthy" {
		t.Fatalf("event = %+v", event)
	}
}

func TestWebhookSinkHonorsMinLevel(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	sink, err := NewSink(api.LogSinkSpec{
		Type:     "webhook",
		MinLevel: "warning",
		Webhook:  api.LogSinkWebhookSpec{URL: server.URL},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if err := sink.Emit(Event{Level: LevelInfo, Message: "ignored"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if called {
		t.Fatal("webhook was called after ignored event")
	}
}

func TestFormatEvent(t *testing.T) {
	line := formatEvent(Event{
		Level:   LevelWarning,
		Message: "changed",
		Router:  "r1",
		Command: "apply",
		Fields:  map[string]string{"phase": "Drifted"},
	})
	for _, want := range []string{"warning", "changed", "router=r1", "command=apply", "phase=Drifted"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain %q", line, want)
		}
	}
}
