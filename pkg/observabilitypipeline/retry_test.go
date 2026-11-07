// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventconsumer"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type retryOutput struct {
	bytes.Buffer
	err    error
	writes int
}

func (w *retryOutput) Write(data []byte) (int, error) {
	w.writes++
	if w.err != nil {
		return 0, w.err
	}
	return w.Buffer.Write(data)
}

func TestSampledStoredEventRetryCannotAdvancePastFailedExport(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	consumer := "observabilitypipeline/test"
	if _, err := store.LoadOrInitializeEventConsumerCursor(consumer); err != nil {
		t.Fatal(err)
	}
	events := bus.NewWithStore(store)
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.test", daemonapi.SeverityInfo)
	if err := events.Publish(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("sink unavailable")
	output := &retryOutput{err: failure}
	exporter := &Exporter{name: "test", sampleRate: .002, stdout: output, sinks: []api.ObservabilityPipelineLogSink{{Type: "stdout"}}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := eventconsumer.Drain(t.Context(), store, consumer, exporter.processStoredEvent); !errors.Is(err, failure) {
			t.Fatalf("retry %d dropped selected event: %v", attempt, err)
		}
		cursor, err := store.LoadOrInitializeEventConsumerCursor(consumer)
		if err != nil || cursor != 0 {
			t.Fatalf("failed export advanced cursor: %d err=%v", cursor, err)
		}
	}
	output.err = nil
	if err := eventconsumer.Drain(t.Context(), store, consumer, exporter.processStoredEvent); err != nil {
		t.Fatal(err)
	}
	if output.writes != 3 || bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("selected event was not retried through success: writes=%d output=%q", output.writes, output.Bytes())
	}
	cursor, err := store.LoadOrInitializeEventConsumerCursor(consumer)
	if err != nil || cursor != 1 {
		t.Fatalf("successful export did not commit cursor: %d err=%v", cursor, err)
	}
}

type canceledExportTransport struct{ entered, exited chan struct{} }

func (t canceledExportTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	close(t.entered)
	<-request.Context().Done()
	close(t.exited)
	return nil, request.Context().Err()
}

type replacementOutput struct {
	previousExited <-chan struct{}
	wrote          chan bool
}

func (w replacementOutput) Write(data []byte) (int, error) {
	select {
	case <-w.previousExited:
		w.wrote <- true
	default:
		w.wrote <- false
	}
	return len(data), nil
}

func TestPipelineReplacementWaitsForPreviousExportOwner(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.LoadOrInitializeEventConsumerCursor("observabilitypipeline/test"); err != nil {
		t.Fatal(err)
	}
	events := bus.NewWithStore(store)
	transport := canceledExportTransport{entered: make(chan struct{}), exited: make(chan struct{})}
	output := replacementOutput{previousExited: transport.exited, wrote: make(chan bool, 4)}
	spec := api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "loki", Loki: api.ObservabilityLokiSinkSpec{URL: "http://export.invalid"}}}}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: spec}}}}
	controller := &Controller{Router: router, Bus: events, Events: store, HTTPClient: &http.Client{Transport: transport}, Stdout: output, Poll: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := events.Publish(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.test", daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.entered:
	case <-time.After(time.Second):
		t.Fatal("initial exporter did not begin")
	}
	spec.Logs.Sinks = []api.ObservabilityPipelineLogSink{{Type: "stdout"}}
	router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case exited := <-output.wrote:
		if !exited {
			t.Fatal("replacement raced previous cursor/effect owner")
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not retry stored event")
	}
	router.Spec.Resources = nil
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
}

type pipelineStatusFault struct {
	err   error
	calls int
}

func (s *pipelineStatusFault) SaveObjectStatus(string, string, string, map[string]any) error {
	s.calls++
	return s.err
}

func TestPipelineReportsStatusWriteFailureAndRetriesUnchangedWorker(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failure := errors.New("status unavailable")
	status := &pipelineStatusFault{err: failure}
	controller := testStoredController(bus.NewWithStore(store), store, store, &lockedBuffer{})
	controller.Store = status
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := controller.Reconcile(ctx); !errors.Is(err, failure) {
		t.Fatalf("status write failure hidden: %v", err)
	}
	status.err = nil
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if status.calls != 2 {
		t.Fatalf("unchanged worker did not retry failed publication: %d", status.calls)
	}
	controller.Router.Spec.Resources = nil
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPipelineRejectsUnsupportedEnabledSink(t *testing.T) {
	_, err := (&Controller{Stdout: io.Discard}).exporter("unsupported", api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "kafka"}}}})
	if err == nil {
		t.Fatal("unsupported sink accepted and would discard events")
	}
}
