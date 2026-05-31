// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MobilityCaptureEpochRecord is the local fencing-token state for one physical
// capture holder selection. It is deliberately separate from AddressLease:
// AddressLease.Epoch tracks the observed address owner, while CaptureEpoch
// tracks the node currently selected to hold provider/on-link capture.
type MobilityCaptureEpochRecord struct {
	CaptureKey    string    `json:"captureKey" yaml:"captureKey"`
	Pool          string    `json:"pool" yaml:"pool"`
	Address       string    `json:"address" yaml:"address"`
	CaptureDomain string    `json:"captureDomain" yaml:"captureDomain"`
	Holder        string    `json:"holder" yaml:"holder"`
	Epoch         int64     `json:"epoch" yaml:"epoch"`
	CreatedAt     time.Time `json:"createdAt" yaml:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt" yaml:"updatedAt"`
}

func (s *SQLiteStore) ensureMobilityCaptureEpochColumns() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS mobility_capture_epochs (
  capture_key TEXT PRIMARY KEY,
  pool TEXT NOT NULL,
  address TEXT NOT NULL,
  capture_domain TEXT NOT NULL,
  holder TEXT NOT NULL,
  epoch INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS mobility_capture_epochs_pool ON mobility_capture_epochs(pool, address, capture_domain);
`)
	return err
}

// ReconcileMobilityCaptureEpochs records the current holder projection and
// bumps the epoch only when that holder changes.
func (s *SQLiteStore) ReconcileMobilityCaptureEpochs(desired []MobilityCaptureEpochRecord) ([]MobilityCaptureEpochRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errStoreClosed
	}
	now := s.now().UTC()
	out := make([]MobilityCaptureEpochRecord, 0, len(desired))
	for _, rec := range desired {
		rec = normalizeMobilityCaptureEpoch(rec)
		if rec.CaptureKey == "" {
			return nil, fmt.Errorf("mobility capture epoch key is required")
		}
		if rec.Pool == "" || rec.Address == "" || rec.CaptureDomain == "" || rec.Holder == "" {
			return nil, fmt.Errorf("mobility capture epoch %q requires pool, address, captureDomain, and holder", rec.CaptureKey)
		}
		current, ok, err := s.getMobilityCaptureEpochLocked(rec.CaptureKey)
		if err != nil {
			return nil, err
		}
		if ok {
			rec.CreatedAt = current.CreatedAt
			rec.Epoch = current.Epoch
			if current.Holder != rec.Holder {
				rec.Epoch++
			}
		} else {
			rec.CreatedAt = now
			rec.Epoch = 1
		}
		rec.UpdatedAt = now
		if _, err := s.db.Exec(`
INSERT INTO mobility_capture_epochs(capture_key,pool,address,capture_domain,holder,epoch,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(capture_key) DO UPDATE SET
  pool = excluded.pool,
  address = excluded.address,
  capture_domain = excluded.capture_domain,
  holder = excluded.holder,
  epoch = excluded.epoch,
  updated_at = excluded.updated_at
`, rec.CaptureKey, rec.Pool, rec.Address, rec.CaptureDomain, rec.Holder, rec.Epoch, formatStateTime(rec.CreatedAt), formatStateTime(rec.UpdatedAt)); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *SQLiteStore) GetMobilityCaptureEpoch(key string) (MobilityCaptureEpochRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return MobilityCaptureEpochRecord{}, false, errStoreClosed
	}
	return s.getMobilityCaptureEpochLocked(strings.TrimSpace(key))
}

func (s *SQLiteStore) getMobilityCaptureEpochLocked(key string) (MobilityCaptureEpochRecord, bool, error) {
	if strings.TrimSpace(key) == "" {
		return MobilityCaptureEpochRecord{}, false, nil
	}
	row := s.db.QueryRow(`SELECT capture_key,pool,address,capture_domain,holder,epoch,created_at,updated_at FROM mobility_capture_epochs WHERE capture_key = ?`, strings.TrimSpace(key))
	rec, err := scanMobilityCaptureEpoch(row.Scan)
	if err == sql.ErrNoRows {
		return MobilityCaptureEpochRecord{}, false, nil
	}
	if err != nil {
		return MobilityCaptureEpochRecord{}, false, err
	}
	return rec, true, nil
}

func scanMobilityCaptureEpoch(scan func(...any) error) (MobilityCaptureEpochRecord, error) {
	var rec MobilityCaptureEpochRecord
	var created, updated string
	if err := scan(&rec.CaptureKey, &rec.Pool, &rec.Address, &rec.CaptureDomain, &rec.Holder, &rec.Epoch, &created, &updated); err != nil {
		return MobilityCaptureEpochRecord{}, err
	}
	rec.CreatedAt = parseStateTime(created)
	rec.UpdatedAt = parseStateTime(updated)
	return rec, nil
}

func normalizeMobilityCaptureEpoch(rec MobilityCaptureEpochRecord) MobilityCaptureEpochRecord {
	rec.CaptureKey = strings.TrimSpace(rec.CaptureKey)
	rec.Pool = strings.TrimSpace(rec.Pool)
	rec.Address = strings.TrimSpace(rec.Address)
	rec.CaptureDomain = strings.TrimSpace(rec.CaptureDomain)
	rec.Holder = strings.TrimSpace(rec.Holder)
	return rec
}
