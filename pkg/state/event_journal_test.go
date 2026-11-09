// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestStorageFullSetsOutOfDatabaseAlert(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var pages int64
	if err := store.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`PRAGMA max_page_count = ` + fmt.Sprint(pages)); err != nil {
		t.Fatal(err)
	}
	_, err = store.RecordBusEvent(context.Background(), daemonapi.DaemonEvent{Type: "routerd.test.large", Message: strings.Repeat("x", 256*1024)})
	if err == nil || !IsStorageFullError(err) {
		t.Fatalf("error = %v, want storage-full error", err)
	}
	var marked interface{ NonFatalPersistence() bool }
	if !errors.As(err, &marked) || !marked.NonFatalPersistence() {
		t.Fatalf("error is not marked as non-fatal persistence failure: %T", err)
	}
	alert := store.StorageAlert()
	if alert == nil || !alert.Critical || alert.Condition != "EventJournalReadOnly" || !strings.Contains(alert.RecoveryAction, "reboot-volatile") {
		t.Fatalf("storage alert = %#v", alert)
	}
	path := filepath.Join(t.TempDir(), "run", "storage-critical.json")
	if err := WriteStorageAlertFile(path, *alert); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "EventJournalReadOnly") {
		t.Fatalf("alert file: %v %s", err, data)
	}
}

func TestBoundedJournalEventTruncatesOversizedFields(t *testing.T) {
	event := daemonapi.DaemonEvent{Message: strings.Repeat("m", maxJournalMessageBytes+100), Attributes: map[string]string{"large": strings.Repeat("a", maxJournalAttributesBytes+100)}}
	bounded, attrs := boundedJournalEvent(event)
	if len(bounded.Message) > maxJournalMessageBytes || !strings.Contains(bounded.Message, "sha256=") {
		t.Fatalf("message was not bounded: %d", len(bounded.Message))
	}
	if len(attrs) > maxJournalAttributesBytes || !strings.Contains(attrs, "_routerdTruncated") {
		t.Fatalf("attributes were not bounded: %d %s", len(attrs), attrs)
	}
}

func TestWriteStorageAlertFilePreservesTimestamp(t *testing.T) {
	when := time.Date(2026, 9, 12, 17, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "storage-critical.json")
	if err := WriteStorageAlertFile(path, StorageAlert{Critical: true, ObservedAt: when}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "2026-09-12T17:00:00Z") {
		t.Fatalf("alert file: %v %s", err, data)
	}
}
