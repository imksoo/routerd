// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DHCPFingerprint struct {
	MAC              string    `json:"mac"`
	Hostname         string    `json:"hostname,omitempty"`
	VendorClass      string    `json:"vendorClass,omitempty"`
	RequestedOptions []int     `json:"requestedOptions,omitempty"`
	OSFamily         string    `json:"osFamily,omitempty"`
	DeviceClass      string    `json:"deviceClass,omitempty"`
	DeviceName       string    `json:"deviceName,omitempty"`
	Confidence       int       `json:"confidence,omitempty"`
	Signal           string    `json:"signal,omitempty"`
	ObservedAt       time.Time `json:"observedAt,omitempty"`
	Source           string    `json:"source,omitempty"`
}

type DHCPFingerprintFilter struct {
	Since time.Time
	MAC   string
	Limit int
}

type DHCPFingerprintLog struct {
	db *sql.DB
}

func OpenDHCPFingerprintLog(path string) (*DHCPFingerprintLog, error) {
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
	log := &DHCPFingerprintLog{db: db}
	if err := log.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return log, nil
}

func OpenDHCPFingerprintLogReadOnly(path string) (*DHCPFingerprintLog, error) {
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil, err
	}
	return &DHCPFingerprintLog{db: db}, nil
}

func (l *DHCPFingerprintLog) Init(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS dhcp_fingerprint (
  mac TEXT PRIMARY KEY,
  hostname TEXT,
  vendor_class TEXT,
  requested_options TEXT,
  os_family TEXT,
  device_class TEXT,
  device_name TEXT,
  confidence INTEGER NOT NULL DEFAULT 0,
  signal TEXT,
  observed_at INTEGER NOT NULL,
  source TEXT
);
CREATE INDEX IF NOT EXISTS dhcp_fingerprint_observed_at ON dhcp_fingerprint(observed_at);
`)
	return err
}

func (l *DHCPFingerprintLog) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *DHCPFingerprintLog) Upsert(ctx context.Context, fp DHCPFingerprint) error {
	if l == nil || l.db == nil {
		return nil
	}
	fp.MAC = strings.ToLower(strings.TrimSpace(fp.MAC))
	if fp.MAC == "" {
		return nil
	}
	if fp.ObservedAt.IsZero() {
		fp.ObservedAt = time.Now().UTC()
	}
	_, err := l.db.ExecContext(ctx, `INSERT INTO dhcp_fingerprint(mac,hostname,vendor_class,requested_options,os_family,device_class,device_name,confidence,signal,observed_at,source)
VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(mac) DO UPDATE SET
  hostname=excluded.hostname,
  vendor_class=excluded.vendor_class,
  requested_options=excluded.requested_options,
  os_family=excluded.os_family,
  device_class=excluded.device_class,
  device_name=excluded.device_name,
  confidence=excluded.confidence,
  signal=excluded.signal,
  observed_at=excluded.observed_at,
  source=excluded.source`,
		fp.MAC,
		fp.Hostname,
		fp.VendorClass,
		joinInts(fp.RequestedOptions),
		fp.OSFamily,
		fp.DeviceClass,
		fp.DeviceName,
		fp.Confidence,
		fp.Signal,
		fp.ObservedAt.UnixNano(),
		fp.Source,
	)
	return err
}

func (l *DHCPFingerprintLog) List(ctx context.Context, filter DHCPFingerprintFilter) ([]DHCPFingerprint, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}
	var clauses []string
	var args []any
	if !filter.Since.IsZero() {
		clauses = append(clauses, "observed_at >= ?")
		args = append(args, filter.Since.UnixNano())
	}
	if strings.TrimSpace(filter.MAC) != "" {
		clauses = append(clauses, "mac = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(filter.MAC)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT mac,coalesce(hostname,''),coalesce(vendor_class,''),coalesce(requested_options,''),coalesce(os_family,''),coalesce(device_class,''),coalesce(device_name,''),coalesce(confidence,0),coalesce(signal,''),observed_at,coalesce(source,'')
FROM dhcp_fingerprint`+where+` ORDER BY observed_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DHCPFingerprint
	for rows.Next() {
		var fp DHCPFingerprint
		var options string
		var observed int64
		if err := rows.Scan(&fp.MAC, &fp.Hostname, &fp.VendorClass, &options, &fp.OSFamily, &fp.DeviceClass, &fp.DeviceName, &fp.Confidence, &fp.Signal, &observed, &fp.Source); err != nil {
			return nil, err
		}
		fp.RequestedOptions = splitInts(options)
		fp.ObservedAt = time.Unix(0, observed).UTC()
		out = append(out, fp)
	}
	return out, rows.Err()
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}

func splitInts(value string) []int {
	var out []int
	for _, token := range strings.Split(value, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		n, err := strconv.Atoi(token)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}
