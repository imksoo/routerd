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

const mobilityHeartbeatEventType = "routerd.mobility.member.heartbeat"

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
		if key, ok := mobilityHeartbeatDedupeKey(rec); ok {
			rec.DedupeKey = key
		} else {
			rec.DedupeKey = rec.ID
		}
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
	if isCompactableMobilityHeartbeat(rec) {
		if _, err := s.compactFederationHeartbeatsLocked(rec.Group, rec.DedupeKey); err != nil {
			return err
		}
	}
	return nil
}

// FederationHeartbeatCompactionStats reports duplicate compactable mobility
// heartbeat rows that should normally be removed by RecordFederationEvent.
type FederationHeartbeatCompactionStats struct {
	DuplicateRows int64
	Keys          []string
}

func (s *SQLiteStore) CompactFederationHeartbeats(group string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errStoreClosed
	}
	return s.compactFederationHeartbeatsLocked(group, "")
}

func (s *SQLiteStore) FederationHeartbeatCompactionStats(group string) (FederationHeartbeatCompactionStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return FederationHeartbeatCompactionStats{}, errStoreClosed
	}
	return s.federationHeartbeatCompactionStatsLocked(group)
}

func (s *SQLiteStore) compactFederationHeartbeatsLocked(group, dedupeKey string) (int64, error) {
	if group != "" && dedupeKey != "" {
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
		`, mobilityHeartbeatEventType, group, dedupeKey, mobilityHeartbeatEventType, group, dedupeKey)
		if err != nil {
			return 0, fmt.Errorf("compact federation heartbeats: %w", err)
		}
		n, _ := res.RowsAffected()
		return n, nil
	}

	where := `old.type = ? AND old.dedupe_key <> ''`
	args := []any{mobilityHeartbeatEventType}
	if group != "" {
		where += ` AND old.group_name = ?`
		args = append(args, group)
	}
	if dedupeKey != "" {
		where += ` AND old.dedupe_key = ?`
		args = append(args, dedupeKey)
	}
	query := fmt.Sprintf(`
		DELETE FROM federation_events AS old
		WHERE %s
		  AND EXISTS (
		    SELECT 1
		    FROM federation_events AS newer
		    WHERE newer.type = old.type
		      AND newer.group_name = old.group_name
		      AND newer.dedupe_key = old.dedupe_key
		      AND (
		        newer.observed_at > old.observed_at
		        OR (newer.observed_at = old.observed_at AND newer.recorded_at > old.recorded_at)
		        OR (newer.observed_at = old.observed_at AND newer.recorded_at = old.recorded_at AND newer.id > old.id)
		      )
		  )
	`, where)
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("compact federation heartbeats: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *SQLiteStore) federationHeartbeatCompactionStatsLocked(group string) (FederationHeartbeatCompactionStats, error) {
	query := `
		SELECT group_name, dedupe_key, count(*)
		FROM federation_events
		WHERE type = ? AND dedupe_key <> ''
	`
	args := []any{mobilityHeartbeatEventType}
	if group != "" {
		query += ` AND group_name = ?`
		args = append(args, group)
	}
	query += `
		GROUP BY group_name, dedupe_key
		HAVING count(*) > 1
		ORDER BY group_name, dedupe_key
	`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return FederationHeartbeatCompactionStats{}, fmt.Errorf("inspect federation heartbeat compaction: %w", err)
	}
	defer rows.Close()
	var stats FederationHeartbeatCompactionStats
	for rows.Next() {
		var groupName, key string
		var count int64
		if err := rows.Scan(&groupName, &key, &count); err != nil {
			return FederationHeartbeatCompactionStats{}, fmt.Errorf("scan federation heartbeat compaction stats: %w", err)
		}
		stats.DuplicateRows += count - 1
		stats.Keys = append(stats.Keys, groupName+"/"+key)
	}
	if err := rows.Err(); err != nil {
		return FederationHeartbeatCompactionStats{}, fmt.Errorf("iterate federation heartbeat compaction stats: %w", err)
	}
	return stats, nil
}

func isCompactableMobilityHeartbeat(rec EventRecord) bool {
	return rec.Type == mobilityHeartbeatEventType && strings.TrimSpace(rec.DedupeKey) != ""
}

func mobilityHeartbeatDedupeKey(rec EventRecord) (string, bool) {
	if rec.Type != mobilityHeartbeatEventType {
		return "", false
	}
	pool := strings.TrimSpace(rec.Payload["pool"])
	node := strings.TrimSpace(rec.Payload["node"])
	if pool == "" || node == "" {
		subjectPool, subjectNode, ok := strings.Cut(strings.TrimSpace(rec.Subject), "/")
		if ok && pool == "" {
			pool = strings.TrimSpace(subjectPool)
		}
		if ok && node == "" {
			node = strings.TrimSpace(subjectNode)
		}
	}
	if node == "" {
		node = strings.TrimSpace(rec.SourceNode)
	}
	if pool == "" || node == "" {
		return "", false
	}
	return "mobility-heartbeat:" + pool + ":" + node, true
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
