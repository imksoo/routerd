// SPDX-License-Identifier: BSD-3-Clause

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
	FlowKey              string            `json:"flowKey"`
	StartedAt            time.Time         `json:"tsStarted"`
	EndedAt              time.Time         `json:"tsEnded,omitempty"`
	ClientAddress        string            `json:"clientAddress,omitempty"`
	ClientPort           int               `json:"clientPort,omitempty"`
	PeerAddress          string            `json:"peerAddress,omitempty"`
	PeerPort             int               `json:"peerPort,omitempty"`
	Protocol             string            `json:"protocol"`
	NATTranslatedAddress string            `json:"natTranslatedAddress,omitempty"`
	Accounting           bool              `json:"accounting,omitempty"`
	BytesOut             int64             `json:"bytesOut,omitempty"`
	BytesIn              int64             `json:"bytesIn,omitempty"`
	PacketsOut           int64             `json:"packetsOut,omitempty"`
	PacketsIn            int64             `json:"packetsIn,omitempty"`
	AppName              string            `json:"appName,omitempty"`
	AppCategory          string            `json:"appCategory,omitempty"`
	AppConfidence        int               `json:"appConfidence,omitempty"`
	DetectedProtocol     string            `json:"detectedProtocol,omitempty"`
	MasterProtocol       string            `json:"masterProtocol,omitempty"`
	ApplicationProtocol  string            `json:"applicationProtocol,omitempty"`
	Category             string            `json:"category,omitempty"`
	Risk                 []string          `json:"risk,omitempty"`
	Confidence           int               `json:"confidence,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
	Engine               string            `json:"engine,omitempty"`
	Source               string            `json:"source,omitempty"`
	TLSSNI               string            `json:"tlsSNI,omitempty"`
	HTTPHost             string            `json:"httpHost,omitempty"`
	DNSQuery             string            `json:"dnsQuery,omitempty"`
	ResolvedHostname     string            `json:"resolvedHostname,omitempty"`
}

// TrafficFlowFilterLimitMax is the maximum number of rows returned by a single List call.
// Issue #36: raised from 1000 to 10000.
const TrafficFlowFilterLimitMax = 10000

const (
	trafficFlowWALAutoCheckpointPages = 1000
	trafficFlowJournalSizeLimitBytes  = 32 * 1024 * 1024
)

// TrafficFlowFilter selects rows from the traffic flow log.
//
// Issue #36: added Until / PeerSuffix / Protocol / Asymmetric.
type TrafficFlowFilter struct {
	Since      time.Time
	Until      time.Time
	Client     string
	Peer       string
	PeerSuffix string
	Protocol   string
	Asymmetric bool
	Limit      int
}

// TrafficFlowAggregate is the summary returned by Aggregate.
type TrafficFlowAggregate struct {
	Total         int            `json:"total"`
	Since         time.Time      `json:"since"`
	Until         time.Time      `json:"until"`
	TotalBytesIn  int64          `json:"totalBytesIn"`
	TotalBytesOut int64          `json:"totalBytesOut"`
	ByClient      map[string]int `json:"byClient"`
	ByPeer        map[string]int `json:"byPeer"`
	ByProtocol    map[string]int `json:"byProtocol"`
}

type TrafficFlowLog struct {
	db *sql.DB
}

type trafficFlowExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
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
	if _, err := db.Exec(`
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA wal_autocheckpoint = 1000;
PRAGMA journal_size_limit = 33554432;
`); err != nil {
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
  accounting INTEGER,
  bytes_out INTEGER,
  bytes_in INTEGER,
  packets_out INTEGER,
  packets_in INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  detected_protocol TEXT,
  master_protocol TEXT,
  application_protocol TEXT,
  category TEXT,
  risk TEXT,
  confidence INTEGER,
  metadata_json TEXT,
  engine TEXT,
  source TEXT,
  tls_sni TEXT,
  http_host TEXT,
  dns_query TEXT,
  resolved_hostname TEXT
);
CREATE INDEX IF NOT EXISTS flows_client_ts ON flows(client_address, ts_started);
CREATE INDEX IF NOT EXISTS flows_peer_ts ON flows(peer_address, ts_started);
`)
	if err != nil {
		return err
	}
	_, err = l.db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN accounting INTEGER`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = l.db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN http_host TEXT`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = l.db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN dns_query TEXT`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = l.db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN engine TEXT`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = l.db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN source TEXT`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	for _, column := range []struct {
		name string
		typ  string
	}{
		{"detected_protocol", "TEXT"},
		{"master_protocol", "TEXT"},
		{"application_protocol", "TEXT"},
		{"category", "TEXT"},
		{"risk", "TEXT"},
		{"confidence", "INTEGER"},
		{"metadata_json", "TEXT"},
	} {
		_, err = l.db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN `+column.name+` `+column.typ)
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
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
	return upsertActiveTrafficFlow(ctx, l.db, flow)
}

func (l *TrafficFlowLog) SyncActive(ctx context.Context, flows []TrafficFlow, activeKeys []string, endedAt time.Time) error {
	if l == nil || l.db == nil {
		return nil
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, flow := range flows {
		if err := upsertActiveTrafficFlow(ctx, tx, flow); err != nil {
			return err
		}
	}
	if err := endMissingTrafficFlows(ctx, tx, activeKeys, endedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func upsertActiveTrafficFlow(ctx context.Context, exec trafficFlowExec, flow TrafficFlow) error {
	if flow.StartedAt.IsZero() {
		flow.StartedAt = time.Now().UTC()
	}
	if flow.FlowKey == "" {
		flow.FlowKey = FlowKey(flow.Protocol, flow.ClientAddress, flow.ClientPort, flow.PeerAddress, flow.PeerPort)
	}
	flow.Engine = strings.ToLower(strings.TrimSpace(flow.Engine))
	flow.Source = strings.ToLower(strings.TrimSpace(flow.Source))
	flow.DetectedProtocol = strings.ToLower(strings.TrimSpace(flow.DetectedProtocol))
	flow.MasterProtocol = strings.ToLower(strings.TrimSpace(flow.MasterProtocol))
	flow.ApplicationProtocol = strings.ToLower(strings.TrimSpace(flow.ApplicationProtocol))
	flow.Category = strings.ToLower(strings.TrimSpace(flow.Category))
	riskJSON := jsonString(flow.Risk)
	metadataJSON := jsonString(flow.Metadata)
	_, err := exec.ExecContext(ctx, `INSERT INTO flows(flow_key,ts_started,client_address,client_port,peer_address,peer_port,protocol,nat_translated_address,accounting,bytes_out,bytes_in,packets_out,packets_in,app_name,app_category,app_confidence,detected_protocol,master_protocol,application_protocol,category,risk,confidence,metadata_json,engine,source,tls_sni,http_host,dns_query,resolved_hostname)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(flow_key) DO UPDATE SET
  ts_ended = NULL,
  client_address = excluded.client_address,
  client_port = excluded.client_port,
  peer_address = excluded.peer_address,
  peer_port = excluded.peer_port,
  protocol = excluded.protocol,
  nat_translated_address = excluded.nat_translated_address,
  accounting = excluded.accounting,
  bytes_out = excluded.bytes_out,
  bytes_in = excluded.bytes_in,
  packets_out = excluded.packets_out,
  packets_in = excluded.packets_in,
  app_name = CASE WHEN excluded.app_name != '' THEN excluded.app_name ELSE app_name END,
  app_category = CASE WHEN excluded.app_category != '' THEN excluded.app_category ELSE app_category END,
  app_confidence = CASE WHEN excluded.app_confidence != 0 THEN excluded.app_confidence ELSE app_confidence END,
  detected_protocol = CASE WHEN excluded.detected_protocol != '' THEN excluded.detected_protocol ELSE detected_protocol END,
  master_protocol = CASE WHEN excluded.master_protocol != '' THEN excluded.master_protocol ELSE master_protocol END,
  application_protocol = CASE WHEN excluded.application_protocol != '' THEN excluded.application_protocol ELSE application_protocol END,
  category = CASE WHEN excluded.category != '' THEN excluded.category ELSE category END,
  risk = CASE WHEN excluded.risk != '' THEN excluded.risk ELSE risk END,
  confidence = CASE WHEN excluded.confidence != 0 THEN excluded.confidence ELSE confidence END,
  metadata_json = CASE WHEN excluded.metadata_json != '' THEN excluded.metadata_json ELSE metadata_json END,
  engine = CASE WHEN excluded.engine != '' THEN excluded.engine ELSE engine END,
  source = CASE WHEN excluded.source != '' THEN excluded.source ELSE source END,
  tls_sni = CASE WHEN excluded.tls_sni != '' THEN excluded.tls_sni ELSE tls_sni END,
  http_host = CASE WHEN excluded.http_host != '' THEN excluded.http_host ELSE http_host END,
  dns_query = CASE WHEN excluded.dns_query != '' THEN excluded.dns_query ELSE dns_query END,
  resolved_hostname = CASE WHEN excluded.resolved_hostname != '' THEN excluded.resolved_hostname ELSE resolved_hostname END`,
		flow.FlowKey,
		flow.StartedAt.UnixNano(),
		flow.ClientAddress,
		flow.ClientPort,
		flow.PeerAddress,
		flow.PeerPort,
		flow.Protocol,
		flow.NATTranslatedAddress,
		flow.Accounting,
		flow.BytesOut,
		flow.BytesIn,
		flow.PacketsOut,
		flow.PacketsIn,
		flow.AppName,
		flow.AppCategory,
		flow.AppConfidence,
		flow.DetectedProtocol,
		flow.MasterProtocol,
		flow.ApplicationProtocol,
		flow.Category,
		riskJSON,
		flow.Confidence,
		metadataJSON,
		flow.Engine,
		flow.Source,
		flow.TLSSNI,
		flow.HTTPHost,
		flow.DNSQuery,
		flow.ResolvedHostname,
	)
	return err
}

func (l *TrafficFlowLog) EndMissing(ctx context.Context, activeKeys []string, endedAt time.Time) error {
	if l == nil || l.db == nil {
		return nil
	}
	return endMissingTrafficFlows(ctx, l.db, activeKeys, endedAt)
}

func endMissingTrafficFlows(ctx context.Context, exec trafficFlowExec, activeKeys []string, endedAt time.Time) error {
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	if len(activeKeys) == 0 {
		_, err := exec.ExecContext(ctx, `UPDATE flows SET ts_ended = ? WHERE ts_ended IS NULL`, endedAt.UnixNano())
		return err
	}
	quoted := make([]string, len(activeKeys))
	args := []any{endedAt.UnixNano()}
	for i, key := range activeKeys {
		quoted[i] = "?"
		args = append(args, key)
	}
	_, err := exec.ExecContext(ctx, `UPDATE flows SET ts_ended = ? WHERE ts_ended IS NULL AND flow_key NOT IN (`+strings.Join(quoted, ",")+`)`, args...)
	return err
}

func (l *TrafficFlowLog) buildTrafficQuery(filter TrafficFlowFilter) (string, []any) {
	var clauses []string
	var args []any
	if !filter.Since.IsZero() {
		clauses = append(clauses, "ts_started >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	if !filter.Until.IsZero() {
		clauses = append(clauses, "ts_started <= ?")
		args = append(args, filter.Until.UnixNano())
	}
	if strings.TrimSpace(filter.Client) != "" {
		clauses = append(clauses, "client_address = ?")
		args = append(args, filter.Client)
	}
	if strings.TrimSpace(filter.Peer) != "" {
		clauses = append(clauses, "peer_address = ?")
		args = append(args, filter.Peer)
	}
	if suffix := strings.TrimSpace(filter.PeerSuffix); suffix != "" {
		clauses = append(clauses, "(peer_address LIKE ? OR resolved_hostname LIKE ?)")
		args = append(args, "%"+suffix, "%"+suffix)
	}
	if proto := strings.TrimSpace(filter.Protocol); proto != "" {
		clauses = append(clauses, "protocol = ?")
		args = append(args, proto)
	}
	if filter.Asymmetric {
		clauses = append(clauses, "(coalesce(bytes_in,0) = 0 OR coalesce(bytes_out,0) = 0)")
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

func (l *TrafficFlowLog) List(ctx context.Context, filter TrafficFlowFilter) ([]TrafficFlow, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	columns, err := tableColumns(ctx, l.db, "flows")
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > TrafficFlowFilterLimitMax {
		limit = TrafficFlowFilterLimitMax
	}
	where, args := l.buildTrafficQuery(filter)
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT flow_key,ts_started,coalesce(ts_ended,0),coalesce(client_address,''),coalesce(client_port,0),coalesce(peer_address,''),coalesce(peer_port,0),protocol,coalesce(nat_translated_address,''),`+optionalIntColumn(columns, "accounting")+`,coalesce(bytes_out,0),coalesce(bytes_in,0),coalesce(packets_out,0),coalesce(packets_in,0),coalesce(app_name,''),coalesce(app_category,''),coalesce(app_confidence,0),`+optionalTextColumn(columns, "detected_protocol")+`,`+optionalTextColumn(columns, "master_protocol")+`,`+optionalTextColumn(columns, "application_protocol")+`,`+optionalTextColumn(columns, "category")+`,`+optionalTextColumn(columns, "risk")+`,`+optionalIntColumn(columns, "confidence")+`,`+optionalTextColumn(columns, "metadata_json")+`,`+optionalTextColumn(columns, "engine")+`,`+optionalTextColumn(columns, "source")+`,coalesce(tls_sni,''),`+optionalTextColumn(columns, "http_host")+`,`+optionalTextColumn(columns, "dns_query")+`,coalesce(resolved_hostname,'')
FROM flows`+where+` ORDER BY ts_started DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficFlow
	for rows.Next() {
		var flow TrafficFlow
		var started, ended int64
		var riskJSON, metadataJSON string
		if err := rows.Scan(&flow.FlowKey, &started, &ended, &flow.ClientAddress, &flow.ClientPort, &flow.PeerAddress, &flow.PeerPort, &flow.Protocol, &flow.NATTranslatedAddress, &flow.Accounting, &flow.BytesOut, &flow.BytesIn, &flow.PacketsOut, &flow.PacketsIn, &flow.AppName, &flow.AppCategory, &flow.AppConfidence, &flow.DetectedProtocol, &flow.MasterProtocol, &flow.ApplicationProtocol, &flow.Category, &riskJSON, &flow.Confidence, &metadataJSON, &flow.Engine, &flow.Source, &flow.TLSSNI, &flow.HTTPHost, &flow.DNSQuery, &flow.ResolvedHostname); err != nil {
			return nil, err
		}
		flow.Risk = jsonStringSlice(riskJSON)
		flow.Metadata = jsonStringMap(metadataJSON)
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

// Aggregate returns summary statistics for flows matching filter.
func (l *TrafficFlowLog) Aggregate(ctx context.Context, filter TrafficFlowFilter) (TrafficFlowAggregate, error) {
	if l == nil || l.db == nil {
		return TrafficFlowAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	result := TrafficFlowAggregate{
		Since:      filter.Since,
		Until:      filter.Until,
		ByClient:   map[string]int{},
		ByPeer:     map[string]int{},
		ByProtocol: map[string]int{},
	}
	where, args := l.buildTrafficQuery(filter)
	var total int
	var sumIn, sumOut sql.NullInt64
	row := l.db.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(bytes_in),0), coalesce(sum(bytes_out),0) FROM flows`+where, args...)
	if err := row.Scan(&total, &sumIn, &sumOut); err != nil {
		return result, err
	}
	result.Total = total
	if sumIn.Valid {
		result.TotalBytesIn = sumIn.Int64
	}
	if sumOut.Valid {
		result.TotalBytesOut = sumOut.Int64
	}
	if err := aggregateGroupBy(ctx, l.db, `SELECT coalesce(client_address,''), count(*) FROM flows`+where+` GROUP BY client_address`, args, result.ByClient); err != nil {
		return result, err
	}
	if err := aggregateGroupBy(ctx, l.db, `SELECT coalesce(peer_address,''), count(*) FROM flows`+where+` GROUP BY peer_address`, args, result.ByPeer); err != nil {
		return result, err
	}
	if err := aggregateGroupBy(ctx, l.db, `SELECT protocol, count(*) FROM flows`+where+` GROUP BY protocol`, args, result.ByProtocol); err != nil {
		return result, err
	}
	return result, nil
}
