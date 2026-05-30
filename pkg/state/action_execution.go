// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Provider-action execution journal statuses (ADR 0007, Phase 5). Every
// execution attempt is journaled; routerd core never holds provider
// credentials, so these rows record only the approved action plan and its
// outcome.
const (
	// ActionPending is an imported action awaiting approval.
	ActionPending = "pending"
	// ActionApproved is an action approved (by operator or policy) but not yet
	// executed.
	ActionApproved = "approved"
	// ActionSucceeded is an action the executor reported as applied.
	ActionSucceeded = "succeeded"
	// ActionFailed is an action the executor reported as failed.
	ActionFailed = "failed"
	// ActionSkipped is an action that was not executed (e.g. duplicate
	// idempotency key already succeeded, or policy declined).
	ActionSkipped = "skipped"
	// ActionRolledBack is an action whose best-effort undo was applied.
	ActionRolledBack = "rolledBack"
)

// ActionExecutionRecord is one row of the action_executions journal. It mirrors
// the approved ActionPlan (no secrets) plus its execution lifecycle state.
type ActionExecutionRecord struct {
	ID             int64     `json:"id" yaml:"id"`
	IdempotencyKey string    `json:"idempotencyKey" yaml:"idempotencyKey"`
	Source         string    `json:"source,omitempty" yaml:"source,omitempty"`
	Provider       string    `json:"provider" yaml:"provider"`
	ProviderRef    string    `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	Action         string    `json:"action" yaml:"action"`
	TargetJSON     string    `json:"targetJSON,omitempty" yaml:"targetJSON,omitempty"`
	ParametersJSON string    `json:"parametersJSON,omitempty" yaml:"parametersJSON,omitempty"`
	UndoJSON       string    `json:"undoJSON,omitempty" yaml:"undoJSON,omitempty"`
	RiskLevel      string    `json:"riskLevel,omitempty" yaml:"riskLevel,omitempty"`
	Status         string    `json:"status" yaml:"status"`
	ApprovedBy     string    `json:"approvedBy,omitempty" yaml:"approvedBy,omitempty"`
	ApprovedAt     time.Time `json:"approvedAt,omitempty" yaml:"approvedAt,omitempty"`
	ExecutedAt     time.Time `json:"executedAt,omitempty" yaml:"executedAt,omitempty"`
	ResultMessage  string    `json:"resultMessage,omitempty" yaml:"resultMessage,omitempty"`
	Error          string    `json:"error,omitempty" yaml:"error,omitempty"`
	// Observed are the non-secret facts the executor reported on its last
	// terminal result (decoded from the observed_json column). The undo of
	// ensure-forwarding-enabled relies on Observed["priorSourceDestCheck"], which
	// the executor captures BEFORE mutating. Never contains credentials.
	Observed  map[string]string `json:"observed,omitempty" yaml:"observed,omitempty"`
	CreatedAt time.Time         `json:"createdAt" yaml:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt" yaml:"updatedAt"`
}

// ActionExecutionFilter narrows ListActions. Empty fields match anything.
type ActionExecutionFilter struct {
	Status   string
	Provider string
}

// ensureActionExecutionColumns is the additive migration for the
// action_executions journal. It creates the table when a pre-existing database
// lacks it (backward compatible: a database without the table simply gets it
// created here) and is the place to add future columns via tableHasColumn
// checks, mirroring the other ensure*Columns migrations.
func (s *SQLiteStore) ensureActionExecutionColumns() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS action_executions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  idempotency_key TEXT NOT NULL UNIQUE,
  source TEXT,
  provider TEXT NOT NULL,
  provider_ref TEXT,
  action TEXT NOT NULL,
  target_json TEXT,
  parameters_json TEXT,
  undo_json TEXT,
  risk_level TEXT,
  status TEXT NOT NULL,
  approved_by TEXT,
  approved_at TEXT,
  executed_at TEXT,
  result_message TEXT,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS action_executions_status ON action_executions(status, id);
`); err != nil {
		return err
	}
	// Additive column for the executor's Observed facts (e.g.
	// priorSourceDestCheck). Backward compatible: a pre-existing table without it
	// gets the column added; a table created fresh above does not yet have it, so
	// we add it here either way via the tableHasColumn guard.
	hasObserved, err := s.tableHasColumn("action_executions", "observed_json")
	if err != nil {
		return err
	}
	if !hasObserved {
		if _, err := s.db.Exec(`ALTER TABLE action_executions ADD COLUMN observed_json TEXT`); err != nil {
			return err
		}
	}
	return nil
}

// ImportAction inserts an action into the journal as pending, keyed by its
// idempotency key. It uses ON CONFLICT(idempotency_key) DO NOTHING so a repeated
// key never creates a duplicate execution row; inserted=false reports the key
// already existed.
func (s *SQLiteStore) ImportAction(rec ActionExecutionRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, nil
	}
	if strings.TrimSpace(rec.IdempotencyKey) == "" {
		return false, fmt.Errorf("action import requires idempotencyKey")
	}
	if strings.TrimSpace(rec.Provider) == "" {
		return false, fmt.Errorf("action import requires provider")
	}
	if strings.TrimSpace(rec.Action) == "" {
		return false, fmt.Errorf("action import requires action")
	}
	now := s.now().UTC()
	if rec.Status == "" {
		rec.Status = ActionPending
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	result, err := s.db.Exec(`INSERT INTO action_executions(idempotency_key,source,provider,provider_ref,action,target_json,parameters_json,undo_json,risk_level,status,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(idempotency_key) DO NOTHING`,
		rec.IdempotencyKey, nullableString(rec.Source), rec.Provider, nullableString(rec.ProviderRef), rec.Action,
		nullableString(rec.TargetJSON), nullableString(rec.ParametersJSON), nullableString(rec.UndoJSON), nullableString(rec.RiskLevel),
		rec.Status, formatStateTime(rec.CreatedAt), formatStateTime(rec.UpdatedAt))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

const actionExecutionColumns = `id,idempotency_key,coalesce(source,''),provider,coalesce(provider_ref,''),action,coalesce(target_json,''),coalesce(parameters_json,''),coalesce(undo_json,''),coalesce(risk_level,''),status,coalesce(approved_by,''),coalesce(approved_at,''),coalesce(executed_at,''),coalesce(result_message,''),coalesce(error,''),coalesce(observed_json,''),created_at,updated_at`

func scanActionExecution(scan func(...any) error) (ActionExecutionRecord, error) {
	var rec ActionExecutionRecord
	var approved, executed, created, updated, observed string
	if err := scan(&rec.ID, &rec.IdempotencyKey, &rec.Source, &rec.Provider, &rec.ProviderRef, &rec.Action,
		&rec.TargetJSON, &rec.ParametersJSON, &rec.UndoJSON, &rec.RiskLevel, &rec.Status, &rec.ApprovedBy,
		&approved, &executed, &rec.ResultMessage, &rec.Error, &observed, &created, &updated); err != nil {
		return ActionExecutionRecord{}, err
	}
	rec.ApprovedAt = parseStateTime(approved)
	rec.ExecutedAt = parseStateTime(executed)
	rec.CreatedAt = parseStateTime(created)
	rec.UpdatedAt = parseStateTime(updated)
	if strings.TrimSpace(observed) != "" {
		var m map[string]string
		if err := json.Unmarshal([]byte(observed), &m); err != nil {
			return ActionExecutionRecord{}, fmt.Errorf("decode observed_json for action %d: %w", rec.ID, err)
		}
		rec.Observed = m
	}
	return rec, nil
}

// GetActionByID returns the journal row with the given id, or ok=false.
func (s *SQLiteStore) GetActionByID(id int64) (ActionExecutionRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ActionExecutionRecord{}, false, nil
	}
	row := s.db.QueryRow(`SELECT `+actionExecutionColumns+` FROM action_executions WHERE id = ?`, id)
	rec, err := scanActionExecution(row.Scan)
	if err == sql.ErrNoRows {
		return ActionExecutionRecord{}, false, nil
	}
	if err != nil {
		return ActionExecutionRecord{}, false, err
	}
	return rec, true, nil
}

// GetActionByIdempotencyKey returns the journal row with the given key, or
// ok=false.
func (s *SQLiteStore) GetActionByIdempotencyKey(key string) (ActionExecutionRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ActionExecutionRecord{}, false, nil
	}
	row := s.db.QueryRow(`SELECT `+actionExecutionColumns+` FROM action_executions WHERE idempotency_key = ?`, key)
	rec, err := scanActionExecution(row.Scan)
	if err == sql.ErrNoRows {
		return ActionExecutionRecord{}, false, nil
	}
	if err != nil {
		return ActionExecutionRecord{}, false, err
	}
	return rec, true, nil
}

// ListActions returns journal rows newest-first, optionally filtered by status
// and/or provider.
func (s *SQLiteStore) ListActions(filter ActionExecutionFilter) ([]ActionExecutionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []ActionExecutionRecord{}, nil
	}
	query := `SELECT ` + actionExecutionColumns + ` FROM action_executions`
	var conds []string
	var args []any
	if status := strings.TrimSpace(filter.Status); status != "" {
		conds = append(conds, "status = ?")
		args = append(args, status)
	}
	if provider := strings.TrimSpace(filter.Provider); provider != "" {
		conds = append(conds, "provider = ?")
		args = append(args, provider)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY id DESC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActionExecutionRecord
	for rows.Next() {
		rec, err := scanActionExecution(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ApproveAction transitions a pending row to approved. It errors if the row is
// missing or not in the pending state (only pending actions can be approved).
func (s *SQLiteStore) ApproveAction(id int64, approvedBy string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	result, err := s.db.Exec(`UPDATE action_executions SET status = ?, approved_by = ?, approved_at = ?, updated_at = ? WHERE id = ? AND status = ?`,
		ActionApproved, nullableString(approvedBy), formatStateTime(now), formatStateTime(now), id, ActionPending)
	if err != nil {
		return err
	}
	return requireRowAffected(result, id, "approve", ActionPending)
}

// MarkActionResult records a terminal execution outcome (succeeded, failed, or
// skipped) for an approved action. It errors if the row is not currently
// approved. The observed map carries the executor's non-secret reported facts
// (e.g. priorSourceDestCheck), which the undo path later reads back from the
// journal; nil or empty leaves observed_json NULL.
func (s *SQLiteStore) MarkActionResult(id int64, status, message, errMsg string, observed map[string]string, executedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	switch status {
	case ActionSucceeded, ActionFailed, ActionSkipped:
	default:
		return fmt.Errorf("action %d result status %q must be succeeded, failed, or skipped", id, status)
	}
	now := s.now().UTC()
	if executedAt.IsZero() {
		executedAt = now
	}
	observedJSON := ""
	if len(observed) > 0 {
		b, err := json.Marshal(observed)
		if err != nil {
			return fmt.Errorf("encode observed for action %d: %w", id, err)
		}
		observedJSON = string(b)
	}
	result, err := s.db.Exec(`UPDATE action_executions SET status = ?, result_message = ?, error = ?, observed_json = ?, executed_at = ?, updated_at = ? WHERE id = ? AND status = ?`,
		status, nullableString(message), nullableString(errMsg), nullableString(observedJSON), formatStateTime(executedAt), formatStateTime(now), id, ActionApproved)
	if err != nil {
		return err
	}
	return requireRowAffected(result, id, "record result for", ActionApproved)
}

// MarkActionRolledBack records that an action's best-effort undo was applied. It
// transitions a succeeded action to rolledBack.
func (s *SQLiteStore) MarkActionRolledBack(id int64, message string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	result, err := s.db.Exec(`UPDATE action_executions SET status = ?, result_message = ?, updated_at = ? WHERE id = ? AND status = ?`,
		ActionRolledBack, nullableString(message), formatStateTime(now), id, ActionSucceeded)
	if err != nil {
		return err
	}
	return requireRowAffected(result, id, "roll back", ActionSucceeded)
}

func requireRowAffected(result sql.Result, id int64, verb, fromStatus string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("cannot %s action %d: row missing or not in %s state", verb, id, fromStatus)
	}
	return nil
}
