// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
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
