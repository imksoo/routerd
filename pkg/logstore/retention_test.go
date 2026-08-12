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

func TestApplyRetentionUsesRegisteredSignalTableOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE dns_queries (ts INTEGER NOT NULL);
CREATE TABLE dhcp_fingerprint (mac TEXT PRIMARY KEY, observed_at INTEGER NOT NULL);
INSERT INTO dns_queries(ts) VALUES (0);
INSERT INTO dhcp_fingerprint(mac, observed_at) VALUES ('aa:bb:cc:dd:ee:ff', 0);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyRetention(context.Background(), RetentionTarget{File: path, Retention: time.Hour, Signals: []string{RetentionSignalDNSQueries}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1", result.Deleted)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fingerprints int
	if err := db.QueryRow(`SELECT count(*) FROM dhcp_fingerprint`).Scan(&fingerprints); err != nil {
		t.Fatal(err)
	}
	if fingerprints != 1 {
		t.Fatalf("dhcp fingerprint rows = %d, want 1", fingerprints)
	}
}

func TestApplyRetentionExportsRowsBeforeDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	log, err := OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Record(context.Background(), DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), ClientAddress: "172.18.0.10", QuestionName: "old.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	var exported []RetentionRecord
	_, err = ApplyRetention(context.Background(), RetentionTarget{
		File: path, Retention: 24 * time.Hour, Signals: []string{RetentionSignalDNSQueries},
		BeforeDelete: func(record RetentionRecord) error {
			exported = append(exported, record)
			return nil
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(exported) != 1 || exported[0].Signal != RetentionSignalDNSQueries || exported[0].Values["question_name"] != "old.example" {
		t.Fatalf("exported = %#v", exported)
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
