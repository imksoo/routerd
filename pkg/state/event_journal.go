// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"crypto/sha256"
	"database/sql"
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

// eventJournalExecutor is the existing SQLite boundary, shared by ordinary
// appends and transactions which commit status, required history and pruning.
type eventJournalExecutor interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

type eventJournalUsage struct {
	rows, payloadBytes, dataVersion int64
	lastAgePrune                    time.Time
}

func readEventJournalUsage(executor eventJournalExecutor, usage *eventJournalUsage) error {
	// Capture before the aggregate so a later external commit forces a refresh.
	if err := executor.QueryRow(`PRAGMA data_version`).Scan(&usage.dataVersion); err != nil {
		return err
	}
	return executor.QueryRow(`SELECT count(*), coalesce(sum(`+eventJournalPayloadSQL+`),0) FROM events`).Scan(&usage.rows, &usage.payloadBytes)
}

func (s *SQLiteStore) applyEventJournalUsageLocked(usage eventJournalUsage) {
	s.eventJournalRows = usage.rows
	s.eventJournalPayloadBytes = usage.payloadBytes
	s.eventJournalDataVersion = usage.dataVersion
	s.eventJournalLastAgePrune = usage.lastAgePrune
}

func (s *SQLiteStore) loadEventJournalUsage() error {
	usage := eventJournalUsage{lastAgePrune: s.eventJournalLastAgePrune}
	if err := readEventJournalUsage(s.db, &usage); err != nil {
		return err
	}
	s.applyEventJournalUsageLocked(usage)
	return nil
}

// prepareEventJournalBatchLocked reserves the actual prepared event payload.
// Transaction callers publish the returned usage only after commit; rollback
// then retains both earlier history and its original accounting.
func (s *SQLiteStore) prepareEventJournalBatchLocked(executor eventJournalExecutor, incomingRows, incomingBytes int64) (eventJournalUsage, error) {
	usage := eventJournalUsage{s.eventJournalRows, s.eventJournalPayloadBytes, s.eventJournalDataVersion, s.eventJournalLastAgePrune}
	if incomingRows < 0 || incomingBytes < 0 || incomingRows > s.eventJournalMaxRows || incomingBytes > s.eventJournalMaxBytes {
		return usage, fmt.Errorf("event journal transaction exceeds its configured capacity")
	}
	var version int64
	if err := executor.QueryRow(`PRAGMA data_version`).Scan(&version); err != nil {
		return usage, err
	}
	if version != usage.dataVersion {
		if err := readEventJournalUsage(executor, &usage); err != nil {
			return usage, err
		}
	}
	now := s.now().UTC()
	if usage.lastAgePrune.IsZero() || now.Sub(usage.lastAgePrune) >= eventJournalAgePruneInterval {
		cutoff := now.Add(-defaultEventJournalMaxAge).Format(time.RFC3339Nano)
		result, err := executor.Exec(`DELETE FROM events WHERE id IN (SELECT id FROM events WHERE created_at < ? ORDER BY id LIMIT ?)`, cutoff, eventJournalPruneBatchRows)
		if err != nil {
			return usage, err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return usage, err
		}
		if deleted > 0 {
			if err := readEventJournalUsage(executor, &usage); err != nil {
				return usage, err
			}
		} else {
			usage.lastAgePrune = now
		}
	}
	targetRows := s.eventJournalMaxRows * 9 / 10
	for usage.rows+incomingRows > s.eventJournalMaxRows || usage.payloadBytes+incomingBytes > s.eventJournalMaxBytes {
		remove := usage.rows - targetRows
		if remove < 1 {
			remove = 1
		}
		if remove > eventJournalPruneBatchRows {
			remove = eventJournalPruneBatchRows
		}
		var rows, payload int64
		err := executor.QueryRow(`SELECT count(*), coalesce(sum(`+eventJournalPayloadSQL+`),0) FROM events WHERE id IN (SELECT id FROM events ORDER BY id LIMIT ?)`, remove).Scan(&rows, &payload)
		if err != nil {
			return usage, err
		}
		if rows == 0 {
			return usage, fmt.Errorf("event journal payload exceeds the safe limit without prunable rows")
		}
		result, err := executor.Exec(`DELETE FROM events WHERE id IN (SELECT id FROM events ORDER BY id LIMIT ?)`, rows)
		if err != nil {
			return usage, err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return usage, err
		}
		usage.rows -= deleted
		usage.payloadBytes -= payload
	}
	return usage, nil
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
		event.Attributes = map[string]string{
			"_routerdTruncated":     "true",
			"_routerdOriginalBytes": fmt.Sprint(len(attrs)),
			"_routerdSHA256":        hex.EncodeToString(digest[:]),
		}
		attrs, _ = json.Marshal(event.Attributes)
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
