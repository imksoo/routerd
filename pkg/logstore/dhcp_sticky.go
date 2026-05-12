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

type DHCPStickyLease struct {
	MAC           string    `json:"mac"`
	IP            string    `json:"ip"`
	Hostname      string    `json:"hostname,omitempty"`
	Family        string    `json:"family,omitempty"`
	AllocatedAt   time.Time `json:"allocatedAt,omitempty"`
	ReleasedAt    time.Time `json:"releasedAt,omitempty"`
	LastRequestAt time.Time `json:"lastRequestAt,omitempty"`
	StickyUntil   time.Time `json:"stickyUntil,omitempty"`
}

type DHCPStickyFilter struct {
	HeldOnly bool
	Now      time.Time
	MAC      string
	IP       string
	Limit    int
}

type DHCPStickyLog struct {
	db *sql.DB
}

func OpenDHCPStickyLog(path string) (*DHCPStickyLog, error) {
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
	log := &DHCPStickyLog{db: db}
	if err := log.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return log, nil
}

func OpenDHCPStickyLogReadOnly(path string) (*DHCPStickyLog, error) {
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil, err
	}
	return &DHCPStickyLog{db: db}, nil
}

func (l *DHCPStickyLog) Init(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS dhcp_sticky_leases (
  mac TEXT NOT NULL,
  ip TEXT NOT NULL,
  hostname TEXT,
  family TEXT,
  allocated_at INTEGER NOT NULL DEFAULT 0,
  released_at INTEGER NOT NULL DEFAULT 0,
  last_request_at INTEGER NOT NULL DEFAULT 0,
  sticky_until INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(mac, ip)
);
CREATE INDEX IF NOT EXISTS dhcp_sticky_until ON dhcp_sticky_leases(sticky_until);
CREATE INDEX IF NOT EXISTS dhcp_sticky_ip ON dhcp_sticky_leases(ip);
`)
	return err
}

func (l *DHCPStickyLog) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *DHCPStickyLog) RecordLeaseEvent(ctx context.Context, action, mac, ip, hostname string, holdDays int, now time.Time) error {
	if l == nil || l.db == nil {
		return nil
	}
	mac = strings.ToLower(strings.TrimSpace(mac))
	ip = strings.TrimSpace(ip)
	hostname = strings.TrimSpace(hostname)
	if mac == "" || ip == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	family := "ipv4"
	if strings.Contains(ip, ":") {
		family = "ipv6"
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "removed", "del", "release", "released", "expired":
		var stickyUntil int64
		if holdDays > 0 {
			stickyUntil = now.Add(time.Duration(holdDays) * 24 * time.Hour).UnixNano()
		}
		_, err := l.db.ExecContext(ctx, `INSERT INTO dhcp_sticky_leases(mac,ip,hostname,family,released_at,sticky_until)
VALUES(?,?,?,?,?,?)
ON CONFLICT(mac,ip) DO UPDATE SET
  hostname=coalesce(nullif(excluded.hostname,''),dhcp_sticky_leases.hostname),
  family=excluded.family,
  released_at=excluded.released_at,
  sticky_until=excluded.sticky_until`,
			mac, ip, hostname, family, now.UnixNano(), stickyUntil)
		return err
	default:
		_, err := l.db.ExecContext(ctx, `INSERT INTO dhcp_sticky_leases(mac,ip,hostname,family,allocated_at,last_request_at,sticky_until)
VALUES(?,?,?,?,?,?,0)
ON CONFLICT(mac,ip) DO UPDATE SET
  hostname=coalesce(nullif(excluded.hostname,''),dhcp_sticky_leases.hostname),
  family=excluded.family,
  allocated_at=CASE WHEN dhcp_sticky_leases.allocated_at = 0 THEN excluded.allocated_at ELSE dhcp_sticky_leases.allocated_at END,
  last_request_at=excluded.last_request_at,
  sticky_until=0`,
			mac, ip, hostname, family, now.UnixNano(), now.UnixNano())
		return err
	}
}

func (l *DHCPStickyLog) List(ctx context.Context, filter DHCPStickyFilter) ([]DHCPStickyLease, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 10000 {
		limit = 10000
	}
	now := filter.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var clauses []string
	var args []any
	if filter.HeldOnly {
		clauses = append(clauses, "sticky_until > ?")
		args = append(args, now.UnixNano())
	}
	if strings.TrimSpace(filter.MAC) != "" {
		clauses = append(clauses, "mac = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(filter.MAC)))
	}
	if strings.TrimSpace(filter.IP) != "" {
		clauses = append(clauses, "ip = ?")
		args = append(args, strings.TrimSpace(filter.IP))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := l.db.QueryContext(ctx, `SELECT mac,ip,coalesce(hostname,''),coalesce(family,''),allocated_at,released_at,last_request_at,sticky_until
FROM dhcp_sticky_leases`+where+` ORDER BY sticky_until DESC,last_request_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DHCPStickyLease
	for rows.Next() {
		var row DHCPStickyLease
		var allocatedAt, releasedAt, lastRequestAt, stickyUntil int64
		if err := rows.Scan(&row.MAC, &row.IP, &row.Hostname, &row.Family, &allocatedAt, &releasedAt, &lastRequestAt, &stickyUntil); err != nil {
			return nil, err
		}
		row.AllocatedAt = unixNanoTime(allocatedAt)
		row.ReleasedAt = unixNanoTime(releasedAt)
		row.LastRequestAt = unixNanoTime(lastRequestAt)
		row.StickyUntil = unixNanoTime(stickyUntil)
		out = append(out, row)
	}
	return out, rows.Err()
}

func unixNanoTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}
