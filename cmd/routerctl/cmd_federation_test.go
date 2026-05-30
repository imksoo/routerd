// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFederationEventEmitThenList(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")

	var emitOut bytes.Buffer
	emitArgs := []string{
		"federation", "event", "emit",
		"--state-file", statePath,
		"--group", "cloudedge",
		"--type", "routerd.client.ipv4.observed",
		"--subject", "10.88.60.9/32",
		"--source-node", "onprem",
		"--id", "evt-test-1",
		"--payload", "mac=aa:bb:cc:dd:ee:ff",
		"--ttl", "30m",
		"-o", "json",
	}
	if err := run(emitArgs, &emitOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("emit: %v\n%s", err, emitOut.String())
	}

	var emitted routerstate.EventRecord
	if err := json.Unmarshal(emitOut.Bytes(), &emitted); err != nil {
		t.Fatalf("decode emit output: %v\n%s", err, emitOut.String())
	}
	if emitted.ID != "evt-test-1" {
		t.Fatalf("emitted id = %q, want evt-test-1", emitted.ID)
	}
	// DedupeKey defaults to ID when not provided.
	if emitted.DedupeKey != "evt-test-1" {
		t.Fatalf("emitted dedupeKey = %q, want it to default to id", emitted.DedupeKey)
	}
	if emitted.Payload["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("emitted payload mac = %q, want round-trip", emitted.Payload["mac"])
	}
	if emitted.ExpiresAt.IsZero() {
		t.Fatalf("emitted expiresAt is zero, want ttl-derived value")
	}

	// List back, filtered by group.
	var listOut bytes.Buffer
	listArgs := []string{
		"federation", "event", "list",
		"--state-file", statePath,
		"--group", "cloudedge",
		"-o", "json",
	}
	if err := run(listArgs, &listOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("list: %v\n%s", err, listOut.String())
	}
	var listed []routerstate.EventRecord
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("decode list output: %v\n%s", err, listOut.String())
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d events, want 1: %+v", len(listed), listed)
	}
	got := listed[0]
	if got.ID != "evt-test-1" || got.Group != "cloudedge" || got.Subject != "10.88.60.9/32" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.SourceNode != "onprem" {
		t.Fatalf("round-trip sourceNode = %q, want onprem", got.SourceNode)
	}
	if got.DedupeKey != "evt-test-1" {
		t.Fatalf("round-trip dedupeKey = %q, want evt-test-1", got.DedupeKey)
	}
	if got.Payload["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("round-trip payload mac = %q", got.Payload["mac"])
	}

	// Group filter should exclude other groups.
	var otherOut bytes.Buffer
	if err := run([]string{
		"federation", "event", "list",
		"--state-file", statePath,
		"--group", "no-such-group",
		"-o", "json",
	}, &otherOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("list other group: %v", err)
	}
	var other []routerstate.EventRecord
	if err := json.Unmarshal(otherOut.Bytes(), &other); err != nil {
		t.Fatalf("decode other list: %v\n%s", err, otherOut.String())
	}
	if len(other) != 0 {
		t.Fatalf("group filter leaked %d events: %+v", len(other), other)
	}
}

func TestFederationEventDeliveriesFilters(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")

	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Seed events in two groups, then deliveries for them.
	events := []routerstate.EventRecord{
		{ID: "g1-a", Group: "groupone", Type: "t"},
		{ID: "g1-b", Group: "groupone", Type: "t"},
		{ID: "g2-a", Group: "grouptwo", Type: "t"},
	}
	for _, ev := range events {
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record event %s: %v", ev.ID, err)
		}
	}
	type seed struct {
		eventID, peer, status string
	}
	seeds := []seed{
		{"g1-a", "peer-x", routerstate.DeliveryDelivered},
		{"g1-b", "peer-y", routerstate.DeliveryFailed},
		{"g2-a", "peer-x", routerstate.DeliveryPending},
	}
	for _, s := range seeds {
		if err := store.RecordDelivery(s.eventID, s.peer); err != nil {
			t.Fatalf("record delivery %s/%s: %v", s.eventID, s.peer, err)
		}
		if s.status != routerstate.DeliveryPending {
			if err := store.UpdateDeliveryStatus(s.eventID, s.peer, s.status, 1, "", time.Time{}); err != nil {
				t.Fatalf("update delivery %s/%s: %v", s.eventID, s.peer, err)
			}
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	listDeliveries := func(t *testing.T, extra ...string) []routerstate.DeliveryRecord {
		t.Helper()
		args := append([]string{
			"federation", "event", "deliveries",
			"--state-file", statePath,
			"-o", "json",
		}, extra...)
		var out bytes.Buffer
		if err := run(args, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("deliveries %v: %v\n%s", extra, err, out.String())
		}
		var recs []routerstate.DeliveryRecord
		if err := json.Unmarshal(out.Bytes(), &recs); err != nil {
			t.Fatalf("decode deliveries %v: %v\n%s", extra, err, out.String())
		}
		return recs
	}

	idsOf := func(recs []routerstate.DeliveryRecord) []string {
		ids := make([]string, 0, len(recs))
		for _, r := range recs {
			ids = append(ids, r.EventID)
		}
		return ids
	}
	wantIDs := func(t *testing.T, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("event ids = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("event ids = %v, want %v", got, want)
			}
		}
	}

	// (1) No filter -> all three.
	wantIDs(t, idsOf(listDeliveries(t)), []string{"g1-a", "g1-b", "g2-a"})

	// (2) --group filters to that group's deliveries.
	wantIDs(t, idsOf(listDeliveries(t, "--group", "groupone")), []string{"g1-a", "g1-b"})

	// (3) --event-id filters.
	wantIDs(t, idsOf(listDeliveries(t, "--event-id", "g2-a")), []string{"g2-a"})

	// (4) --group + --event-id combined.
	wantIDs(t, idsOf(listDeliveries(t, "--group", "groupone", "--event-id", "g1-b")), []string{"g1-b"})

	// (5) --status failed filters.
	failed := listDeliveries(t, "--status", "failed")
	wantIDs(t, idsOf(failed), []string{"g1-b"})
	if len(failed) == 1 && failed[0].Status != routerstate.DeliveryFailed {
		t.Fatalf("status filter returned status %q, want failed", failed[0].Status)
	}

	// (6) Unknown group -> empty result, exit 0, no error.
	if recs := listDeliveries(t, "--group", "no-such-group"); len(recs) != 0 {
		t.Fatalf("unknown group leaked %d rows: %+v", len(recs), recs)
	}

	// (7) Invalid --status -> error.
	var errOut bytes.Buffer
	if err := run([]string{
		"federation", "event", "deliveries",
		"--state-file", statePath,
		"--status", "bogus",
	}, &errOut, &bytes.Buffer{}); err == nil {
		t.Fatalf("invalid --status should error, got nil\n%s", errOut.String())
	}
}
