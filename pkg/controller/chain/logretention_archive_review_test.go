// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestReviewArchiveFailureCannotDeleteRowsUnderVerbosityOverride(t *testing.T) {
	for _, outcome := range []string{"HTTP failure", "cancel before ACK"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			directory := t.TempDir()
			path := filepath.Join(directory, "dns.db")
			dns, err := logstore.OpenDNSQueryLog(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := dns.Record(t.Context(), logstore.DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), QuestionName: "retain.example", QuestionType: "A"}); err != nil {
				t.Fatal(err)
			}
			if err := dns.Close(); err != nil {
				t.Fatal(err)
			}
			var delivered atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var record eventlog.Event
				if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
					t.Error(err)
				}
				if record.Fields["signal"] != "dnsQueries" || record.Fields["record"] == "" {
					t.Errorf("missing actual archive row: %+v", record)
				}
				delivered.Add(1)
				if outcome == "cancel before ACK" {
					cancel()
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer receiver.Close()
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "dns"}, Spec: api.DNSResolverSpec{QueryLog: api.DNSResolverQueryLogSpec{Enabled: true, Path: path}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "retention"}, Spec: api.LogRetentionSpec{Signals: []string{"dnsQueries"}, Retention: "24h", Sinks: []string{"LogSink/archive"}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"}, Metadata: api.ObjectMeta{Name: "archive"}, Spec: api.LogSinkSpec{Type: "webhook", Webhook: api.LogSinkWebhookSpec{URL: receiver.URL}}},
			}}}
			previous := eventlog.LevelOverride()
			t.Cleanup(func() { eventlog.SetLevelOverride(previous) })
			level := eventlog.LevelError
			eventlog.SetLevelOverride(&level)
			ordinary, err := eventlog.New(router)
			if err != nil {
				t.Fatal(err)
			}
			ordinary.Emit(eventlog.LevelInfo, "review", "ordinary event remains filtered", nil)
			if err := ordinary.Close(); err != nil {
				t.Fatal(err)
			}
			if delivered.Load() != 0 {
				t.Fatal("archive fix disabled ordinary runtime filtering")
			}
			store, err := routerstate.OpenSQLite(filepath.Join(directory, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			controller := LogRetentionController{Router: router, Store: store}
			reconcileErr := controller.Reconcile(ctx)
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var rows int
			if err := db.QueryRow("SELECT count(*) FROM dns_queries").Scan(&rows); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadObjectStatus(api.SystemAPIVersion, "LogRetention", "retention")
			if err != nil {
				t.Fatal(err)
			}
			if reconcileErr == nil || delivered.Load() != 1 || rows != 1 || status["phase"] == "Applied" {
				t.Fatalf("failed/canceled archive deleted or acknowledged rows: err=%v delivered=%d rows=%d phase=%v", reconcileErr, delivered.Load(), rows, status["phase"])
			}
		})
	}
}
