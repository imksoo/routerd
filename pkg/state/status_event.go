// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

// StatusEventBuilder evaluates a transition against the transaction's current
// status. It must be pure: it must not call the store or mutate either input.
// False rejects a stale write; a nil event permits a status-only update.
type StatusEventBuilder func(current, next map[string]any) (write bool, event *daemonapi.DaemonEvent)

// SaveObjectStatusAndEvent commits a status and its required transition history
// together. The returned event has its persisted cursor; a nil event means there
// is no transition to deliver. Delivery happens only after this method returns.
func (s *SQLiteStore) SaveObjectStatusAndEvent(apiVersion, kind, name string, status map[string]any, build StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	return s.writeObjectStatusAndEvent(apiVersion, kind, name, status, build, false)
}

// MergeObjectStatusAndEvent retains fields written by other status producers,
// with the same merge semantics as MergeObjectStatus.
func (s *SQLiteStore) MergeObjectStatusAndEvent(apiVersion, kind, name string, updates map[string]any, build StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	return s.writeObjectStatusAndEvent(apiVersion, kind, name, updates, build, true)
}

func (s *SQLiteStore) writeObjectStatusAndEvent(apiVersion, kind, name string, status map[string]any, build StatusEventBuilder, merge bool) (*daemonapi.DaemonEvent, error) {
	if build == nil {
		return nil, fmt.Errorf("status event builder is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw string
	var generation sql.NullInt64
	err = tx.QueryRow(`SELECT coalesce(status,'{}'), observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&raw, &generation)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			return nil, fmt.Errorf("decode status for transactional update: %w", err)
		}
	}
	if merge {
		next := map[string]any{}
		for key, value := range current {
			next[key] = value
		}
		for key, value := range status {
			next[key] = value
		}
		status = next
	}
	write, event := build(current, status)
	if !write {
		return nil, nil
	}
	data, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	currentData, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	sameStatus := found && string(currentData) == string(data)
	sameGeneration := (!generation.Valid && s.generation == 0) || (s.generation != 0 && generation.Valid && generation.Int64 == s.generation)
	if sameStatus && sameGeneration {
		s.statusSkipCount++
		s.incrementStatusKindSkipLocked(kind)
		return nil, nil
	}
	// Observed-generation refresh is required even if status is unchanged;
	// it is not itself a new status transition.
	if sameStatus {
		event = nil
	}
	if event != nil && (event.Resource == nil || event.Resource.APIVersion != apiVersion || event.Resource.Kind != kind || event.Resource.Name != name) {
		return nil, fmt.Errorf("status event must reference the updated object")
	}
	now := s.now().UTC()
	_, err = tx.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at)
VALUES(?,?,?,?,1,?,?,?,?)
ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,observed_generation=excluded.observed_generation,status=excluded.status,modified_at=excluded.modified_at`,
		apiVersion, kind, name, apiVersion+"/"+kind+"/"+name, nullGeneration(s.generation), string(data), formatStateTime(now), formatStateTime(now))
	if err != nil {
		return nil, err
	}
	var cursor string
	if event != nil {
		prepared := *event
		if prepared.Time.IsZero() {
			prepared.Time = now
		}
		event = &prepared
		cursor, err = recordBusEvent(tx, s.generation, now, *event)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked(kind)
	if event == nil {
		return nil, nil
	}
	committed := *event
	committed.Cursor = cursor
	return &committed, nil
}
