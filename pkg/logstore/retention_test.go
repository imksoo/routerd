package logstore

import (
	"context"
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
