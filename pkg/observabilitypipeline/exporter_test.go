// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	for i := 0; i < 300; i++ {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test"}, "routerd.test", daemonapi.SeverityInfo)
		if err := b.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	close(blockedEvents.release)
	waitForLines(t, firstOutput, 300)
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
}

func (s *blockingEventStore) ListEvents(query routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.SQLiteStore.ListEvents(query)
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
	deadline := time.Now().Add(2 * time.Second)
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
