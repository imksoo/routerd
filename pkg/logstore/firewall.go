package logstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type FirewallLogEntry struct {
	ID          int64     `json:"id,omitempty"`
	Timestamp   time.Time `json:"ts"`
	ZoneFrom    string    `json:"zoneFrom,omitempty"`
	ZoneTo      string    `json:"zoneTo,omitempty"`
	RuleName    string    `json:"ruleName,omitempty"`
	Action      string    `json:"action"`
	SrcAddress  string    `json:"srcAddress"`
	SrcPort     int       `json:"srcPort,omitempty"`
	DstAddress  string    `json:"dstAddress"`
	DstPort     int       `json:"dstPort,omitempty"`
	Protocol    string    `json:"protocol"`
	L3Proto     string    `json:"l3Proto"`
	InIface     string    `json:"inIface,omitempty"`
	OutIface    string    `json:"outIface,omitempty"`
	PacketBytes int       `json:"packetBytes,omitempty"`
	Hint        string    `json:"hint,omitempty"`
}

type FirewallLogFilter struct {
	Since  time.Time
	Action string
	Src    string
	Limit  int
}

type FirewallLog struct {
	db *sql.DB
}

func OpenFirewallLog(path string) (*FirewallLog, error) {
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
	log := &FirewallLog{db: db}
	if err := log.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return log, nil
}

func (l *FirewallLog) Init(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS firewall_logs (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  zone_from TEXT,
  zone_to TEXT,
  rule_name TEXT,
  action TEXT NOT NULL,
  src_address TEXT NOT NULL,
  src_port INTEGER,
  dst_address TEXT NOT NULL,
  dst_port INTEGER,
  protocol TEXT NOT NULL,
  l3_proto TEXT NOT NULL,
  in_iface TEXT,
  out_iface TEXT,
  packet_bytes INTEGER,
  hint TEXT
);
CREATE INDEX IF NOT EXISTS firewall_logs_ts ON firewall_logs(ts);
CREATE INDEX IF NOT EXISTS firewall_logs_src_ts ON firewall_logs(src_address, ts);
CREATE INDEX IF NOT EXISTS firewall_logs_action_ts ON firewall_logs(action, ts);
CREATE INDEX IF NOT EXISTS firewall_logs_zone ON firewall_logs(zone_from, zone_to, ts);
`)
	return err
}

func (l *FirewallLog) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *FirewallLog) Record(ctx context.Context, entry FirewallLogEntry) error {
	if l == nil || l.db == nil {
		return nil
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	_, err := l.db.ExecContext(ctx, `INSERT INTO firewall_logs(ts,zone_from,zone_to,rule_name,action,src_address,src_port,dst_address,dst_port,protocol,l3_proto,in_iface,out_iface,packet_bytes,hint)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.Timestamp.UnixNano(), entry.ZoneFrom, entry.ZoneTo, entry.RuleName, entry.Action, entry.SrcAddress, entry.SrcPort, entry.DstAddress, entry.DstPort, entry.Protocol, entry.L3Proto, entry.InIface, entry.OutIface, entry.PacketBytes, entry.Hint)
	return err
}

func (l *FirewallLog) List(ctx context.Context, filter FirewallLogFilter) ([]FirewallLogEntry, error) {
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
	if strings.TrimSpace(filter.Action) != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, filter.Action)
	}
	if strings.TrimSpace(filter.Src) != "" {
		clauses = append(clauses, "src_address = ?")
		args = append(args, filter.Src)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT id,ts,coalesce(zone_from,''),coalesce(zone_to,''),coalesce(rule_name,''),action,src_address,coalesce(src_port,0),dst_address,coalesce(dst_port,0),protocol,l3_proto,coalesce(in_iface,''),coalesce(out_iface,''),coalesce(packet_bytes,0),coalesce(hint,'')
FROM firewall_logs`+where+` ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FirewallLogEntry
	for rows.Next() {
		var entry FirewallLogEntry
		var ts int64
		if err := rows.Scan(&entry.ID, &ts, &entry.ZoneFrom, &entry.ZoneTo, &entry.RuleName, &entry.Action, &entry.SrcAddress, &entry.SrcPort, &entry.DstAddress, &entry.DstPort, &entry.Protocol, &entry.L3Proto, &entry.InIface, &entry.OutIface, &entry.PacketBytes, &entry.Hint); err != nil {
			return nil, err
		}
		entry.Timestamp = time.Unix(0, ts).UTC()
		out = append(out, entry)
	}
	return out, rows.Err()
}
