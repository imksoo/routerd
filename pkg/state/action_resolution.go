// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ResolveActionOutcome records an operator's independent inspection, never a
// provider mutation or a timed retry. A live invocation retains the same guard
// and cannot be resolved away. Prior facts needed for undo remain unchanged.
func (s *SQLiteStore) ResolveActionOutcome(ctx context.Context, id int64, outcome, by, evidence string, now time.Time) (err error) {
	by = strings.TrimSpace(by)
	evidence = strings.TrimSpace(evidence)
	if by == "" || evidence == "" {
		return fmt.Errorf("action outcome resolution requires operator identity and provider inspection evidence")
	}
	guard, err := s.TryLockActionOperations(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, guard.Close()) }()
	rec, found, err := s.GetActionByID(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("action %d not found", id)
	}
	switch rec.Status {
	case ActionOutcomeUnknown:
		if outcome != ActionSucceeded && outcome != ActionFailed {
			return fmt.Errorf("forward outcome must be succeeded or failed")
		}
	case ActionRollbackUnknown:
		if outcome != ActionRolledBack && outcome != ActionRollbackFailed {
			return fmt.Errorf("rollback outcome must be rolledBack or rollbackFailed")
		}
	default:
		return fmt.Errorf("action %d is %s, not an unknown outcome", id, rec.Status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	message := fmt.Sprintf("operator %s confirmed %s: %s; prior result: %s; prior error: %s", by, outcome, evidence, rec.ResultMessage, rec.Error)
	executedAt := rec.ExecutedAt
	if rec.Status == ActionOutcomeUnknown {
		executedAt = now
	}
	result, err := s.db.ExecContext(ctx, `UPDATE action_executions SET status=?,result_message=?,error=NULL,executed_at=?,updated_at=? WHERE id=? AND status=?`, outcome, message, nullableStateTime(executedAt), formatStateTime(now), id, rec.Status)
	if err != nil {
		return err
	}
	return requireRowAffected(result, id, "resolve", rec.Status)
}
