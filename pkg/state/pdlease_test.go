// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

func TestPDLeaseHasFreshTransactionEvidence(t *testing.T) {
	now := time.Date(2026, 5, 1, 21, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		lease PDLease
		want  bool
	}{
		{"no reply", PDLease{CurrentPrefix: "2001:db8::/60"}, false},
		{"no lifetime", PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: now.Add(-time.Hour)}, false},
		{"valid", PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: now.Add(-time.Hour), ValidSeconds: 14400}, true},
		{"expired", PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: now.Add(-24 * time.Hour), ValidSeconds: 14400}, false},
		{"future reply", PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: now.Add(time.Hour), ValidSeconds: 14400}, false},
		{"infinite", PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: now.AddDate(-200, 0, 0), ValidSeconds: 0xffffffff}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lease.HasFreshTransactionEvidence(now); got != tc.want {
				t.Fatalf("fresh=%v want %v", got, tc.want)
			}
		})
	}
}

func TestPDLeaseCanonicalPersistenceAndStatusProjection(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	lease := PDLease{CurrentPrefix: "2001:db8:1::/56", LastPrefix: "2001:db8:1::/56", LastReplyAt: now, LastObservedAt: now, ValidSeconds: 120, PreferredSeconds: 90, Phase: daemonapi.ResourcePhaseBound}
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd", map[string]any{"phase": "Bound", "applyMode": "active", "currentPrefix": "2001:db8:99::/56", "observed": map[string]string{"currentPrefix": "2001:db8:99::/56", "phase": "Bound", "aftrName": "aftr.example"}}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.db.QueryRow(`SELECT status FROM objects WHERE kind='DHCPv6PrefixDelegation' AND name='wan-pd'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document["observation"]; !ok {
		t.Fatal("canonical typed observation missing")
	}
	if document["currentPrefix"] != nil || document["_variables"] != nil {
		t.Fatalf("duplicate lease authority persisted: %s", raw)
	}
	got, ok, err := store.ReadPDLease("wan-pd")
	if err != nil || !ok || got.CurrentPrefix != lease.CurrentPrefix || got.ValidSeconds != 120 {
		t.Fatalf("typed read=%+v %v %v", got, ok, err)
	}
	status := store.ObjectStatus("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd")
	if status["currentPrefix"] != lease.CurrentPrefix || status["applyMode"] != "active" {
		t.Fatalf("canonical projection=%+v", status)
	}
	if len(store.Variables()) != 0 {
		t.Fatal("lease JSON leaked into variable transport")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.now = func() time.Time { return now }
	got, ok, err = restored.ReadPDLease("wan-pd")
	if err != nil || !ok || !got.LastReplyAt.Equal(now) || got.ValidSeconds != 120 {
		t.Fatalf("restart lost lifetime: %+v %v", got, err)
	}
	now = now.Add(121 * time.Second)
	status = restored.ObjectStatus("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd")
	if status["currentPrefix"] != "" || status["phase"] != daemonapi.ResourcePhaseExpired {
		t.Fatalf("expired projection still ready: %+v", status)
	}
}

func TestPDLeaseSnapshotMergeKeepsConfigAndHonorsRevocationAndSource(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.now = func() time.Time { return now }
	config := PDLeaseConfig{Client: "routerd", Profile: "default", UplinkIfname: "wan0", PrefixLength: 56, ClientDUID: "01", IAID: 7}
	if _, _, err := store.ConfigurePDLease("wan-pd", config); err != nil {
		t.Fatal(err)
	}
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: "wan0", ClientDUID: "01", ServerDUID: "02", IAID: 7, State: pdclient.StateBound, CurrentPrefix: "2001:db8:1::/56", UpdatedAt: now, Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1::/56", Preferred: 90, Valid: 120, AcquiredAt: now}, {Prefix: "2001:db8:2::/56", Preferred: 100, Valid: 180, AcquiredAt: now}}}
	if err := store.SavePDLeaseSnapshot("wan-pd", snapshot); err != nil {
		t.Fatal(err)
	}
	config.Profile = "updated"
	if _, _, err := store.ConfigurePDLease("wan-pd", config); err != nil {
		t.Fatal(err)
	}
	snapshot.UpdatedAt = now.Add(time.Second)
	snapshot.Prefixes = snapshot.Prefixes[:1]
	if err := store.SavePDLeaseSnapshot("wan-pd", snapshot); err != nil {
		t.Fatal(err)
	}
	lease, _, err := store.ReadPDLease("wan-pd")
	if err != nil || lease.Profile != "updated" || len(lease.PreviousPrefixes) != 0 || lease.ValidSeconds != 120 || !lease.LastReplyAt.Equal(now) {
		t.Fatalf("typed merge changed owner fields or resurrected revocation: %+v %v", lease, err)
	}
	stale := snapshot
	stale.UpdatedAt = now
	stale.CurrentPrefix = "2001:db8:9::/56"
	stale.Prefixes = []pdclient.PrefixSnapshot{{Prefix: stale.CurrentPrefix, Preferred: 90, Valid: 120, AcquiredAt: now}}
	if err := store.SavePDLeaseSnapshot("wan-pd", stale); err != nil {
		t.Fatal(err)
	}
	lease, _, _ = store.ReadPDLease("wan-pd")
	if lease.CurrentPrefix != snapshot.CurrentPrefix {
		t.Fatal("old snapshot overwrote newer observation")
	}
	wrong := snapshot
	wrong.Interface = "wan1"
	if err := store.SavePDLeaseSnapshot("wan-pd", wrong); err == nil {
		t.Fatal("foreign source accepted")
	}
	snapshot.State = pdclient.StateReleased
	snapshot.CurrentPrefix = ""
	snapshot.Prefixes = nil
	snapshot.UpdatedAt = now.Add(2 * time.Second)
	if err := store.SavePDLeaseSnapshot("wan-pd", snapshot); err != nil {
		t.Fatal(err)
	}
	lease, _, _ = store.ReadPDLease("wan-pd")
	if lease.CurrentPrefix != "" || len(lease.PreviousPrefixes) != 0 || lease.LastPrefix == "" {
		t.Fatalf("Release lost history or retained authority: %+v", lease)
	}
}

func TestPDLeaseStoreReportsClosedWritesAndCopiesMemoryOwnership(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, _, err := store.ReadPDLease("wan"); !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	if err := store.SavePDLease("wan", PDLease{}); !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	if _, _, err := store.ConfigurePDLease("wan", PDLeaseConfig{}); !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	memory := NewMemory()
	lease := PDLease{PreviousPrefixes: []PDPreviousPrefix{{Prefix: "2001:db8::/56", ExpiresAt: time.Now().Add(time.Hour)}}}
	if err := memory.SavePDLease("wan", lease); err != nil {
		t.Fatal(err)
	}
	lease.PreviousPrefixes[0].Prefix = "2001:db8:99::/56"
	read, _, _ := memory.ReadPDLease("wan")
	if read.PreviousPrefixes[0].Prefix != "2001:db8::/56" {
		t.Fatal("store retained caller's mutable slice")
	}
	read.PreviousPrefixes[0].Prefix = "2001:db8:88::/56"
	read, _, _ = memory.ReadPDLease("wan")
	if read.PreviousPrefixes[0].Prefix != "2001:db8::/56" {
		t.Fatal("reader acquired store slice ownership")
	}
}
