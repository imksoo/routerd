package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDNSQueryLogRecordAndList(t *testing.T) {
	log, err := OpenDNSQueryLog(filepath.Join(t.TempDir(), "dns-queries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Date(2026, 5, 4, 1, 2, 3, 0, time.UTC)
	if err := log.Record(context.Background(), DNSQuery{
		Timestamp:     now,
		ClientAddress: "172.18.0.10",
		QuestionName:  "www.example.com.",
		QuestionType:  "AAAA",
		ResponseCode:  "NOERROR",
		Answers:       []string{"2001:db8::1"},
		Upstream:      "nextdns",
		CacheHit:      true,
		Duration:      1200 * time.Microsecond,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := log.List(context.Background(), DNSQueryFilter{Since: now.Add(-time.Minute), Client: "172.18.0.10", QName: "www.example.com", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].QuestionName != "www.example.com" || got[0].QuestionType != "AAAA" || !got[0].CacheHit || got[0].Duration != 1200*time.Microsecond {
		t.Fatalf("query = %#v", got[0])
	}
	if len(got[0].Answers) != 1 || got[0].Answers[0] != "2001:db8::1" {
		t.Fatalf("answers = %#v", got[0].Answers)
	}
}
