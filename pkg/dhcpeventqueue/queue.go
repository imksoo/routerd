// SPDX-License-Identifier: BSD-3-Clause

// Package dhcpeventqueue durably accepts local DHCP callbacks independently of
// routerd's control socket. A single ordered consumer runs inside routerd.
package dhcpeventqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	MaxPending       = 4096
	MaxPayloadBytes  = 2048
	OperationTimeout = 3 * time.Second
)

var ErrFull = errors.New("DHCP event queue is full")

type Event struct {
	Sequence   int64                            `json:"sequence"`
	ObservedAt time.Time                        `json:"observedAt"`
	Lease      controlapi.DHCPLeaseEventRequest `json:"lease"`
}

type Queue struct {
	db   *sql.DB
	path string
}

// Open uses a private directory, a private file, a 100 ms SQLite lock wait, and
// an 8 MiB database ceiling. DELETE journaling bounds the rollback journal by
// the database size, without an indefinitely growing WAL behind a reader.
// On production systems both callers run as root. Unprivileged sandboxes own
// their own directory; a differently owned or writable queue is never adopted.
func Open(ctx context.Context, path string) (*Queue, error) {
	if err := supportedPlatform(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("DHCP event queue path must be absolute")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := privatePath(dir, true); err != nil {
		return nil, err
	}
	if err := createPrivateFile(path); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm", ".consumer"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			if err := privatePath(path+suffix, false); err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	q := &Queue{db: db, path: path}
	_, err = db.ExecContext(ctx, `PRAGMA busy_timeout=100;
PRAGMA page_size=4096;
PRAGMA max_page_count=2048;
PRAGMA journal_mode=DELETE;
PRAGMA synchronous=FULL;
CREATE TABLE IF NOT EXISTS pending (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 observed_at INTEGER NOT NULL,
 payload TEXT NOT NULL CHECK(length(CAST(payload AS BLOB)) <= 2048)
);`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return q, nil
}

func createPrivateFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		err = f.Close()
	} else if os.IsExist(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	return privatePath(path, false)
}

func (q *Queue) Close() error { return q.db.Close() }

// Enqueue returns success only after SQLite has committed the callback. The
// capacity check and insertion share an IMMEDIATE transaction across producers.
func (q *Queue) Enqueue(ctx context.Context, lease controlapi.DHCPLeaseEventRequest, at time.Time) error {
	if _, ok := daemonapi.DHCPLeaseEventType(lease.Action); !ok || lease.IP == "" || at.IsZero() {
		return errors.New("invalid DHCP event")
	}
	if len(lease.MAC) > 64 || len(lease.IP) > 64 || len(lease.Hostname) > 253 || len(lease.Interface) > 64 {
		return errors.New("DHCP event field exceeds size limit")
	}
	payload, err := json.Marshal(lease)
	if err != nil {
		return err
	}
	if len(payload) > MaxPayloadBytes {
		return errors.New("DHCP event exceeds size limit")
	}
	conn, err := q.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var n int
	if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM pending").Scan(&n); err != nil {
		return err
	}
	if n >= MaxPending {
		return ErrFull
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO pending(observed_at, payload) VALUES (?, ?)", at.UTC().UnixNano(), string(payload)); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}

// Store retries transient SQLite contention within one operation budget,
// including opening/initialization. Each failed attempt is rolled back and
// closed before retrying; observation time is never replaced by retry time.
func Store(ctx context.Context, path string, lease controlapi.DHCPLeaseEventRequest, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := storeOnce(ctx, path, lease, at)
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != sqlite3.SQLITE_BUSY {
			return err
		}
		// Retry only BUSY, not full/corrupt/read-only/I/O failures whose cause
		// or commit outcome cannot be made safe by repeating this operation.
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

func storeOnce(ctx context.Context, path string, lease controlapi.DHCPLeaseEventRequest, at time.Time) error {
	q, err := Open(ctx, path)
	if err != nil {
		return fmt.Errorf("open DHCP event queue: %w", err)
	}
	defer q.Close()
	if err := q.Enqueue(ctx, lease, at); err != nil {
		return fmt.Errorf("enqueue DHCP event: %w", err)
	}
	return nil
}

// ProcessOne holds a cross-process consumer lock, but no SQLite transaction,
// while applying the oldest event. Producers remain independent of its work.
// Failed processing/deletion leaves the same head in place. A crash between
// processing and deletion may repeat that event, always before newer events.
func (q *Queue) ProcessOne(ctx context.Context, process func(context.Context, Event) error) (bool, error) {
	lockPath := q.path + ".consumer"
	if err := createPrivateFile(lockPath); err != nil {
		return false, err
	}
	unlock, err := lockConsumer(lockPath)
	if err != nil {
		return false, err
	}
	defer unlock()
	var event Event
	var at int64
	var payload string
	err = q.db.QueryRowContext(ctx, "SELECT sequence, observed_at, payload FROM pending ORDER BY sequence LIMIT 1").Scan(&event.Sequence, &at, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(payload), &event.Lease); err != nil {
		return false, err
	}
	event.ObservedAt = time.Unix(0, at).UTC()
	if err := process(ctx, event); err != nil {
		return false, err
	}
	_, err = q.db.ExecContext(ctx, "DELETE FROM pending WHERE sequence = ?", event.Sequence)
	return err == nil, err
}
