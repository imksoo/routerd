// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// PublishPluginResult commits an invocation only if no newer result
// for its source has been published while the external plugin was running.
// The external process is never run inside this transaction.
func (s *SQLiteStore) PublishPluginResult(ctx context.Context, previousGeneration int64, part DynamicConfigPartRecord) error {
	if !strings.HasPrefix(part.Source, "Plugin/") || previousGeneration < 0 || part.Generation != previousGeneration+1 {
		return fmt.Errorf("plugin publication requires its next Plugin source generation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0) FROM dynamic_config_parts WHERE source=?`, part.Source).Scan(&current); err != nil {
		return err
	}
	if current != previousGeneration {
		return fmt.Errorf("plugin source %q changed from generation %d to %d during invocation", part.Source, previousGeneration, current)
	}
	if _, err := tx.ExecContext(ctx, upsertDynamicConfigPartSQL, dynamicConfigPartArguments(part, s.now().UTC())...); err != nil {
		return err
	}
	return tx.Commit()
}
