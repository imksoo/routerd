// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// EventJournalTip observes the journal's high-water mark without creating a
// consumer. sqlite_sequence retains that position when retention removes rows.
func (s *SQLiteStore) EventJournalTip(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, sql.ErrConnDone
	}
	var tip int64
	err := s.db.QueryRowContext(ctx, `SELECT coalesce((SELECT seq FROM sqlite_sequence WHERE name='events'),0)`).Scan(&tip)
	return tip, err
}

// InitializeEventConsumerCursor admits a previously prepared consumer at its
// captured journal position. Existing recovery progress always wins.
func (s *SQLiteStore) InitializeEventConsumerCursor(ctx context.Context, consumer string, initial int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(consumer) == "" || initial < 0 {
		return fmt.Errorf("invalid event consumer initialization")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO event_consumer_cursors(consumer,cursor,updated_at)
VALUES(?,?,?) ON CONFLICT(consumer) DO NOTHING`, consumer, initial, formatStateTime(s.now().UTC()))
	return err
}
