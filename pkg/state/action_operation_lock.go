// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrActionOperationBusy = errors.New("a provider action operation is already in progress")

// ActionOperationLock fences actual provider operations across the daemon and
// CLI processes sharing a journal. The one companion file is never removed:
// unlinking a lock file would let another process lock a different inode.
type ActionOperationLock struct {
	file *os.File
	once sync.Once
	err  error
}

func (l *ActionOperationLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		l.err = errors.Join(syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN), l.file.Close())
	})
	return l.err
}

// TryLockActionOperations never guesses that an old operation is dead. A busy
// guard is authoritative even when the journal timestamp is far in the past.
func (s *SQLiteStore) TryLockActionOperations(ctx context.Context) (*ActionOperationLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, sql.ErrConnDone
	}
	path := s.path
	s.mu.RUnlock()
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("provider action execution requires a filesystem-backed SQLite journal")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve provider journal: %w", err)
	}
	file, err := os.OpenFile(path+".actions.lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open provider operation guard: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrActionOperationBusy
		}
		return nil, fmt.Errorf("lock provider operation guard: %w", err)
	}
	guard := &ActionOperationLock{file: file}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, guard.Close())
	}
	return guard, nil
}

// RecoverInterruptedActionOperations records uncertainty, never a fresh
// approval. An executor may have changed the provider before its owner died.
// Only acquiring the actual process-lifetime guard permits this classification;
// no elapsed-time threshold can steal work from a slow living executor.
func (s *SQLiteStore) RecoverInterruptedActionOperations(ctx context.Context, now time.Time) (count int, err error) {
	guard, err := s.TryLockActionOperations(ctx)
	if errors.Is(err, ErrActionOperationBusy) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, guard.Close()) }()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, sql.ErrConnDone
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE action_executions SET status=CASE status WHEN ? THEN ? ELSE ? END,result_message=?,error=?,updated_at=? WHERE status IN (?,?)`, ActionRunning, ActionOutcomeUnknown, ActionRollbackUnknown, "provider operation ended without a durable result", "provider outcome is unconfirmed; inspect provider state before a new operation", formatStateTime(now), ActionRunning, ActionRollingBack)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}
