// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// errStoreClosed is returned by the federation event store methods once the
// underlying SQLiteStore has been closed.
var errStoreClosed = errors.New("state store is closed")

// EventRecord is a persisted CloudEdge Event Federation event (ADR 0006). It is
// stored in the federation_events table, which is distinct from the
// observability event store (that name belongs to eventlog/eventfile).
type EventRecord struct {
	ID         string
	Group      string
	SourceNode string
	Type       string
	Subject    string
	DedupeKey  string
	Payload    map[string]string
	ObservedAt time.Time
	ExpiresAt  time.Time
	RecordedAt time.Time
}

// RecordFederationEvent persists a federation event idempotently. A duplicate ID
// is a no-op (ON CONFLICT(id) DO NOTHING), matching the at-least-once delivery
// model: re-recording the same event id returns nil without overwriting.
func (s *SQLiteStore) RecordFederationEvent(rec EventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	if rec.ID == "" {
		return fmt.Errorf("federation event id is required")
	}
	if rec.Group == "" {
		return fmt.Errorf("federation event group is required")
	}
	if rec.Type == "" {
		return fmt.Errorf("federation event type is required")
	}
	if rec.DedupeKey == "" {
		rec.DedupeKey = rec.ID
	}
	now := s.now().UTC()
	if rec.RecordedAt.IsZero() {
		rec.RecordedAt = now
	}
	if rec.ObservedAt.IsZero() {
		rec.ObservedAt = now
	}
	payloadJSON := "{}"
	if len(rec.Payload) > 0 {
		data, err := json.Marshal(rec.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		payloadJSON = string(data)
	}
	var expiresAt sql.NullInt64
	if !rec.ExpiresAt.IsZero() {
		expiresAt = sql.NullInt64{Int64: rec.ExpiresAt.UTC().Unix(), Valid: true}
	}
	_, err := s.db.Exec(`
		INSERT INTO federation_events (id, group_name, source_node, type, subject, dedupe_key, payload, observed_at, expires_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, rec.ID, rec.Group, rec.SourceNode, rec.Type, rec.Subject, rec.DedupeKey, payloadJSON, rec.ObservedAt.UTC().Unix(), expiresAt, rec.RecordedAt.UTC().Unix())
	if err != nil {
		return fmt.Errorf("record federation event: %w", err)
	}
	return nil
}

// ListFederationEvents returns federation events ordered by observed_at. When
// group is non-empty it filters to that group. Unless includeExpired is set,
// events whose expires_at is set and in the past relative to now (unix seconds)
// are excluded.
func (s *SQLiteStore) ListFederationEvents(group string, includeExpired bool, now int64) ([]EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errStoreClosed
	}
	query := `
		SELECT id, group_name, source_node, type, subject, dedupe_key, payload, observed_at, expires_at, recorded_at
		FROM federation_events
	`
	var (
		clauses []string
		args    []any
	)
	if group != "" {
		clauses = append(clauses, "group_name = ?")
		args = append(args, group)
	}
	if !includeExpired {
		// Keep rows with no expiry (0/NULL) or that have not yet expired.
		clauses = append(clauses, "(expires_at IS NULL OR expires_at = 0 OR expires_at >= ?)")
		args = append(args, now)
	}
	for i, clause := range clauses {
		if i == 0 {
			query += " WHERE "
		} else {
			query += " AND "
		}
		query += clause
	}
	query += " ORDER BY observed_at"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list federation events: %w", err)
	}
	defer rows.Close()
	var records []EventRecord
	for rows.Next() {
		var (
			rec        EventRecord
			payloadRaw string
			observedAt int64
			expiresAt  sql.NullInt64
			recordedAt int64
		)
		if err := rows.Scan(&rec.ID, &rec.Group, &rec.SourceNode, &rec.Type, &rec.Subject, &rec.DedupeKey, &payloadRaw, &observedAt, &expiresAt, &recordedAt); err != nil {
			return nil, fmt.Errorf("scan federation event: %w", err)
		}
		if payloadRaw != "" && payloadRaw != "{}" {
			if err := json.Unmarshal([]byte(payloadRaw), &rec.Payload); err != nil {
				return nil, fmt.Errorf("unmarshal payload: %w", err)
			}
		}
		rec.ObservedAt = time.Unix(observedAt, 0).UTC()
		rec.RecordedAt = time.Unix(recordedAt, 0).UTC()
		if expiresAt.Valid && expiresAt.Int64 > 0 {
			rec.ExpiresAt = time.Unix(expiresAt.Int64, 0).UTC()
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate federation events: %w", err)
	}
	return records, nil
}

// TODO(phase1+): PruneEvents honoring EventGroup retention (MaxEvents/MaxAge).
// Deferred from Phase 1 — listing already filters expired events at read time.
