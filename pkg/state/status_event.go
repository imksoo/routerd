// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

// StatusEventBuilder evaluates a transition against the transaction's current
// status. It must be pure: it must not call the store or mutate either input.
// False rejects a stale write; a nil event permits a status-only update.
type StatusEventBuilder func(current, next map[string]any) (write bool, event *daemonapi.DaemonEvent)

// SaveObjectStatusAndEvent commits a status and its required transition history
// together. The returned event has its persisted cursor; a nil event means there
// is no transition to deliver. Delivery happens only after this method returns.
func (s *SQLiteStore) SaveObjectStatusAndEvent(apiVersion, kind, name string, status map[string]any, build StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	return s.writeObjectStatusAndEvent(apiVersion, kind, name, status, build, false, nil)
}

// MergeObjectStatusAndEvent retains fields written by other status producers,
// with the same merge semantics as MergeObjectStatus.
func (s *SQLiteStore) MergeObjectStatusAndEvent(apiVersion, kind, name string, updates map[string]any, build StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	return s.writeObjectStatusAndEvent(apiVersion, kind, name, updates, build, true, nil)
}

type pdObservationUpdate struct {
	expected PDLeaseConfig
	lease    PDLease
	snapshot *pdclient.Snapshot
	observed *daemonapi.ResourceStatus
}

func (s *SQLiteStore) writeObjectStatusAndEvent(apiVersion, kind, name string, status map[string]any, build StatusEventBuilder, merge bool, update *pdObservationUpdate) (*daemonapi.DaemonEvent, error) {
	if build == nil {
		return nil, fmt.Errorf("status event builder is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	// Reserve enough journal capacity before entering the atomic status/event
	// transaction. SQLite maintenance cannot run through the single DB
	// connection while that transaction is open.
	const maxStatusEventPayload = maxJournalMessageBytes + maxJournalAttributesBytes + 4096
	if err := s.ensureEventJournalCapacityLocked(maxStatusEventPayload); err != nil {
		return nil, s.noteStorageWriteErrorLocked(err)
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
	write := true
	var event *daemonapi.DaemonEvent
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6PrefixDelegation" {
		var document struct {
			Observation *PDLease `json:"observation"`
		}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &document); err != nil {
				return nil, err
			}
		}
		previous := PDLease{}
		if document.Observation != nil {
			previous = *document.Observation
		}
		if update != nil {
			if previous.Config() != update.expected {
				return nil, ErrStalePDLeaseSource
			}
			next := update.lease
			if samePDConfiguredSource(next.Config(), previous.Config()) && next.SnapshotUpdatedAt.Before(previous.SnapshotUpdatedAt) {
				return nil, nil
			}
			if update.snapshot != nil {
				if samePDConfiguredSource(next.Config(), previous.Config()) && update.snapshot.UpdatedAt.Before(previous.SnapshotUpdatedAt) {
					return nil, nil
				}
				if err := ValidatePDLeaseSnapshot(next, name, *update.snapshot); err != nil {
					return nil, err
				}
				next = ObservePDLease(next, *update.snapshot, s.now().UTC())
			} else {
				next = next.Live(s.now().UTC())
			}
			if err := validatePDLease(next); err != nil {
				return nil, err
			}
			status = cleanPDStatus(current)
			status["observation"] = next
			if update.snapshot != nil {
				status["observed"] = pdDaemonObserved(*update.snapshot, update.observed)
			} else if !samePDConfiguredSource(next.Config(), previous.Config()) {
				delete(status, "observed")
			}
			previousView := map[string]any{}
			nextView := map[string]any{}
			for key, value := range current {
				previousView[key] = value
			}
			for key, value := range status {
				nextView[key] = value
			}
			if err := projectCommittedPDStatus(previousView, document.Observation); err != nil {
				return nil, err
			}
			if err := projectCommittedPDStatus(nextView, &next); err != nil {
				return nil, err
			}
			delete(previousView, "observation")
			delete(nextView, "observation")
			write, event = build(previousView, nextView)
		} else {
			status = cleanPDStatus(status)
			if document.Observation != nil {
				status["observation"] = *document.Observation
			}
			if observed, ok := current["observed"]; ok {
				status["observed"] = observed
			} else {
				delete(status, "observed")
			}
			previousView := map[string]any{}
			nextView := map[string]any{}
			for key, value := range current {
				previousView[key] = value
			}
			for key, value := range status {
				nextView[key] = value
			}
			if err := projectTypedPDStatus(previousView, document.Observation, s.now().UTC()); err != nil {
				return nil, err
			}
			if err := projectTypedPDStatus(nextView, document.Observation, s.now().UTC()); err != nil {
				return nil, err
			}
			write, event = build(previousView, nextView)
		}
	} else {
		write, event = build(current, status)
	}
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
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6PrefixDelegation" {
		currentData = []byte(raw)
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
	var eventPayloadBytes int64
	if event != nil {
		prepared := *event
		if prepared.Time.IsZero() {
			prepared.Time = now
		}
		event = &prepared
		cursor, eventPayloadBytes, err = recordBusEvent(tx, s.generation, now, *event)
		if err != nil {
			return nil, s.noteStorageWriteErrorLocked(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, s.noteStorageWriteErrorLocked(err)
	}
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked(kind)
	if event == nil {
		return nil, nil
	}
	s.eventJournalRows++
	s.eventJournalPayloadBytes += eventPayloadBytes
	committed := *event
	committed.Cursor = cursor
	return &committed, nil
}

// SavePDLeaseObservationAndEvent is the single observation/event commit owner.
// expected binds the producer's read to its source; config changes and the new
// observation commit together, before local publication or EventRule delivery.
func (s *SQLiteStore) SavePDLeaseObservationAndEvent(name string, expected PDLeaseConfig, lease PDLease, snapshot *pdclient.Snapshot, observed daemonapi.ResourceStatus, build StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	return s.writeObjectStatusAndEvent("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", name, nil, build, false, &pdObservationUpdate{expected: expected, lease: lease, snapshot: snapshot, observed: &observed})
}
