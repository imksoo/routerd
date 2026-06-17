// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// errStoreClosed is returned by the federation event store methods once the
// underlying SQLiteStore has been closed.
var errStoreClosed = errors.New("state store is closed")

const (
	mobilityObservedEventType       = "routerd.client.ipv4.observed"
	mobilityProviderDiscoverySource = "provider-discovery"
)

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
		ON CONFLICT(id) DO UPDATE SET
			source_node = excluded.source_node,
			observed_at = MAX(excluded.observed_at, federation_events.observed_at),
			expires_at  = MAX(excluded.expires_at,  federation_events.expires_at),
			recorded_at = excluded.recorded_at
	`, rec.ID, rec.Group, rec.SourceNode, rec.Type, rec.Subject, rec.DedupeKey, payloadJSON, rec.ObservedAt.UTC().Unix(), expiresAt, rec.RecordedAt.UTC().Unix())
	if err != nil {
		return fmt.Errorf("record federation event: %w", err)
	}
	if isCompactableProviderDiscoveryObserved(rec) {
		if _, err := s.compactFederationEventsByTypeAndDedupeLocked(rec.Type, rec.Group, rec.DedupeKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) compactFederationEventsByTypeAndDedupeLocked(eventType, group, dedupeKey string) (int64, error) {
	res, err := s.db.Exec(`
			DELETE FROM federation_events
			WHERE type = ?
			  AND group_name = ?
			  AND dedupe_key = ?
			  AND id NOT IN (
			    SELECT id
			    FROM federation_events
			    WHERE type = ?
			      AND group_name = ?
			      AND dedupe_key = ?
			    ORDER BY observed_at DESC, recorded_at DESC, id DESC
			    LIMIT 1
			  )
		`, eventType, group, dedupeKey, eventType, group, dedupeKey)
	if err != nil {
		return 0, fmt.Errorf("compact federation events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func isCompactableProviderDiscoveryObserved(rec EventRecord) bool {
	return rec.Type == mobilityObservedEventType &&
		strings.TrimSpace(rec.DedupeKey) != "" &&
		strings.TrimSpace(rec.Payload["source"]) == mobilityProviderDiscoverySource
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

// PruneFederationEvents enforces EventGroup retention (ADR 0006). It removes
// rows older than maxAge (when maxAge > 0) and, when maxEvents > 0, trims the
// group down to the newest maxEvents rows (by observed_at desc, id desc). It
// returns the total number of rows deleted.
//
// group=="" means all groups. For an empty group the maxAge prune is applied
// across the whole table, but the maxEvents cap is intentionally SKIPPED:
// capping a mixed-group table to N newest rows would silently delete events
// from other groups, so per-group retention requires a group. Callers iterate
// per EventGroup to enforce maxEvents.
func (s *SQLiteStore) PruneFederationEvents(group string, maxAge time.Duration, maxEvents int, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errStoreClosed
	}
	var total int64
	if maxAge > 0 {
		cutoff := now.UTC().Add(-maxAge).Unix()
		var (
			res sql.Result
			err error
		)
		if group != "" {
			res, err = s.db.Exec(`DELETE FROM federation_events WHERE group_name = ? AND observed_at < ?`, group, cutoff)
		} else {
			res, err = s.db.Exec(`DELETE FROM federation_events WHERE observed_at < ?`, cutoff)
		}
		if err != nil {
			return total, fmt.Errorf("prune federation events by age: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			total += n
		}
	}
	if maxEvents > 0 && group != "" {
		res, err := s.db.Exec(`
			DELETE FROM federation_events
			WHERE group_name = ?
			  AND id NOT IN (
			    SELECT id FROM federation_events
			    WHERE group_name = ?
			    ORDER BY observed_at DESC, id DESC
			    LIMIT ?
			  )
		`, group, group, maxEvents)
		if err != nil {
			return total, fmt.Errorf("prune federation events by count: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			total += n
		}
	}
	return total, nil
}
