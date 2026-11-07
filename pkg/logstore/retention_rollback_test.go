// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// Only the real driver's Rollback is delayed. Preserve its optional connection
// interfaces, including those database/sql uses to decide whether cancellation
// can return a connection to the pool.
type retentionRollbackConnector struct {
	dsn       string
	entered   chan struct{}
	release   chan struct{}
	finished  atomic.Bool
	completed chan struct{}
}

func (c *retentionRollbackConnector) Driver() driver.Driver { return &sqlite.Driver{} }
func (c *retentionRollbackConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &retentionRollbackConn{
		Conn:               conn,
		ConnPrepareContext: conn.(driver.ConnPrepareContext),
		ExecerContext:      conn.(driver.ExecerContext),
		QueryerContext:     conn.(driver.QueryerContext),
		SessionResetter:    conn.(driver.SessionResetter),
		Validator:          conn.(driver.Validator),
		gate:               c,
	}, nil
}

type retentionRollbackConn struct {
	driver.Conn
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.SessionResetter
	driver.Validator
	gate *retentionRollbackConnector
}

func (c *retentionRollbackConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &retentionRollbackTx{Tx: tx, gate: c.gate}, nil
}

type retentionRollbackTx struct {
	driver.Tx
	gate *retentionRollbackConnector
}

func (tx *retentionRollbackTx) Rollback() error {
	close(tx.gate.entered)
	<-tx.gate.release
	err := tx.Tx.Rollback()
	tx.gate.finished.Store(true)
	close(tx.gate.completed)
	return err
}

func TestCanceledRetentionWaitsForDriverRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retention.db")
	observer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if _, err := observer.Exec(`CREATE TABLE events (created_at TEXT); INSERT INTO events VALUES ('2000-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	gate := &retentionRollbackConnector{dsn: path, entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	db := sql.OpenDB(gate)
	defer db.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	returned := make(chan error, 1)
	target := RetentionTarget{BeforeDelete: func(RetentionRecord) error {
		cancel()
		select {
		case <-gate.entered:
		case <-t.Context().Done():
			return t.Context().Err()
		}
		return ctx.Err()
	}}
	go func() {
		_, err := deleteExpired(ctx, db, time.Now(), RetentionTables([]string{RetentionSignalEvents}), target)
		returned <- err
	}()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		close(gate.release)
		t.Fatal("cancellation never reached the real driver's rollback")
	}
	// The read transaction still owns its actual SQLite lock while rollback is
	// gated. No busy timeout is installed on this independent connection.
	_, lockErr := observer.Exec(`BEGIN EXCLUSIVE`)
	var sqliteErr *sqlite.Error
	if !errors.As(lockErr, &sqliteErr) || sqliteErr.Code() != 5 {
		close(gate.release)
		t.Fatalf("expected the active transaction's SQLite lock, got %v", lockErr)
	}
	var retentionErr error
	premature := false
	select {
	case retentionErr = <-returned:
		premature = !gate.finished.Load()
	case <-time.After(100 * time.Millisecond):
		// A bounded negative assertion on a deliberately blocked driver operation;
		// this is not a retry or delay for an uncontrolled database lock.
	}
	close(gate.release)
	select {
	case <-gate.completed:
	case <-time.After(5 * time.Second):
		t.Fatal("real driver rollback did not finish after release")
	}
	if retentionErr == nil {
		select {
		case retentionErr = <-returned:
		case <-time.After(5 * time.Second):
			t.Fatal("retention did not finish after driver rollback")
		}
	}
	if premature {
		t.Error("retention returned before the canceled transaction finished driver rollback")
	}
	if !errors.Is(retentionErr, context.Canceled) {
		t.Errorf("canceled retention error = %v", retentionErr)
	}
	if !gate.finished.Load() {
		t.Fatal("driver rollback has not completed")
	}
	var rows int
	if err := observer.QueryRow(`SELECT count(*) FROM events`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("immediate independent read after retention: rows=%d, err=%v", rows, err)
	}
	// The same independent connection can acquire a write lock immediately too.
	if _, err := observer.Exec(`BEGIN EXCLUSIVE; ROLLBACK`); err != nil {
		t.Fatal(err)
	}
}
