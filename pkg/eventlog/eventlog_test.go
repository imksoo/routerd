// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebhookSinkWritesEventJSON(t *testing.T) {
	events := make(chan Event, 1)
	sink, err := NewSink(api.LogSinkSpec{
		Type:     "webhook",
		MinLevel: "debug",
		Webhook:  api.LogSinkWebhookSpec{URL: "http://routerd.test/events", Timeout: "2s"},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	sink.(*WebhookSink).client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
			return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		events <- event
		return okHTTPResponse(), nil
	})}

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
	SetLevelOverride(nil)
	defer SetLevelOverride(nil)

	var called bool
	sink, err := NewSink(api.LogSinkSpec{
		Type:     "webhook",
		MinLevel: "warning",
		Webhook:  api.LogSinkWebhookSpec{URL: "http://routerd.test/events"},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	sink.(*WebhookSink).client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return okHTTPResponse(), nil
	})}
	if err := sink.Emit(Event{Level: LevelInfo, Message: "ignored"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if called {
		t.Fatal("webhook was called after ignored event")
	}
}

func okHTTPResponse() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(""))}
}

func TestLevelOverrideAllowsMoreVerboseEvents(t *testing.T) {
	SetLevelOverride(nil)
	defer SetLevelOverride(nil)

	var calls atomic.Int32
	sink, err := NewSink(api.LogSinkSpec{
		Type:     "webhook",
		MinLevel: "warning",
		Webhook:  api.LogSinkWebhookSpec{URL: "http://routerd.test/events"},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	sink.(*WebhookSink).client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return okHTTPResponse(), nil
	})}
	if err := sink.Emit(Event{Level: LevelDebug, Message: "ignored"}); err != nil {
		t.Fatalf("emit before override: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("calls before override = %d, want 0", calls.Load())
	}

	level := LevelDebug
	SetLevelOverride(&level)
	if got := LevelOverride(); got == nil || *got != LevelDebug {
		t.Fatalf("LevelOverride() = %v, want debug", got)
	}
	if err := sink.Emit(Event{Level: LevelDebug, Message: "emitted"}); err != nil {
		t.Fatalf("emit after override: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls after override = %d, want 1", calls.Load())
	}
}

func TestLevelOverrideClearRestoresSinkMinLevel(t *testing.T) {
	SetLevelOverride(nil)
	defer SetLevelOverride(nil)

	var calls atomic.Int32
	sink, err := NewSink(api.LogSinkSpec{
		Type:     "webhook",
		MinLevel: "warning",
		Webhook:  api.LogSinkWebhookSpec{URL: "http://routerd.test/events"},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	sink.(*WebhookSink).client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return okHTTPResponse(), nil
	})}

	level := LevelDebug
	SetLevelOverride(&level)
	if err := sink.Emit(Event{Level: LevelInfo, Message: "emitted"}); err != nil {
		t.Fatalf("emit with override: %v", err)
	}
	SetLevelOverride(nil)
	if got := LevelOverride(); got != nil {
		t.Fatalf("LevelOverride() = %v, want nil", *got)
	}
	if err := sink.Emit(Event{Level: LevelInfo, Message: "ignored"}); err != nil {
		t.Fatalf("emit after clear: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
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
