// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
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
