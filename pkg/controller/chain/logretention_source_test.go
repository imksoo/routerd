// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestLogRetentionActualGenerationPrunesItsIdleJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecordEvent("v1", "Test", "old", "Normal", "Old", "old"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE events SET created_at = ?`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "retention-source"}}
	runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"log-retention"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE reason = 'Old'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("actual evented generation left %d expired events in its own state file", rows)
	}
}

func TestLogRetentionActualGenerationUsesItsCustomStateFileAndRecoversArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, run := range []routerstate.PluginRunRecord{
		{Plugin: "old-source", TriggerType: "manual", StartedAt: time.Now().Add(-48 * time.Hour), Status: "succeeded"},
		{Plugin: "new-source", TriggerType: "manual", StartedAt: time.Now(), Status: "succeeded"},
	} {
		if _, err := store.RecordPluginRun(run); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Set("unrelated-state", "keep", "not operational history"); err != nil {
		t.Fatal(err)
	}
	var accept atomic.Bool
	var attempted atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var event struct {
			Fields map[string]string `json:"fields"`
		}
		if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var record struct {
			Plugin string `json:"plugin"`
		}
		if err := json.Unmarshal([]byte(event.Fields["record"]), &record); err != nil || event.Fields["signal"] != "pluginRuns" || record.Plugin != "old-source" {
			t.Errorf("archive record=%+v fields=%v err=%v", record, event.Fields, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		attempted.Add(1)
		if !accept.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "custom-retention"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"}, Metadata: api.ObjectMeta{Name: "archive"}, Spec: api.LogSinkSpec{Type: "webhook", Webhook: api.LogSinkWebhookSpec{URL: receiver.URL}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.LogRetentionSpec{Retention: "24h", Signals: []string{"pluginRuns"}, Sinks: []string{"LogSink/archive"}}},
	}}}
	reconcile := func() error {
		t.Helper()
		runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"log-retention"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
		return runner.ReconcileOnce(t.Context())
	}
	// The real generation uses its status adapter and separate raw source.
	if err := reconcile(); err == nil {
		t.Fatal("failed HTTP archive was acknowledged")
	}
	runs, err := store.ListPluginRuns("")
	status, statusErr := store.ReadObjectStatus(api.SystemAPIVersion, "LogRetention", "logs")
	if err != nil || statusErr != nil || len(runs) != 2 || attempted.Load() != 1 || status["phase"] == "Applied" {
		t.Fatalf("failed archive did not retain actual source: runs=%v attempts=%d status=%v err=%v/%v", runs, attempted.Load(), status, err, statusErr)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	accept.Store(true)
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	runs, err = store.ListPluginRuns("")
	status, statusErr = store.ReadObjectStatus(api.SystemAPIVersion, "LogRetention", "logs")
	if err != nil || statusErr != nil || len(runs) != 1 || runs[0].Plugin != "new-source" || attempted.Load() != 2 || status["phase"] != "Applied" {
		t.Fatalf("reopened custom-source retention: runs=%v attempts=%d status=%v err=%v/%v", runs, attempted.Load(), status, err, statusErr)
	}
	value, err := store.ReadVariable("unrelated-state")
	if err != nil || value.Value != "keep" {
		t.Fatalf("retention changed non-log state: %+v %v", value, err)
	}
	source, err := store.LogRetentionPath()
	if err != nil || source != path {
		t.Fatalf("actual state source=%q want=%q err=%v", source, path, err)
	}
}

func TestLogRetentionUnavailableStateSourceDoesNotChoosePlatformDefaults(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.db")
	data, err := routerstate.OpenSQLite(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.RecordPluginRun(routerstate.PluginRunRecord{Plugin: "preserve", StartedAt: time.Now().Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := data.Close(); err != nil {
		t.Fatal(err)
	}
	status, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer status.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.LogRetentionSpec{Retention: "24h", Signals: []string{"pluginRuns"}}}}}}
	controller := LogRetentionController{Router: router, Store: eventedStore{Store: status}, Data: data}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("unavailable actual source was replaced by a default: %v", err)
	}
	controller.Data = nil
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("missing actual source was replaced by a default")
	}
	data, err = routerstate.OpenSQLite(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	controller.Data = data
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := controller.Reconcile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retention=%v", err)
	}
	runs, err := data.ListPluginRuns("")
	if err != nil || len(runs) != 1 {
		t.Fatalf("unavailable/canceled source lost records: %v %v", runs, err)
	}
}
