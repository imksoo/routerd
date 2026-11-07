// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func namedLeaseEvent(action string, observed time.Time) daemonapi.DaemonEvent {
	topic, _ := daemonapi.DHCPLeaseEventType(action)
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "routerd-dhcp-event-relay"}, topic, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server", Name: "lan"}
	event.Attributes = map[string]string{"mac": "client-a", "ip": "192.0.2.55", "hostname": "client", "observedAt": observed.Format(time.RFC3339Nano), "expiresAt": observed.Add(time.Hour).Format(time.RFC3339Nano)}
	return event
}

func TestNamedDHCPObservationSurvivesHistoryRetentionAndDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-26 * time.Hour)
	store.now = func() time.Time { return at }
	if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("added", at)); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	removed := namedLeaseEvent("removed", at)
	if _, err := store.RecordBusEvent(t.Context(), removed); err != nil {
		t.Fatal(err)
	}
	// Older callbacks and history-only records retain history but cannot replace
	// the actual latest current observation/tombstone.
	if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("added", at.Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	historyOnly := namedLeaseEvent("added", at)
	historyOnly.Attributes["replayDisposition"] = "history-only-unverified-dhcp-source"
	if _, err := store.RecordBusEvent(t.Context(), historyOnly); err != nil {
		t.Fatal(err)
	}
	store.now = time.Now
	store.eventJournalLastAgePrune = time.Time{}
	if err := store.RecordEvent("v1", "Test", "prune", "Normal", "Current", "prune expired history"); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := store.db.QueryRow(`SELECT count(*) FROM events WHERE topic=?`, removed.Type).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("old diagnostic history retained: %d %v", retained, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	observations, err := reopened.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "removed" || observations[0].Source != "DHCPv4Server/lan" || !observations[0].ObservedAt.Equal(at) {
		t.Fatalf("current removal lost with history/reopen: %+v %v", observations, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReadDHCPLeaseObservations(t.Context()); err != sql.ErrConnDone {
		t.Fatalf("closed current-state read became empty: %v", err)
	}
}

func TestNamedDHCPProjectionAndJournalRollbackTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("added", now)); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []struct{ name, create, drop string }{
		{"projection", `CREATE TRIGGER fail_dhcp_projection BEFORE UPDATE ON dhcp_lease_observations BEGIN SELECT RAISE(ABORT,'projection write failed'); END`, `DROP TRIGGER fail_dhcp_projection`},
		{"journal", `CREATE TRIGGER fail_dhcp_journal BEFORE INSERT ON events WHEN NEW.topic='routerd.dhcp.lease.removed' BEGIN SELECT RAISE(ABORT,'journal write failed'); END`, `DROP TRIGGER fail_dhcp_journal`},
	} {
		if _, err := store.db.Exec(fault.create); err != nil {
			t.Fatal(err)
		}
		before := store.EventJournalStats()
		if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("removed", now.Add(time.Second))); err == nil {
			t.Fatalf("%s failure acknowledged", fault.name)
		}
		observations, err := store.ReadDHCPLeaseObservations(t.Context())
		if err != nil || len(observations) != 1 || observations[0].Action != "added" {
			t.Fatalf("%s failure partially changed current state: %+v %v", fault.name, observations, err)
		}
		if after := store.EventJournalStats(); after.Rows != before.Rows || after.PayloadBytes != before.PayloadBytes {
			t.Fatalf("%s failure changed journal accounting", fault.name)
		}
		if _, err := store.db.Exec(fault.drop); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	observations, err := reopened.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "added" {
		t.Fatalf("rollback lost after reopen: %+v %v", observations, err)
	}
	removed := namedLeaseEvent("removed", now.Add(time.Second))
	for i := 0; i < 2; i++ {
		if _, err := reopened.RecordBusEvent(t.Context(), removed); err != nil {
			t.Fatal(err)
		}
	}
	observations, err = reopened.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "removed" {
		t.Fatalf("retry/lost-reply recovery failed: %+v %v", observations, err)
	}
}

func TestNamedDHCPDiskFullDoesNotAcknowledgeUnstoredTombstone(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Add(-time.Minute)
	if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("added", now)); err != nil {
		t.Fatal(err)
	}
	var pages int64
	if err := store.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`PRAGMA max_page_count=` + fmt.Sprint(pages)); err != nil {
		t.Fatal(err)
	}
	removed := namedLeaseEvent("removed", now.Add(time.Second))
	removed.Message = strings.Repeat("required current lease withdrawal ", 4096)
	if _, err := store.RecordBusEvent(context.Background(), removed); !IsStorageFullError(err) {
		t.Fatalf("actual SQLite full not observed: %v", err)
	}
	observations, err := store.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "added" {
		t.Fatalf("disk-full publication partially committed: %+v %v", observations, err)
	}
}

func TestMalformedNamedDHCPEventCannotBecomeAcknowledgedMissingProjection(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	event := namedLeaseEvent("added", time.Now().UTC())
	delete(event.Attributes, "observedAt")
	if _, err := store.RecordBusEvent(t.Context(), event); err == nil {
		t.Fatal("malformed named lease acknowledged")
	}
	if stats := store.EventJournalStats(); stats.Rows != 0 {
		t.Fatal("malformed lease partially journaled")
	}
	observations, err := store.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 0 {
		t.Fatalf("malformed lease gained current state: %+v %v", observations, err)
	}
}

func TestConflictingEqualTimeDHCPObservationCannotBeAcknowledged(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	at := time.Now().UTC().Add(-time.Minute)
	initial := namedLeaseEvent("added", at)
	if _, err := store.RecordBusEvent(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	// A reply lost after the same commit can be retried idempotently.
	if _, err := store.RecordBusEvent(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	before := store.EventJournalStats()
	conflicting := namedLeaseEvent("removed", at)
	if _, err := store.RecordBusEvent(t.Context(), conflicting); err == nil {
		t.Fatal("conflicting equal-time action acknowledged")
	}
	if after := store.EventJournalStats(); after.Rows != before.Rows || after.PayloadBytes != before.PayloadBytes {
		t.Fatal("rejected conflict partially journaled")
	}
	observations, err := store.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "added" {
		t.Fatalf("conflict changed actual projection: %+v %v", observations, err)
	}
}

func TestUnrelatedClientRemovalPreservesCurrentNamedAllocationAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute).UTC()
	current := namedLeaseEvent("added", at)
	current.Attributes["mac"] = "CURRENT-client"
	current.Attributes["hostname"] = "current"
	if _, err := store.RecordBusEvent(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	removal := namedLeaseEvent("removed", at.Add(time.Second))
	removal.Attributes["mac"] = "former-client"
	if _, err := store.RecordBusEvent(t.Context(), removal); err != nil {
		t.Fatal(err)
	}
	if stats := store.EventJournalStats(); stats.Rows != 2 {
		t.Fatal("other-client removal history discarded")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	observations, err := reopened.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "added" || observations[0].MAC != "CURRENT-client" || observations[0].Hostname != "current" || !observations[0].ObservedAt.Equal(at) {
		t.Fatalf("other client removal clobbered actual current owner: %+v %v", observations, err)
	}
	// The current client's own removal is still authoritative, ignoring only MAC
	// spelling case across the callback/journal boundary.
	ownRemoval := namedLeaseEvent("removed", at.Add(2*time.Second))
	ownRemoval.Attributes["mac"] = "current-CLIENT"
	if _, err := reopened.RecordBusEvent(t.Context(), ownRemoval); err != nil {
		t.Fatal(err)
	}
	observations, err = reopened.ReadDHCPLeaseObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].Action != "removed" {
		t.Fatalf("current client's revocation rejected: %+v %v", observations, err)
	}
}
