// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestLogRetentionExplicitEventPolicyOwnsIdleExpiry(t *testing.T) {
	for _, notDue := range []bool{false, true} {
		t.Run(map[bool]string{false: "retention-exceeds-safety-age", true: "schedule-not-due"}[notDue], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.RecordEvent("v1", "Test", "old", "Normal", "Old", "preserve until owned archive"); err != nil {
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
			var delivered atomic.Int32
			sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { delivered.Add(1); w.WriteHeader(http.StatusNoContent) }))
			defer sink.Close()
			retention := "72h"
			if notDue {
				retention = "1h"
				if err := store.SaveObjectStatus(api.SystemAPIVersion, "LogRetention", "events", map[string]any{"phase": "Applied", "lastRunAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
					t.Fatal(err)
				}
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "events"}, Spec: api.LogRetentionSpec{Retention: retention, Signals: []string{"events"}, Sinks: []string{"LogSink/archive"}, Schedule: "daily"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"}, Metadata: api.ObjectMeta{Name: "archive"}, Spec: api.LogSinkSpec{Type: "webhook", Webhook: api.LogSinkWebhookSpec{URL: sink.URL}}},
			}}}
			controller := LogRetentionController{Router: router, Store: store, Data: store}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			var rows int
			if err := db.QueryRow(`SELECT count(*) FROM events WHERE reason = 'Old'`).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadObjectStatus(api.SystemAPIVersion, "LogRetention", "events")
			if err != nil {
				t.Fatal(err)
			}
			if rows != 1 || delivered.Load() != 0 {
				t.Fatalf("generic idle prune bypassed the policy: rows=%d archive=%d status=%v", rows, delivered.Load(), status)
			}
		})
	}
}
