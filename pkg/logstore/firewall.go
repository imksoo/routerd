// SPDX-License-Identifier: BSD-3-Clause

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
	ID                int64     `json:"id,omitempty"`
	Timestamp         time.Time `json:"ts"`
	ZoneFrom          string    `json:"zoneFrom,omitempty"`
	ZoneTo            string    `json:"zoneTo,omitempty"`
	RuleName          string    `json:"ruleName,omitempty"`
	Action            string    `json:"action"`
	SrcAddress        string    `json:"srcAddress"`
	SrcPort           int       `json:"srcPort,omitempty"`
	DstAddress        string    `json:"dstAddress"`
	DstPort           int       `json:"dstPort,omitempty"`
	Protocol          string    `json:"protocol"`
	TCPFlags          string    `json:"tcpFlags,omitempty"`
	L3Proto           string    `json:"l3Proto"`
	InIface           string    `json:"inIface,omitempty"`
	OutIface          string    `json:"outIface,omitempty"`
	PacketBytes       int       `json:"packetBytes,omitempty"`
	Hint              string    `json:"hint,omitempty"`
	DPIApp            string    `json:"dpiApp,omitempty"`
	DPICategory       string    `json:"dpiCategory,omitempty"`
	DPITLSSNI         string    `json:"dpiTlsSNI,omitempty"`
	DPIHTTPHost       string    `json:"dpiHttpHost,omitempty"`
	DPIDNSQuery       string    `json:"dpiDnsQuery,omitempty"`
	DPIConfidence     int       `json:"dpiConfidence,omitempty"`
	Correlation       string    `json:"correlation,omitempty"`
	CorrelationDetail string    `json:"correlationDetail,omitempty"`
	ExpiredAgeSeconds int       `json:"expiredAgeSeconds,omitempty"`
	ExpiredBytes      int64     `json:"expiredBytes,omitempty"`
}

type ExpiredFlowEntry struct {
	ID           int64     `json:"id,omitempty"`
	Timestamp    time.Time `json:"ts"`
	L3Proto      string    `json:"l3Proto"`
	Protocol     string    `json:"protocol"`
	OrigSrc      string    `json:"origSrc"`
	OrigSrcPort  int       `json:"origSrcPort,omitempty"`
	OrigDst      string    `json:"origDst"`
	OrigDstPort  int       `json:"origDstPort,omitempty"`
	ReplySrc     string    `json:"replySrc"`
	ReplySrcPort int       `json:"replySrcPort,omitempty"`
	ReplyDst     string    `json:"replyDst"`
	ReplyDstPort int       `json:"replyDstPort,omitempty"`
	Packets      int64     `json:"packets,omitempty"`
	Bytes        int64     `json:"bytes,omitempty"`
	Raw          string    `json:"raw,omitempty"`
}

type DPIFlowEntry struct {
	FlowID        string    `json:"flowID,omitempty"`
	FirstSeen     time.Time `json:"firstSeen,omitempty"`
	LastSeen      time.Time `json:"lastSeen,omitempty"`
	L3Proto       string    `json:"l3Proto,omitempty"`
	Protocol      string    `json:"protocol"`
	SrcAddress    string    `json:"srcAddress"`
	SrcPort       int       `json:"srcPort,omitempty"`
	DstAddress    string    `json:"dstAddress"`
	DstPort       int       `json:"dstPort,omitempty"`
	AppName       string    `json:"appName,omitempty"`
	AppCategory   string    `json:"appCategory,omitempty"`
	AppConfidence int       `json:"appConfidence,omitempty"`
	TLSSNI        string    `json:"tlsSNI,omitempty"`
	HTTPHost      string    `json:"httpHost,omitempty"`
	DNSQuery      string    `json:"dnsQuery,omitempty"`
	ClassifiedAt  time.Time `json:"classifiedAt,omitempty"`
	PacketCount   int       `json:"packetCount,omitempty"`
}

type FirewallLogFilter struct {
	Since  time.Time
	Action string
	Src    string
	Limit  int
}

type DPIFlowFilter struct {
	Since time.Time
	Limit int
}

type ExpiredFlowFilter struct {
	Since time.Time
	Limit int
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

func OpenFirewallLogReadOnly(path string) (*FirewallLog, error) {
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil, err
	}
	return &FirewallLog{db: db}, nil
}

func (l *FirewallLog) Init(ctx context.Context) error {
	if _, err := l.db.ExecContext(ctx, `
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
  tcp_flags TEXT,
  l3_proto TEXT NOT NULL,
  in_iface TEXT,
  out_iface TEXT,
  packet_bytes INTEGER,
  hint TEXT,
  dpi_app TEXT,
  dpi_category TEXT,
  dpi_tls_sni TEXT,
  dpi_http_host TEXT,
  dpi_dns_query TEXT,
  dpi_confidence INTEGER,
  correlation TEXT,
  correlation_detail TEXT,
  expired_age_seconds INTEGER,
  expired_bytes INTEGER
);
CREATE TABLE IF NOT EXISTS expired_flows (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  l3_proto TEXT,
  protocol TEXT NOT NULL,
  orig_src TEXT NOT NULL,
  orig_src_port INTEGER,
  orig_dst TEXT NOT NULL,
  orig_dst_port INTEGER,
  reply_src TEXT,
  reply_src_port INTEGER,
  reply_dst TEXT,
  reply_dst_port INTEGER,
  packets INTEGER,
  bytes INTEGER,
  raw TEXT
);
CREATE TABLE IF NOT EXISTS dpi_flow (
  flow_id TEXT PRIMARY KEY,
  ts_first INTEGER NOT NULL,
  ts_last INTEGER NOT NULL,
  l3_proto TEXT,
  protocol TEXT NOT NULL,
  src_address TEXT NOT NULL,
  src_port INTEGER,
  dst_address TEXT NOT NULL,
  dst_port INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  tls_sni TEXT,
  http_host TEXT,
  dns_query TEXT,
  classified_at INTEGER,
  packet_count INTEGER
);
CREATE INDEX IF NOT EXISTS firewall_logs_ts ON firewall_logs(ts);
CREATE INDEX IF NOT EXISTS firewall_logs_src_ts ON firewall_logs(src_address, ts);
CREATE INDEX IF NOT EXISTS firewall_logs_action_ts ON firewall_logs(action, ts);
CREATE INDEX IF NOT EXISTS firewall_logs_zone ON firewall_logs(zone_from, zone_to, ts);
CREATE INDEX IF NOT EXISTS expired_flows_ts ON expired_flows(ts);
CREATE INDEX IF NOT EXISTS expired_flows_reply ON expired_flows(protocol, reply_src, reply_dst, reply_src_port, reply_dst_port, ts);
CREATE INDEX IF NOT EXISTS dpi_flow_tuple ON dpi_flow(protocol, src_address, dst_address, src_port, dst_port, ts_last);
CREATE INDEX IF NOT EXISTS dpi_flow_reverse ON dpi_flow(protocol, dst_address, src_address, dst_port, src_port, ts_last);
`); err != nil {
		return err
	}
	return l.ensureDPIColumns(ctx)
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
	_, err := l.db.ExecContext(ctx, `INSERT INTO firewall_logs(ts,zone_from,zone_to,rule_name,action,src_address,src_port,dst_address,dst_port,protocol,tcp_flags,l3_proto,in_iface,out_iface,packet_bytes,hint,dpi_app,dpi_category,dpi_tls_sni,dpi_http_host,dpi_dns_query,dpi_confidence,correlation,correlation_detail,expired_age_seconds,expired_bytes)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.Timestamp.UnixNano(), entry.ZoneFrom, entry.ZoneTo, entry.RuleName, entry.Action, entry.SrcAddress, entry.SrcPort, entry.DstAddress, entry.DstPort, entry.Protocol, entry.TCPFlags, entry.L3Proto, entry.InIface, entry.OutIface, entry.PacketBytes, entry.Hint, entry.DPIApp, entry.DPICategory, entry.DPITLSSNI, entry.DPIHTTPHost, entry.DPIDNSQuery, entry.DPIConfidence, entry.Correlation, entry.CorrelationDetail, entry.ExpiredAgeSeconds, entry.ExpiredBytes)
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
	rows, err := l.db.QueryContext(ctx, `SELECT id,ts,coalesce(zone_from,''),coalesce(zone_to,''),coalesce(rule_name,''),action,src_address,coalesce(src_port,0),dst_address,coalesce(dst_port,0),protocol,coalesce(tcp_flags,''),l3_proto,coalesce(in_iface,''),coalesce(out_iface,''),coalesce(packet_bytes,0),coalesce(hint,''),coalesce(dpi_app,''),coalesce(dpi_category,''),coalesce(dpi_tls_sni,''),coalesce(dpi_http_host,''),coalesce(dpi_dns_query,''),coalesce(dpi_confidence,0),coalesce(correlation,''),coalesce(correlation_detail,''),coalesce(expired_age_seconds,0),coalesce(expired_bytes,0)
FROM firewall_logs`+where+` ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FirewallLogEntry
	for rows.Next() {
		var entry FirewallLogEntry
		var ts int64
		if err := rows.Scan(&entry.ID, &ts, &entry.ZoneFrom, &entry.ZoneTo, &entry.RuleName, &entry.Action, &entry.SrcAddress, &entry.SrcPort, &entry.DstAddress, &entry.DstPort, &entry.Protocol, &entry.TCPFlags, &entry.L3Proto, &entry.InIface, &entry.OutIface, &entry.PacketBytes, &entry.Hint, &entry.DPIApp, &entry.DPICategory, &entry.DPITLSSNI, &entry.DPIHTTPHost, &entry.DPIDNSQuery, &entry.DPIConfidence, &entry.Correlation, &entry.CorrelationDetail, &entry.ExpiredAgeSeconds, &entry.ExpiredBytes); err != nil {
			return nil, err
		}
		entry.Timestamp = time.Unix(0, ts).UTC()
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (l *FirewallLog) ensureDPIColumns(ctx context.Context) error {
	rows, err := l.db.QueryContext(ctx, `PRAGMA table_info(firewall_logs)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		typ  string
	}{
		{"dpi_app", "TEXT"},
		{"tcp_flags", "TEXT"},
		{"dpi_category", "TEXT"},
		{"dpi_tls_sni", "TEXT"},
		{"dpi_http_host", "TEXT"},
		{"dpi_dns_query", "TEXT"},
		{"dpi_confidence", "INTEGER"},
		{"correlation", "TEXT"},
		{"correlation_detail", "TEXT"},
		{"expired_age_seconds", "INTEGER"},
		{"expired_bytes", "INTEGER"},
	} {
		if existing[column.name] {
			continue
		}
		if _, err := l.db.ExecContext(ctx, `ALTER TABLE firewall_logs ADD COLUMN `+column.name+` `+column.typ); err != nil {
			return err
		}
	}
	_, err = l.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS firewall_logs_dpi_app_ts ON firewall_logs(dpi_app, ts);`)
	return err
}

func (l *FirewallLog) RecordExpiredFlow(ctx context.Context, flow ExpiredFlowEntry, ttl time.Duration, limit int) error {
	if l == nil || l.db == nil {
		return nil
	}
	if flow.Timestamp.IsZero() {
		flow.Timestamp = time.Now().UTC()
	}
	flow.Protocol = strings.ToLower(strings.TrimSpace(flow.Protocol))
	flow.L3Proto = strings.ToLower(strings.TrimSpace(flow.L3Proto))
	if flow.Protocol == "" || flow.OrigSrc == "" || flow.OrigDst == "" {
		return nil
	}
	if flow.ReplySrc == "" {
		flow.ReplySrc = flow.OrigDst
		flow.ReplySrcPort = flow.OrigDstPort
	}
	if flow.ReplyDst == "" {
		flow.ReplyDst = flow.OrigSrc
		flow.ReplyDstPort = flow.OrigSrcPort
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO expired_flows(ts,l3_proto,protocol,orig_src,orig_src_port,orig_dst,orig_dst_port,reply_src,reply_src_port,reply_dst,reply_dst_port,packets,bytes,raw)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		flow.Timestamp.UnixNano(), flow.L3Proto, flow.Protocol, flow.OrigSrc, flow.OrigSrcPort, flow.OrigDst, flow.OrigDstPort, flow.ReplySrc, flow.ReplySrcPort, flow.ReplyDst, flow.ReplyDstPort, flow.Packets, flow.Bytes, flow.Raw); err != nil {
		return err
	}
	return l.PruneExpiredFlows(ctx, time.Now().UTC(), ttl, limit)
}

func (l *FirewallLog) PruneExpiredFlows(ctx context.Context, now time.Time, ttl time.Duration, limit int) error {
	if l == nil || l.db == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if limit <= 0 {
		limit = 100000
	}
	if _, err := l.db.ExecContext(ctx, `DELETE FROM expired_flows WHERE ts < ?`, now.Add(-ttl).UnixNano()); err != nil {
		return err
	}
	_, err := l.db.ExecContext(ctx, `DELETE FROM expired_flows WHERE id IN (
SELECT id FROM expired_flows ORDER BY ts DESC, id DESC LIMIT -1 OFFSET ?
)`, limit)
	return err
}

func (l *FirewallLog) RecordDPIFlow(ctx context.Context, flow DPIFlowEntry, ttl time.Duration, limit int) error {
	if l == nil || l.db == nil {
		return nil
	}
	flow.Protocol = strings.ToLower(strings.TrimSpace(flow.Protocol))
	flow.L3Proto = strings.ToLower(strings.TrimSpace(flow.L3Proto))
	if flow.Protocol == "" || flow.SrcAddress == "" || flow.DstAddress == "" {
		return nil
	}
	if flow.AppName == "" || flow.AppName == "unknown" {
		return nil
	}
	now := time.Now().UTC()
	if flow.FirstSeen.IsZero() {
		flow.FirstSeen = now
	}
	if flow.LastSeen.IsZero() {
		flow.LastSeen = flow.FirstSeen
	}
	if flow.ClassifiedAt.IsZero() {
		flow.ClassifiedAt = flow.LastSeen
	}
	if flow.FlowID == "" {
		flow.FlowID = FlowKey(flow.Protocol, flow.SrcAddress, flow.SrcPort, flow.DstAddress, flow.DstPort)
	}
	if flow.PacketCount <= 0 {
		flow.PacketCount = 1
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO dpi_flow(flow_id,ts_first,ts_last,l3_proto,protocol,src_address,src_port,dst_address,dst_port,app_name,app_category,app_confidence,tls_sni,http_host,dns_query,classified_at,packet_count)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(flow_id) DO UPDATE SET
  ts_last = excluded.ts_last,
  l3_proto = excluded.l3_proto,
  app_name = excluded.app_name,
  app_category = excluded.app_category,
  app_confidence = excluded.app_confidence,
  tls_sni = excluded.tls_sni,
  http_host = excluded.http_host,
  dns_query = excluded.dns_query,
  classified_at = excluded.classified_at,
  packet_count = coalesce(dpi_flow.packet_count, 0) + excluded.packet_count`,
		flow.FlowID, flow.FirstSeen.UnixNano(), flow.LastSeen.UnixNano(), flow.L3Proto, flow.Protocol, flow.SrcAddress, flow.SrcPort, flow.DstAddress, flow.DstPort, flow.AppName, flow.AppCategory, flow.AppConfidence, flow.TLSSNI, flow.HTTPHost, flow.DNSQuery, flow.ClassifiedAt.UnixNano(), flow.PacketCount); err != nil {
		return err
	}
	return l.PruneDPIFlows(ctx, now, ttl, limit)
}

func (l *FirewallLog) PruneDPIFlows(ctx context.Context, now time.Time, ttl time.Duration, limit int) error {
	if l == nil || l.db == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if limit <= 0 {
		limit = 100000
	}
	if _, err := l.db.ExecContext(ctx, `DELETE FROM dpi_flow WHERE ts_last < ?`, now.Add(-ttl).UnixNano()); err != nil {
		return err
	}
	_, err := l.db.ExecContext(ctx, `DELETE FROM dpi_flow WHERE flow_id IN (
SELECT flow_id FROM dpi_flow ORDER BY ts_last DESC LIMIT -1 OFFSET ?
)`, limit)
	return err
}

func (l *FirewallLog) ListDPIFlows(ctx context.Context, filter DPIFlowFilter) ([]DPIFlowEntry, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}
	var clauses []string
	var args []any
	if !filter.Since.IsZero() {
		clauses = append(clauses, "ts_last >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT flow_id,ts_first,ts_last,coalesce(l3_proto,''),protocol,src_address,coalesce(src_port,0),dst_address,coalesce(dst_port,0),coalesce(app_name,''),coalesce(app_category,''),coalesce(app_confidence,0),coalesce(tls_sni,''),coalesce(http_host,''),coalesce(dns_query,''),coalesce(classified_at,0),coalesce(packet_count,0)
FROM dpi_flow`+where+` ORDER BY ts_last DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DPIFlowEntry
	for rows.Next() {
		var flow DPIFlowEntry
		var first, last, classified int64
		if err := rows.Scan(&flow.FlowID, &first, &last, &flow.L3Proto, &flow.Protocol, &flow.SrcAddress, &flow.SrcPort, &flow.DstAddress, &flow.DstPort, &flow.AppName, &flow.AppCategory, &flow.AppConfidence, &flow.TLSSNI, &flow.HTTPHost, &flow.DNSQuery, &classified, &flow.PacketCount); err != nil {
			return nil, err
		}
		flow.FirstSeen = time.Unix(0, first).UTC()
		flow.LastSeen = time.Unix(0, last).UTC()
		if classified > 0 {
			flow.ClassifiedAt = time.Unix(0, classified).UTC()
		}
		out = append(out, flow)
	}
	return out, rows.Err()
}

func (l *FirewallLog) ListExpiredFlows(ctx context.Context, filter ExpiredFlowFilter) ([]ExpiredFlowEntry, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}
	var clauses []string
	var args []any
	if !filter.Since.IsZero() {
		clauses = append(clauses, "ts >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT id,ts,coalesce(l3_proto,''),protocol,orig_src,coalesce(orig_src_port,0),orig_dst,coalesce(orig_dst_port,0),coalesce(reply_src,''),coalesce(reply_src_port,0),coalesce(reply_dst,''),coalesce(reply_dst_port,0),coalesce(packets,0),coalesce(bytes,0),coalesce(raw,'')
FROM expired_flows`+where+` ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpiredFlowEntry
	for rows.Next() {
		var flow ExpiredFlowEntry
		var ts int64
		if err := rows.Scan(&flow.ID, &ts, &flow.L3Proto, &flow.Protocol, &flow.OrigSrc, &flow.OrigSrcPort, &flow.OrigDst, &flow.OrigDstPort, &flow.ReplySrc, &flow.ReplySrcPort, &flow.ReplyDst, &flow.ReplyDstPort, &flow.Packets, &flow.Bytes, &flow.Raw); err != nil {
			return nil, err
		}
		flow.Timestamp = time.Unix(0, ts).UTC()
		out = append(out, flow)
	}
	return out, rows.Err()
}

func (l *FirewallLog) FindDPIFlowForFirewallEntry(ctx context.Context, entry FirewallLogEntry, now time.Time, ttl time.Duration) (DPIFlowEntry, bool, error) {
	if l == nil || l.db == nil {
		return DPIFlowEntry{}, false, nil
	}
	return l.findDPIFlow(ctx, dpiFlowLookup{
		Protocol:   entry.Protocol,
		SrcAddress: entry.SrcAddress,
		SrcPort:    entry.SrcPort,
		DstAddress: entry.DstAddress,
		DstPort:    entry.DstPort,
		Now:        now,
		TTL:        ttl,
	})
}

func (l *FirewallLog) FindDPIFlowForExpiredFlow(ctx context.Context, flow ExpiredFlowEntry, now time.Time, ttl time.Duration) (DPIFlowEntry, bool, error) {
	if l == nil || l.db == nil {
		return DPIFlowEntry{}, false, nil
	}
	return l.findDPIFlow(ctx, dpiFlowLookup{
		Protocol:   flow.Protocol,
		SrcAddress: flow.OrigSrc,
		SrcPort:    flow.OrigSrcPort,
		DstAddress: flow.OrigDst,
		DstPort:    flow.OrigDstPort,
		Now:        now,
		TTL:        ttl,
	})
}

type dpiFlowLookup struct {
	Protocol   string
	SrcAddress string
	SrcPort    int
	DstAddress string
	DstPort    int
	Now        time.Time
	TTL        time.Duration
}

func (l *FirewallLog) findDPIFlow(ctx context.Context, lookup dpiFlowLookup) (DPIFlowEntry, bool, error) {
	protocol := strings.ToLower(strings.TrimSpace(lookup.Protocol))
	if protocol == "" || lookup.SrcAddress == "" || lookup.DstAddress == "" {
		return DPIFlowEntry{}, false, nil
	}
	now := lookup.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := lookup.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	rows, err := l.db.QueryContext(ctx, `SELECT flow_id,ts_first,ts_last,coalesce(l3_proto,''),protocol,src_address,coalesce(src_port,0),dst_address,coalesce(dst_port,0),coalesce(app_name,''),coalesce(app_category,''),coalesce(app_confidence,0),coalesce(tls_sni,''),coalesce(http_host,''),coalesce(dns_query,''),coalesce(classified_at,0),coalesce(packet_count,0)
FROM dpi_flow
WHERE ts_last >= ? AND protocol = ? AND (
  (src_address = ? AND dst_address = ? AND coalesce(src_port,0) = ? AND coalesce(dst_port,0) = ?)
  OR
  (dst_address = ? AND src_address = ? AND coalesce(dst_port,0) = ? AND coalesce(src_port,0) = ?)
)
ORDER BY ts_last DESC LIMIT 1`,
		now.Add(-ttl).UnixNano(), protocol,
		lookup.SrcAddress, lookup.DstAddress, lookup.SrcPort, lookup.DstPort,
		lookup.SrcAddress, lookup.DstAddress, lookup.SrcPort, lookup.DstPort)
	if err != nil {
		return DPIFlowEntry{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return DPIFlowEntry{}, false, rows.Err()
	}
	var flow DPIFlowEntry
	var first, last, classified int64
	if err := rows.Scan(&flow.FlowID, &first, &last, &flow.L3Proto, &flow.Protocol, &flow.SrcAddress, &flow.SrcPort, &flow.DstAddress, &flow.DstPort, &flow.AppName, &flow.AppCategory, &flow.AppConfidence, &flow.TLSSNI, &flow.HTTPHost, &flow.DNSQuery, &classified, &flow.PacketCount); err != nil {
		return DPIFlowEntry{}, false, err
	}
	flow.FirstSeen = time.Unix(0, first).UTC()
	flow.LastSeen = time.Unix(0, last).UTC()
	if classified > 0 {
		flow.ClassifiedAt = time.Unix(0, classified).UTC()
	}
	return flow, true, nil
}

func (l *FirewallLog) FindExpiredReturn(ctx context.Context, entry FirewallLogEntry, now time.Time, ttl time.Duration) (ExpiredFlowEntry, bool, error) {
	if l == nil || l.db == nil || !isFirewallDeny(entry.Action) {
		return ExpiredFlowEntry{}, false, nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	since := now.Add(-ttl).UnixNano()
	rows, err := l.db.QueryContext(ctx, `SELECT id,ts,coalesce(l3_proto,''),protocol,orig_src,coalesce(orig_src_port,0),orig_dst,coalesce(orig_dst_port,0),coalesce(reply_src,''),coalesce(reply_src_port,0),coalesce(reply_dst,''),coalesce(reply_dst_port,0),coalesce(packets,0),coalesce(bytes,0),coalesce(raw,'')
FROM expired_flows
WHERE ts >= ? AND protocol = ? AND (
  (reply_src = ? AND reply_dst = ? AND coalesce(reply_src_port,0) = ? AND coalesce(reply_dst_port,0) = ?)
  OR
  (orig_dst = ? AND orig_src = ? AND coalesce(orig_dst_port,0) = ? AND coalesce(orig_src_port,0) = ?)
)
ORDER BY ts DESC, id DESC LIMIT 1`,
		since, strings.ToLower(strings.TrimSpace(entry.Protocol)),
		entry.SrcAddress, entry.DstAddress, entry.SrcPort, entry.DstPort,
		entry.SrcAddress, entry.DstAddress, entry.SrcPort, entry.DstPort)
	if err != nil {
		return ExpiredFlowEntry{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return ExpiredFlowEntry{}, false, rows.Err()
	}
	var flow ExpiredFlowEntry
	var ts int64
	if err := rows.Scan(&flow.ID, &ts, &flow.L3Proto, &flow.Protocol, &flow.OrigSrc, &flow.OrigSrcPort, &flow.OrigDst, &flow.OrigDstPort, &flow.ReplySrc, &flow.ReplySrcPort, &flow.ReplyDst, &flow.ReplyDstPort, &flow.Packets, &flow.Bytes, &flow.Raw); err != nil {
		return ExpiredFlowEntry{}, false, err
	}
	flow.Timestamp = time.Unix(0, ts).UTC()
	return flow, true, nil
}

func isFirewallDeny(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "drop", "deny", "reject", "block":
		return true
	default:
		return false
	}
}
