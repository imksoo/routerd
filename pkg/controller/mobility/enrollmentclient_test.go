// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSAMEnrollmentClientJoinsWhenRRSetMissing(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want 1/1", client.submitCount, client.fetchCount)
	}
	if records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs"); err != nil || len(records) != 1 {
		t.Fatalf("SAMRRSet dynamic records = %#v err=%v, want one", records, err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["observedRRSet"] != "SAMRRSet/pve-rrs" || status["lastSuccess"] == "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSAMEnrollmentClientSkipsValidNonExpiringRRSet(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := seedSAMEnrollmentClientTopology(store, now, now.Add(time.Hour), nil); err != nil {
		t.Fatalf("seed rrset: %v", err)
	}
	status := map[string]any{"claimDigest": samEnrollmentClientClaimDigest(testSAMEnrollmentClaimResource("nonce-a"))}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", status); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want 0/0", client.submitCount, client.fetchCount)
	}
}

func TestSAMEnrollmentClientRefreshesNearExpiry(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := seedSAMEnrollmentClientTopology(store, now.Add(-4*time.Minute), now.Add(2*time.Minute), nil); err != nil {
		t.Fatalf("seed rrset: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want refresh 1/1", client.submitCount, client.fetchCount)
	}
}

func TestSAMEnrollmentClientBacksOffAfterFailure(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, submitErr: errors.New("rr unavailable")}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 {
		t.Fatalf("submitCount = %d, want 1", client.submitCount)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["backoff"] != "10s" || status["nextAttempt"] == "" {
		t.Fatalf("failure status = %#v", status)
	}
	client.submitErr = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if client.submitCount != 1 {
		t.Fatalf("submitCount after backoff-gated reconcile = %d, want still 1", client.submitCount)
	}
}

func TestSAMEnrollmentClientRefreshesWhenClaimChanges(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-b")

	if err := seedSAMEnrollmentClientTopology(store, now, now.Add(time.Hour), nil); err != nil {
		t.Fatalf("seed rrset: %v", err)
	}
	status := map[string]any{"claimDigest": samEnrollmentClientClaimDigest(testSAMEnrollmentClaimResource("nonce-a"))}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", status); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want claim-change refresh 1/1", client.submitCount, client.fetchCount)
	}
	if got := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")["reason"]; got != "claim-changed" {
		t.Fatalf("status reason = %#v, want claim-changed", got)
	}
}

func TestSAMEnrollmentClientPersistsDirectTopologyAtomically(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one atomic topology part", records)
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(records[0].ResourcesJSON), &resources); err != nil {
		t.Fatalf("decode resources: %v", err)
	}
	if len(resources) != 2 || resources[0].Kind != "SAMRRSet" || resources[1].Kind != "SAMPeerGroup" {
		t.Fatalf("resources = %#v, want RRSet plus SAMPeerGroup", resources)
	}
	if records[0].Source != "SAMRRSet/pve-rrs" {
		t.Fatalf("source = %q", records[0].Source)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want observed direct peer group", status)
	}
}

func TestSAMEnrollmentClientPersistsRRSetWithoutOptionalDirectTopology(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want 1/1", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetFallback(t, store)
}

func TestSAMEnrollmentClientPersistsRRSetWhenOptionalDirectTopologyIsMalformed(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	peerGroup.Spec = map[string]any{"nodes": "not-a-node-list"}
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want 1/1", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetFallback(t, store)
}

func TestSAMEnrollmentClientPersistsRRSetWhenOptionalDirectTopologyIsSemanticallyInvalid(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	peerGroup.Spec = api.SAMPeerGroupSpec{
		EnrollmentPolicyRef:  "SAMEnrollmentPolicy/pve-wg-leaves",
		TransportFingerprint: "sha256:mesh",
		Nodes: []api.SAMNodeSpec{{
			NodeRef:        "pve-rr",
			RouteReflector: true,
			SAMEndpoint:    "10.30.0.10",
		}},
	}
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMEnrollmentClientRRSetFallback(t, store)
}

func TestSAMEnrollmentClientRejectsDirectPeerGroupForNonDirectClaim(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs"); err != nil || len(records) != 0 {
		t.Fatalf("records = %#v err=%v, want no unrequested direct topology", records, err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	reason, _ := status["reason"].(string)
	if status["phase"] != "Degraded" || !strings.Contains(reason, "non-direct claim") {
		t.Fatalf("status = %#v", status)
	}
}

func TestSAMEnrollmentClientUsesRRSetOnlyCacheForDirectClaim(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

	if err := seedSAMEnrollmentClientTopology(store, now, now.Add(time.Hour), nil); err != nil {
		t.Fatalf("seed old rrset-only record: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(claim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want cached RRSet fallback", client.submitCount, client.fetchCount)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["reason"] != "rrset-current" {
		t.Fatalf("status = %#v, want ready RRSet fallback", status)
	}
	if _, found := status["observedDirectPeerGroup"]; found {
		t.Fatalf("status = %#v, want no observed direct peer group", status)
	}
}

func TestSAMEnrollmentClientRefreshesDirectTopologyBeforeRRLeaseExpiry(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	expiresAt := now.Add(time.Hour)
	emptyPeerGroup := testSAMEnrollmentDirectPeerGroupResource()
	emptySpec := emptyPeerGroup.Spec.(api.SAMPeerGroupSpec)
	emptySpec.Nodes = nil
	emptySpec.OwnedPrefixesByNode = nil
	emptyPeerGroup.Spec = emptySpec
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &emptyPeerGroup); err != nil {
		t.Fatalf("seed empty direct topology: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(claim),
		"lastSuccess": now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client.peerGroup = &peerGroup

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want direct GET-only refresh 0/1", client.submitCount, client.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want one refreshed topology", records, err)
	}
	if !records[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("refreshed direct topology expiry = %s, want existing RR lease %s", records[0].ExpiresAt, expiresAt)
	}
	assertSAMEnrollmentClientRecordContains(t, records[0], "pve-leaf-b")
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["reason"] != "direct-topology-refresh" || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want refreshed direct topology", status)
	}
}

func TestSAMEnrollmentClientDropsDirectTopologyWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, fetchErr: errors.New("rr unavailable")}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(claim),
		"lastSuccess": now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want failed direct GET-only refresh 0/1", client.submitCount, client.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want RR fallback", records, err)
	}
	if !records[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("RR fallback expiry = %s, want existing RR lease %s", records[0].ExpiresAt, expiresAt)
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(records[0].ResourcesJSON), &resources); err != nil {
		t.Fatalf("decode fallback resources: %v", err)
	}
	if len(resources) != 1 || resources[0].Kind != "SAMRRSet" {
		t.Fatalf("resources = %#v, want RR fallback only", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" {
		t.Fatalf("status = %#v, want direct refresh degraded with RR fallback", status)
	}
	if _, found := status["observedDirectPeerGroup"]; found {
		t.Fatalf("status = %#v, want stale direct group removed", status)
	}
}

func TestSAMEnrollmentClientDropsDirectTopologyWhenRRLeaseRefreshFails(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, submitErr: errors.New("rr unavailable")}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	expiresAt := now.Add(2 * time.Minute)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(claim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want failed lease refresh 1/0", client.submitCount, client.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want RR fallback", records, err)
	}
	if !records[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("RR fallback expiry = %s, want existing RR lease %s", records[0].ExpiresAt, expiresAt)
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(records[0].ResourcesJSON), &resources); err != nil {
		t.Fatalf("decode fallback resources: %v", err)
	}
	if len(resources) != 1 || resources[0].Kind != "SAMRRSet" {
		t.Fatalf("resources = %#v, want RR fallback only", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["reason"] == "" {
		t.Fatalf("status = %#v, want failed RR lease refresh with fallback", status)
	}
	if _, found := status["observedDirectPeerGroup"]; found {
		t.Fatalf("status = %#v, want stale direct group removed", status)
	}
}

func TestSAMEnrollmentClientDropsCachedDirectTopologyWhenOptOutRefreshFails(t *testing.T) {
	tests := []struct {
		name        string
		submitErr   error
		fetchErr    error
		wantSubmits int
		wantFetches int
	}{
		{
			name:        "submit failure",
			submitErr:   errors.New("rr unavailable"),
			wantSubmits: 1,
			wantFetches: 0,
		},
		{
			name:        "topology fetch failure",
			fetchErr:    errors.New("rr unavailable"),
			wantSubmits: 1,
			wantFetches: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
			store := newSAMEnrollmentClientTestStore()
			client := &fakeSAMEnrollmentJoinClient{now: now, submitErr: tt.submitErr, fetchErr: tt.fetchErr}
			controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
			oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
			if err != nil {
				t.Fatalf("samEnrollmentClientClaim: %v", err)
			}
			peerGroup := testSAMEnrollmentDirectPeerGroupResource()
			expiresAt := now.Add(time.Hour)
			if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
				t.Fatalf("seed direct topology: %v", err)
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, false)
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
				"claimDigest": samEnrollmentClientClaimDigest(oldClaim),
				"lastSuccess": now.Format(time.RFC3339),
			}); err != nil {
				t.Fatalf("SaveObjectStatus: %v", err)
			}

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if client.submitCount != tt.wantSubmits || client.fetchCount != tt.wantFetches {
				t.Fatalf("submit/fetch = %d/%d, want %d/%d", client.submitCount, client.fetchCount, tt.wantSubmits, tt.wantFetches)
			}
			records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
			if err != nil || len(records) != 1 {
				t.Fatalf("records = %#v err=%v, want RR fallback", records, err)
			}
			if !records[0].ExpiresAt.Equal(expiresAt) {
				t.Fatalf("RR fallback expiry = %s, want existing RR lease %s", records[0].ExpiresAt, expiresAt)
			}
			var resources []api.Resource
			if err := json.Unmarshal([]byte(records[0].ResourcesJSON), &resources); err != nil {
				t.Fatalf("decode fallback resources: %v", err)
			}
			if len(resources) != 1 || resources[0].Kind != "SAMRRSet" {
				t.Fatalf("resources = %#v, want RR fallback only", resources)
			}
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if status["phase"] != "Degraded" || status["reason"] == "" {
				t.Fatalf("status = %#v, want failed opt-out refresh with fallback", status)
			}
			if _, found := status["observedDirectPeerGroup"]; found {
				t.Fatalf("status = %#v, want cached direct group removed", status)
			}
		})
	}
}

func TestSAMEnrollmentClientDropsCachedDirectTopologyWhenPolicyChangeRefreshFails(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, submitErr: errors.New("rr unavailable")}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	setSAMEnrollmentClientPolicyRef(t, controller.Router, "SAMEnrollmentPolicy/pve-wg-leaves-next")
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(oldClaim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want failed policy refresh 1/0", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["reason"] == "" {
		t.Fatalf("status = %#v, want failed policy refresh with fallback", status)
	}
	if _, found := status["observedDirectPeerGroup"]; found {
		t.Fatalf("status = %#v, want cached direct group removed", status)
	}
}

func TestSAMEnrollmentClientRetriesDirectOptOutWhenFallbackPersistenceFails(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	current := now
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	controller.Now = func() time.Time { return current }
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, false)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(oldClaim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	store.upsertErr = errors.New("state unavailable")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch after failed local withdrawal = %d/%d, want 0/0", client.submitCount, client.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 || !strings.Contains(records[0].ResourcesJSON, "pve-direct-leaves") {
		t.Fatalf("records = %#v err=%v, want retained direct group after failed persistence", records, err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["claimDigest"] != samEnrollmentClientClaimDigest(oldClaim) {
		t.Fatalf("status = %#v, want pending direct opt-out", status)
	}

	store.upsertErr = nil
	// The first failed local withdrawal wrote the configured retry backoff.
	// Reconcile after it expires, just as the one-minute production controller
	// would, rather than expecting an immediate retry at the same timestamp.
	current = current.Add(11 * time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch after successful retry = %d/%d, want 1/1", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status = store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" {
		t.Fatalf("status = %#v, want successful direct opt-out", status)
	}
}

func TestSAMEnrollmentClientDropsCachedDirectTopologyForCombinedOptOutAndPolicyChange(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, submitErr: errors.New("rr unavailable")}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, false)
	setSAMEnrollmentClientPolicyRef(t, controller.Router, "SAMEnrollmentPolicy/pve-wg-leaves-next")
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(oldClaim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want failed policy refresh 1/0", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
}

func TestSAMEnrollmentClientRequiresDirectTopologyAgreementAcrossEndpoints(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	peerGroupOther := testSAMEnrollmentDirectPeerGroupResourceFor("pve-leaf-c", "10.30.0.23", "10.77.60.23/32")
	tests := []struct {
		name       string
		second     *api.Resource
		wantDirect bool
	}{
		{name: "matching topology", second: &peerGroup, wantDirect: true},
		{name: "mismatched topology falls back", second: &peerGroupOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newSAMEnrollmentClientTestStore()
			rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
			rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: tt.second}
			controller := SAMEnrollmentClientController{
				Router: testSAMEnrollmentClientRouter("nonce-a"),
				Store:  store,
				Now:    func() time.Time { return now },
				ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
					return []SAMEnrollmentJoinClient{rrA, rrB}
				},
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if rrA.submitCount != 1 || rrB.submitCount != 1 || rrA.fetchCount != 1 || rrB.fetchCount != 1 {
				t.Fatalf("RR A submit/fetch=%d/%d, RR B=%d/%d, want 1/1 each", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
			}
			records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
			if err != nil || len(records) != 1 {
				t.Fatalf("records = %#v err=%v, want one topology", records, err)
			}
			containsDirect := strings.Contains(records[0].ResourcesJSON, "pve-direct-leaves")
			if containsDirect != tt.wantDirect {
				t.Fatalf("resources = %s, direct=%t want %t", records[0].ResourcesJSON, containsDirect, tt.wantDirect)
			}
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if tt.wantDirect && status["phase"] != "Ready" {
				t.Fatalf("status = %#v, want ready direct topology", status)
			}
			if !tt.wantDirect && status["phase"] != "Degraded" {
				t.Fatalf("status = %#v, want disagreement degradation with RR fallback", status)
			}
		})
	}
}

func TestSAMEnrollmentClientRetriesDirectClaimRotationUntilEveryRRAcceptsIt(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	current := now
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup, submitErr: errors.New("stale RR")}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return current },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rrA, rrB}
		},
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	setSAMEnrollmentClientJoinNonce(t, controller.Router, "nonce-b")
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(oldClaim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["claimDigest"] != samEnrollmentClientClaimDigest(oldClaim) {
		t.Fatalf("status = %#v, want unconfirmed direct claim rotation", status)
	}

	current = current.Add(11 * time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if rrA.submitCount != 2 || rrB.submitCount != 2 || rrA.fetchCount != 2 || rrB.fetchCount != 0 {
		t.Fatalf("RR A submit/fetch=%d/%d, RR B=%d/%d, want retry against every RR", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
}

func TestSAMEnrollmentClientDropsDirectTopologyWhenRRsDisagreeDuringRefresh(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	stalePeerGroup := testSAMEnrollmentDirectPeerGroupResource()
	rrFresh := &fakeSAMEnrollmentJoinClient{now: now}
	rrStale := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &stalePeerGroup}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rrFresh, rrStale}
		},
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &stalePeerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(claim),
		"lastSuccess": now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rrFresh.fetchCount != 1 || rrStale.fetchCount != 1 {
		t.Fatalf("RR fetch counts = %d/%d, want 1/1", rrFresh.fetchCount, rrStale.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" {
		t.Fatalf("status = %#v, want RR disagreement to withdraw direct", status)
	}
}

func TestSAMEnrollmentClientBoundsHungDirectTopologyRefresh(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, blockFetch: true}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	controller.requestTimeout = time.Millisecond
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(claim),
		"lastSuccess": now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	started := time.Now()
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("hung direct refresh elapsed %s, want bounded request timeout", elapsed)
	}
	if client.submitCount != 0 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want timed-out GET-only refresh 0/1", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
}

func TestSAMEnrollmentClientWithholdsDirectTopologyWhenRRDoesNotAttestCurrentClaim(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	tests := []struct {
		name      string
		configure func(*fakeSAMEnrollmentJoinClient)
	}{
		{
			name: "same-name stale claim digest",
			configure: func(client *fakeSAMEnrollmentJoinClient) {
				client.topologyClaimDigest = "sha256:previous-claim"
			},
		},
		{
			name: "older RR omits claim digest",
			configure: func(client *fakeSAMEnrollmentJoinClient) {
				client.omitTopologyClaimDigest = true
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newSAMEnrollmentClientTestStore()
			rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
			rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
			tt.configure(rrB)
			controller := SAMEnrollmentClientController{
				Router: testSAMEnrollmentClientRouter("nonce-a"),
				Store:  store,
				Now:    func() time.Time { return now },
				ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
					return []SAMEnrollmentJoinClient{rrA, rrB}
				},
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
			expiresAt := now.Add(time.Hour)
			if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
				t.Fatalf("seed direct topology: %v", err)
			}
			claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
			if err != nil {
				t.Fatalf("samEnrollmentClientClaim: %v", err)
			}
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
				"claimDigest": samEnrollmentClientClaimDigest(claim),
				"lastSuccess": now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
			}); err != nil {
				t.Fatalf("SaveObjectStatus: %v", err)
			}

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if rrA.fetchCount != 1 || rrB.fetchCount != 1 {
				t.Fatalf("RR fetch counts = %d/%d, want 1/1", rrA.fetchCount, rrB.fetchCount)
			}
			if rrA.lastTopologyRequest.ClaimDigest != samEnrollmentClientClaimDigest(claim) || rrB.lastTopologyRequest.ClaimDigest != samEnrollmentClientClaimDigest(claim) {
				t.Fatalf("direct refresh did not bind both GETs to current claim: A=%#v B=%#v", rrA.lastTopologyRequest, rrB.lastTopologyRequest)
			}
			assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if status["phase"] != "Degraded" {
				t.Fatalf("status = %#v, want RR-only degraded fallback", status)
			}
		})
	}
}

func TestSAMEnrollmentClientDoesNotCountInvalidSubmitReceiptForDirectTopology(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	valid := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, now, now.Add(time.Hour))
	wrongClaim := valid
	wrongClaim.ClaimRef = "SAMEnrollmentClaim/other-leaf"
	wrongSource := valid
	wrongSource.DynamicSource = "SAMEnrollmentClaim/other-leaf"
	rejected := valid
	rejected.Accepted = false
	tests := []struct {
		name   string
		result *controlapi.SAMEnrollmentClaimSubmitResult
	}{
		{name: "nil result", result: nil},
		{name: "not accepted", result: &rejected},
		{name: "wrong claim ref", result: &wrongClaim},
		{name: "wrong dynamic source", result: &wrongSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newSAMEnrollmentClientTestStore()
			peerGroup := testSAMEnrollmentDirectPeerGroupResource()
			rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
			rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup, submitResultSet: true, submitResult: tt.result}
			controller := SAMEnrollmentClientController{
				Router: testSAMEnrollmentClientRouter("nonce-a"),
				Store:  store,
				Now:    func() time.Time { return now },
				ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
					return []SAMEnrollmentJoinClient{rrA, rrB}
				},
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if rrA.submitCount != 1 || rrA.fetchCount != 1 || rrB.submitCount != 1 || rrB.fetchCount != 0 {
				t.Fatalf("RR A submit/fetch=%d/%d, RR B=%d/%d; invalid receipt must not admit direct topology", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
			}
			assertSAMEnrollmentClientRRSetOnlyRecord(t, store, now.Add(time.Hour))
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if status["phase"] != "Degraded" {
				t.Fatalf("status = %#v, want RR-only degraded fallback", status)
			}
		})
	}
}

func TestSAMEnrollmentClientSubmitsToAllBootstrapEndpoints(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	rr1 := &fakeSAMEnrollmentJoinClient{now: now}
	rr2 := &fakeSAMEnrollmentJoinClient{now: now}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rr1, rr2}
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rr1.submitCount != 1 || rr2.submitCount != 1 {
		t.Fatalf("submit counts rr1/rr2 = %d/%d, want 1/1", rr1.submitCount, rr2.submitCount)
	}
	if rr1.fetchCount+rr2.fetchCount != 1 {
		t.Fatalf("total fetch count = %d, want 1", rr1.fetchCount+rr2.fetchCount)
	}
	if records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs"); err != nil || len(records) != 1 {
		t.Fatalf("SAMRRSet dynamic records = %#v err=%v, want one", records, err)
	}
}

type fakeSAMEnrollmentJoinClient struct {
	now                     time.Time
	submitErr               error
	fetchErr                error
	blockSubmit             bool
	blockFetch              bool
	peerGroup               *api.Resource
	submitResultSet         bool
	submitResult            *controlapi.SAMEnrollmentClaimSubmitResult
	topologyClaimDigest     string
	omitTopologyClaimDigest bool
	lastTopologyRequest     controlapi.SAMEnrollmentTopologyGetRequest
	submitCount             int
	fetchCount              int
}

func (c *fakeSAMEnrollmentJoinClient) SubmitSAMEnrollmentClaim(ctx context.Context, _ controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
	c.submitCount++
	if c.blockSubmit {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.submitErr != nil {
		return nil, c.submitErr
	}
	if c.submitResultSet {
		return c.submitResult, nil
	}
	result := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, c.now, c.now.Add(time.Hour))
	return &result, nil
}

func (c *fakeSAMEnrollmentJoinClient) GetSAMEnrollmentTopology(ctx context.Context, request controlapi.SAMEnrollmentTopologyGetRequest) (*controlapi.SAMEnrollmentTopologyGetResult, error) {
	c.fetchCount++
	c.lastTopologyRequest = request
	if c.blockFetch {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.fetchErr != nil {
		return nil, c.fetchErr
	}
	digest := request.ClaimDigest
	if c.omitTopologyClaimDigest {
		digest = ""
	} else if c.topologyClaimDigest != "" {
		digest = c.topologyClaimDigest
	}
	result := controlapi.NewSAMEnrollmentTopologyGetResult("pve-rrs", digest, testSAMEnrollmentRRSetResource(), c.peerGroup)
	return &result, nil
}

func testSAMEnrollmentClientController(store *samEnrollmentClientTestStore, client *fakeSAMEnrollmentJoinClient, now time.Time, nonce string) SAMEnrollmentClientController {
	return SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter(nonce),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{client}
		},
	}
}

func testSAMEnrollmentClientRouter(nonce string) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: "routerd.net/v1alpha1", Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "pve-leaf-a"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			testSAMEnrollmentClaimResource(nonce),
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClient"},
				Metadata: api.ObjectMeta{Name: "pve-leaf-a"},
				Spec: api.SAMEnrollmentClientSpec{
					ClaimRef:              "SAMEnrollmentClaim/pve-leaf-a",
					BootstrapEndpoints:    []string{"http://10.30.0.10:65432"},
					StateTTLRefreshBefore: "10m",
					RetryBackoff:          api.SAMEnrollmentRetryBackoffSpec{Min: "10s", Max: "15m"},
				},
			},
		}},
	}
}

func testSAMEnrollmentClaimResource(nonce string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
		Metadata: api.ObjectMeta{Name: "pve-leaf-a"},
		Spec: api.SAMEnrollmentClaimSpec{
			PolicyRef:     "SAMEnrollmentPolicy/pve-wg-leaves",
			RRSetRef:      "SAMRRSet/pve-rrs",
			LeafID:        "pve-leaf-a",
			JoinNonce:     nonce,
			TunnelAddress: "10.255.10.21/32",
		},
	}
}

func testSAMEnrollmentRRSetResource() api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "pve-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/pve-wg-leaves",
			Nodes: []api.SAMNodeSpec{{
				NodeRef:        "pve-rr",
				RouteReflector: true,
				SAMEndpoint:    "10.30.0.10",
			}},
		},
	}
}

func testSAMEnrollmentDirectPeerGroupResource() api.Resource {
	return testSAMEnrollmentDirectPeerGroupResourceFor("pve-leaf-b", "10.30.0.22", "10.77.60.22/32")
}

func testSAMEnrollmentDirectPeerGroupResourceFor(nodeRef, endpoint, ownedPrefix string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "pve-direct-leaves"},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  "SAMEnrollmentPolicy/pve-wg-leaves",
			TransportFingerprint: "sha256:mesh",
			Nodes: []api.SAMNodeSpec{{
				NodeRef:     nodeRef,
				SAMEndpoint: endpoint,
			}},
			OwnedPrefixesByNode: map[string][]string{
				nodeRef: {ownedPrefix},
			},
		},
	}
}

func setSAMEnrollmentClientDirectMesh(t *testing.T, router *api.Router, directMesh bool) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != "pve-leaf-a" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			t.Fatalf("claim spec: %v", err)
		}
		claim.DirectMesh = directMesh
		router.Spec.Resources[i].Spec = claim
		return
	}
	t.Fatalf("SAMEnrollmentClaim/pve-leaf-a not found")
}

func setSAMEnrollmentClientPolicyRef(t *testing.T, router *api.Router, policyRef string) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != "pve-leaf-a" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			t.Fatalf("claim spec: %v", err)
		}
		claim.PolicyRef = policyRef
		router.Spec.Resources[i].Spec = claim
		return
	}
	t.Fatalf("SAMEnrollmentClaim/pve-leaf-a not found")
}

func setSAMEnrollmentClientJoinNonce(t *testing.T, router *api.Router, nonce string) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != "pve-leaf-a" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			t.Fatalf("claim spec: %v", err)
		}
		claim.JoinNonce = nonce
		router.Spec.Resources[i].Spec = claim
		return
	}
	t.Fatalf("SAMEnrollmentClaim/pve-leaf-a not found")
}

func seedSAMEnrollmentClientTopology(store *samEnrollmentClientTestStore, observedAt, expiresAt time.Time, peerGroup *api.Resource) error {
	resource := testSAMEnrollmentRRSetResource()
	record, err := codec.FetchedSAMEnrollmentTopologyRecord(resource, peerGroup, observedAt, expiresAt, codec.FetchedSAMEnrollmentTopologyRecordOptions{
		Name:                              safeName("fetched-sam-enrollment-topology-" + resource.Metadata.Name),
		Generation:                        dynamicGeneration,
		DefaultTTL:                        DefaultLeaseTTL,
		IncludeEmptyDirectivesActionPlans: true,
		Digest:                            digestDynamicPart,
	})
	if err != nil {
		return err
	}
	return store.UpsertDynamicConfigPart(record)
}

type samEnrollmentClientTestStore struct {
	status    map[string]map[string]any
	parts     map[string][]routerstate.DynamicConfigPartRecord
	upsertErr error
}

func newSAMEnrollmentClientTestStore() *samEnrollmentClientTestStore {
	return &samEnrollmentClientTestStore{
		status: map[string]map[string]any{},
		parts:  map[string][]routerstate.DynamicConfigPartRecord{},
	}
}

func (s *samEnrollmentClientTestStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s.status[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *samEnrollmentClientTestStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	status := s.status[apiVersion+"/"+kind+"/"+name]
	if status == nil {
		return map[string]any{}
	}
	return status
}

func (s *samEnrollmentClientTestStore) UpsertDynamicConfigPart(record routerstate.DynamicConfigPartRecord) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	records := s.parts[record.Source]
	for i, existing := range records {
		if existing.Generation == record.Generation {
			records[i] = record
			s.parts[record.Source] = records
			return nil
		}
	}
	s.parts[record.Source] = append(records, record)
	return nil
}

func (s *samEnrollmentClientTestStore) GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error) {
	return append([]routerstate.DynamicConfigPartRecord(nil), s.parts[source]...), nil
}

func assertSAMEnrollmentClientRecordContains(t *testing.T, record routerstate.DynamicConfigPartRecord, want string) {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
		t.Fatalf("resources json: %v", err)
	}
	if !strings.Contains(record.ResourcesJSON, want) {
		t.Fatalf("record resources = %#v, want %q", resources, want)
	}
}

func assertSAMEnrollmentClientRRSetOnlyRecord(t *testing.T, store *samEnrollmentClientTestStore, wantExpiry time.Time) {
	t.Helper()
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want RR fallback", records, err)
	}
	if !wantExpiry.IsZero() && !records[0].ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("RR fallback expiry = %s, want %s", records[0].ExpiresAt, wantExpiry)
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(records[0].ResourcesJSON), &resources); err != nil {
		t.Fatalf("decode fallback resources: %v", err)
	}
	if len(resources) != 1 || resources[0].Kind != "SAMRRSet" {
		t.Fatalf("resources = %#v, want RR fallback only", resources)
	}
}

func assertSAMEnrollmentClientRRSetFallback(t *testing.T, store *samEnrollmentClientTestStore) {
	t.Helper()
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one RRSet fallback", records)
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(records[0].ResourcesJSON), &resources); err != nil {
		t.Fatalf("decode resources: %v", err)
	}
	if len(resources) != 1 || resources[0].Kind != "SAMRRSet" {
		t.Fatalf("resources = %#v, want RRSet fallback only", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["observedRRSet"] != "SAMRRSet/pve-rrs" {
		t.Fatalf("status = %#v, want ready RRSet fallback", status)
	}
	if _, found := status["observedDirectPeerGroup"]; found {
		t.Fatalf("status = %#v, want no observed direct peer group", status)
	}
}
