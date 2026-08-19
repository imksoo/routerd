// SPDX-License-Identifier: BSD-3-Clause

package federationguard

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
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
		dynamicPart(t, now, localCaptureIntent("capture-10", "10.77.60.10/32", "proxy-arp")),
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
		dynamicPart(t, now, localCaptureIntent("capture-10", "10.77.60.10/32", "proxy-arp")),
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
		dynamicPart(t, now, localCaptureIntent("capture-10", "10.77.60.10/32", "proxy-arp")),
	}}
	if err := RejectSelfCapturedObservedEvent(store, federation.Event{
		Type:    "routerd.mobility.unrelated",
		Subject: "10.77.60.10/32",
	}, now); err != nil {
		t.Fatalf("RejectSelfCapturedObservedEvent: %v, want nil", err)
	}
}

func TestRejectSelfCapturedObservedEventIgnoresNonIPSubjectsAndPayloads(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, localCaptureIntent("capture-10", "10.77.60.10/32", "proxy-arp")),
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
		dynamicPart(t, now, localCaptureIntent("capture-10", "10.77.60.10/32", "provider-secondary-ip")),
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

func TestRejectSelfCapturedObservedEventIgnoresNonMobilityPlanSource(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	part := dynamicPart(t, now, localCaptureIntent("capture-10", "10.77.60.10/32", "proxy-arp"))
	part.Source = "plugin/untrusted"
	if err := RejectSelfCapturedObservedEvent(fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{part}}, federation.Event{
		Type: federation.ObservedIPv4EventType, Subject: "10.77.60.10/32",
	}, now); err != nil {
		t.Fatalf("non-MobilityPool typed payload suppressed an observed event: %v", err)
	}
}

func TestRejectSelfCapturedObservedEventGuardsHeldCapture(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	intent := localCaptureIntent("capture-10", "10.77.60.10/32", "proxy-arp")
	intent.Disposition = dynamicconfig.CaptureHold
	err := RejectSelfCapturedObservedEvent(fakeDynamicConfigPartStore{parts: []routerstate.DynamicConfigPartRecord{
		dynamicPart(t, now, intent),
	}}, federation.Event{Type: federation.ObservedIPv4EventType, Subject: "10.77.60.10/32"}, now)
	var guardErr SelfCapturedObservedEventError
	if !errors.As(err, &guardErr) {
		t.Fatalf("held capture error = %v, want SelfCapturedObservedEventError", err)
	}
}

func dynamicPart(t *testing.T, now time.Time, intents ...dynamicconfig.LocalCaptureIntent) routerstate.DynamicConfigPartRecord {
	t.Helper()
	data, err := json.Marshal(dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.77.60.0/24", Captures: intents})
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:                "MobilityPool/cloudedge/node/test",
		Generation:            1,
		ObservedAt:            now,
		MobilityDataplaneJSON: string(data),
		Status:                "active",
		ExpiresAt:             now.Add(5 * time.Minute),
		Digest:                "sha256:self-capture-guard",
	}
}

func localCaptureIntent(name, address, captureType string) dynamicconfig.LocalCaptureIntent {
	return dynamicconfig.LocalCaptureIntent{ID: name, PoolRef: "cloudedge", Address: address, CaptureType: captureType, CaptureInterface: "lan0", Disposition: dynamicconfig.CaptureDesired}
}
