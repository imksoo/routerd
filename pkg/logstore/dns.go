package logstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

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

type DNSQueryFilter struct {
	Since  time.Time
	Client string
	QName  string
	Limit  int
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

func (l *DNSQueryLog) List(ctx context.Context, filter DNSQueryFilter) ([]DNSQuery, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	var clauses []string
	var args []any
	if !filter.Since.IsZero() {
		clauses = append(clauses, "ts >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	if strings.TrimSpace(filter.Client) != "" {
		clauses = append(clauses, "client_address = ?")
		args = append(args, filter.Client)
	}
	if strings.TrimSpace(filter.QName) != "" {
		clauses = append(clauses, "question_name LIKE ?")
		args = append(args, strings.TrimSuffix(filter.QName, "."))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
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

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
