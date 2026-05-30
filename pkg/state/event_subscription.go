// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
)

// SubscriptionRun is a persisted EventSubscriptionController processing record
// for one (subscription, event) pair (ADR 0006, Phase 3). It backs at-least-once
// + idempotent delivery: a succeeded row is never re-run, and a failed row is
// retried until attempts reaches MaxAttempts.
type SubscriptionRun struct {
	ID                int64
	Subscription      string
	EventID           string
	EventGroup        string
	Plugin            string
	Status            string // pending|succeeded|failed
	Attempts          int
	StartedAt         string
	CompletedAt       string
	DynamicSource     string
	DynamicGeneration int64
	Error             string
}

// SubscriptionRunStatus returns the recorded status and attempt count for a
// (subscription, event) pair. found is false when no row exists yet (a new,
// never-processed event).
func (s *SQLiteStore) SubscriptionRunStatus(subscription, eventID string) (status string, attempts int, found bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", 0, false, errStoreClosed
	}
	row := s.db.QueryRow(`SELECT status, attempts FROM event_subscription_runs WHERE subscription = ? AND event_id = ?`, subscription, eventID)
	scanErr := row.Scan(&status, &attempts)
	switch scanErr {
	case nil:
		return status, attempts, true, nil
	case sql.ErrNoRows:
		return "", 0, false, nil
	default:
		return "", 0, false, fmt.Errorf("query subscription run status: %w", scanErr)
	}
}

// UpsertSubscriptionRunStart records that processing of (subscription, event)
// is starting. For a new pair it inserts a pending row with attempts=1. For an
// existing not-yet-succeeded pair it increments attempts and refreshes
// started_at (a retry). Callers must skip already-succeeded events before
// calling this; a succeeded row is left untouched by the ON CONFLICT guard.
func (s *SQLiteStore) UpsertSubscriptionRunStart(subscription, eventID, eventGroup, plugin string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	if subscription == "" {
		return fmt.Errorf("subscription is required")
	}
	if eventID == "" {
		return fmt.Errorf("event id is required")
	}
	now := formatStateTime(s.now().UTC())
	_, err := s.db.Exec(`
		INSERT INTO event_subscription_runs (subscription, event_id, event_group, plugin, status, attempts, started_at)
		VALUES (?, ?, ?, ?, 'pending', 1, ?)
		ON CONFLICT(subscription, event_id) DO UPDATE SET
			status = CASE WHEN event_subscription_runs.status = 'succeeded' THEN event_subscription_runs.status ELSE 'pending' END,
			attempts = CASE WHEN event_subscription_runs.status = 'succeeded' THEN event_subscription_runs.attempts ELSE event_subscription_runs.attempts + 1 END,
			started_at = CASE WHEN event_subscription_runs.status = 'succeeded' THEN event_subscription_runs.started_at ELSE excluded.started_at END,
			plugin = CASE WHEN event_subscription_runs.status = 'succeeded' THEN event_subscription_runs.plugin ELSE excluded.plugin END
	`, subscription, eventID, eventGroup, plugin, now)
	if err != nil {
		return fmt.Errorf("upsert subscription run start: %w", err)
	}
	return nil
}

// MarkSubscriptionRunResult records the terminal outcome of a processing
// attempt. status is succeeded or failed; dynamicSource/dynamicGeneration are
// set on success, errMsg on failure.
func (s *SQLiteStore) MarkSubscriptionRunResult(subscription, eventID, status, dynamicSource string, dynamicGeneration int64, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	completedAt := formatStateTime(s.now().UTC())
	var genArg any
	if dynamicGeneration > 0 {
		genArg = dynamicGeneration
	}
	_, err := s.db.Exec(`
		UPDATE event_subscription_runs
		SET status = ?, completed_at = ?, dynamic_source = ?, dynamic_generation = ?, error = ?
		WHERE subscription = ? AND event_id = ?
	`, status, completedAt, nullableString(dynamicSource), genArg, nullableString(errMsg), subscription, eventID)
	if err != nil {
		return fmt.Errorf("mark subscription run result: %w", err)
	}
	return nil
}

// ListSubscriptionRuns returns all run rows for a subscription ordered by id.
func (s *SQLiteStore) ListSubscriptionRuns(subscription string) ([]SubscriptionRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errStoreClosed
	}
	rows, err := s.db.Query(`
		SELECT id, subscription, event_id, event_group, plugin, status, attempts,
		       started_at, coalesce(completed_at, ''), coalesce(dynamic_source, ''),
		       coalesce(dynamic_generation, 0), coalesce(error, '')
		FROM event_subscription_runs
		WHERE subscription = ?
		ORDER BY id
	`, subscription)
	if err != nil {
		return nil, fmt.Errorf("list subscription runs: %w", err)
	}
	defer rows.Close()
	var out []SubscriptionRun
	for rows.Next() {
		var run SubscriptionRun
		if err := rows.Scan(&run.ID, &run.Subscription, &run.EventID, &run.EventGroup, &run.Plugin,
			&run.Status, &run.Attempts, &run.StartedAt, &run.CompletedAt,
			&run.DynamicSource, &run.DynamicGeneration, &run.Error); err != nil {
			return nil, fmt.Errorf("scan subscription run: %w", err)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription runs: %w", err)
	}
	return out, nil
}
