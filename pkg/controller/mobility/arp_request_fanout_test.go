// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
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
