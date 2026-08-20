// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventd"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func arpFanoutPoolSpec() testMobilityPoolSpec {
	discovery := api.MobilityOwnershipDiscovery{
		Mode: "onprem-l2",
		Sources: []api.MobilityOwnershipDiscoverySource{{
			Type: OnPremSourceOnDemandARP, Interface: "svnet1",
		}},
		Scope: api.MobilityOwnershipDiscoveryScope{
			ExcludeAddresses: []string{"192.168.123.1/32"},
		},
	}
	capture := api.MobilityMemberCapture{Type: "proxy-arp", Interface: "svnet1", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
	return testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix: "192.168.123.0/24", GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
		{NodeRef: "pve-rt07", Site: "pve07", Role: "onprem", Capture: capture, OwnershipDiscovery: discovery},
		{NodeRef: "pve-rt08", Site: "pve08", Role: "onprem", Capture: capture, OwnershipDiscovery: discovery},
	}}
}

func TestARPRequestFactFansOutToRemoteLeafOnce(t *testing.T) {
	now := time.Date(2026, 8, 20, 17, 0, 0, 0, time.UTC)
	spec := arpFanoutPoolSpec()
	sourceStore := testStore(t, now)
	source := DiscoveryController{Router: staticRouter("pve-rt07", spec), Store: sourceStore, Now: func() time.Time { return now }}
	request := daemonapi.DaemonEvent{
		Type: OnPremARPRequestObservedEvent,
		Time: now,
		Attributes: map[string]string{
			"target": "192.168.123.129", "pool": "cloudedge", "interface": "svnet1",
			"requesterIP": "192.168.123.132", "requesterMAC": "02:00:00:00:00:32",
		},
	}
	if err := source.HandleEvent(context.Background(), request); err != nil {
		t.Fatalf("source HandleEvent: %v", err)
	}
	events, err := sourceStore.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("source events = %#v, want one ARP request fact", events)
	}
	event := events[0]
	if event.Type != OnPremARPRequestObservedEvent || event.SourceNode != "pve-rt07" || event.Subject != "192.168.123.129/32" || event.ExpiresAt.Sub(event.ObservedAt) != onPremARPRequestTTL {
		t.Fatalf("source event = %#v", event)
	}

	remoteStore := testStore(t, now)
	if err := remoteStore.RecordFederationEvent(event); err != nil {
		t.Fatal(err)
	}
	var probes []string
	remote := DiscoveryController{
		Router:           staticRouter("pve-rt08", spec),
		Store:            remoteStore,
		Now:              func() time.Time { return now.Add(time.Second) },
		ARPProbeRequests: NewARPProbeRequestTracker(),
		ProbeARP: func(_ context.Context, pool, address string) error {
			probes = append(probes, pool+"/"+address)
			return nil
		},
	}
	if err := remote.ReconcileARPProbeRequests(context.Background()); err != nil {
		t.Fatalf("remote ReconcileARPProbeRequests: %v", err)
	}
	if err := remote.ReconcileARPProbeRequests(context.Background()); err != nil {
		t.Fatalf("remote second ReconcileARPProbeRequests: %v", err)
	}
	if len(probes) != 1 || probes[0] != "cloudedge/192.168.123.129/32" {
		t.Fatalf("probes = %#v, want one remote target probe", probes)
	}

	// The source leaf already probed synchronously while observing the packet;
	// its own federation fact must not cause another command probe.
	source.ProbeARP = func(_ context.Context, _, _ string) error {
		t.Fatal("source leaf reprocessed its own ARP request fact")
		return nil
	}
	source.ARPProbeRequests = NewARPProbeRequestTracker()
	if err := source.ReconcileARPProbeRequests(context.Background()); err != nil {
		t.Fatalf("source ReconcileARPProbeRequests: %v", err)
	}
}

func TestARPRequestFactCrossesSignedFederationTransport(t *testing.T) {
	now := time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	secret := []byte("test-only-shared-event-secret")
	spec := arpFanoutPoolSpec()
	sourceStore := testStore(t, now)
	remoteStore := testStore(t, now)

	source := DiscoveryController{
		Router: staticRouter("pve-rt07", spec),
		Store:  sourceStore,
		Now:    func() time.Time { return now },
	}
	request := daemonapi.DaemonEvent{
		Type: OnPremARPRequestObservedEvent,
		Time: now,
		Attributes: map[string]string{
			"target": "192.168.123.129", "pool": "cloudedge", "interface": "svnet1",
		},
	}
	if err := source.HandleEvent(context.Background(), request); err != nil {
		t.Fatalf("source HandleEvent: %v", err)
	}

	receiver := eventd.NewReceiver(remoteStore, secret, "cloudedge", "pve-rt08", time.Minute, func() time.Time { return now })
	server := httptest.NewServer(receiver.Handler())
	t.Cleanup(server.Close)
	pusher := eventd.NewPusher(sourceStore, secret, []eventd.PeerConfig{{
		NodeName: "pve-rt08", Endpoint: server.URL,
		Types: []string{OnPremARPRequestObservedEvent}, SubjectPrefixes: []string{"192.168.123."},
	}}, eventd.PushRetry{MaxAttempts: 1}, server.Client(), func() time.Time { return now }, func(time.Duration) {})
	outbox := eventd.NewOutbox(sourceStore, sourceStore, pusher, "cloudedge", "pve-rt07", time.Second, func() time.Time { return now })
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("signed federation outbox: %v", err)
	}
	if status := receiver.Status(); status.Received != 1 || status.Rejected != 0 || status.StoredEvents != 1 {
		t.Fatalf("receiver status = %#v, want one accepted event", status)
	}

	var probes []string
	remote := DiscoveryController{
		Router:           staticRouter("pve-rt08", spec),
		Store:            remoteStore,
		Now:              func() time.Time { return now.Add(time.Second) },
		ARPProbeRequests: NewARPProbeRequestTracker(),
		ProbeARP: func(_ context.Context, pool, address string) error {
			probes = append(probes, pool+"/"+address)
			return nil
		},
	}
	if err := remote.ReconcileARPProbeRequests(context.Background()); err != nil {
		t.Fatalf("remote ReconcileARPProbeRequests: %v", err)
	}
	if len(probes) != 1 || probes[0] != "cloudedge/192.168.123.129/32" {
		t.Fatalf("probes = %#v, want signed remote target probe", probes)
	}
}

func TestARPRequestDaemonReplayDoesNotRefreshExpiredDemand(t *testing.T) {
	now := time.Date(2026, 8, 20, 17, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := DiscoveryController{Router: staticRouter("pve-rt07", arpFanoutPoolSpec()), Store: store, Now: func() time.Time { return now }}
	event := daemonapi.DaemonEvent{
		Type: OnPremARPRequestObservedEvent,
		Time: now.Add(-onPremARPRequestTTL),
		Attributes: map[string]string{
			"target": "192.168.123.129", "pool": "cloudedge", "interface": "svnet1",
		},
	}
	if err := controller.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("stale replay produced events: %#v", events)
	}
}

func TestFederatedARPRequestTargetFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 20, 17, 0, 0, 0, time.UTC)
	spec := arpFanoutPoolSpec()
	router := staticRouter("pve-rt08", spec)
	poolSpec, err := router.Spec.Resources[2].MobilityPoolSpec()
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := resolveNormalizedMobilityPool(router, poolSpec)
	if err != nil {
		t.Fatal(err)
	}
	pool := normalized.Pool
	pool.Name = "cloudedge"
	base := onPremARPRequestEvent(pool, "192.168.123.129/32", onPremARPRequest{}, now)
	base.SourceNode = "pve-rt07"
	if target, ok := federatedARPRequestTarget(pool, base, now.Add(time.Second)); !ok || target != "192.168.123.129/32" {
		t.Fatalf("valid target = %q ok=%v", target, ok)
	}

	for name, mutate := range map[string]func(*routerstate.EventRecord){
		"self origin":      func(event *routerstate.EventRecord) { event.SourceNode = "pve-rt08" },
		"unknown member":   func(event *routerstate.EventRecord) { event.SourceNode = "pve-rt99" },
		"expired":          func(event *routerstate.EventRecord) { event.ExpiresAt = now.Add(-time.Second) },
		"wrong group":      func(event *routerstate.EventRecord) { event.Group = "other" },
		"wrong pool":       func(event *routerstate.EventRecord) { event.Payload["pool"] = "other" },
		"subject mismatch": func(event *routerstate.EventRecord) { event.Subject = "192.168.123.130/32" },
		"excluded": func(event *routerstate.EventRecord) {
			event.Subject = "192.168.123.1/32"
			event.Payload["address"] = event.Subject
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Rebuild a valid baseline before applying each isolated mutation.
			event := onPremARPRequestEvent(pool, "192.168.123.129/32", onPremARPRequest{}, now)
			event.SourceNode = "pve-rt07"
			mutate(&event)
			if target, ok := federatedARPRequestTarget(pool, event, now.Add(time.Second)); ok {
				t.Fatalf("unsafe event accepted as %s", target)
			}
		})
	}
}

func TestARPProbeRequestTrackerAllowsRefreshedStableEvent(t *testing.T) {
	tracker := NewARPProbeRequestTracker()
	now := time.Date(2026, 8, 20, 17, 0, 0, 0, time.UTC)
	if !tracker.claim("same", now, now.Add(time.Minute), now) {
		t.Fatal("initial event was not claimed")
	}
	if tracker.claim("same", now, now.Add(time.Minute), now) {
		t.Fatal("duplicate event was claimed")
	}
	if !tracker.claim("same", now.Add(10*time.Second), now.Add(time.Minute), now.Add(10*time.Second)) {
		t.Fatal("refreshed stable event ID was not claimed")
	}
}
