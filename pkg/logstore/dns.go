// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DNSQueryFilterLimitMax is the maximum number of rows returned by a single List call.
// Issue #36: raised from 1000 to 10000 to support longer-range investigations.
const DNSQueryFilterLimitMax = 10000

type DNSQuery struct {
	ID            int64         `json:"id,omitempty"`
	Timestamp     time.Time     `json:"ts"`
	ClientAddress string        `json:"clientAddress"`
	QuestionName  string        `json:"questionName"`
	QuestionType  string        `json:"questionType"`
	ResponseCode  string        `json:"responseCode,omitempty"`
	Answers       []string      `json:"answers,omitempty"`
	Upstream      string        `json:"upstream,omitempty"`
	CacheHit      bool          `json:"cacheHit"`
	Duration      time.Duration `json:"duration,omitempty"`
}

// DNSQueryFilter selects rows from the DNS query log.
//
// Issue #36: added Until / ResponseCode / QNameSuffix / Upstream / DurationMinUS.
type DNSQueryFilter struct {
	Since         time.Time
	Until         time.Time
	Client        string
	QName         string
	QNameSuffix   string
	ResponseCode  string
	Upstream      string
	DurationMinUS int64
	Limit         int
}

// DNSQueryAggregate is the summary returned by Aggregate.
type DNSQueryAggregate struct {
	Total          int            `json:"total"`
	Since          time.Time      `json:"since"`
	Until          time.Time      `json:"until"`
	DurationP50US  int64          `json:"durationP50Us"`
	DurationP95US  int64          `json:"durationP95Us"`
	DurationP99US  int64          `json:"durationP99Us"`
	ByResponseCode map[string]int `json:"byResponseCode"`
	ByClient       map[string]int `json:"byClient"`
	ByUpstream     map[string]int `json:"byUpstream"`
	ByQNameSuffix  map[string]int `json:"byQNameSuffix,omitempty"`
}

type DNSQueryLog struct {
	db *sql.DB
}

func OpenDNSQueryLog(path string) (*DNSQueryLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	log := &DNSQueryLog{db: db}
	if err := log.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return log, nil
}

func OpenDNSQueryLogReadOnly(path string) (*DNSQueryLog, error) {
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil, err
	}
	return &DNSQueryLog{db: db}, nil
}

func (l *DNSQueryLog) Init(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS dns_queries (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  client_address TEXT NOT NULL,
  question_name TEXT NOT NULL,
  question_type TEXT NOT NULL,
  response_code TEXT,
  answers_json TEXT,
  upstream TEXT,
  cache_hit INTEGER NOT NULL,
  duration_us INTEGER
);
CREATE INDEX IF NOT EXISTS dns_queries_client_ts ON dns_queries(client_address, ts);
CREATE INDEX IF NOT EXISTS dns_queries_qname ON dns_queries(question_name);
`)
	return err
}

func (l *DNSQueryLog) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *DNSQueryLog) Record(ctx context.Context, q DNSQuery) error {
	if l == nil || l.db == nil {
		return nil
	}
	if q.Timestamp.IsZero() {
		q.Timestamp = time.Now().UTC()
	}
	answers, _ := json.Marshal(q.Answers)
	_, err := l.db.ExecContext(ctx, `INSERT INTO dns_queries(ts,client_address,question_name,question_type,response_code,answers_json,upstream,cache_hit,duration_us)
VALUES(?,?,?,?,?,?,?,?,?)`,
		q.Timestamp.UnixNano(),
		q.ClientAddress,
		strings.TrimSuffix(q.QuestionName, "."),
		q.QuestionType,
		q.ResponseCode,
		string(answers),
		q.Upstream,
		boolInt(q.CacheHit),
		q.Duration.Microseconds(),
	)
	return err
}

func (l *DNSQueryLog) buildDNSQuery(filter DNSQueryFilter) (string, []any) {
	var clauses []string
	var args []any
	if !filter.Since.IsZero() {
		clauses = append(clauses, "ts >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	if !filter.Until.IsZero() {
		clauses = append(clauses, "ts <= ?")
		args = append(args, filter.Until.UnixNano())
	}
	if strings.TrimSpace(filter.Client) != "" {
		clauses = append(clauses, "client_address = ?")
		args = append(args, filter.Client)
	}
	if strings.TrimSpace(filter.QName) != "" {
		clauses = append(clauses, "question_name LIKE ?")
		args = append(args, strings.TrimSuffix(filter.QName, "."))
	}
	if suffix := strings.TrimSpace(filter.QNameSuffix); suffix != "" {
		suffix = strings.TrimSuffix(suffix, ".")
		// match exact name OR any subdomain (e.g. "example.com" matches "example.com" and "x.example.com")
		clauses = append(clauses, "(question_name = ? OR question_name LIKE ?)")
		args = append(args, suffix, "%."+suffix)
	}
	if rcode := strings.TrimSpace(filter.ResponseCode); rcode != "" {
		clauses = append(clauses, "response_code = ?")
		args = append(args, rcode)
	}
	if upstream := strings.TrimSpace(filter.Upstream); upstream != "" {
		clauses = append(clauses, "upstream = ?")
		args = append(args, upstream)
	}
	if filter.DurationMinUS > 0 {
		clauses = append(clauses, "duration_us >= ?")
		args = append(args, filter.DurationMinUS)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

func (l *DNSQueryLog) List(ctx context.Context, filter DNSQueryFilter) ([]DNSQuery, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > DNSQueryFilterLimitMax {
		limit = DNSQueryFilterLimitMax
	}
	where, args := l.buildDNSQuery(filter)
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT id,ts,client_address,question_name,question_type,coalesce(response_code,''),coalesce(answers_json,'[]'),coalesce(upstream,''),cache_hit,coalesce(duration_us,0)
FROM dns_queries`+where+` ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DNSQuery
	for rows.Next() {
		var q DNSQuery
		var ts int64
		var answers string
		var cacheHit int
		var durationUS int64
		if err := rows.Scan(&q.ID, &ts, &q.ClientAddress, &q.QuestionName, &q.QuestionType, &q.ResponseCode, &answers, &q.Upstream, &cacheHit, &durationUS); err != nil {
			return nil, err
		}
		q.Timestamp = time.Unix(0, ts).UTC()
		q.CacheHit = cacheHit != 0
		q.Duration = time.Duration(durationUS) * time.Microsecond
		_ = json.Unmarshal([]byte(answers), &q.Answers)
		out = append(out, q)
	}
	return out, rows.Err()
}

// Aggregate returns summary statistics for rows matching filter.
// Limit is treated as an upper bound on rows scanned for percentile computation
// (default 100000 rows when filter.Limit <= 0).
func (l *DNSQueryLog) Aggregate(ctx context.Context, filter DNSQueryFilter) (DNSQueryAggregate, error) {
	if l == nil || l.db == nil {
		return DNSQueryAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	result := DNSQueryAggregate{
		Since:          filter.Since,
		Until:          filter.Until,
		ByResponseCode: map[string]int{},
		ByClient:       map[string]int{},
		ByUpstream:     map[string]int{},
		ByQNameSuffix:  map[string]int{},
	}
	where, args := l.buildDNSQuery(filter)

	// Total
	var total int
	if err := l.db.QueryRowContext(ctx, `SELECT count(*) FROM dns_queries`+where, args...).Scan(&total); err != nil {
		return result, err
	}
	result.Total = total

	if err := aggregateGroupBy(ctx, l.db, `SELECT coalesce(response_code,''), count(*) FROM dns_queries`+where+` GROUP BY response_code`, args, result.ByResponseCode); err != nil {
		return result, err
	}
	if err := aggregateGroupBy(ctx, l.db, `SELECT client_address, count(*) FROM dns_queries`+where+` GROUP BY client_address`, args, result.ByClient); err != nil {
		return result, err
	}
	if err := aggregateGroupBy(ctx, l.db, `SELECT coalesce(upstream,''), count(*) FROM dns_queries`+where+` GROUP BY upstream`, args, result.ByUpstream); err != nil {
		return result, err
	}

	// QName suffix (eTLD+1 approximation: last two labels of question_name)
	qnameRows, err := l.db.QueryContext(ctx, `SELECT question_name, count(*) FROM dns_queries`+where+` GROUP BY question_name`, args...)
	if err != nil {
		return result, err
	}
	defer qnameRows.Close()
	for qnameRows.Next() {
		var name string
		var cnt int
		if err := qnameRows.Scan(&name, &cnt); err != nil {
			return result, err
		}
		suffix := suffixOfQName(name)
		if suffix == "" {
			continue
		}
		result.ByQNameSuffix[suffix] += cnt
	}
	if err := qnameRows.Err(); err != nil {
		return result, err
	}

	// Percentiles: scan duration_us, compute in Go (chunked to limit memory).
	scanLimit := filter.Limit
	if scanLimit <= 0 || scanLimit > 100000 {
		scanLimit = 100000
	}
	durRows, err := l.db.QueryContext(ctx, `SELECT coalesce(duration_us,0) FROM dns_queries`+where+` ORDER BY ts DESC, id DESC LIMIT ?`, append(append([]any{}, args...), scanLimit)...)
	if err != nil {
		return result, err
	}
	defer durRows.Close()
	var durations []int64
	for durRows.Next() {
		var d int64
		if err := durRows.Scan(&d); err != nil {
			return result, err
		}
		durations = append(durations, d)
	}
	if err := durRows.Err(); err != nil {
		return result, err
	}
	result.DurationP50US = percentileInt64(durations, 0.50)
	result.DurationP95US = percentileInt64(durations, 0.95)
	result.DurationP99US = percentileInt64(durations, 0.99)
	return result, nil
}

// aggregateGroupBy issues a SQL aggregation that yields (key TEXT, count INTEGER)
// rows and fills the supplied map.
func aggregateGroupBy(ctx context.Context, db *sql.DB, query string, args []any, target map[string]int) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var cnt int
		if err := rows.Scan(&key, &cnt); err != nil {
			return err
		}
		target[key] = cnt
	}
	return rows.Err()
}

// suffixOfQName returns the last two labels of a DNS name (very rough eTLD+1
// approximation). For "www.example.com" returns "example.com". For "example.com"
// returns "example.com". For "localhost" returns "localhost". Empty input -> "".
func suffixOfQName(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(name)), ".")
	if name == "" {
		return ""
	}
	parts := strings.Split(name, ".")
	if len(parts) <= 2 {
		return name
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// percentileInt64 returns the requested percentile (0..1) of values. It sorts a
// copy of the input. Empty input returns 0.
func percentileInt64(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	// Nearest-rank method, 1-indexed.
	rank := int(float64(len(sorted)-1)*p + 0.5)
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

