// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPipelineStartCannotPublishRunningWithAnUnavailableJournal(t *testing.T) {
	journal, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	status, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer status.Close()
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "wire"}, Spec: api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "stdout"}}}}}}}},
		Bus:    bus.New(), Events: journal, Store: status,
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := controller.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := controller.Start(t.Context()); err == nil {
		t.Fatal("unavailable journal was acknowledged as started")
	}
	if got := status.ObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", "wire"); got["phase"] == "Running" {
		t.Fatalf("unavailable journal published Running: %v", got)
	}
}

// The receivers decode actual wire data with the standard HTTP/TCP libraries.
// They do not share the exporter's encoding or cursor decisions.
type pipelineWireTransport struct {
	base   *http.Transport
	failed chan error
}

func (w pipelineWireTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := w.base.RoundTrip(request)
	if err != nil {
		select {
		case w.failed <- err:
		default:
		}
	}
	return response, err
}

func TestPipelineActualHTTPAndTCPRecoverJournalDeliveryAndStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	failures := make(chan error, 16)
	httpReceived, tcpReceived := make(chan string, 16), make(chan string, 16)
	// Capture an actual connection-refused outcome before starting this receiver.
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpAddress := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcpDone := make(chan struct{})
	go func() {
		defer close(tcpDone)
		for {
			connection, err := tcpListener.Accept()
			if err != nil {
				return
			}
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			line, err := bufio.NewReader(connection).ReadString('\n')
			_ = connection.Close()
			if err != nil {
				failures <- err
				continue
			}
			start := strings.IndexByte(line, '{')
			var event struct {
				Message string `json:"message"`
				Cursor  string `json:"cursor"`
				Type    string `json:"type"`
			}
			if !strings.HasPrefix(line, "<30>") || start < 0 {
				failures <- errors.New("syslog receiver did not get daemon/info framing")
				continue
			}
			if err := json.Unmarshal([]byte(line[start:]), &event); err != nil {
				failures <- err
				continue
			}
			if event.Message == "" || event.Cursor == "" || event.Type != "routerd.logging.wire" {
				failures <- errors.New("syslog receiver lost event identity")
				continue
			}
			tcpReceived <- event.Message
		}
	}()
	t.Cleanup(func() {
		_ = tcpListener.Close()
		select {
		case <-tcpDone:
		case <-time.After(2 * time.Second):
			t.Error("private TCP receiver did not stop")
		}
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var push struct {
			Streams []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"`
			} `json:"streams"`
		}
		if request.Method != http.MethodPost || request.URL.Path != "/loki/api/v1/push" || request.Header.Get("Content-Type") != "application/json" {
			failures <- errors.New("HTTP receiver did not get a Loki push request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&push); err != nil {
			failures <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(push.Streams) != 1 || push.Streams[0].Stream["pipeline"] != "wire" || len(push.Streams[0].Values) != 1 || len(push.Streams[0].Values[0]) != 2 {
			failures <- errors.New("HTTP receiver did not get one labelled log record")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var event struct {
			Message string `json:"message"`
			Cursor  string `json:"cursor"`
			Type    string `json:"type"`
		}
		if err := json.Unmarshal([]byte(push.Streams[0].Values[0][1]), &event); err != nil {
			failures <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if event.Message == "" || event.Cursor == "" || event.Type != "routerd.logging.wire" {
			failures <- errors.New("HTTP receiver lost event identity")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		httpReceived <- event.Message
		w.WriteHeader(http.StatusNoContent)
	})
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	httpDone := make(chan struct{})
	httpStarted := false
	t.Cleanup(func() {
		_ = httpServer.Close()
		if httpStarted {
			select {
			case <-httpDone:
			case <-time.After(time.Second):
				t.Error("private HTTP receiver did not stop")
			}
		}
	})
	db := filepath.Join(t.TempDir(), "events.db")
	store, err := routerstate.OpenSQLite(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	events := bus.NewWithStore(store)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "wire"},
		Spec: api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{
			{Type: "loki", Loki: api.ObservabilityLokiSinkSpec{URL: "http://" + httpAddress + "/loki/api/v1/push"}},
			{Type: "syslog", Syslog: api.LogSinkSyslogSpec{Network: "tcp", Address: tcpListener.Addr().String(), Facility: "daemon", Tag: "routerd-wire"}},
		}}},
	}}}}
	transport := &http.Transport{DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	transportFailures := make(chan error, 8)
	client := &http.Client{Timeout: 2 * time.Second, Transport: pipelineWireTransport{base: transport, failed: transportFailures}}
	controller := &Controller{Router: router, Bus: events, Store: store, Events: store, HTTPClient: client, Poll: 10 * time.Millisecond}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stopCancel()
		if err := controller.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	})
	published := 0
	publish := func() string {
		t.Helper()
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "independent-input"}, "routerd.logging.wire", daemonapi.SeverityInfo)
		published++
		event.Message = "wire-record-" + strconv.Itoa(published)
		if err := events.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
		return event.Message
	}
	await := func(received <-chan string, id string) {
		t.Helper()
		select {
		case got := <-received:
			if got != id {
				t.Fatalf("wire event=%s want=%s", got, id)
			}
		case err := <-failures:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	waitCursor := func(want int64) {
		t.Helper()
		for {
			got, err := store.LoadOrInitializeEventConsumerCursor("observabilitypipeline/wire")
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("cursor=%d want=%d: %v", got, want, ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
	}
	quiet := func() {
		t.Helper()
		select {
		case got := <-httpReceived:
			t.Fatalf("stopped exporter delivered HTTP event %s", got)
		case got := <-tcpReceived:
			t.Fatalf("stopped exporter delivered TCP event %s", got)
		case err := <-failures:
			t.Fatal(err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := controller.Start(ctx); err != nil {
		t.Fatal(err)
	}
	first := publish()
	select {
	case <-transportFailures:
	case <-ctx.Done():
		t.Fatal("actual refused connection was not observed")
	}
	if cursor, err := store.LoadOrInitializeEventConsumerCursor("observabilitypipeline/wire"); err != nil || cursor != 0 {
		t.Fatalf("failed delivery advanced journal cursor: %d %v", cursor, err)
	}
	httpListener, err := net.Listen("tcp", httpAddress)
	if err != nil {
		t.Fatal(err)
	}
	httpStarted = true
	go func() {
		defer close(httpDone)
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failures <- err
		}
	}()
	await(httpReceived, first)
	await(tcpReceived, first)
	waitCursor(1)
	if err := controller.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	second := publish()
	quiet()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(db)
	if err != nil {
		t.Fatal(err)
	}
	events = bus.NewWithStore(store)
	controller = &Controller{Router: router, Bus: events, Store: store, Events: store, HTTPClient: client, Poll: 10 * time.Millisecond}
	if err := controller.Start(ctx); err != nil {
		t.Fatal(err)
	}
	await(httpReceived, second)
	await(tcpReceived, second)
	waitCursor(2)
	controller.Router = &api.Router{}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	publish()
	quiet()
	status, err := store.ReadObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", "wire")
	if err != nil || status["phase"] != "Stopped" {
		t.Fatalf("withdrawn pipeline status=%v err=%v", status, err)
	}
	if len(controller.running) != 0 {
		t.Fatal("withdrawn exporter still has a worker")
	}
}

// Cancellation happens immediately after a real SQLite cursor operation, at
// the startup wait boundary rather than by substituting exporter decisions.
type canceledCursorJournal struct {
	*routerstate.SQLiteStore
	cancel context.CancelFunc
}

func (s canceledCursorJournal) LoadOrInitializeEventConsumerCursor(name string) (int64, error) {
	cursor, err := s.SQLiteStore.LoadOrInitializeEventConsumerCursor(name)
	s.cancel()
	return cursor, err
}
func TestPipelineCanceledCursorWaitCannotStartAnotherWorker(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	controller := testStoredController(bus.NewWithStore(store), store, canceledCursorJournal{SQLiteStore: store, cancel: cancel}, &lockedBuffer{})
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stopCancel()
		if err := controller.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	})
	err = controller.Start(ctx)
	status, readErr := store.ReadObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", "test")
	if !errors.Is(err, context.Canceled) || readErr != nil || len(controller.running) != 0 || status["phase"] == "Running" {
		t.Fatalf("canceled startup: err=%v status=%v workers=%d readErr=%v", err, status, len(controller.running), readErr)
	}
}
