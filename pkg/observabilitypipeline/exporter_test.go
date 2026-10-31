// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventconsumer"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestExporterPushesLokiEvent(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header = %q", r.Header.Get("X-Scope-OrgID"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	exporter := &Exporter{name: "remote", sampleRate: 1, httpClient: server.Client(), attrs: map[string]string{"site": "lab"}}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.test", daemonapi.SeverityInfo)
	event.Message = "hello"
	err := exporter.exportLoki(context.Background(), api.ObservabilityPipelineLogSink{
		Type: "loki",
		Loki: api.ObservabilityLokiSinkSpec{URL: server.URL + "/loki/api/v1/push", Tenant: "tenant-a"},
	}, event)
	if err != nil {
		t.Fatalf("export loki: %v", err)
	}
	streams, ok := got["streams"].([]any)
	if !ok || len(streams) != 1 {
		t.Fatalf("payload streams = %#v", got["streams"])
	}
	stream := streams[0].(map[string]any)["stream"].(map[string]any)
	if stream["site"] != "lab" || stream["topic"] != "routerd_test" {
		t.Fatalf("stream labels = %#v", stream)
	}
}

func TestStoredExporterRecoversMissedWakeupAndRestoresCursor(t *testing.T) {
	const eventCount = 257

	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	b := bus.NewWithStore(store)
	blockedEvents := &blockingEventStore{
		SQLiteStore: store,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}

	firstOutput := &lockedBuffer{}
	// Snapshot before store.Close: reading the database here could itself wait
	// behind the operation that caused the timeout.
	defer blockedEvents.reportFailure(t, firstOutput)
	first := testStoredController(b, store, blockedEvents, firstOutput)
	ctx, cancel := context.WithCancel(context.Background())
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blockedEvents.entered:
	case <-time.After(time.Second):
		t.Fatal("stored exporter drain did not start")
	}
	for i := 0; i < eventCount; i++ {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test"}, "routerd.test", daemonapi.SeverityInfo)
		if err := b.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	close(blockedEvents.release)
	waitForLines(t, firstOutput, eventCount)
	cancel()

	secondOutput := &lockedBuffer{}
	restarted := testStoredController(b, store, store, secondOutput)
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	if err := restarted.Start(restartCtx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := secondOutput.lines(); got != 0 {
		t.Fatalf("restart replayed %d processed events", got)
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test"}, "routerd.third", daemonapi.SeverityInfo)
	if err := b.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	waitForLines(t, secondOutput, 1)
}

func testStoredController(b *bus.Bus, store *routerstate.SQLiteStore, events eventconsumer.Store, stdout *lockedBuffer) *Controller {
	return &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"},
			Metadata: api.ObjectMeta{Name: "test"},
			Spec: api.ObservabilityPipelineSpec{
				Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "stdout"}}},
			},
		}}}},
		Bus:    b,
		Store:  store,
		Events: events,
		Stdout: stdout,
		Poll:   20 * time.Millisecond,
	}
}

type blockingEventStore struct {
	*routerstate.SQLiteStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once

	progressMu     sync.Mutex
	activeCall     string
	listCalls      int
	listedEvents   int
	savesStarted   int
	savesCompleted int
	savingCursor   int64
	savedCursor    int64
	storeErrors    int
	lastError      error
}

func (s *blockingEventStore) ListEvents(query routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	s.progressMu.Lock()
	s.activeCall = "ListEvents"
	s.listCalls++
	s.progressMu.Unlock()
	events, err := s.SQLiteStore.ListEvents(query)
	s.progressMu.Lock()
	s.activeCall = ""
	s.listedEvents += len(events)
	if err != nil {
		s.storeErrors++
		s.lastError = err
	}
	s.progressMu.Unlock()
	return events, err
}

func (s *blockingEventStore) SaveEventConsumerCursor(consumer string, cursor int64) error {
	s.progressMu.Lock()
	s.activeCall = "SaveEventConsumerCursor"
	s.savesStarted++
	s.savingCursor = cursor
	s.progressMu.Unlock()
	err := s.SQLiteStore.SaveEventConsumerCursor(consumer, cursor)
	s.progressMu.Lock()
	s.activeCall = ""
	s.savesCompleted++
	if err != nil {
		s.storeErrors++
		s.lastError = err
	} else {
		s.savedCursor = cursor
	}
	s.progressMu.Unlock()
	return err
}

func (s *blockingEventStore) reportFailure(t *testing.T, output *lockedBuffer) {
	if !t.Failed() {
		return
	}
	output.mu.Lock()
	data := append([]byte(nil), output.Bytes()...)
	output.mu.Unlock()
	unique := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var event daemonapi.DaemonEvent
		if json.Unmarshal(line, &event) == nil {
			unique[event.Cursor] = true
		}
	}
	s.progressMu.Lock()
	t.Logf("journal progress: active=%q list_calls=%d listed=%d saves_started=%d saves_completed=%d saving_cursor=%d saved_cursor=%d store_errors=%d last_error=%v output_lines=%d unique_cursors=%d",
		s.activeCall, s.listCalls, s.listedEvents, s.savesStarted, s.savesCompleted,
		s.savingCursor, s.savedCursor, s.storeErrors, s.lastError,
		bytes.Count(data, []byte{'\n'}), len(unique))
	s.progressMu.Unlock()
	// Keep failure output scoped to the drain rather than dumping unrelated
	// goroutines. This identifies a live DB/syscall wait without waiting for it.
	stack := make([]byte, 1<<20)
	n := runtime.Stack(stack, true)
	if n == len(stack) {
		t.Log("journal failure stack exceeded the diagnostic buffer")
	}
	for _, goroutine := range bytes.Split(stack[:n], []byte("\n\n")) {
		if bytes.Contains(goroutine, []byte("eventconsumer.Drain(")) {
			t.Logf("journal drain at failure:\n%s", goroutine)
		}
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(data)
}

func (b *lockedBuffer) lines() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Count(b.Bytes(), []byte{'\n'})
}

func waitForLines(t *testing.T, output *lockedBuffer, want int) {
	t.Helper()
	// This test asserts eventual store recovery across a dropped bus wake-up,
	// not a fixed export latency. Leave enough bounded time for per-event
	// SQLite cursor persistence on loaded CI hosts.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if output.lines() == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("output lines = %d, want %d", output.lines(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
