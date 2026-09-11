// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestFaultEvaluationSnapshotPreservesRawEvidenceAndExcludesWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	src, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	src.now = func() time.Time { return now.Add(-time.Hour) }
	src.Set("fixture", "value", "observed")
	if err := src.UpsertDynamicConfigPart(DynamicConfigPartRecord{Source: "MobilityPool/pool/node/router", Generation: 1, Digest: "fixture", ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Status: "active"}); err != nil {
		t.Fatal(err)
	}
	// Corrupt evidence must remain invalid after copying. Upsert would repair
	// the missing observation time; copying through it is unsafe.
	if _, err := src.db.Exec("UPDATE dynamic_config_parts SET observed_at = ''"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.RecordBusEvent(context.Background(), daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "fixture"}, "routerd.fixture", daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	if err := src.SaveEventConsumerCursor("fixture", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := src.ImportAction(ActionExecutionRecord{IdempotencyKey: "fixture", Provider: "fixture", Action: "observe"}); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	dst, err := OpenSQLite(filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if err := reader.CopyEvaluationStateTo(dst); err != nil {
		t.Fatal(err)
	}
	if got, want := dst.Get("fixture"), src.Get("fixture"); !reflect.DeepEqual(got, want) {
		t.Fatalf("variable freshness = %+v, want %+v", got, want)
	}
	parts, err := dst.ListDynamicConfigParts()
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || !parts[0].ObservedAt.IsZero() || !parts[0].ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("invalid envelope was changed: %+v", parts)
	}
	for _, table := range []string{"events", "event_consumer_cursors", "action_executions", "generations"} {
		var count int
		if err := dst.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("snapshot copied %d %s rows", count, table)
		}
	}
	// The copy has released its source read transaction. A new writer can
	// change the source and checkpoint; the isolated result remains unchanged.
	if _, err := src.db.Exec("UPDATE dynamic_config_parts SET digest = 'later'; PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	parts, err = dst.ListDynamicConfigParts()
	if err != nil || parts[0].Digest != "fixture" {
		t.Fatalf("snapshot changed after source mutation: %+v %v", parts, err)
	}
}
