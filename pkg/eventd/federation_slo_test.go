// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// TestFederationSLO_PeerPartitionReplayRecoversWithinSLO exercises the full
// outbox replay pipeline: peer partition causes delivery failure, peer recovery
// allows replay, and the recovered delivery completes within SLO thresholds.
//
// Scenario:
//  1. httptest peer returns 503 (partition)
//  2. Seed a local event in the sender store
//  3. Outbox.RunOnce -> delivery fails, recorded as "failed"
//  4. Flip httptest to 200 (peer recovery)
//  5. Outbox.RunOnce -> outbox replays failed delivery -> succeeds
//  6. Verify delivery status is "delivered"
//  7. Verify delivery lag is within SLO (warn < 60s, fail < 180s)
func TestFederationSLO_PeerPartitionReplayRecoversWithinSLO(t *testing.T) {
	const (
		group    = "cloudedge"
		peerName = "cloud-a"
		nodeName = "onprem"

		lagWarnSeconds = 60.0
		lagFailSeconds = 180.0
	)

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// The clock at partition time: 10 seconds after the event was observed.
	partitionTime := observedAt.Add(10 * time.Second)
	// The clock at recovery time: 30 seconds after the event was observed.
	// This keeps the total lag well within both SLO thresholds.
	recoveryTime := observedAt.Add(30 * time.Second)

	// Mutable clock: starts at partition time, advanced to recovery time later.
	var clockMu atomic.Int64
	clockMu.Store(partitionTime.UnixNano())
	clock := func() time.Time {
		return time.Unix(0, clockMu.Load())
	}

	senderStore := openStore(t, "slo-sender.db")
	receiverStore := openStore(t, "slo-receiver.db")

	// Receiver: accepts signed events once the partition is lifted.
	receiver := eventd.NewReceiver(receiverStore, testSecret, group, "cloud", 5*time.Minute, clock)

	// Atomic flag controls whether the peer is partitioned (503) or healthy (200).
	var partitioned atomic.Bool
	partitioned.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if partitioned.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		receiver.Handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	noSleep := func(time.Duration) {}
	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	pusher := eventd.NewPusher(senderStore, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 1, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)

	outbox := eventd.NewOutbox(senderStore, senderStore, pusher, group, nodeName,
		10*time.Second, clock)

	// --- Step 1-2: Seed a local event. ---
	seedEvent(t, senderStore, "slo-evt-1", group,
		"routerd.client.ipv4.observed", "10.88.60.100/32", observedAt)

	// --- Step 3: RunOnce during partition -> delivery fails. ---
	ctx := context.Background()
	if err := outbox.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce (partitioned): %v", err)
	}

	deliveries, err := senderStore.ListDeliveries("slo-evt-1", peerName)
	if err != nil {
		t.Fatalf("list deliveries after partition: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("delivery count after partition = %d, want 1", len(deliveries))
	}
	if deliveries[0].Status != routerstate.DeliveryFailed {
		t.Fatalf("delivery status after partition = %q, want %q",
			deliveries[0].Status, routerstate.DeliveryFailed)
	}

	// --- Step 4: Peer recovers. Advance clock to recovery time. ---
	partitioned.Store(false)
	clockMu.Store(recoveryTime.UnixNano())

	// --- Step 5: RunOnce after recovery -> outbox replays failed delivery. ---
	if err := outbox.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce (recovered): %v", err)
	}

	// --- Step 6: Verify delivery status is "delivered". ---
	deliveries, err = senderStore.ListDeliveries("slo-evt-1", peerName)
	if err != nil {
		t.Fatalf("list deliveries after recovery: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("delivery count after recovery = %d, want 1", len(deliveries))
	}
	if deliveries[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("delivery status after recovery = %q, want %q",
			deliveries[0].Status, routerstate.DeliveryDelivered)
	}

	// --- Step 7: Verify delivery lag is within SLO thresholds. ---
	// The lag is the time between ObservedAt and DeliveredAt (which is set by
	// the pusher's clock at the moment the delivery succeeds).
	if deliveries[0].DeliveredAt.IsZero() {
		t.Fatal("DeliveredAt is zero after successful delivery")
	}
	lag := deliveries[0].DeliveredAt.Sub(observedAt).Seconds()
	if lag < 0 {
		t.Fatalf("delivery lag = %.1fs, expected non-negative", lag)
	}
	if lag >= lagFailSeconds {
		t.Fatalf("SLO FAIL: delivery lag = %.1fs, exceeds fail threshold of %.0fs",
			lag, lagFailSeconds)
	}
	if lag >= lagWarnSeconds {
		t.Logf("SLO WARN: delivery lag = %.1fs, exceeds warn threshold of %.0fs",
			lag, lagWarnSeconds)
	}
	t.Logf("delivery lag = %.1fs (warn=%.0fs, fail=%.0fs) -- within SLO",
		lag, lagWarnSeconds, lagFailSeconds)

	// Verify the receiver actually persisted the event.
	recvEvents, err := receiverStore.ListFederationEvents(group, true, recoveryTime.Unix())
	if err != nil {
		t.Fatalf("list receiver events: %v", err)
	}
	if len(recvEvents) != 1 {
		t.Fatalf("receiver event count = %d, want 1", len(recvEvents))
	}
	if recvEvents[0].Subject != "10.88.60.100/32" {
		t.Errorf("receiver event subject = %q, want 10.88.60.100/32", recvEvents[0].Subject)
	}
}
