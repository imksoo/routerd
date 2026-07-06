// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyRetentionDeletesExpiredDNSQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	log, err := OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()
	if err := log.Record(context.Background(), DNSQuery{Timestamp: old, ClientAddress: "172.18.0.10", QuestionName: "old.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Record(context.Background(), DNSQuery{Timestamp: recent, ClientAddress: "172.18.0.10", QuestionName: "new.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	result, err := ApplyRetention(context.Background(), RetentionTarget{File: path, Retention: 24 * time.Hour}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 {
		t.Fatalf("deleted = %d", result.Deleted)
	}
	reopened, err := OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := reopened.List(context.Background(), DNSQueryFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].QuestionName != "new.example" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestVacuumAfterRetentionShrinksFreelist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freelist.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; CREATE TABLE entries (id INTEGER PRIMARY KEY, payload BLOB);`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 512; i++ {
		if _, err := db.Exec(`INSERT INTO entries(payload) VALUES(zeroblob(4096));`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM entries;`); err != nil {
		t.Fatal(err)
	}

	vacuumed, freelistPages, err := vacuumAfterRetention(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if !vacuumed {
		t.Fatal("vacuumed = false, want true")
	}
	if freelistPages == 0 {
		t.Fatal("freelistPages = 0, want > 0")
	}
	var remaining int64
	if err := db.QueryRow(`PRAGMA freelist_count;`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("remaining freelist pages = %d, want 0", remaining)
	}
}
