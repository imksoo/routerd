// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	AddressLeaseStatusActive  = "active"
	AddressLeaseStatusHolding = "holding"
	AddressLeaseStatusExpired = "expired"
)

// AddressLeaseRecord is derived runtime state for the CloudEdge Mobility
// Control Plane. It is projected from federation events and is never
// operator-authored configuration.
type AddressLeaseRecord struct {
	Pool       string    `json:"pool" yaml:"pool"`
	Address    string    `json:"address" yaml:"address"`
	Status     string    `json:"status" yaml:"status"`
	OwnerNode  string    `json:"ownerNode" yaml:"ownerNode"`
	OwnerSite  string    `json:"ownerSite" yaml:"ownerSite"`
	OwnerRole  string    `json:"ownerRole,omitempty" yaml:"ownerRole,omitempty"`
	Epoch      int64     `json:"epoch" yaml:"epoch"`
	ObservedAt time.Time `json:"observedAt,omitempty" yaml:"observedAt,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`

	SourceEventID string `json:"sourceEventId,omitempty" yaml:"sourceEventId,omitempty"`
	SourceGroup   string `json:"sourceGroup,omitempty" yaml:"sourceGroup,omitempty"`
	SourceType    string `json:"sourceType,omitempty" yaml:"sourceType,omitempty"`
	DedupeKey     string `json:"dedupeKey,omitempty" yaml:"dedupeKey,omitempty"`

	CandidateOwnerNode  string    `json:"candidateOwnerNode,omitempty" yaml:"candidateOwnerNode,omitempty"`
	CandidateOwnerSite  string    `json:"candidateOwnerSite,omitempty" yaml:"candidateOwnerSite,omitempty"`
	CandidateOwnerRole  string    `json:"candidateOwnerRole,omitempty" yaml:"candidateOwnerRole,omitempty"`
	CandidateEventID    string    `json:"candidateEventId,omitempty" yaml:"candidateEventId,omitempty"`
	CandidateGroup      string    `json:"candidateGroup,omitempty" yaml:"candidateGroup,omitempty"`
	CandidateType       string    `json:"candidateType,omitempty" yaml:"candidateType,omitempty"`
	CandidateDedupeKey  string    `json:"candidateDedupeKey,omitempty" yaml:"candidateDedupeKey,omitempty"`
	CandidateObservedAt time.Time `json:"candidateObservedAt,omitempty" yaml:"candidateObservedAt,omitempty"`
	CandidateExpiresAt  time.Time `json:"candidateExpiresAt,omitempty" yaml:"candidateExpiresAt,omitempty"`
	ConflictReason      string    `json:"conflictReason,omitempty" yaml:"conflictReason,omitempty"`

	RecordedAt time.Time `json:"recordedAt" yaml:"recordedAt"`
	UpdatedAt  time.Time `json:"updatedAt" yaml:"updatedAt"`
}

type AddressLeaseStore interface {
	UpsertAddressLease(AddressLeaseRecord) error
	ListAddressLeases(pool string, includeExpired bool, now time.Time) ([]AddressLeaseRecord, error)
	GetAddressLease(pool, address string) (AddressLeaseRecord, bool, error)
}

func (s *SQLiteStore) UpsertAddressLease(rec AddressLeaseRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	rec.Pool = strings.TrimSpace(rec.Pool)
	rec.Address = strings.TrimSpace(rec.Address)
	if rec.Pool == "" {
		return fmt.Errorf("address lease pool is required")
	}
	if rec.Address == "" {
		return fmt.Errorf("address lease address is required")
	}
	if strings.TrimSpace(rec.Status) == "" {
		rec.Status = AddressLeaseStatusActive
	}
	if rec.Epoch <= 0 {
		rec.Epoch = 1
	}
	now := s.now().UTC()
	if rec.RecordedAt.IsZero() {
		rec.RecordedAt = now
	}
	rec.UpdatedAt = now
	_, err := s.db.Exec(`
		INSERT INTO address_leases (
		  pool, address, status, owner_node, owner_site, owner_role,
		  source_event_id, source_group, source_type, dedupe_key, epoch,
		  observed_at, expires_at,
		  candidate_owner_node, candidate_owner_site, candidate_owner_role,
		  candidate_event_id, candidate_group, candidate_type, candidate_dedupe_key,
		  candidate_observed_at, candidate_expires_at, conflict_reason,
		  recorded_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pool, address) DO UPDATE SET
		  status = excluded.status,
		  owner_node = excluded.owner_node,
		  owner_site = excluded.owner_site,
		  owner_role = excluded.owner_role,
		  source_event_id = excluded.source_event_id,
		  source_group = excluded.source_group,
		  source_type = excluded.source_type,
		  dedupe_key = excluded.dedupe_key,
		  epoch = excluded.epoch,
		  observed_at = excluded.observed_at,
		  expires_at = excluded.expires_at,
		  candidate_owner_node = excluded.candidate_owner_node,
		  candidate_owner_site = excluded.candidate_owner_site,
		  candidate_owner_role = excluded.candidate_owner_role,
		  candidate_event_id = excluded.candidate_event_id,
		  candidate_group = excluded.candidate_group,
		  candidate_type = excluded.candidate_type,
		  candidate_dedupe_key = excluded.candidate_dedupe_key,
		  candidate_observed_at = excluded.candidate_observed_at,
		  candidate_expires_at = excluded.candidate_expires_at,
		  conflict_reason = excluded.conflict_reason,
		  updated_at = excluded.updated_at
	`, rec.Pool, rec.Address, rec.Status, rec.OwnerNode, rec.OwnerSite, rec.OwnerRole,
		rec.SourceEventID, rec.SourceGroup, rec.SourceType, rec.DedupeKey, rec.Epoch,
		timeString(rec.ObservedAt), timeString(rec.ExpiresAt),
		rec.CandidateOwnerNode, rec.CandidateOwnerSite, rec.CandidateOwnerRole,
		rec.CandidateEventID, rec.CandidateGroup, rec.CandidateType, rec.CandidateDedupeKey,
		timeString(rec.CandidateObservedAt), timeString(rec.CandidateExpiresAt), rec.ConflictReason,
		timeString(rec.RecordedAt), timeString(rec.UpdatedAt))
	if err != nil {
		return fmt.Errorf("upsert address lease: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListAddressLeases(pool string, includeExpired bool, now time.Time) ([]AddressLeaseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errStoreClosed
	}
	query := addressLeaseSelect()
	var clauses []string
	var args []any
	if pool = strings.TrimSpace(pool); pool != "" {
		clauses = append(clauses, "pool = ?")
		args = append(args, pool)
	}
	if !includeExpired {
		clauses = append(clauses, "(status != ? AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?))")
		args = append(args, AddressLeaseStatusExpired, timeString(now.UTC()))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY pool, address"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list address leases: %w", err)
	}
	defer rows.Close()
	return scanAddressLeaseRows(rows)
}

func (s *SQLiteStore) GetAddressLease(pool, address string) (AddressLeaseRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return AddressLeaseRecord{}, false, errStoreClosed
	}
	row := s.db.QueryRow(addressLeaseSelect()+" WHERE pool = ? AND address = ?", strings.TrimSpace(pool), strings.TrimSpace(address))
	rec, err := scanAddressLeaseRow(row)
	if err == nil {
		return rec, true, nil
	}
	if err == sql.ErrNoRows {
		return AddressLeaseRecord{}, false, nil
	}
	return AddressLeaseRecord{}, false, err
}

type addressLeaseScanner interface {
	Scan(dest ...any) error
}

func addressLeaseSelect() string {
	return `SELECT
		pool, address, status, owner_node, owner_site, coalesce(owner_role, ''),
		coalesce(source_event_id, ''), coalesce(source_group, ''), coalesce(source_type, ''), coalesce(dedupe_key, ''),
		epoch, coalesce(observed_at, ''), coalesce(expires_at, ''),
		coalesce(candidate_owner_node, ''), coalesce(candidate_owner_site, ''), coalesce(candidate_owner_role, ''),
		coalesce(candidate_event_id, ''), coalesce(candidate_group, ''), coalesce(candidate_type, ''), coalesce(candidate_dedupe_key, ''),
		coalesce(candidate_observed_at, ''), coalesce(candidate_expires_at, ''), coalesce(conflict_reason, ''),
		recorded_at, updated_at
		FROM address_leases`
}

func scanAddressLeaseRows(rows *sql.Rows) ([]AddressLeaseRecord, error) {
	var out []AddressLeaseRecord
	for rows.Next() {
		rec, err := scanAddressLeaseRow(rows)
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

func scanAddressLeaseRow(row addressLeaseScanner) (AddressLeaseRecord, error) {
	var rec AddressLeaseRecord
	var observedAt, expiresAt, candidateObservedAt, candidateExpiresAt, recordedAt, updatedAt string
	if err := row.Scan(
		&rec.Pool, &rec.Address, &rec.Status, &rec.OwnerNode, &rec.OwnerSite, &rec.OwnerRole,
		&rec.SourceEventID, &rec.SourceGroup, &rec.SourceType, &rec.DedupeKey,
		&rec.Epoch, &observedAt, &expiresAt,
		&rec.CandidateOwnerNode, &rec.CandidateOwnerSite, &rec.CandidateOwnerRole,
		&rec.CandidateEventID, &rec.CandidateGroup, &rec.CandidateType, &rec.CandidateDedupeKey,
		&candidateObservedAt, &candidateExpiresAt, &rec.ConflictReason,
		&recordedAt, &updatedAt,
	); err != nil {
		return AddressLeaseRecord{}, err
	}
	rec.ObservedAt = parseTimeString(observedAt)
	rec.ExpiresAt = parseTimeString(expiresAt)
	rec.CandidateObservedAt = parseTimeString(candidateObservedAt)
	rec.CandidateExpiresAt = parseTimeString(candidateExpiresAt)
	rec.RecordedAt = parseTimeString(recordedAt)
	rec.UpdatedAt = parseTimeString(updatedAt)
	return rec, nil
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimeString(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return t
}
