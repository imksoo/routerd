// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"fmt"
	"strings"
	"time"
)

// MobilityDeprovisionMarkerRecord is durable internal work-item state for
// provider de-provision actions. It intentionally lives outside
// DynamicConfigPart resources so a disappearing RemoteAddressClaim cannot erase
// the required unassign/forwarding-disable intent before the action journal
// imports and executes it.
type MobilityDeprovisionMarkerRecord struct {
	Key            string    `json:"key" yaml:"key"`
	Source         string    `json:"source" yaml:"source"`
	IdempotencyKey string    `json:"idempotencyKey" yaml:"idempotencyKey"`
	Action         string    `json:"action" yaml:"action"`
	ActionPlanJSON string    `json:"actionPlanJSON" yaml:"actionPlanJSON"`
	CreatedAt      time.Time `json:"createdAt" yaml:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt" yaml:"updatedAt"`
}

func (s *SQLiteStore) ensureMobilityDeprovisionMarkerColumns() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS mobility_deprovision_markers (
  marker_key TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  action TEXT NOT NULL,
  actionplan_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS mobility_deprovision_markers_source ON mobility_deprovision_markers(source, marker_key);
`)
	return err
}

func (s *SQLiteStore) UpsertMobilityDeprovisionMarker(rec MobilityDeprovisionMarkerRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	rec.Key = strings.TrimSpace(rec.Key)
	rec.Source = strings.TrimSpace(rec.Source)
	rec.IdempotencyKey = strings.TrimSpace(rec.IdempotencyKey)
	rec.Action = strings.TrimSpace(rec.Action)
	rec.ActionPlanJSON = strings.TrimSpace(rec.ActionPlanJSON)
	if rec.Key == "" {
		rec.Key = rec.IdempotencyKey
	}
	if rec.Key == "" {
		return fmt.Errorf("mobility deprovision marker key is required")
	}
	if rec.Source == "" {
		return fmt.Errorf("mobility deprovision marker source is required")
	}
	if rec.IdempotencyKey == "" {
		return fmt.Errorf("mobility deprovision marker idempotencyKey is required")
	}
	if rec.Action == "" {
		return fmt.Errorf("mobility deprovision marker action is required")
	}
	if rec.ActionPlanJSON == "" {
		return fmt.Errorf("mobility deprovision marker actionPlanJSON is required")
	}
	now := s.now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	_, err := s.db.Exec(`
INSERT INTO mobility_deprovision_markers(marker_key, source, idempotency_key, action, actionplan_json, created_at, updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(marker_key) DO UPDATE SET
  source = excluded.source,
  idempotency_key = excluded.idempotency_key,
  action = excluded.action,
  actionplan_json = excluded.actionplan_json,
  updated_at = excluded.updated_at
`, rec.Key, rec.Source, rec.IdempotencyKey, rec.Action, rec.ActionPlanJSON, formatStateTime(rec.CreatedAt), formatStateTime(rec.UpdatedAt))
	return err
}

func (s *SQLiteStore) ListMobilityDeprovisionMarkers(source string) ([]MobilityDeprovisionMarkerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errStoreClosed
	}
	query := `SELECT marker_key, source, idempotency_key, action, actionplan_json, created_at, updated_at FROM mobility_deprovision_markers`
	var args []any
	if strings.TrimSpace(source) != "" {
		query += ` WHERE source = ?`
		args = append(args, strings.TrimSpace(source))
	}
	query += ` ORDER BY source, marker_key`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MobilityDeprovisionMarkerRecord
	for rows.Next() {
		var rec MobilityDeprovisionMarkerRecord
		var created, updated string
		if err := rows.Scan(&rec.Key, &rec.Source, &rec.IdempotencyKey, &rec.Action, &rec.ActionPlanJSON, &created, &updated); err != nil {
			return nil, err
		}
		rec.CreatedAt = parseStateTime(created)
		rec.UpdatedAt = parseStateTime(updated)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteStore) DeleteMobilityDeprovisionMarker(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	_, err := s.db.Exec(`DELETE FROM mobility_deprovision_markers WHERE marker_key = ?`, strings.TrimSpace(key))
	return err
}
