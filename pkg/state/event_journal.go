// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

const (
	defaultEventJournalMaxRows         = int64(100_000)
	defaultEventJournalMaxPayloadBytes = int64(64 * 1024 * 1024)
	defaultEventJournalMaxAge          = 24 * time.Hour
	eventJournalAgePruneInterval       = time.Hour
	eventJournalPruneBatchRows         = int64(10_000)
	maxJournalMessageBytes             = 64 * 1024
	maxJournalAttributesBytes          = 256 * 1024
)

const eventJournalPayloadSQL = `
coalesce(length(cast(api_version AS blob)),0)+coalesce(length(cast(kind AS blob)),0)+coalesce(length(cast(name AS blob)),0)+
coalesce(length(cast(type AS blob)),0)+coalesce(length(cast(reason AS blob)),0)+coalesce(length(cast(message AS blob)),0)+
coalesce(length(cast(topic AS blob)),0)+coalesce(length(cast(source_kind AS blob)),0)+coalesce(length(cast(source_instance AS blob)),0)+
coalesce(length(cast(resource_api_version AS blob)),0)+coalesce(length(cast(resource_kind AS blob)),0)+
coalesce(length(cast(resource_name AS blob)),0)+coalesce(length(cast(severity AS blob)),0)+coalesce(length(cast(attributes AS blob)),0)`

// StorageAlert is deliberately small and can be mirrored outside the state
// filesystem. A full SQLite database cannot be relied upon to store its own
// alarm.
type StorageAlert struct {
	Critical       bool      `json:"critical" yaml:"critical"`
	Condition      string    `json:"condition" yaml:"condition"`
	Message        string    `json:"message" yaml:"message"`
	RecoveryAction string    `json:"recoveryAction" yaml:"recoveryAction"`
	StateFile      string    `json:"stateFile" yaml:"stateFile"`
	ObservedAt     time.Time `json:"observedAt" yaml:"observedAt"`
}

// StorageWriteError marks a journal persistence failure that must not prevent
// delivery of an already-derived event to local controllers. Existing routing
// is more important than local forensic history under storage exhaustion.
type StorageWriteError struct{ Err error }

func (e *StorageWriteError) Error() string             { return e.Err.Error() }
func (e *StorageWriteError) Unwrap() error             { return e.Err }
func (e *StorageWriteError) NonFatalPersistence() bool { return true }

func IsStorageFullError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database or disk is full") ||
		strings.Contains(text, "no space left on device") ||
		strings.Contains(text, "sqlite_full")
}

func (s *SQLiteStore) loadEventJournalUsage() error {
	return s.db.QueryRow(`SELECT count(*), coalesce(sum(`+eventJournalPayloadSQL+`),0) FROM events`).
		Scan(&s.eventJournalRows, &s.eventJournalPayloadBytes)
}

func (s *SQLiteStore) ensureEventJournalCapacityLocked(incomingBytes int64) error {
	if err := s.pruneExpiredJournalBatchLocked(); err != nil {
		return err
	}
	targetRows := s.eventJournalMaxRows * 9 / 10
	targetBytes := s.eventJournalMaxBytes * 7 / 8
	pruned := false
	for s.eventJournalRows >= s.eventJournalMaxRows || s.eventJournalPayloadBytes+incomingBytes > s.eventJournalMaxBytes {
		remove := s.eventJournalRows - targetRows
		if remove < 1 {
			remove = 1
		}
		if remove > eventJournalPruneBatchRows {
			remove = eventJournalPruneBatchRows
		}
		var rows, payload int64
		err := s.db.QueryRow(`SELECT count(*), coalesce(sum(`+eventJournalPayloadSQL+`),0)
FROM events WHERE id IN (SELECT id FROM events ORDER BY id LIMIT ?)`, remove).Scan(&rows, &payload)
		if err != nil {
			return err
		}
		if rows == 0 {
			if err := s.loadEventJournalUsage(); err != nil {
				return err
			}
			if s.eventJournalRows < s.eventJournalMaxRows && s.eventJournalPayloadBytes+incomingBytes <= s.eventJournalMaxBytes {
				return nil
			}
			return fmt.Errorf("event journal payload exceeds the safe limit without prunable rows")
		}
		result, err := s.db.Exec(`DELETE FROM events WHERE id IN (SELECT id FROM events ORDER BY id LIMIT ?)`, rows)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		s.eventJournalRows -= deleted
		s.eventJournalPayloadBytes -= payload
		pruned = true
		if s.eventJournalRows < 0 {
			s.eventJournalRows = 0
		}
		if s.eventJournalPayloadBytes < 0 {
			s.eventJournalPayloadBytes = 0
		}
		if s.eventJournalRows <= targetRows && s.eventJournalPayloadBytes+incomingBytes <= targetBytes {
			break
		}
	}
	if pruned {
		_, _ = s.db.Exec(`PRAGMA incremental_vacuum(4096)`)
		_, _ = s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
	}
	return nil
}

func (s *SQLiteStore) pruneExpiredJournalBatchLocked() error {
	now := s.now().UTC()
	if !s.eventJournalLastAgePrune.IsZero() && now.Sub(s.eventJournalLastAgePrune) < eventJournalAgePruneInterval {
		return nil
	}
	cutoff := now.Add(-defaultEventJournalMaxAge).Format(time.RFC3339Nano)
	result, err := s.db.Exec(`DELETE FROM events WHERE id IN (
SELECT id FROM events WHERE created_at < ? ORDER BY id LIMIT ?)`, cutoff, eventJournalPruneBatchRows)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted > 0 {
		if err := s.loadEventJournalUsage(); err != nil {
			return err
		}
		_, _ = s.db.Exec(`PRAGMA incremental_vacuum(4096)`)
		_, _ = s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
		return nil
	}
	s.eventJournalLastAgePrune = now
	return nil
}

func journalPayloadBytes(values ...string) int64 {
	var total int64
	for _, value := range values {
		total += int64(len(value))
	}
	return total
}

func boundedJournalText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	suffix := fmt.Sprintf("...[truncated originalBytes=%d sha256=%s]", len(value), hex.EncodeToString(digest[:]))
	return value[:limit-len(suffix)] + suffix
}

func boundedJournalEvent(event daemonapi.DaemonEvent) (daemonapi.DaemonEvent, string) {
	event.Message = boundedJournalText(event.Message, maxJournalMessageBytes)
	attrs, _ := json.Marshal(event.Attributes)
	if len(attrs) > maxJournalAttributesBytes {
		digest := sha256.Sum256(attrs)
		attrs, _ = json.Marshal(map[string]string{
			"_routerdTruncated":     "true",
			"_routerdOriginalBytes": fmt.Sprint(len(attrs)),
			"_routerdSHA256":        hex.EncodeToString(digest[:]),
		})
	}
	return event, string(attrs)
}

func (s *SQLiteStore) noteStorageWriteErrorLocked(err error) error {
	if !IsStorageFullError(err) {
		return err
	}
	if s.storageAlert == nil {
		alert := NewStorageAlert(s.path, s.now().UTC())
		s.storageAlert = &alert
		if s.storageAlertHandler != nil {
			s.storageAlertHandler(alert)
		}
	}
	return &StorageWriteError{Err: err}
}

func NewStorageAlert(stateFile string, observedAt time.Time) StorageAlert {
	return StorageAlert{
		Critical:       true,
		Condition:      "EventJournalReadOnly",
		Message:        "routerd state database cannot be written; existing routing is kept running where possible",
		RecoveryAction: "reboot-volatile-router-os-or-compact-or-expand-persistent-storage",
		StateFile:      stateFile,
		ObservedAt:     observedAt.UTC(),
	}
}

func (s *SQLiteStore) SetStorageAlertHandler(handler func(StorageAlert)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storageAlertHandler = handler
}

func (s *SQLiteStore) StorageAlert() *StorageAlert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.storageAlert == nil {
		return nil
	}
	copy := *s.storageAlert
	return &copy
}

type EventJournalStats struct {
	Rows            int64         `json:"rows" yaml:"rows"`
	MaxRows         int64         `json:"maxRows" yaml:"maxRows"`
	PayloadBytes    int64         `json:"payloadBytes" yaml:"payloadBytes"`
	MaxPayloadBytes int64         `json:"maxPayloadBytes" yaml:"maxPayloadBytes"`
	StorageAlert    *StorageAlert `json:"storageAlert,omitempty" yaml:"storageAlert,omitempty"`
}

func (s *SQLiteStore) EventJournalStats() EventJournalStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := EventJournalStats{Rows: s.eventJournalRows, MaxRows: s.eventJournalMaxRows, PayloadBytes: s.eventJournalPayloadBytes, MaxPayloadBytes: s.eventJournalMaxBytes}
	if s.storageAlert != nil {
		copy := *s.storageAlert
		stats.StorageAlert = &copy
	}
	return stats
}

func WriteStorageAlertFile(path string, alert StorageAlert) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(alert, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
