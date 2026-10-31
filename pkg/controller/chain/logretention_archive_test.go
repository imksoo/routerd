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
)

func TestRetentionRequiresEverySelectedArchiveDestinationBeforeDeleting(t *testing.T) {
	for _, mode := range []string{"missing", "disabled", "filtered", "delivered", "override"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dns.db")
			dns, err := logstore.OpenDNSQueryLog(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := dns.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), QuestionName: "retain.example", QuestionType: "A"}); err != nil {
				t.Fatal(err)
			}
			if err := dns.Close(); err != nil {
				t.Fatal(err)
			}
			var delivered atomic.Int32
			sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var event eventlog.Event
				if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if event.Fields["signal"] != "dnsQueries" || event.Fields["record"] == "" {
					t.Errorf("archive record missing: %+v", event)
				}
				delivered.Add(1)
			}))
			defer sink.Close()
			resources := []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "dns"}, Spec: api.DNSResolverSpec{QueryLog: api.DNSResolverQueryLogSpec{Enabled: true, Path: path}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "retention"}, Spec: api.LogRetentionSpec{Signals: []string{"dnsQueries"}, Retention: "24h", Sinks: []string{"LogSink/archive"}}},
			}
			spec := api.LogSinkSpec{Type: "webhook", Webhook: api.LogSinkWebhookSpec{URL: sink.URL}}
			if mode == "disabled" {
				enabled := false
				spec.Enabled = &enabled
			}
			if mode == "filtered" {
				spec.MinLevel = "warning"
			}
			if mode != "missing" {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogSink"}, Metadata: api.ObjectMeta{Name: "archive"}, Spec: spec})
			}
			if mode == "override" {
				previous := eventlog.LevelOverride()
				t.Cleanup(func() { eventlog.SetLevelOverride(previous) })
				level := eventlog.LevelError
				eventlog.SetLevelOverride(&level)
			}
			controller := LogRetentionController{Router: &api.Router{Spec: api.RouterSpec{Resources: resources}}, Store: mapStore{}}
			err = controller.Reconcile(context.Background())
			db, openErr := sql.Open("sqlite", path)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer db.Close()
			var rows int
			if queryErr := db.QueryRow(`SELECT count(*) FROM dns_queries`).Scan(&rows); queryErr != nil {
				t.Fatal(queryErr)
			}
			if mode == "delivered" || mode == "override" {
				if err != nil || rows != 0 || delivered.Load() != 1 {
					t.Fatalf("archive-before-delete rows=%d delivered=%d err=%v", rows, delivered.Load(), err)
				}
			} else if err == nil || rows != 1 || delivered.Load() != 0 {
				t.Fatalf("failed archive selection deleted data: rows=%d delivered=%d err=%v", rows, delivered.Load(), err)
			}
		})
	}
}
