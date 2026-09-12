// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestLogRetentionControllerDeletesExpiredRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	dnsLog, err := logstore.OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), ClientAddress: "172.18.0.2", QuestionName: "old.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	store := mapStore{}
	controller := LogRetentionController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.DNSResolverSpec{QueryLog: api.DNSResolverQueryLogSpec{Enabled: true, Path: path}},
		}, {
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.LogRetentionSpec{Schedule: "daily", Vacuum: true, Signals: []string{"dnsQueries"}, Retention: "24h"},
		}}}},
		Bus:   bus.New(),
		Store: store,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "LogRetention", "default")
	if status["phase"] != "Applied" || status["deleted"] != int64(1) {
		t.Fatalf("status = %#v", status)
	}
}

func TestLogRetentionControllerPrunesDefaultEventAgeWithoutResource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordEvent("v1", "Test", "old", "Normal", "Old", "old"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET created_at = ?`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := LogRetentionController{Router: &api.Router{}, Bus: bus.New(), Store: store}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.EventJournalStats().Rows; got != 0 {
		t.Fatalf("default retention left %d expired events", got)
	}
}
