// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func seedDNSQueryRows(t *testing.T, log *DNSQueryLog, base time.Time) {
	t.Helper()
	rows := []DNSQuery{
		{Timestamp: base, ClientAddress: "192.168.1.10", QuestionName: "www.example.com.", QuestionType: "A", ResponseCode: "NOERROR", Upstream: "1.1.1.1", Duration: 12 * time.Millisecond},
		{Timestamp: base.Add(10 * time.Second), ClientAddress: "192.168.1.10", QuestionName: "api.example.com.", QuestionType: "A", ResponseCode: "NOERROR", Upstream: "1.1.1.1", Duration: 35 * time.Millisecond},
		{Timestamp: base.Add(20 * time.Second), ClientAddress: "192.168.1.11", QuestionName: "tracker.example.org.", QuestionType: "A", ResponseCode: "NXDOMAIN", Upstream: "9.9.9.9", Duration: 200 * time.Millisecond},
		{Timestamp: base.Add(30 * time.Second), ClientAddress: "192.168.1.12", QuestionName: "broken.example.net.", QuestionType: "A", ResponseCode: "SERVFAIL", Upstream: "8.8.8.8", Duration: 500 * time.Millisecond},
		{Timestamp: base.Add(40 * time.Second), ClientAddress: "192.168.1.10", QuestionName: "stale.example.com.", QuestionType: "A", ResponseCode: "NOERROR", Upstream: "1.1.1.1", Duration: 80 * time.Millisecond},
	}
	for _, row := range rows {
		if err := log.Record(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenDNSQueryLogConfiguresWALCheckpoint(t *testing.T) {
	log, err := OpenDNSQueryLog(filepath.Join(t.TempDir(), "dns-queries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	var autoCheckpoint int
	if err := log.db.QueryRow(`PRAGMA wal_autocheckpoint`).Scan(&autoCheckpoint); err != nil {
		t.Fatal(err)
	}
	if autoCheckpoint != dnsQueryWALAutoCheckpointPages {
		t.Fatalf("wal_autocheckpoint = %d, want %d", autoCheckpoint, dnsQueryWALAutoCheckpointPages)
	}

	var synchronous int
	if err := log.db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want NORMAL", synchronous)
	}

	var journalSizeLimit int64
	if err := log.db.QueryRow(`PRAGMA journal_size_limit`).Scan(&journalSizeLimit); err != nil {
		t.Fatal(err)
	}
	if journalSizeLimit != dnsQueryJournalSizeLimitBytes {
		t.Fatalf("journal_size_limit = %d, want %d", journalSizeLimit, dnsQueryJournalSizeLimitBytes)
	}
}

func TestDNSQueryLogFiltersAndAggregate(t *testing.T) {
	log, err := OpenDNSQueryLog(filepath.Join(t.TempDir(), "dns-queries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	seedDNSQueryRows(t, log, base)

	// Until filter excludes rows past the boundary.
	until := base.Add(15 * time.Second)
	rows, err := log.List(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), Until: until, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("until filter expected 2 rows, got %d", len(rows))
	}

	// ResponseCode filter
	rows, err = log.List(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), ResponseCode: "NXDOMAIN", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ResponseCode != "NXDOMAIN" {
		t.Fatalf("rcode filter rows=%#v", rows)
	}

	// Upstream filter
	rows, err = log.List(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), Upstream: "1.1.1.1", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("upstream filter expected 3, got %d", len(rows))
	}

	// DurationMinUS
	rows, err = log.List(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), DurationMinUS: int64((100 * time.Millisecond).Microseconds()), Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("duration min filter expected 2, got %d", len(rows))
	}

	// QNameSuffix: matches both exact and subdomain
	rows, err = log.List(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), QNameSuffix: "example.com", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("suffix filter expected 3, got %d", len(rows))
	}

	// Aggregate
	agg, err := log.Aggregate(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), Until: base.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Total != 5 {
		t.Fatalf("aggregate total = %d", agg.Total)
	}
	if agg.ByResponseCode["NOERROR"] != 3 || agg.ByResponseCode["NXDOMAIN"] != 1 || agg.ByResponseCode["SERVFAIL"] != 1 {
		t.Fatalf("ByResponseCode = %#v", agg.ByResponseCode)
	}
	if agg.ByClient["192.168.1.10"] != 3 {
		t.Fatalf("ByClient = %#v", agg.ByClient)
	}
	if agg.ByUpstream["1.1.1.1"] != 3 {
		t.Fatalf("ByUpstream = %#v", agg.ByUpstream)
	}
	if agg.ByQNameSuffix["example.com"] != 3 {
		t.Fatalf("ByQNameSuffix = %#v", agg.ByQNameSuffix)
	}
	// Percentile: durations sorted = 12ms, 35ms, 80ms, 200ms, 500ms; nearest-rank
	// p50 -> rank floor(4*0.5+0.5)=2 -> 80ms, p95 -> rank floor(4*0.95+0.5)=4 -> 500ms
	wantP50 := int64((80 * time.Millisecond).Microseconds())
	if agg.DurationP50US != wantP50 {
		t.Fatalf("p50 = %d, want %d", agg.DurationP50US, wantP50)
	}
	wantP95 := int64((500 * time.Millisecond).Microseconds())
	if agg.DurationP95US != wantP95 {
		t.Fatalf("p95 = %d, want %d", agg.DurationP95US, wantP95)
	}
}

func TestDNSQueryLogLimitHardCap(t *testing.T) {
	log, err := OpenDNSQueryLog(filepath.Join(t.TempDir(), "dns-queries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := log.Record(context.Background(), DNSQuery{Timestamp: base.Add(time.Duration(i) * time.Second), ClientAddress: "192.168.1.10", QuestionName: "a.example.com.", QuestionType: "A", ResponseCode: "NOERROR"}); err != nil {
			t.Fatal(err)
		}
	}
	// Request more than DNSQueryFilterLimitMax; should clamp without erroring.
	rows, err := log.List(context.Background(), DNSQueryFilter{Since: base.Add(-time.Minute), Limit: DNSQueryFilterLimitMax + 5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
}

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

func TestDNSQueryLogStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	log, err := OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	now := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	if err := log.Record(context.Background(), DNSQuery{
		Timestamp:     now,
		ClientAddress: "192.0.2.10",
		QuestionName:  "stats.example.",
		QuestionType:  "A",
		ResponseCode:  "NOERROR",
	}); err != nil {
		t.Fatal(err)
	}

	stats := log.Stats()
	if stats.Path != path {
		t.Fatalf("path = %q, want %q", stats.Path, path)
	}
	if stats.Records != 1 || stats.RecordErrors != 0 {
		t.Fatalf("stats counts = %#v", stats)
	}
	if !stats.LastRecordTime.Equal(now) {
		t.Fatalf("last record time = %s, want %s", stats.LastRecordTime, now)
	}
	if stats.DBBytes <= 0 {
		t.Fatalf("DBBytes = %d, want > 0", stats.DBBytes)
	}
	if stats.WALBytes < 0 {
		t.Fatalf("WALBytes = %d, want >= 0", stats.WALBytes)
	}
}
