// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
	"time"
)

// Delivery status values for the event_deliveries table. A delivery starts as
// DeliveryPending, transitions to DeliveryDelivered on success, or
// DeliveryFailed once retries are exhausted (semantics owned by the daemon in
// Chunk 2).
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
)

// DeliveryRecord is one (event, peer) push attempt tracked in event_deliveries
// (ADR 0006, Phase 2). Times are persisted as unix seconds; zero time is NULL.
type DeliveryRecord struct {
	ID             int64
	EventID        string
	Peer           string
	Status         string
	Attempts       int
	LastAttemptAt  time.Time
	LastError      string
	DeliveredAt    time.Time
	EventExpiresAt time.Time
}

func (s *SQLiteStore) ensureEventDeliveryColumns() error {
	hasCol, err := s.tableHasColumn("event_deliveries", "event_expires_at")
	if err != nil {
		return err
	}
	if !hasCol {
		if _, err := s.db.Exec(`ALTER TABLE event_deliveries ADD COLUMN event_expires_at INTEGER`); err != nil {
			return err
		}
	}
	return nil
}

// RecordDelivery enqueues a (event_id, peer) delivery row in status pending with
// attempts=0. It is idempotent: if the row already exists it is a no-op
// (ON CONFLICT(event_id, peer) DO NOTHING), matching at-least-once enqueue.
func (s *SQLiteStore) RecordDelivery(eventID, peer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	if eventID == "" {
		return fmt.Errorf("delivery event id is required")
	}
	if peer == "" {
		return fmt.Errorf("delivery peer is required")
	}
	_, err := s.db.Exec(`
		INSERT INTO event_deliveries (event_id, peer, status, attempts)
		VALUES (?, ?, ?, 0)
		ON CONFLICT(event_id, peer) DO NOTHING
	`, eventID, peer, DeliveryPending)
	if err != nil {
		return fmt.Errorf("record delivery: %w", err)
	}
	return nil
}

// UpdateDeliveryStatus updates the status, attempts, last_attempt_at,
// last_error, delivered_at, and event_expires_at for the (event_id, peer) row.
// last_attempt_at is stamped with the store clock. deliveredAt and
// eventExpiresAt are stored only when non-zero (otherwise left NULL).
func (s *SQLiteStore) UpdateDeliveryStatus(eventID, peer, status string, attempts int, lastErr string, deliveredAt time.Time, eventExpiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	if eventID == "" {
		return fmt.Errorf("delivery event id is required")
	}
	if peer == "" {
		return fmt.Errorf("delivery peer is required")
	}
	lastAttempt := s.now().UTC().Unix()
	var delivered sql.NullInt64
	if !deliveredAt.IsZero() {
		delivered = sql.NullInt64{Int64: deliveredAt.UTC().Unix(), Valid: true}
	}
	var expiresAt sql.NullInt64
	if !eventExpiresAt.IsZero() {
		expiresAt = sql.NullInt64{Int64: eventExpiresAt.UTC().Unix(), Valid: true}
	}
	_, err := s.db.Exec(`
		UPDATE event_deliveries
		SET status = ?, attempts = ?, last_attempt_at = ?, last_error = ?, delivered_at = ?, event_expires_at = ?
		WHERE event_id = ? AND peer = ?
	`, status, attempts, lastAttempt, lastErr, delivered, expiresAt, eventID, peer)
	if err != nil {
		return fmt.Errorf("update delivery status: %w", err)
	}
	return nil
}

// ListDeliveries returns delivery rows ordered by id. Empty eventID and/or peer
// act as wildcards (no filter on that column).
func (s *SQLiteStore) ListDeliveries(eventID, peer string) ([]DeliveryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errStoreClosed
	}
	query := `
		SELECT id, event_id, peer, status, attempts, last_attempt_at, last_error, delivered_at, event_expires_at
		FROM event_deliveries
	`
	var (
		clauses []string
		args    []any
	)
	if eventID != "" {
		clauses = append(clauses, "event_id = ?")
		args = append(args, eventID)
	}
	if peer != "" {
		clauses = append(clauses, "peer = ?")
		args = append(args, peer)
	}
	for i, clause := range clauses {
		if i == 0 {
			query += " WHERE "
		} else {
			query += " AND "
		}
		query += clause
	}
	query += " ORDER BY id"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	var records []DeliveryRecord
	for rows.Next() {
		var (
			rec            DeliveryRecord
			lastAttempt    sql.NullInt64
			lastError      sql.NullString
			delivered      sql.NullInt64
			eventExpiresAt sql.NullInt64
		)
		if err := rows.Scan(&rec.ID, &rec.EventID, &rec.Peer, &rec.Status, &rec.Attempts, &lastAttempt, &lastError, &delivered, &eventExpiresAt); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		if lastAttempt.Valid && lastAttempt.Int64 > 0 {
			rec.LastAttemptAt = time.Unix(lastAttempt.Int64, 0).UTC()
		}
		if lastError.Valid {
			rec.LastError = lastError.String
		}
		if delivered.Valid && delivered.Int64 > 0 {
			rec.DeliveredAt = time.Unix(delivered.Int64, 0).UTC()
		}
		if eventExpiresAt.Valid && eventExpiresAt.Int64 > 0 {
			rec.EventExpiresAt = time.Unix(eventExpiresAt.Int64, 0).UTC()
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return records, nil
}

// ListDeliveriesFiltered returns delivery rows filtered by any combination of
// group, eventID, peer, and status (empty string = no filter on that column).
// When group != "" it JOINs federation_events on event_id = id and filters by
// group_name; otherwise it queries event_deliveries directly so deliveries for
// pruned events still surface. Rows are ordered by event_id, peer. The shape
// matches ListDeliveries (times decoded from unix seconds, 0/NULL -> zero time).
func (s *SQLiteStore) ListDeliveriesFiltered(group, eventID, peer, status string) ([]DeliveryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errStoreClosed
	}

	var (
		query   string
		clauses []string
		args    []any
		col     string // column prefix: "d." when joined, "" otherwise
	)
	if group != "" {
		col = "d."
		query = `
			SELECT d.event_id, d.peer, d.status, d.attempts, d.last_attempt_at, d.last_error, d.delivered_at, d.event_expires_at
			FROM event_deliveries d
			JOIN federation_events e ON d.event_id = e.id
		`
		clauses = append(clauses, "e.group_name = ?")
		args = append(args, group)
	} else {
		query = `
			SELECT event_id, peer, status, attempts, last_attempt_at, last_error, delivered_at, event_expires_at
			FROM event_deliveries
		`
	}
	if eventID != "" {
		clauses = append(clauses, col+"event_id = ?")
		args = append(args, eventID)
	}
	if peer != "" {
		clauses = append(clauses, col+"peer = ?")
		args = append(args, peer)
	}
	if status != "" {
		clauses = append(clauses, col+"status = ?")
		args = append(args, status)
	}
	for i, clause := range clauses {
		if i == 0 {
			query += " WHERE "
		} else {
			query += " AND "
		}
		query += clause
	}
	query += " ORDER BY " + col + "event_id, " + col + "peer"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries filtered: %w", err)
	}
	defer rows.Close()
	var records []DeliveryRecord
	for rows.Next() {
		var (
			rec            DeliveryRecord
			lastAttempt    sql.NullInt64
			lastError      sql.NullString
			delivered      sql.NullInt64
			eventExpiresAt sql.NullInt64
		)
		if err := rows.Scan(&rec.EventID, &rec.Peer, &rec.Status, &rec.Attempts, &lastAttempt, &lastError, &delivered, &eventExpiresAt); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		if lastAttempt.Valid && lastAttempt.Int64 > 0 {
			rec.LastAttemptAt = time.Unix(lastAttempt.Int64, 0).UTC()
		}
		if lastError.Valid {
			rec.LastError = lastError.String
		}
		if delivered.Valid && delivered.Int64 > 0 {
			rec.DeliveredAt = time.Unix(delivered.Int64, 0).UTC()
		}
		if eventExpiresAt.Valid && eventExpiresAt.Int64 > 0 {
			rec.EventExpiresAt = time.Unix(eventExpiresAt.Int64, 0).UTC()
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return records, nil
}
