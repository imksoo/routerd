// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFederationEventEmitRejectsSelfCapturedObservedAddress(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	intents, err := json.Marshal(dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.88.60.0/24", Captures: []dynamicconfig.LocalCaptureIntent{{
		ID: "cloud-a/10.88.60.9", PoolRef: "cloudedge", Address: "10.88.60.9/32",
		CaptureType: "proxy-arp", CaptureInterface: "lan0", Disposition: dynamicconfig.CaptureDesired,
	}}})
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	now := time.Now().UTC()
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:                "MobilityPool/cloudedge/node/cloud-a",
		Generation:            1,
		ObservedAt:            now,
		ExpiresAt:             now.Add(5 * time.Minute),
		Digest:                "sha256:self-capture-test",
		MobilityDataplaneJSON: string(intents),
		Status:                "active",
	}); err != nil {
		t.Fatalf("seed dynamic part: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{
		"federation", "event", "emit",
		"--state-file", statePath,
		"--group", "cloudedge",
		"--type", "routerd.client.ipv4.observed",
		"--subject", "10.88.60.9/32",
		"--source-node", "cloud-a",
		"--id", "evt-self-capture",
	}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("self-captured observed event should be rejected")
	}
	if !strings.Contains(err.Error(), "locally captured") {
		t.Fatalf("error = %q, want locally captured message", err)
	}

	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	events, err := store.ListFederationEvents("cloudedge", true, time.Now().Unix())
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("rejected event was recorded: %+v", events)
	}

	out.Reset()
	if err := run([]string{
		"federation", "event", "emit",
		"--state-file", statePath,
		"--group", "cloudedge",
		"--type", "routerd.client.ipv4.observed",
		"--subject", "10.88.60.10/32",
		"--source-node", "onprem",
		"--id", "evt-remote",
		"-o", "json",
	}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("non-self observed event should pass: %v", err)
	}
	events, err = store.ListFederationEvents("cloudedge", true, time.Now().Unix())
	if err != nil {
		t.Fatalf("list events after pass: %v", err)
	}
	if len(events) != 1 || events[0].ID != "evt-remote" {
		t.Fatalf("want only non-self event recorded, got %+v", events)
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
			if err := store.UpdateDeliveryStatus(s.eventID, s.peer, s.status, 1, "", time.Time{}, time.Time{}); err != nil {
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
