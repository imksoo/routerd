// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// TestEmitThenOutboxPush proves the real routerctl emit path connects to the
// eventd Outbox: `routerctl federation event emit` writes a local event into the
// state DB, and an Outbox running over that SAME DB pushes it to a peer receiver.
// This catches CLI/daemon disconnects. The Outbox nodeName MUST equal the emit's
// --source-node, otherwise loop prevention skips the event — that coupling is
// exactly what this test documents.
func TestEmitThenOutboxPush(t *testing.T) {
	dir := t.TempDir()
	senderDB := filepath.Join(dir, "sender.db")

	const group = "cloudedge-lab"
	const sourceNode = "onprem-router07"
	const peerName = "cloud"
	const eventID = "evt-emit-outbox-1"
	const subject = "10.88.60.9/32"

	// 1. Emit a federation event via the real CLI command into senderDB.
	emitArgs := []string{
		"--state-file", senderDB,
		"--group", group,
		"--type", "routerd.client.ipv4.observed",
		"--subject", subject,
		"--source-node", sourceNode,
		"--id", eventID,
		"--payload", "address=10.88.60.9/32",
		"--ttl", "30m",
	}
	if err := federationEventEmitCommand(emitArgs, io.Discard); err != nil {
		t.Fatalf("emit: %v", err)
	}

	// 2. Open the SAME state DB that the CLI wrote to.
	senderStore, err := routerstate.OpenSQLite(senderDB)
	if err != nil {
		t.Fatalf("open sender db: %v", err)
	}
	t.Cleanup(func() { _ = senderStore.Close() })

	// 3. Stand up a receiver on a separate store behind an httptest server.
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	testSecret := []byte("0123456789abcdef0123456789abcdef")

	recvPath := filepath.Join(dir, "receiver.db")
	recvStore, err := routerstate.OpenSQLite(recvPath)
	if err != nil {
		t.Fatalf("open receiver db: %v", err)
	}
	t.Cleanup(func() { _ = recvStore.Close() })

	receiver := eventd.NewReceiver(recvStore, testSecret, group, peerName, 5*time.Minute, clock)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	// 4. Build a Pusher + Outbox over the sender DB. nodeName == --source-node.
	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	noSleep := func(time.Duration) {}
	pusher := eventd.NewPusher(senderStore, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)
	outbox := eventd.NewOutbox(senderStore, senderStore, pusher, group, sourceNode, time.Second, clock)
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// 5a. Receiver store has the emitted event with subject + payload intact.
	got, err := recvStore.ListFederationEvents(group, true, now.Unix())
	if err != nil {
		t.Fatalf("list receiver events: %v", err)
	}
	var found *routerstate.EventRecord
	for i := range got {
		if got[i].ID == eventID {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("receiver missing emitted event %q; got %+v", eventID, got)
	}
	if found.Subject != subject {
		t.Errorf("receiver subject = %q, want %q", found.Subject, subject)
	}
	if found.Payload["address"] != "10.88.60.9/32" {
		t.Errorf("receiver payload address = %q, want 10.88.60.9/32", found.Payload["address"])
	}

	// 5b. Sender records a delivered row for the peer with attempts>=1.
	deliveries, err := senderStore.ListDeliveries(eventID, peerName)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("delivery count = %d, want 1: %+v", len(deliveries), deliveries)
	}
	if deliveries[0].Status != routerstate.DeliveryDelivered {
		t.Errorf("delivery status = %q, want delivered", deliveries[0].Status)
	}
	if deliveries[0].Attempts < 1 {
		t.Errorf("delivery attempts = %d, want >=1", deliveries[0].Attempts)
	}
}
