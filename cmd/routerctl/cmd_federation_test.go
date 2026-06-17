// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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

func TestFederationEventEmitRejectsSelfCapturedObservedAddress(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	resources, err := json.Marshal([]api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{
			Name:        "claim-local",
			Annotations: map[string]string{"routerd.net/dynamic-source": "MobilityPool/cloudedge/node/cloud-a"},
		},
		Spec: api.RemoteAddressClaimSpec{
			Address:   "10.88.60.9/32",
			OwnerSide: "onprem",
			Capture:   api.AddressCapture{Type: "proxy-arp", Interface: "lan0"},
			Delivery:  api.AddressDelivery{Mode: "route", PeerRef: "onprem"},
		},
	}})
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:        "MobilityPool/cloudedge/node/cloud-a",
		Generation:    1,
		ObservedAt:    time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
		ResourcesJSON: string(resources),
		Status:        "active",
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

func TestFederationDeliveriesSummary(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")

	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	now := time.Now().UTC()
	observedAt := now.Add(-30 * time.Second)
	expiresAt := now.Add(10 * time.Minute)

	events := []routerstate.EventRecord{
		{ID: "evt-1", Group: "cloudedge", Type: "routerd.mobility.shard.assigned", Subject: "shard-a", SourceNode: "node-a", ObservedAt: observedAt, ExpiresAt: expiresAt},
		{ID: "evt-2", Group: "cloudedge", Type: "routerd.mobility.shard.assigned", Subject: "shard-b", SourceNode: "node-a", ObservedAt: observedAt, ExpiresAt: expiresAt},
		{ID: "evt-3", Group: "other", Type: "routerd.client.ipv4.observed", Subject: "10.1.2.3/32", SourceNode: "node-b", ObservedAt: observedAt, ExpiresAt: expiresAt},
	}
	for _, ev := range events {
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record event %s: %v", ev.ID, err)
		}
	}

	type delivery struct {
		eventID, peer, status string
		deliveredAt           time.Time
		eventExpiresAt        time.Time
	}
	deliveries := []delivery{
		{"evt-1", "peer-a", routerstate.DeliveryDelivered, now.Add(-25 * time.Second), expiresAt},
		{"evt-1", "peer-b", routerstate.DeliveryDelivered, now.Add(-20 * time.Second), expiresAt},
		{"evt-2", "peer-a", routerstate.DeliveryDelivered, now.Add(-22 * time.Second), expiresAt.Add(-5 * time.Minute)},
		{"evt-2", "peer-b", routerstate.DeliveryFailed, time.Time{}, time.Time{}},
		{"evt-3", "peer-c", routerstate.DeliveryPending, time.Time{}, time.Time{}},
	}
	for _, d := range deliveries {
		if err := store.RecordDelivery(d.eventID, d.peer); err != nil {
			t.Fatalf("record delivery %s/%s: %v", d.eventID, d.peer, err)
		}
		if d.status != routerstate.DeliveryPending {
			if err := store.UpdateDeliveryStatus(d.eventID, d.peer, d.status, 1, "", d.deliveredAt, d.eventExpiresAt); err != nil {
				t.Fatalf("update delivery %s/%s: %v", d.eventID, d.peer, err)
			}
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	summaryJSON := func(t *testing.T, extra ...string) []routerstate.DeliverySummaryRow {
		t.Helper()
		args := append([]string{
			"federation", "deliveries", "summary",
			"--state-file", statePath,
			"-o", "json",
		}, extra...)
		var out bytes.Buffer
		if err := run(args, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("summary %v: %v\n%s", extra, err, out.String())
		}
		var rows []routerstate.DeliverySummaryRow
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
			t.Fatalf("decode summary %v: %v\n%s", extra, err, out.String())
		}
		return rows
	}

	// (1) All groups, all peers.
	all := summaryJSON(t)
	if len(all) != 3 {
		t.Fatalf("want 3 summary rows (cloudedge/peer-a, cloudedge/peer-b, other/peer-c), got %d: %+v", len(all), all)
	}

	// (2) Filter by group.
	cloudedge := summaryJSON(t, "--group", "cloudedge")
	if len(cloudedge) != 2 {
		t.Fatalf("want 2 rows for cloudedge, got %d: %+v", len(cloudedge), cloudedge)
	}
	// peer-a: 2 delivered (1 stale TTL), 0 failed.
	peerA := cloudedge[0]
	if peerA.Peer != "peer-a" {
		t.Fatalf("first row peer = %q, want peer-a", peerA.Peer)
	}
	if peerA.Events != 2 || peerA.Delivered != 2 || peerA.Failed != 0 {
		t.Fatalf("peer-a summary = events=%d delivered=%d failed=%d, want 2/2/0", peerA.Events, peerA.Delivered, peerA.Failed)
	}
	if peerA.StaleTTL != 1 {
		t.Fatalf("peer-a staleTTL = %d, want 1 (evt-2 delivered with older eventExpiresAt)", peerA.StaleTTL)
	}
	if peerA.MaxLagSeconds <= 0 {
		t.Fatalf("peer-a maxLagSeconds = %d, want > 0", peerA.MaxLagSeconds)
	}

	// peer-b: 1 delivered, 1 failed.
	peerB := cloudedge[1]
	if peerB.Peer != "peer-b" {
		t.Fatalf("second row peer = %q, want peer-b", peerB.Peer)
	}
	if peerB.Delivered != 1 || peerB.Failed != 1 {
		t.Fatalf("peer-b summary = delivered=%d failed=%d, want 1/1", peerB.Delivered, peerB.Failed)
	}

	// (3) Filter by peer.
	byPeer := summaryJSON(t, "--group", "cloudedge", "--peer", "peer-a")
	if len(byPeer) != 1 || byPeer[0].Peer != "peer-a" {
		t.Fatalf("peer filter: got %+v", byPeer)
	}

	// (4) Filter by type.
	byType := summaryJSON(t, "--type", "routerd.client.ipv4.observed")
	if len(byType) != 1 || byType[0].Group != "other" {
		t.Fatalf("type filter: got %+v", byType)
	}

	// (5) Unknown group -> empty.
	empty := summaryJSON(t, "--group", "no-such-group")
	if len(empty) != 0 {
		t.Fatalf("unknown group: got %d rows", len(empty))
	}

	// (6) Table output doesn't error.
	var tableOut bytes.Buffer
	if err := run([]string{
		"federation", "deliveries", "summary",
		"--state-file", statePath,
		"--group", "cloudedge",
	}, &tableOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("table output: %v\n%s", err, tableOut.String())
	}
	table := tableOut.String()
	for _, want := range []string{"GROUP", "PEER", "DELIVERED", "STALE_TTL", "MAX_LAG"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table output missing %q:\n%s", want, table)
		}
	}
}

func TestFederationDeliveriesSummaryMinExpiresIn(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")

	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID:         "evt-ttl",
		Group:      "g",
		Type:       "t",
		SourceNode: "n",
		ObservedAt: now.Add(-10 * time.Second),
		ExpiresAt:  now.Add(5 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-ttl", "p"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-ttl", "p", routerstate.DeliveryDelivered, 1, "", now, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{
		"federation", "deliveries", "summary",
		"--state-file", statePath,
		"-o", "json",
	}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("summary: %v\n%s", err, out.String())
	}
	var rows []routerstate.DeliverySummaryRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].MinExpiresInSeconds <= 0 || rows[0].MinExpiresInSeconds > 300 {
		t.Fatalf("minExpiresInSeconds = %d, want (0, 300]", rows[0].MinExpiresInSeconds)
	}
	if rows[0].StaleTTL != 0 {
		t.Fatalf("staleTTL = %d, want 0 (no TTL drift)", rows[0].StaleTTL)
	}
}
