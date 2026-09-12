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

func TestEventJournalDefaultLimitPrunesOnlyOldEvents(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.eventJournalMaxRows = 20
	store.eventJournalMaxBytes = 1 << 20
	if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "BGPRouter", "lan", map[string]any{"phase": "Established"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.resource.status.changed", daemonapi.SeverityDebug)
		event.Message = strings.Repeat("x", 32)
		if _, err := store.RecordBusEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	stats := store.EventJournalStats()
	if stats.Rows > store.eventJournalMaxRows || stats.Rows < 18 {
		t.Fatalf("journal rows = %d, want 18..20", stats.Rows)
	}
	if got := store.ObjectStatus("net.routerd.net/v1alpha1", "BGPRouter", "lan")["phase"]; got != "Established" {
		t.Fatalf("state table was changed while pruning events: phase=%v", got)
	}
}

func TestEventJournalPayloadLimitIsBounded(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.eventJournalMaxRows = 1_000
	store.eventJournalMaxBytes = 4_096
	for i := 0; i < 40; i++ {
		if err := store.RecordEvent("v1", "Test", "item", "Normal", "Observed", strings.Repeat("x", 512)); err != nil {
			t.Fatal(err)
		}
	}
	stats := store.EventJournalStats()
	if stats.PayloadBytes > store.eventJournalMaxBytes {
		t.Fatalf("journal payload = %d, max = %d", stats.PayloadBytes, store.eventJournalMaxBytes)
	}
}

func TestEventJournalCountsUTF8PayloadBytes(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	values := []string{"v1", "試験", "項目", "Normal", "観測", "容量警告"}
	if err := store.RecordEvent(values[0], values[1], values[2], values[3], values[4], values[5]); err != nil {
		t.Fatal(err)
	}
	want := journalPayloadBytes(values...)
	if got := store.EventJournalStats().PayloadBytes; got != want {
		t.Fatalf("UTF-8 journal payload = %d, want %d bytes", got, want)
	}
}

func TestEventJournalAcceleratedSoakRemainsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.eventJournalMaxRows = 1_000
	store.eventJournalMaxBytes = 1 << 20
	for i := 0; i < 5_000; i++ {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.resource.status.changed", daemonapi.SeverityDebug)
		event.Message = strings.Repeat("x", 256)
		if _, err := store.RecordBusEvent(context.Background(), event); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
	}
	stats := store.EventJournalStats()
	if stats.Rows > store.eventJournalMaxRows || stats.PayloadBytes > store.eventJournalMaxBytes {
		t.Fatalf("accelerated soak escaped bounds: %#v", stats)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 4<<20 {
		t.Fatalf("bounded journal database grew to %d bytes", info.Size())
	}
}

func TestEventJournalHasSafeAgeDefaultWithoutLogRetention(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 12, 17, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now.Add(-25 * time.Hour) }
	if err := store.RecordEvent("v1", "Test", "old", "Normal", "Old", "old"); err != nil {
		t.Fatal(err)
	}
	store.eventJournalLastAgePrune = time.Time{}
	store.now = func() time.Time { return now }
	if err := store.RecordEvent("v1", "Test", "new", "Normal", "New", "new"); err != nil {
		t.Fatal(err)
	}
	if got := store.EventJournalStats().Rows; got != 1 {
		t.Fatalf("journal rows = %d, want only the event within 24h", got)
	}
}

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
