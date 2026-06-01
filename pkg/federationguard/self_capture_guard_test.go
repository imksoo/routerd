// SPDX-License-Identifier: BSD-3-Clause

package federationguard

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type fakeDynamicConfigPartStore struct {
	parts []routerstate.DynamicConfigPartRecord
}

func (s fakeDynamicConfigPartStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return s.parts, nil
}

func TestRejectSelfCapturedObservedEventRejectsActiveCapturedAddress(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, remoteAddressClaim("capture-10", "10.77.60.10/32", "proxy-arp")),
	}}
	err := RejectSelfCapturedObservedEvent(store, federation.Event{
		Type:    federation.ObservedIPv4EventType,
		Subject: "10.77.60.10/32",
	}, now)
	var guardErr SelfCapturedObservedEventError
	if !errors.As(err, &guardErr) {
		t.Fatalf("error = %v, want SelfCapturedObservedEventError", err)
	}
	if guardErr.Address != "10.77.60.10" {
		t.Fatalf("guard address = %q, want bare IP", guardErr.Address)
	}
}

func TestRejectSelfCapturedObservedEventAllowsNonCapturedAddress(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, remoteAddressClaim("capture-10", "10.77.60.10/32", "proxy-arp")),
	}}
	if err := RejectSelfCapturedObservedEvent(store, federation.Event{
		Type:    federation.ObservedIPv4EventType,
		Subject: "10.77.60.11/32",
	}, now); err != nil {
		t.Fatalf("RejectSelfCapturedObservedEvent: %v, want nil", err)
	}
}

func TestRejectSelfCapturedObservedEventIgnoresNonObservedTypes(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, remoteAddressClaim("capture-10", "10.77.60.10/32", "proxy-arp")),
	}}
	if err := RejectSelfCapturedObservedEvent(store, federation.Event{
		Type:    federation.MobilityMemberHeartbeatType,
		Subject: "10.77.60.10/32",
	}, now); err != nil {
		t.Fatalf("RejectSelfCapturedObservedEvent: %v, want nil", err)
	}
}

func TestRejectSelfCapturedObservedEventIgnoresNonIPSubjectsAndPayloads(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, remoteAddressClaim("capture-10", "10.77.60.10/32", "proxy-arp")),
	}}
	if err := RejectSelfCapturedObservedEvent(store, federation.Event{
		Type:    federation.ObservedIPv4EventType,
		Subject: "not-an-ip",
		Payload: map[string]string{"address": "also-not-an-ip"},
	}, now); err != nil {
		t.Fatalf("RejectSelfCapturedObservedEvent: %v, want nil", err)
	}
}

func TestRejectSelfCapturedObservedEventPayloadAddressTakesPrecedence(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, remoteAddressClaim("capture-10", "10.77.60.10/32", "provider-secondary-ip")),
	}}
	err := RejectSelfCapturedObservedEvent(store, federation.Event{
		Type:    federation.ObservedIPv4EventType,
		Subject: "10.77.60.11/32",
		Payload: map[string]string{"address": "10.77.60.10/32"},
	}, now)
	var guardErr SelfCapturedObservedEventError
	if !errors.As(err, &guardErr) {
		t.Fatalf("error = %v, want payload address to trigger SelfCapturedObservedEventError", err)
	}
	if guardErr.Address != "10.77.60.10" {
		t.Fatalf("guard address = %q, want payload address", guardErr.Address)
	}
}

func dynamicPart(t *testing.T, now time.Time, resources ...api.Resource) routerstate.DynamicConfigPartRecord {
	t.Helper()
	data, err := json.Marshal(resources)
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:        "test",
		Generation:    1,
		ResourcesJSON: string(data),
		Status:        "active",
		ExpiresAt:     now.Add(time.Minute),
	}
}

func remoteAddressClaim(name, address, captureType string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{"routerd.net/dynamic-source": "test-source"},
		},
		Spec: api.RemoteAddressClaimSpec{
			DomainRef: "cloudedge",
			Address:   address,
			OwnerSide: "cloud",
			Capture:   api.AddressCapture{Type: captureType},
			Delivery:  api.AddressDelivery{Mode: "route"},
		},
	}
}
