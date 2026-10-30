// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionNeverDeletesConcurrentUnarchivedBackdatedRecord(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "queries.db")
	writer, err := OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	old := time.Now().Add(-48 * time.Hour)
	if err := writer.Record(ctx, DNSQuery{Timestamp: old, ClientAddress: "192.0.2.1", QuestionName: "first.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	var archived []string
	target := RetentionTarget{File: path, Retention: time.Hour, Signals: []string{RetentionSignalDNSQueries}, BeforeDelete: func(record RetentionRecord) error {
		archived = append(archived, record.Values["question_name"].(string))
		return writer.Record(ctx, DNSQuery{Timestamp: old, ClientAddress: "192.0.2.2", QuestionName: "late.example", QuestionType: "A"})
	}}
	result, err := ApplyRetention(ctx, target, false)
	if err == nil || result.Deleted != 0 {
		t.Fatalf("stale archive snapshot reported committed deletion: %+v, %v", result, err)
	}
	rows, err := writer.List(ctx, DNSQueryFilter{Limit: 10})
	if err != nil || len(rows) != 2 {
		t.Fatalf("concurrent unarchived data was lost: %+v, %v", rows, err)
	}
	if len(archived) != 1 || archived[0] != "first.example" {
		t.Fatalf("archive observed data outside its snapshot: %v", archived)
	}
	target.BeforeDelete = func(record RetentionRecord) error {
		archived = append(archived, record.Values["question_name"].(string))
		return nil
	}
	result, err = ApplyRetention(ctx, target, false)
	if err != nil || result.Deleted != 2 {
		t.Fatalf("fresh retry did not archive/delete retained rows: %+v, %v", result, err)
	}
	if len(archived) != 3 {
		t.Fatalf("retry archive delivery count = %v", archived)
	}
}
