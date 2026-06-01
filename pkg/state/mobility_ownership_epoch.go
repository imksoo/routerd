// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MobilityOwnershipEpochRecord is the converged ownership fence for one
// MobilityPool address. It bumps only when the confirmed owner node changes.
type MobilityOwnershipEpochRecord struct {
	Pool      string    `json:"pool" yaml:"pool"`
	Address   string    `json:"address" yaml:"address"`
	OwnerNode string    `json:"ownerNode" yaml:"ownerNode"`
	Epoch     int64     `json:"epoch" yaml:"epoch"`
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" yaml:"updatedAt"`
}

func (s *SQLiteStore) ensureMobilityOwnershipEpochColumns() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS mobility_ownership_epochs (
  pool TEXT NOT NULL,
  address TEXT NOT NULL,
  owner_node TEXT NOT NULL,
  epoch INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(pool, address)
);
CREATE INDEX IF NOT EXISTS mobility_ownership_epochs_owner ON mobility_ownership_epochs(owner_node, pool);
`)
	return err
}

// ReconcileMobilityOwnershipEpochs records the current owner projection and
// bumps the epoch only when that owner changes.
func (s *SQLiteStore) ReconcileMobilityOwnershipEpochs(desired []MobilityOwnershipEpochRecord) ([]MobilityOwnershipEpochRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errStoreClosed
	}
	now := s.now().UTC()
	out := make([]MobilityOwnershipEpochRecord, 0, len(desired))
	for _, rec := range desired {
		rec = normalizeMobilityOwnershipEpoch(rec)
		if rec.Pool == "" || rec.Address == "" || rec.OwnerNode == "" {
			return nil, fmt.Errorf("mobility ownership epoch requires pool, address, and ownerNode")
		}
		current, ok, err := s.getMobilityOwnershipEpochLocked(rec.Pool, rec.Address)
		if err != nil {
			return nil, err
		}
		if ok {
			rec.CreatedAt = current.CreatedAt
			rec.Epoch = current.Epoch
			if current.OwnerNode != rec.OwnerNode {
				rec.Epoch++
			}
		} else {
			rec.CreatedAt = now
			rec.Epoch = 1
		}
		rec.UpdatedAt = now
		if _, err := s.db.Exec(`
INSERT INTO mobility_ownership_epochs(pool,address,owner_node,epoch,created_at,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(pool,address) DO UPDATE SET
  owner_node = excluded.owner_node,
  epoch = excluded.epoch,
  updated_at = excluded.updated_at
`, rec.Pool, rec.Address, rec.OwnerNode, rec.Epoch, formatStateTime(rec.CreatedAt), formatStateTime(rec.UpdatedAt)); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *SQLiteStore) GetMobilityOwnershipEpoch(pool, address string) (MobilityOwnershipEpochRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return MobilityOwnershipEpochRecord{}, false, errStoreClosed
	}
	return s.getMobilityOwnershipEpochLocked(strings.TrimSpace(pool), strings.TrimSpace(address))
}

func (s *SQLiteStore) ListMobilityOwnershipEpochs(pool string) ([]MobilityOwnershipEpochRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errStoreClosed
	}
	query := `SELECT pool,address,owner_node,epoch,created_at,updated_at FROM mobility_ownership_epochs`
	args := []any{}
	if strings.TrimSpace(pool) != "" {
		query += ` WHERE pool = ?`
		args = append(args, strings.TrimSpace(pool))
	}
	query += ` ORDER BY pool,address`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MobilityOwnershipEpochRecord
	for rows.Next() {
		rec, err := scanMobilityOwnershipEpoch(rows.Scan)
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

func (s *SQLiteStore) getMobilityOwnershipEpochLocked(pool, address string) (MobilityOwnershipEpochRecord, bool, error) {
	if strings.TrimSpace(pool) == "" || strings.TrimSpace(address) == "" {
		return MobilityOwnershipEpochRecord{}, false, nil
	}
	row := s.db.QueryRow(`SELECT pool,address,owner_node,epoch,created_at,updated_at FROM mobility_ownership_epochs WHERE pool = ? AND address = ?`, strings.TrimSpace(pool), strings.TrimSpace(address))
	rec, err := scanMobilityOwnershipEpoch(row.Scan)
	if err == sql.ErrNoRows {
		return MobilityOwnershipEpochRecord{}, false, nil
	}
	if err != nil {
		return MobilityOwnershipEpochRecord{}, false, err
	}
	return rec, true, nil
}

func scanMobilityOwnershipEpoch(scan func(...any) error) (MobilityOwnershipEpochRecord, error) {
	var rec MobilityOwnershipEpochRecord
	var created, updated string
	if err := scan(&rec.Pool, &rec.Address, &rec.OwnerNode, &rec.Epoch, &created, &updated); err != nil {
		return MobilityOwnershipEpochRecord{}, err
	}
	rec.CreatedAt = parseStateTime(created)
	rec.UpdatedAt = parseStateTime(updated)
	return rec, nil
}

func normalizeMobilityOwnershipEpoch(rec MobilityOwnershipEpochRecord) MobilityOwnershipEpochRecord {
	rec.Pool = strings.TrimSpace(rec.Pool)
	rec.Address = strings.TrimSpace(rec.Address)
	rec.OwnerNode = strings.TrimSpace(rec.OwnerNode)
	return rec
}
