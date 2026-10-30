// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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
	// ActionRunning is an approved action claimed by one executor invocation.
	// It prevents daemon/manual races from launching the same provider mutation
	// twice.
	ActionRunning = "running"
	// ActionSucceeded is an action the executor reported as applied.
	ActionSucceeded = "succeeded"
	// ActionFailed is an action the executor reported as failed.
	ActionFailed = "failed"
	// ActionSkipped is an action that was not executed (e.g. duplicate
	// idempotency key already succeeded, or policy declined).
	ActionSkipped = "skipped"
	// ActionRolledBack is an action whose best-effort undo was applied.
	ActionRolledBack      = "rolledBack"
	ActionRollingBack     = "rollingBack"
	ActionRollbackFailed  = "rollbackFailed"
	ActionOutcomeUnknown  = "outcomeUnknown"
	ActionRollbackUnknown = "rollbackUnknown"
)

// ActionExecutionRecord is one row of the action_executions journal. It mirrors
// the approved ActionPlan (no secrets) plus its execution lifecycle state.
type ActionExecutionRecord struct {
	ID             int64                   `json:"id" yaml:"id"`
	IdempotencyKey string                  `json:"idempotencyKey" yaml:"idempotencyKey"`
	Source         string                  `json:"source,omitempty" yaml:"source,omitempty"`
	Provider       string                  `json:"provider" yaml:"provider"`
	ProviderRef    string                  `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	Action         string                  `json:"action" yaml:"action"`
	Target         map[string]string       `json:"target,omitempty" yaml:"target,omitempty"`
	Parameters     map[string]string       `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Undo           *api.ProviderActionUndo `json:"undo,omitempty" yaml:"undo,omitempty"`
	RiskLevel      string                  `json:"riskLevel,omitempty" yaml:"riskLevel,omitempty"`
	Status         string                  `json:"status" yaml:"status"`
	ApprovedBy     string                  `json:"approvedBy,omitempty" yaml:"approvedBy,omitempty"`
	ApprovedAt     time.Time               `json:"approvedAt,omitempty" yaml:"approvedAt,omitempty"`
	ExecutedAt     time.Time               `json:"executedAt,omitempty" yaml:"executedAt,omitempty"`
	ResultMessage  string                  `json:"resultMessage,omitempty" yaml:"resultMessage,omitempty"`
	Error          string                  `json:"error,omitempty" yaml:"error,omitempty"`
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

// ImportAction inserts an action into the journal as pending, keyed by its
// idempotency key. A repeated key never creates a duplicate execution row.
// Already-succeeded rows remain idempotent. Failed or skipped rows are requeued
// when the same desired action is still being imported, so transient provider
// API or permission failures do not permanently wedge declarative convergence.
func (s *SQLiteStore) ImportAction(rec ActionExecutionRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, sql.ErrConnDone
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
	target, err := json.Marshal(rec.Target)
	if err != nil {
		return false, fmt.Errorf("encode action target: %w", err)
	}
	parameters, err := json.Marshal(rec.Parameters)
	if err != nil {
		return false, fmt.Errorf("encode action parameters: %w", err)
	}
	var undo []byte
	if rec.Undo != nil {
		undo, err = json.Marshal(rec.Undo)
		if err != nil {
			return false, fmt.Errorf("encode action undo: %w", err)
		}
	}
	now := s.now().UTC()
	if rec.Status == "" {
		rec.Status = ActionPending
	}
	if !validActionStatus(rec.Status) {
		return false, fmt.Errorf("invalid action status %q", rec.Status)
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	result, err := s.db.Exec(`INSERT INTO action_executions(idempotency_key,source,provider,provider_ref,action,target_json,parameters_json,undo_json,risk_level,status,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(idempotency_key) DO UPDATE SET
  source = excluded.source,
  provider = excluded.provider,
  provider_ref = excluded.provider_ref,
  action = excluded.action,
  target_json = excluded.target_json,
  parameters_json = excluded.parameters_json,
  undo_json = excluded.undo_json,
  risk_level = excluded.risk_level,
  status = excluded.status,
  approved_by = NULL,
  approved_at = NULL,
  executed_at = NULL,
  result_message = NULL,
  error = NULL,
  observed_json = NULL,
  updated_at = excluded.updated_at
WHERE action_executions.status IN (?, ?)`,
		rec.IdempotencyKey, nullableString(rec.Source), rec.Provider, nullableString(rec.ProviderRef), rec.Action,
		string(target), string(parameters), nullableString(string(undo)), nullableString(rec.RiskLevel),
		rec.Status, formatStateTime(rec.CreatedAt), formatStateTime(rec.UpdatedAt), ActionFailed, ActionSkipped)
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
	var approved, executed, created, updated, observed, target, parameters, undo string
	if err := scan(&rec.ID, &rec.IdempotencyKey, &rec.Source, &rec.Provider, &rec.ProviderRef, &rec.Action,
		&target, &parameters, &undo, &rec.RiskLevel, &rec.Status, &rec.ApprovedBy,
		&approved, &executed, &rec.ResultMessage, &rec.Error, &observed, &created, &updated); err != nil {
		return ActionExecutionRecord{}, err
	}
	if target != "" {
		if err := json.Unmarshal([]byte(target), &rec.Target); err != nil {
			return ActionExecutionRecord{}, fmt.Errorf("decode target_json for action %d: %w", rec.ID, err)
		}
	}
	if parameters != "" {
		if err := json.Unmarshal([]byte(parameters), &rec.Parameters); err != nil {
			return ActionExecutionRecord{}, fmt.Errorf("decode parameters_json for action %d: %w", rec.ID, err)
		}
	}
	if undo != "" {
		// Validate the whole value, then enforce the closed undo object shape.
		if !json.Valid([]byte(undo)) {
			return ActionExecutionRecord{}, fmt.Errorf("decode undo_json for action %d: invalid JSON", rec.ID)
		}
		decoder := json.NewDecoder(strings.NewReader(undo))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rec.Undo); err != nil {
			return ActionExecutionRecord{}, fmt.Errorf("decode undo_json for action %d: %w", rec.ID, err)
		}
	}
	if !validActionStatus(rec.Status) {
		return ActionExecutionRecord{}, fmt.Errorf("invalid status for action %d: %q", rec.ID, rec.Status)
	}
	for _, field := range []struct {
		name, raw string
		target    *time.Time
		required  bool
	}{
		{"approved_at", approved, &rec.ApprovedAt, false}, {"executed_at", executed, &rec.ExecutedAt, false}, {"created_at", created, &rec.CreatedAt, true}, {"updated_at", updated, &rec.UpdatedAt, true},
	} {
		if field.raw == "" && !field.required {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, field.raw)
		if err != nil {
			return ActionExecutionRecord{}, fmt.Errorf("decode %s for action %d: %w", field.name, rec.ID, err)
		}
		*field.target = parsed
	}
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
		return ActionExecutionRecord{}, false, sql.ErrConnDone
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
		return ActionExecutionRecord{}, false, sql.ErrConnDone
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
		return []ActionExecutionRecord{}, sql.ErrConnDone
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
		return sql.ErrConnDone
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

// BeginActionExecution claims an approved action for execution. It is a CAS:
// exactly one caller can move approved -> running. A false return means the row
// is missing or no longer approved and the caller must not launch the executor.
func (s *SQLiteStore) BeginActionExecution(id int64, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, sql.ErrConnDone
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	result, err := s.db.Exec(`UPDATE action_executions SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		ActionRunning, formatStateTime(now), id, ActionApproved)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// MarkActionResult records a terminal execution outcome (succeeded, failed, or
// skipped) or an unconfirmed outcome for a claimed running action. The observed
// map carries the executor's non-secret
// reported facts (e.g. priorSourceDestCheck), which the undo path later reads
// back from the journal; nil or empty leaves observed_json NULL.
func (s *SQLiteStore) MarkActionResult(id int64, status, message, errMsg string, observed map[string]string, executedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	switch status {
	case ActionSucceeded, ActionFailed, ActionSkipped, ActionOutcomeUnknown:
	default:
		return fmt.Errorf("action %d has invalid execution result status %q", id, status)
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
		status, nullableString(message), nullableString(errMsg), nullableString(observedJSON), formatStateTime(executedAt), formatStateTime(now), id, ActionRunning)
	if err != nil {
		return err
	}
	return requireRowAffected(result, id, "record result for", ActionRunning)
}

// MarkActionSkippedByIdempotencyKey fences an unexecuted action out of the
// journal by idempotency key. It is used by import-time gates to retire stale
// pending/approved work that was already imported before newer desired state
// superseded it.
func (s *SQLiteStore) MarkActionSkippedByIdempotencyKey(key, message string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("action idempotencyKey is required")
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	_, err := s.db.Exec(`UPDATE action_executions SET status = ?, result_message = ?, executed_at = ?, updated_at = ? WHERE idempotency_key = ? AND status IN (?, ?)`,
		ActionSkipped, nullableString(message), formatStateTime(now), formatStateTime(now), key, ActionPending, ActionApproved)
	return err
}

// BeginActionRollback persists undo intent before an executor may reverse a
// successful action. Only a definitive failed undo may be retried automatically
// by an explicit rollback request; an unknown outcome remains fenced.
func (s *SQLiteStore) BeginActionRollback(id int64, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, sql.ErrConnDone
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	result, err := s.db.Exec(`UPDATE action_executions SET status=?,result_message=NULL,error=NULL,updated_at=? WHERE id=? AND status IN (?,?) AND coalesce(undo_json,'')<>''`, ActionRollingBack, formatStateTime(now), id, ActionSucceeded, ActionRollbackFailed)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// MarkActionRollbackResult completes a claimed undo without erasing the original
// successful action's observed facts or execution timestamp.
func (s *SQLiteStore) MarkActionRollbackResult(id int64, status, message, errMsg string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	if now.IsZero() {
		now = s.now().UTC()
	}
	switch status {
	case ActionRolledBack, ActionRollbackFailed, ActionRollbackUnknown:
	default:
		return fmt.Errorf("action %d has invalid rollback result status %q", id, status)
	}
	result, err := s.db.Exec(`UPDATE action_executions SET status = ?, result_message = ?, error=?,updated_at = ? WHERE id = ? AND status = ?`,
		status, nullableString(message), nullableString(errMsg), formatStateTime(now), id, ActionRollingBack)
	if err != nil {
		return err
	}
	return requireRowAffected(result, id, "record rollback result for", ActionRollingBack)
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

func validActionStatus(status string) bool {
	switch status {
	case ActionPending, ActionApproved, ActionRunning, ActionSucceeded, ActionFailed, ActionSkipped, ActionRollingBack, ActionRolledBack, ActionRollbackFailed, ActionOutcomeUnknown, ActionRollbackUnknown:
		return true
	default:
		return false
	}
}
