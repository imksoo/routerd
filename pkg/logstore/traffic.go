package logstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type TrafficFlow struct {
	FlowKey              string    `json:"flowKey"`
	StartedAt            time.Time `json:"tsStarted"`
	EndedAt              time.Time `json:"tsEnded,omitempty"`
	ClientAddress        string    `json:"clientAddress,omitempty"`
	ClientPort           int       `json:"clientPort,omitempty"`
	PeerAddress          string    `json:"peerAddress,omitempty"`
	PeerPort             int       `json:"peerPort,omitempty"`
	Protocol             string    `json:"protocol"`
	NATTranslatedAddress string    `json:"natTranslatedAddress,omitempty"`
	BytesOut             int64     `json:"bytesOut,omitempty"`
	BytesIn              int64     `json:"bytesIn,omitempty"`
	PacketsOut           int64     `json:"packetsOut,omitempty"`
	PacketsIn            int64     `json:"packetsIn,omitempty"`
	AppName              string    `json:"appName,omitempty"`
	AppCategory          string    `json:"appCategory,omitempty"`
	AppConfidence        int       `json:"appConfidence,omitempty"`
	TLSSNI               string    `json:"tlsSNI,omitempty"`
	ResolvedHostname     string    `json:"resolvedHostname,omitempty"`
}

type TrafficFlowFilter struct {
	Since  time.Time
	Client string
	Peer   string
	Limit  int
}

type TrafficFlowLog struct {
	db *sql.DB
}

func OpenTrafficFlowLog(path string) (*TrafficFlowLog, error) {
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
	log := &TrafficFlowLog{db: db}
	if err := log.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return log, nil
}

func OpenTrafficFlowLogReadOnly(path string) (*TrafficFlowLog, error) {
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil, err
	}
	return &TrafficFlowLog{db: db}, nil
}

func (l *TrafficFlowLog) Init(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS flows (
  id INTEGER PRIMARY KEY,
  flow_key TEXT UNIQUE NOT NULL,
  ts_started INTEGER NOT NULL,
  ts_ended INTEGER,
  client_address TEXT,
  client_port INTEGER,
  peer_address TEXT,
  peer_port INTEGER,
  protocol TEXT NOT NULL,
  nat_translated_address TEXT,
  bytes_out INTEGER,
  bytes_in INTEGER,
  packets_out INTEGER,
  packets_in INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  tls_sni TEXT,
  resolved_hostname TEXT
);
CREATE INDEX IF NOT EXISTS flows_client_ts ON flows(client_address, ts_started);
CREATE INDEX IF NOT EXISTS flows_peer_ts ON flows(peer_address, ts_started);
`)
	return err
}

func (l *TrafficFlowLog) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *TrafficFlowLog) UpsertActive(ctx context.Context, flow TrafficFlow) error {
	if l == nil || l.db == nil {
		return nil
	}
	if flow.StartedAt.IsZero() {
		flow.StartedAt = time.Now().UTC()
	}
	if flow.FlowKey == "" {
		flow.FlowKey = FlowKey(flow.Protocol, flow.ClientAddress, flow.ClientPort, flow.PeerAddress, flow.PeerPort)
	}
	_, err := l.db.ExecContext(ctx, `INSERT INTO flows(flow_key,ts_started,client_address,client_port,peer_address,peer_port,protocol,nat_translated_address,bytes_out,bytes_in,packets_out,packets_in,app_name,app_category,app_confidence,tls_sni,resolved_hostname)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(flow_key) DO UPDATE SET
  ts_ended = NULL,
  client_address = excluded.client_address,
  client_port = excluded.client_port,
  peer_address = excluded.peer_address,
  peer_port = excluded.peer_port,
  protocol = excluded.protocol,
  nat_translated_address = excluded.nat_translated_address,
  bytes_out = excluded.bytes_out,
  bytes_in = excluded.bytes_in,
  packets_out = excluded.packets_out,
  packets_in = excluded.packets_in,
  app_name = excluded.app_name,
  app_category = excluded.app_category,
  app_confidence = excluded.app_confidence,
  tls_sni = excluded.tls_sni,
  resolved_hostname = excluded.resolved_hostname`,
		flow.FlowKey,
		flow.StartedAt.UnixNano(),
		flow.ClientAddress,
		flow.ClientPort,
		flow.PeerAddress,
		flow.PeerPort,
		flow.Protocol,
		flow.NATTranslatedAddress,
		flow.BytesOut,
		flow.BytesIn,
		flow.PacketsOut,
		flow.PacketsIn,
		flow.AppName,
		flow.AppCategory,
		flow.AppConfidence,
		flow.TLSSNI,
		flow.ResolvedHostname,
	)
	return err
}

func (l *TrafficFlowLog) EndMissing(ctx context.Context, activeKeys []string, endedAt time.Time) error {
	if l == nil || l.db == nil {
		return nil
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	if len(activeKeys) == 0 {
		_, err := l.db.ExecContext(ctx, `UPDATE flows SET ts_ended = ? WHERE ts_ended IS NULL`, endedAt.UnixNano())
		return err
	}
	quoted := make([]string, len(activeKeys))
	args := []any{endedAt.UnixNano()}
	for i, key := range activeKeys {
		quoted[i] = "?"
		args = append(args, key)
	}
	_, err := l.db.ExecContext(ctx, `UPDATE flows SET ts_ended = ? WHERE ts_ended IS NULL AND flow_key NOT IN (`+strings.Join(quoted, ",")+`)`, args...)
	return err
}

func (l *TrafficFlowLog) List(ctx context.Context, filter TrafficFlowFilter) ([]TrafficFlow, error) {
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
		clauses = append(clauses, "ts_started >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	if strings.TrimSpace(filter.Client) != "" {
		clauses = append(clauses, "client_address = ?")
		args = append(args, filter.Client)
	}
	if strings.TrimSpace(filter.Peer) != "" {
		clauses = append(clauses, "peer_address = ?")
		args = append(args, filter.Peer)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT flow_key,ts_started,coalesce(ts_ended,0),coalesce(client_address,''),coalesce(client_port,0),coalesce(peer_address,''),coalesce(peer_port,0),protocol,coalesce(nat_translated_address,''),coalesce(bytes_out,0),coalesce(bytes_in,0),coalesce(packets_out,0),coalesce(packets_in,0),coalesce(app_name,''),coalesce(app_category,''),coalesce(app_confidence,0),coalesce(tls_sni,''),coalesce(resolved_hostname,'')
FROM flows`+where+` ORDER BY ts_started DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficFlow
	for rows.Next() {
		var flow TrafficFlow
		var started, ended int64
		if err := rows.Scan(&flow.FlowKey, &started, &ended, &flow.ClientAddress, &flow.ClientPort, &flow.PeerAddress, &flow.PeerPort, &flow.Protocol, &flow.NATTranslatedAddress, &flow.BytesOut, &flow.BytesIn, &flow.PacketsOut, &flow.PacketsIn, &flow.AppName, &flow.AppCategory, &flow.AppConfidence, &flow.TLSSNI, &flow.ResolvedHostname); err != nil {
			return nil, err
		}
		flow.StartedAt = time.Unix(0, started).UTC()
		if ended > 0 {
			flow.EndedAt = time.Unix(0, ended).UTC()
		}
		out = append(out, flow)
	}
	return out, rows.Err()
}

func FlowKey(protocol, clientAddress string, clientPort int, peerAddress string, peerPort int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{strings.ToLower(protocol), clientAddress, strconv.Itoa(clientPort), peerAddress, strconv.Itoa(peerPort)}, "|")))
	return hex.EncodeToString(sum[:16])
}
