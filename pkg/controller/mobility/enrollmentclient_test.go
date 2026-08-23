// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	if client.submitCount != 1 || client.fetchCount != 2 {
		t.Fatalf("submit/fetch = %d/%d, want direct preflight plus post-submit fetch 1/2", client.submitCount, client.fetchCount)
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
	if client.submitCount != 1 || client.fetchCount != 2 {
		t.Fatalf("submit/fetch = %d/%d, want direct preflight plus post-submit fetch 1/2", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, now.Add(time.Hour))
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	reason, _ := status["reason"].(string)
	if status["phase"] != "Degraded" || status["directTopologyPending"] == true || !strings.Contains(reason, "invalid direct topology") {
		t.Fatalf("status = %#v, want malformed direct topology diagnostic with RR fallback", status)
	}
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
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, now.Add(time.Hour))
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	reason, _ := status["reason"].(string)
	if status["phase"] != "Degraded" || status["directTopologyPending"] == true || !strings.Contains(reason, "invalid direct topology") {
		t.Fatalf("status = %#v, want invalid direct topology diagnostic with RR fallback", status)
	}
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
		// A successful join normally schedules RR lease renewal much later than
		// the direct refresh interval. That ordinary schedule must not be
		// mistaken for a retry backoff and defer discovery of a newly joined
		// direct peer until lease expiry.
		"nextAttempt": expiresAt.Add(-defaultSAMEnrollmentRefreshBefore).Format(time.RFC3339),
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

func TestSAMEnrollmentClientBoundsDirectRefreshTransportFailureBackoff(t *testing.T) {
	// This is the production RR01-restart sequence: the leaf already has a
	// verified RRSet and direct topology, then one RR's control endpoint is
	// temporarily unreachable. The high-preference direct group must be
	// withdrawn, but a large earlier generic failure count must not postpone a
	// GET-only recovery beyond the bounded direct convergence cadence.
	current := time.Date(2026, 8, 23, 6, 46, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	rrCurrent := &fakeSAMEnrollmentJoinClient{now: current, peerGroup: &peerGroup}
	rrRestarting := &fakeSAMEnrollmentJoinClient{
		now:       current,
		peerGroup: &peerGroup,
		fetchErr: &url.Error{
			Op:  "Get",
			URL: "http://192.168.1.38:65432/api/control.routerd.net/v1alpha1/sam-enrollment-topologies/pve-rrs",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("no route to host")},
		},
	}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return current },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rrCurrent, rrRestarting}
		},
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	expiresAt := current.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, current, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest":  samEnrollmentClientClaimDigest(claim),
		"lastSuccess":  current.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
		"lastAttempt":  current.Add(-time.Minute).Format(time.RFC3339),
		"nextAttempt":  current.Add(-time.Second).Format(time.RFC3339),
		"backoff":      defaultSAMEnrollmentBackoffMax.String(),
		"failureCount": 7,
		"reason":       "previous generic failure",
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("transport-failure Reconcile: %v", err)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Backoff" || status["directTopologyPending"] != true || status["failureCount"] != 8 || status["backoff"] != defaultSAMEnrollmentDirectTopologyRefresh.String() {
		t.Fatalf("status = %#v, want bounded pending direct recovery", status)
	}
	if got := controller.NextReconcileAfter(); got != defaultSAMEnrollmentDirectTopologyRefresh {
		t.Fatalf("NextReconcileAfter = %s, want %s", got, defaultSAMEnrollmentDirectTopologyRefresh)
	}
	if rrCurrent.submitCount != 0 || rrRestarting.submitCount != 0 || rrCurrent.fetchCount != 1 || rrRestarting.fetchCount != 1 {
		t.Fatalf("first refresh submit/fetch current=%d/%d restarting=%d/%d, want GET-only 0/1 each", rrCurrent.submitCount, rrCurrent.fetchCount, rrRestarting.submitCount, rrRestarting.fetchCount)
	}

	current = current.Add(defaultSAMEnrollmentDirectTopologyRefresh + time.Second)
	rrCurrent.now = current
	rrRestarting.now = current
	rrRestarting.fetchErr = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("recovered Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["failureCount"] != 0 || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want automatic direct recovery", status)
	}
	if rrCurrent.submitCount != 0 || rrRestarting.submitCount != 0 || rrCurrent.fetchCount != 2 || rrRestarting.fetchCount != 2 {
		t.Fatalf("recovery submit/fetch current=%d/%d restarting=%d/%d, want GET-only 0/2 each", rrCurrent.submitCount, rrCurrent.fetchCount, rrRestarting.submitCount, rrRestarting.fetchCount)
	}
}

func TestSAMEnrollmentClientReadmitsClaimToEveryRRWhenOneRRLostItDuringDirectRefresh(t *testing.T) {
	now := time.Date(2026, 8, 22, 20, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	rrCurrent := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	rrRestarted := &fakeSAMEnrollmentJoinClient{
		now:                   now,
		peerGroup:             &peerGroup,
		fetchErr:              &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage},
		clearFetchErrOnSubmit: true,
	}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rrCurrent, rrRestarted}
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
	if rrCurrent.submitCount != 1 || rrCurrent.fetchCount != 2 || rrRestarted.submitCount != 1 || rrRestarted.fetchCount != 2 {
		t.Fatalf("RR counts current submit/fetch=%d/%d restarted=%d/%d, want 1/2 each after RR claim recovery", rrCurrent.submitCount, rrCurrent.fetchCount, rrRestarted.submitCount, rrRestarted.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want one recovered topology", records, err)
	}
	assertSAMEnrollmentClientRecordContains(t, records[0], "pve-direct-leaves")
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["reason"] != "direct-topology-refresh" || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want recovered direct topology", status)
	}
}

func TestSAMEnrollmentClientReadmitsWhenEveryRRLostTheClaimDuringDirectRefresh(t *testing.T) {
	now := time.Date(2026, 8, 22, 20, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	missingClaim := func() *fakeSAMEnrollmentJoinClient {
		return &fakeSAMEnrollmentJoinClient{
			now:                   now,
			peerGroup:             &peerGroup,
			fetchErr:              &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage},
			clearFetchErrOnSubmit: true,
		}
	}
	rrA, rrB := missingClaim(), missingClaim()
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
	if rrA.submitCount != 1 || rrA.fetchCount != 2 || rrB.submitCount != 1 || rrB.fetchCount != 2 {
		t.Fatalf("RR counts A submit/fetch=%d/%d B=%d/%d, want 1/2 each after full RR claim recovery", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want one recovered topology", records, err)
	}
	assertSAMEnrollmentClientRecordContains(t, records[0], "pve-direct-leaves")
}

func TestSAMEnrollmentClientReadmitsAfterEarlierDirectRefreshBackoff(t *testing.T) {
	// This is the live incident sequence: an initial refresh removes the
	// higher-preference direct group and preserves RR fallback, then a later
	// retry reaches a clean-booted RR that reports its accepted claim missing.
	// The retry must recover without waiting for the RR lease expiry or requiring
	// an operator to rejoin every leaf manually.
	now := time.Date(2026, 8, 22, 20, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup, fetchErr: errors.New("rr unavailable")}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{client}
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
		t.Fatalf("first Reconcile: %v", err)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)

	now = now.Add(defaultSAMEnrollmentBackoffMin)
	client.now = now
	client.fetchErr = &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage}
	client.clearFetchErrOnSubmit = true
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("recovery Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 3 {
		t.Fatalf("submit/fetch = %d/%d, want failed GET then missing GET+submit+GET", client.submitCount, client.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want recovered topology", records, err)
	}
	assertSAMEnrollmentClientRecordContains(t, records[0], "pve-direct-leaves")
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want recovered direct topology", status)
	}
}

func TestSAMEnrollmentClientUsesOnlyGETForLegacyDirectRefreshProbe(t *testing.T) {
	// v2333 overwrote the original direct-refresh error with this generic
	// status shape. A migration probe may shorten that stale retry, but it must
	// remain read-only: only a current, mutually agreed response restores
	// direct; a missing claim is not silently re-submitted.
	for _, tt := range []struct {
		name        string
		firstError  error
		wantReady   bool
		wantFetches int
	}{
		{name: "restores current agreed topology", wantReady: true, wantFetches: 1},
		{
			name:        "does not resubmit an absent claim",
			firstError:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage},
			wantFetches: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 22, 23, 54, 0, 0, time.UTC)
			store := newSAMEnrollmentClientTestStore()
			peerGroup := testSAMEnrollmentDirectPeerGroupResource()
			rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup, fetchErr: tt.firstError}
			rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
			controller := SAMEnrollmentClientController{
				Router: testSAMEnrollmentClientRouter("nonce-a"),
				Store:  store,
				Now:    func() time.Time { return now },
				ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
					return []SAMEnrollmentJoinClient{rrA, rrB}
				},
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
			if err := seedSAMEnrollmentClientTopology(store, now, now.Add(time.Hour), nil); err != nil {
				t.Fatalf("seed RR fallback: %v", err)
			}
			claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
			if err != nil {
				t.Fatalf("samEnrollmentClientClaim: %v", err)
			}
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
				"claimDigest":  samEnrollmentClientClaimDigest(claim),
				"lastSuccess":  now.Add(-2 * defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
				"lastAttempt":  now.Add(-2 * defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
				"nextAttempt":  now.Add(defaultSAMEnrollmentBackoffMax).Format(time.RFC3339),
				"backoff":      defaultSAMEnrollmentBackoffMax.String(),
				"failureCount": 9,
				"reason":       "direct-topology-refresh",
			}); err != nil {
				t.Fatalf("SaveObjectStatus: %v", err)
			}

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if rrA.submitCount != 0 || rrB.submitCount != 0 || rrA.fetchCount != tt.wantFetches || rrB.fetchCount != tt.wantFetches {
				t.Fatalf("RR A submit/fetch=%d/%d RR B=%d/%d, want read-only legacy probe 0/%d", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount, tt.wantFetches)
			}
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if tt.wantReady {
				if status["phase"] != "Ready" || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
					t.Fatalf("status = %#v, want direct GET-only recovery", status)
				}
				return
			}
			reason, _ := status["reason"].(string)
			if status["phase"] != "Degraded" || !strings.Contains(reason, controlapi.SAMEnrollmentTopologyIdentityAbsentMessage) {
				t.Fatalf("status = %#v, want retained RR fallback and missing-claim diagnostic", status)
			}
		})
	}
}

func TestSAMEnrollmentClientKeepsRRFallbackUntilRestartedRRTopologyConverges(t *testing.T) {
	// A restarted RR starts with only this leaf after its readmission, whereas
	// its partner still knows the complete mesh. The leaf must retain RR-only
	// forwarding, retry shortly, and restore direct only after both projections
	// are byte-for-byte equivalent.
	now := time.Date(2026, 8, 22, 23, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	partialGroup := testSAMEnrollmentDirectPeerGroupResourceFor("pve-leaf-c", "10.30.0.23", "10.77.60.23/32")
	missingClaim := &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage}
	rrRestarted := &fakeSAMEnrollmentJoinClient{
		now:                   now,
		peerGroup:             &partialGroup,
		fetchErr:              missingClaim,
		clearFetchErrOnSubmit: true,
	}
	rrCurrent := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rrRestarted, rrCurrent}
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
		t.Fatalf("first Reconcile: %v", err)
	}
	if rrRestarted.submitCount != 1 || rrCurrent.submitCount != 1 || rrRestarted.fetchCount != 2 || rrCurrent.fetchCount != 2 {
		t.Fatalf("RR restarted submit/fetch=%d/%d current=%d/%d, want checked readmission and matching post-submit GETs", rrRestarted.submitCount, rrRestarted.fetchCount, rrCurrent.submitCount, rrCurrent.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Backoff" || status["failureCount"] != 1 || status["backoff"] != defaultSAMEnrollmentBackoffMin.String() || status["reason"] != "direct topology is converging across enrollment endpoints" || status["directTopologyPending"] != true {
		t.Fatalf("status = %#v, want short direct convergence backoff", status)
	}
	if _, found := status["observedDirectPeerGroup"]; found {
		t.Fatalf("status = %#v, must not install a partial direct topology", status)
	}

	now = now.Add(defaultSAMEnrollmentBackoffMin + time.Second)
	rrRestarted.now = now
	rrCurrent.now = now
	rrRestarted.peerGroup = &peerGroup
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("converged Reconcile: %v", err)
	}
	if rrRestarted.submitCount != 1 || rrCurrent.submitCount != 1 || rrRestarted.fetchCount != 3 || rrCurrent.fetchCount != 3 {
		t.Fatalf("RR restarted submit/fetch=%d/%d current=%d/%d, want GET-only restoration after convergence", rrRestarted.submitCount, rrRestarted.fetchCount, rrCurrent.submitCount, rrCurrent.fetchCount)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v, want restored direct topology", records, err)
	}
	assertSAMEnrollmentClientRecordContains(t, records[0], "pve-direct-leaves")
	status = store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["failureCount"] != 0 || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want direct topology restored only after RR agreement", status)
	}
}

func TestSAMEnrollmentClientRestoresEightLeafMeshAfterOneRRRestarts(t *testing.T) {
	// This is the deployed PVE shape, not a two-leaf approximation: eight
	// leaves normally have seven direct remotes and two RRs. A restarted RR
	// relearns claims one leaf at a time. Every leaf must keep the RR fallback
	// until the restarted RR has the complete eight-leaf view; only then may
	// the direct mesh return.
	now := time.Date(2026, 8, 22, 23, 54, 0, 0, time.UTC)
	leaves := make([]string, 0, 8)
	for leaf := 1; leaf <= 8; leaf++ {
		leaves = append(leaves, fmt.Sprintf("pve-rt-%02d", leaf))
	}
	currentRR := newFakeSAMEnrollmentReplica(now, leaves, leaves)
	restartedRR := newFakeSAMEnrollmentReplica(now, leaves, nil)
	type leafRuntime struct {
		name       string
		controller SAMEnrollmentClientController
		store      *samEnrollmentClientTestStore
	}
	runtimes := make([]leafRuntime, 0, len(leaves))
	for index, leaf := range leaves {
		store := newSAMEnrollmentClientTestStore()
		router := testSAMEnrollmentClientRouterForLeaf(leaf, fmt.Sprintf("nonce-%d", index+1))
		setSAMEnrollmentClientDirectMeshForLeaf(t, router, leaf, true)
		controller := SAMEnrollmentClientController{
			Router: router,
			Store:  store,
			Now:    func() time.Time { return now },
			ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
				return []SAMEnrollmentJoinClient{restartedRR, currentRR}
			},
		}
		peerGroup := currentRR.peerGroupFor(leaf)
		if peerGroup == nil || len(peerGroup.Spec.(api.SAMPeerGroupSpec).Nodes) != 7 {
			t.Fatalf("initial peer group for %s = %#v, want seven remotes", leaf, peerGroup)
		}
		if err := seedSAMEnrollmentClientTopology(store, now, now.Add(time.Hour), peerGroup); err != nil {
			t.Fatalf("seed %s direct topology: %v", leaf, err)
		}
		claim, _, err := samEnrollmentClientClaim(router, "SAMEnrollmentClaim/"+leaf)
		if err != nil {
			t.Fatalf("claim %s: %v", leaf, err)
		}
		if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", leaf, map[string]any{
			"claimDigest": samEnrollmentClientClaimDigest(claim),
			"lastSuccess": now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
		}); err != nil {
			t.Fatalf("seed %s client status: %v", leaf, err)
		}
		runtimes = append(runtimes, leafRuntime{name: leaf, controller: controller, store: store})
	}

	for index, runtime := range runtimes {
		if err := runtime.controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("readmit %s: %v", runtime.name, err)
		}
		status := runtime.store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", runtime.name)
		if index < len(runtimes)-1 {
			assertSAMEnrollmentClientRRSetOnlyRecord(t, runtime.store, now.Add(time.Hour))
			if status["phase"] != "Backoff" || status["failureCount"] != 1 || status["reason"] != "direct topology is converging across enrollment endpoints" || status["directTopologyPending"] != true {
				t.Fatalf("partial recovery status for %s = %#v, want RR-only direct convergence backoff", runtime.name, status)
			}
			if got := runtime.controller.NextReconcileAfter(); got != defaultSAMEnrollmentBackoffMin {
				t.Fatalf("direct convergence deadline for %s = %s, want %s", runtime.name, got, defaultSAMEnrollmentBackoffMin)
			}
			continue
		}
		if status["phase"] != "Ready" || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
			t.Fatalf("last readmission status for %s = %#v, want agreed direct topology", runtime.name, status)
		}
	}
	if currentRR.submitCount != len(leaves) || restartedRR.submitCount != len(leaves) {
		t.Fatalf("submit count current/restarted = %d/%d, want %d/%d", currentRR.submitCount, restartedRR.submitCount, len(leaves), len(leaves))
	}

	now = now.Add(defaultSAMEnrollmentBackoffMin + time.Second)
	currentRR.now = now
	restartedRR.now = now
	if got := runtimes[0].controller.NextReconcileAfter(); got != 0 {
		t.Fatalf("past direct convergence deadline = %s, want normal scheduler fallback", got)
	}
	for _, runtime := range runtimes[:len(runtimes)-1] {
		if err := runtime.controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("restore %s: %v", runtime.name, err)
		}
		status := runtime.store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", runtime.name)
		if status["phase"] != "Ready" || status["failureCount"] != 0 || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
			t.Fatalf("restored status for %s = %#v, want full direct topology", runtime.name, status)
		}
		records, err := runtime.store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
		if err != nil || len(records) != 1 {
			t.Fatalf("restored records for %s = %#v err=%v", runtime.name, records, err)
		}
		assertSAMEnrollmentClientRecordContains(t, records[0], "pve-direct-leaves")
	}
}

func TestSAMEnrollmentClientPreservesDirectRefreshFailureReasonWhileBackedOff(t *testing.T) {
	// A status write schedules an immediate controller wake-up. That second
	// reconcile must not replace the real endpoint/agreement error with the
	// generic direct-topology-refresh trigger before an operator can inspect it.
	now := time.Date(2026, 8, 22, 23, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, nil); err != nil {
		t.Fatalf("seed RR fallback: %v", err)
	}
	claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("samEnrollmentClientClaim: %v", err)
	}
	const failureReason = "direct topology differs across enrollment endpoints"
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest":  samEnrollmentClientClaimDigest(claim),
		"lastSuccess":  now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
		"lastAttempt":  now.Format(time.RFC3339),
		"nextAttempt":  now.Add(defaultSAMEnrollmentBackoffMin).Format(time.RFC3339),
		"backoff":      defaultSAMEnrollmentBackoffMin.String(),
		"failureCount": 1,
		"reason":       failureReason,
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want no request during backoff", client.submitCount, client.fetchCount)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Backoff" || status["reason"] != failureReason {
		t.Fatalf("status = %#v, want preserved direct refresh failure", status)
	}
}

func TestSAMEnrollmentClientShowsClaimChangeWhileBackedOff(t *testing.T) {
	// A new claim changes the requested identity. Even when a previous direct
	// refresh is still backed off, status must show that new fact rather than a
	// stale topology error.
	now := time.Date(2026, 8, 22, 23, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	if err := seedSAMEnrollmentClientTopology(store, now, now.Add(time.Hour), nil); err != nil {
		t.Fatalf("seed RR fallback: %v", err)
	}
	oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("old claim: %v", err)
	}
	setSAMEnrollmentClientJoinNonce(t, controller.Router, "nonce-b")
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest":  samEnrollmentClientClaimDigest(oldClaim),
		"lastSuccess":  now.Add(-defaultSAMEnrollmentDirectTopologyRefresh).Format(time.RFC3339),
		"lastAttempt":  now.Format(time.RFC3339),
		"nextAttempt":  now.Add(defaultSAMEnrollmentBackoffMin).Format(time.RFC3339),
		"backoff":      defaultSAMEnrollmentBackoffMin.String(),
		"failureCount": 1,
		"reason":       "direct topology is converging across enrollment endpoints",
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want no request during backoff", client.submitCount, client.fetchCount)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Backoff" || status["reason"] != "claim-changed" {
		t.Fatalf("status = %#v, want current claim-change reason", status)
	}
}

func TestSAMEnrollmentClientConvergenceBackoffHonorsConfiguredMinimum(t *testing.T) {
	if got := samEnrollmentClientConvergenceBackoff(api.SAMEnrollmentClientSpec{}, 9); got != defaultSAMEnrollmentDirectTopologyRefresh {
		t.Fatalf("default convergence backoff = %s, want %s", got, defaultSAMEnrollmentDirectTopologyRefresh)
	}
	spec := api.SAMEnrollmentClientSpec{RetryBackoff: api.SAMEnrollmentRetryBackoffSpec{Min: "5m", Max: "30m"}}
	if got := samEnrollmentClientConvergenceBackoff(spec, 1); got != 5*time.Minute {
		t.Fatalf("configured-min convergence backoff = %s, want 5m", got)
	}
	if got := samEnrollmentClientConvergenceBackoff(spec, 2); got != 10*time.Minute {
		t.Fatalf("configured-min second convergence backoff = %s, want 10m", got)
	}
}

func TestSAMEnrollmentClientDoesNotReadmitRevokedDirectClaim(t *testing.T) {
	now := time.Date(2026, 8, 22, 20, 54, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	client := &fakeSAMEnrollmentJoinClient{
		now:       now,
		peerGroup: &peerGroup,
		fetchErr:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a is revoked"},
	}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
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
	if client.submitCount != 0 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want 0/1 for revoked claim", client.submitCount, client.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["directTopologyPending"] == true || status["backoff"] != defaultSAMEnrollmentBackoffMin.String() {
		t.Fatalf("status = %#v, want ordinary backoff for a reachable revoke response", status)
	}
}

func TestSAMEnrollmentClientDoesNotReplaceDifferentActiveIdentityOutsideClaimChange(t *testing.T) {
	// A stale leaf A must never overwrite an RR's active identity B merely
	// because its cached direct topology needs refresh. The same rule covers an
	// expired or locally missing RRSet when this leaf still remembers A.
	for _, tt := range []struct {
		name           string
		seedRRSet      bool
		expiresAt      func(time.Time) time.Time
		lastSuccess    func(time.Time) time.Time
		wantRRFallback bool
	}{
		{
			name:           "direct topology refresh",
			seedRRSet:      true,
			expiresAt:      func(now time.Time) time.Time { return now.Add(time.Hour) },
			lastSuccess:    func(now time.Time) time.Time { return now.Add(-defaultSAMEnrollmentDirectTopologyRefresh) },
			wantRRFallback: true,
		},
		{
			name:        "RRSet missing with recorded identity",
			lastSuccess: func(now time.Time) time.Time { return now },
		},
		{
			name:           "RRSet renewal",
			seedRRSet:      true,
			expiresAt:      func(now time.Time) time.Time { return now.Add(defaultSAMEnrollmentRefreshBefore) },
			lastSuccess:    func(now time.Time) time.Time { return now },
			wantRRFallback: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 22, 21, 58, 0, 0, time.UTC)
			store := newSAMEnrollmentClientTestStore()
			peerGroup := testSAMEnrollmentDirectPeerGroupResource()
			// This is the server response for active B when this leaf still
			// presents stale A. It is deliberately distinct from an empty RR
			// admission store after clean boot.
			client := &fakeSAMEnrollmentJoinClient{
				now:       now,
				peerGroup: &peerGroup,
				fetchErr:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityMismatchMessage},
			}
			controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
			if tt.seedRRSet {
				if err := seedSAMEnrollmentClientTopology(store, now, tt.expiresAt(now), &peerGroup); err != nil {
					t.Fatalf("seed direct topology: %v", err)
				}
			}
			claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
			if err != nil {
				t.Fatalf("samEnrollmentClientClaim: %v", err)
			}
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
				"claimDigest": samEnrollmentClientClaimDigest(claim),
				"lastSuccess": tt.lastSuccess(now).Format(time.RFC3339),
			}); err != nil {
				t.Fatalf("SaveObjectStatus: %v", err)
			}

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if client.submitCount != 0 || client.fetchCount != 1 {
				t.Fatalf("submit/fetch = %d/%d, want 0/1 for a different active identity", client.submitCount, client.fetchCount)
			}
			if tt.wantRRFallback {
				assertSAMEnrollmentClientRRSetOnlyRecord(t, store, tt.expiresAt(now))
			}
			if status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a"); status["phase"] != "Degraded" {
				t.Fatalf("status = %#v, want RR fallback degradation", status)
			}
		})
	}
}

func TestSAMEnrollmentClientDoesNotReadmitWhenAnotherRRTombstonedTheClaim(t *testing.T) {
	for _, tt := range []struct {
		name           string
		seedRRSet      bool
		expiresAt      func(time.Time) time.Time
		lastSuccess    func(time.Time) time.Time
		wantRRFallback bool
	}{
		{
			name:           "direct topology refresh",
			seedRRSet:      true,
			expiresAt:      func(now time.Time) time.Time { return now.Add(time.Hour) },
			lastSuccess:    func(now time.Time) time.Time { return now.Add(-defaultSAMEnrollmentDirectTopologyRefresh) },
			wantRRFallback: true,
		},
		{
			name:           "RR lease renewal",
			seedRRSet:      true,
			expiresAt:      func(now time.Time) time.Time { return now.Add(defaultSAMEnrollmentRefreshBefore) },
			lastSuccess:    func(now time.Time) time.Time { return now },
			wantRRFallback: true,
		},
		{
			name: "initial admission",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 22, 20, 54, 0, 0, time.UTC)
			store := newSAMEnrollmentClientTestStore()
			peerGroup := testSAMEnrollmentDirectPeerGroupResource()
			// The empty RR answers first while the second RR retains the
			// operator's tombstone. No admission path may POST to the first RR.
			rrRestarted := &fakeSAMEnrollmentJoinClient{
				now:       now,
				peerGroup: &peerGroup,
				fetchErr:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage},
			}
			rrRevoked := &fakeSAMEnrollmentJoinClient{
				now:       now,
				peerGroup: &peerGroup,
				fetchErr:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a is revoked"},
			}
			controller := SAMEnrollmentClientController{
				Router: testSAMEnrollmentClientRouter("nonce-a"),
				Store:  store,
				Now:    func() time.Time { return now },
				ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
					return []SAMEnrollmentJoinClient{rrRestarted, rrRevoked}
				},
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
			if tt.seedRRSet {
				expiresAt := tt.expiresAt(now)
				if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
					t.Fatalf("seed direct topology: %v", err)
				}
				claim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
				if err != nil {
					t.Fatalf("samEnrollmentClientClaim: %v", err)
				}
				if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
					"claimDigest": samEnrollmentClientClaimDigest(claim),
					"lastSuccess": tt.lastSuccess(now).Format(time.RFC3339),
				}); err != nil {
					t.Fatalf("SaveObjectStatus: %v", err)
				}
			}

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if rrRestarted.submitCount != 0 || rrRevoked.submitCount != 0 || rrRestarted.fetchCount != 1 || rrRevoked.fetchCount != 1 {
				t.Fatalf("RR counts restarted submit/fetch=%d/%d revoked=%d/%d, want 0/1 each", rrRestarted.submitCount, rrRestarted.fetchCount, rrRevoked.submitCount, rrRevoked.fetchCount)
			}
			if tt.wantRRFallback {
				assertSAMEnrollmentClientRRSetOnlyRecord(t, store, tt.expiresAt(now))
			}
		})
	}
}

func TestSAMEnrollmentClientDoesNotReadmitAcrossLegacyRRMixedVersion(t *testing.T) {
	// An older RR ignores claimIdentityDigest and returns its ambiguous legacy
	// not-found response for a tombstone. A new clean RR returns the explicit
	// identity-aware marker. The leaf must keep RR-only fallback rather than
	// POSTing the old claim to the legacy reflector.
	now := time.Date(2026, 8, 22, 21, 45, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	legacyRR := &fakeSAMEnrollmentJoinClient{
		now:       now,
		peerGroup: &peerGroup,
		fetchErr:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a not found"},
	}
	currentRR := &fakeSAMEnrollmentJoinClient{
		now:       now,
		peerGroup: &peerGroup,
		fetchErr:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage},
	}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{legacyRR, currentRR}
		},
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	expiresAt := now.Add(defaultSAMEnrollmentRefreshBefore)
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
	if legacyRR.submitCount != 0 || currentRR.submitCount != 0 || legacyRR.fetchCount != 1 || currentRR.fetchCount != 1 {
		t.Fatalf("legacy submit/fetch=%d/%d current=%d/%d, want zero POSTs and one preflight each", legacyRR.submitCount, legacyRR.fetchCount, currentRR.submitCount, currentRR.fetchCount)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
}

func TestSAMEnrollmentClientReadmissionRequiredMatchesOnlyLocalMissingClaim(t *testing.T) {
	claim := testSAMEnrollmentClaimResource("nonce-a")
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "identity-aware local missing claim",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage},
			want: true,
		},
		{
			name: "legacy ambiguous missing claim",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a not found"},
		},
		{
			name: "different active identity",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityMismatchMessage},
		},
		{
			name: "different missing claim",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-b not found"},
		},
		{
			name: "claim absent from effective config",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a not found in effective config"},
		},
		{
			name: "explicit revocation",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a is revoked"},
		},
		{
			name: "unrelated bad request",
			err:  &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: malformed topology request"},
		},
		{
			name: "wrong status",
			err:  &controlapi.APIError{StatusCode: http.StatusInternalServerError, Message: "accepted SAMEnrollmentClaim/pve-leaf-a not found"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := samEnrollmentClientReadmissionRequired(tt.err, claim); got != tt.want {
				t.Fatalf("samEnrollmentClientReadmissionRequired(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
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
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want preflight plus failed lease refresh 1/1", client.submitCount, client.fetchCount)
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
	client := &fakeSAMEnrollmentJoinClient{
		now:       now,
		submitErr: errors.New("rr unavailable"),
		// A new identity asks a current RR to replace its old accepted claim.
		// The identity-aware API reports an active-identity mismatch before Submit.
		fetchErr: &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted SAMEnrollmentClaim/pve-leaf-a " + controlapi.SAMEnrollmentTopologyIdentityMismatchMessage},
	}
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
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want preflight plus failed policy refresh 1/1", client.submitCount, client.fetchCount)
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
		name        string
		second      *api.Resource
		wantDirect  bool
		wantSubmits int
		wantFetches int
		wantRRSet   bool
	}{
		{name: "matching topology", second: &peerGroup, wantDirect: true, wantSubmits: 1, wantFetches: 2, wantRRSet: true},
		{name: "mismatched topology falls back", second: &peerGroupOther, wantSubmits: 1, wantFetches: 2, wantRRSet: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := now
			store := newSAMEnrollmentClientTestStore()
			rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
			rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: tt.second}
			controller := SAMEnrollmentClientController{
				Router: testSAMEnrollmentClientRouter("nonce-a"),
				Store:  store,
				Now:    func() time.Time { return current },
				ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
					return []SAMEnrollmentJoinClient{rrA, rrB}
				},
			}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if rrA.submitCount != tt.wantSubmits || rrB.submitCount != tt.wantSubmits || rrA.fetchCount != tt.wantFetches || rrB.fetchCount != tt.wantFetches {
				t.Fatalf("RR A submit/fetch=%d/%d, RR B=%d/%d, want %d/%d each", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount, tt.wantSubmits, tt.wantFetches)
			}
			records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
			if err != nil {
				t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
			}
			if !tt.wantRRSet {
				if len(records) != 0 {
					t.Fatalf("records = %#v, want no topology after unsafe initial disagreement", records)
				}
			} else {
				if len(records) != 1 {
					t.Fatalf("records = %#v, want one topology", records)
				}
				containsDirect := strings.Contains(records[0].ResourcesJSON, "pve-direct-leaves")
				if containsDirect != tt.wantDirect {
					t.Fatalf("resources = %s, direct=%t want %t", records[0].ResourcesJSON, containsDirect, tt.wantDirect)
				}
			}
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if tt.wantDirect && status["phase"] != "Ready" {
				t.Fatalf("status = %#v, want ready direct topology", status)
			}
			if !tt.wantDirect && (status["phase"] != "Backoff" || status["directTopologyPending"] != true) {
				t.Fatalf("status = %#v, want direct convergence backoff with RR fallback", status)
			}
			if !tt.wantDirect {
				current = current.Add(defaultSAMEnrollmentBackoffMin + time.Second)
				rrA.now = current
				rrB.now = current
				rrB.peerGroup = &peerGroup
				if err := controller.Reconcile(context.Background()); err != nil {
					t.Fatalf("converged Reconcile: %v", err)
				}
				if rrA.submitCount != tt.wantSubmits || rrB.submitCount != tt.wantSubmits || rrA.fetchCount != tt.wantFetches+1 || rrB.fetchCount != tt.wantFetches+1 {
					t.Fatalf("RR A submit/fetch=%d/%d RR B=%d/%d, want GET-only convergence after one admission", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
				}
				status = store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
				if status["phase"] != "Ready" || status["directTopologyPending"] == true || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
					t.Fatalf("status = %#v, want direct topology after GET-only convergence", status)
				}
			}
		})
	}
}

func TestSAMEnrollmentClientCompletesConvergingClaimRotationWithGETOnlyRetry(t *testing.T) {
	now := time.Date(2026, 8, 22, 23, 54, 0, 0, time.UTC)
	current := now
	store := newSAMEnrollmentClientTestStore()
	peerGroup := testSAMEnrollmentDirectPeerGroupResource()
	peerGroupOther := testSAMEnrollmentDirectPeerGroupResourceFor("pve-leaf-c", "10.30.0.23", "10.77.60.23/32")
	rrA := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup}
	rrB := &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroupOther}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return current },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rrA, rrB}
		},
	}
	setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
	expiresAt := now.Add(time.Hour)
	if err := seedSAMEnrollmentClientTopology(store, now, expiresAt, &peerGroup); err != nil {
		t.Fatalf("seed direct topology: %v", err)
	}
	oldClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("old claim: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{
		"claimDigest": samEnrollmentClientClaimDigest(oldClaim),
		"lastSuccess": now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed client status: %v", err)
	}
	setSAMEnrollmentClientJoinNonce(t, controller.Router, "nonce-b")
	newClaim, _, err := samEnrollmentClientClaim(controller.Router, "SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("new claim: %v", err)
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	assertSAMEnrollmentClientRRSetOnlyRecord(t, store, expiresAt)
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Backoff" || status["directTopologyPending"] != true || status["claimDigest"] != samEnrollmentClientClaimDigest(newClaim) {
		t.Fatalf("status = %#v, want attested claim rotation pending peer convergence", status)
	}
	if rrA.submitCount != 1 || rrB.submitCount != 1 || rrA.fetchCount != 2 || rrB.fetchCount != 2 {
		t.Fatalf("RR A submit/fetch=%d/%d RR B=%d/%d, want one rotating admission", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
	}

	current = current.Add(defaultSAMEnrollmentBackoffMin + time.Second)
	rrA.now = current
	rrB.now = current
	rrB.peerGroup = &peerGroup
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("converged Reconcile: %v", err)
	}
	if rrA.submitCount != 1 || rrB.submitCount != 1 || rrA.fetchCount != 3 || rrB.fetchCount != 3 {
		t.Fatalf("RR A submit/fetch=%d/%d RR B=%d/%d, want GET-only convergence after rotation", rrA.submitCount, rrA.fetchCount, rrB.submitCount, rrB.fetchCount)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["directTopologyPending"] == true || status["claimDigest"] != samEnrollmentClientClaimDigest(newClaim) || status["observedDirectPeerGroup"] != "SAMPeerGroup/pve-direct-leaves" {
		t.Fatalf("status = %#v, want completed direct claim rotation", status)
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
	if rrA.submitCount != 2 || rrB.submitCount != 2 || rrA.fetchCount != 4 || rrB.fetchCount != 2 {
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
	if status["phase"] != "Backoff" || status["directTopologyPending"] != true {
		t.Fatalf("status = %#v, want RR disagreement convergence backoff", status)
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
			if rrA.submitCount != 1 || rrA.fetchCount != 2 || rrB.submitCount != 1 || rrB.fetchCount != 1 {
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
	clearFetchErrOnSubmit   bool
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
	if c.clearFetchErrOnSubmit {
		c.fetchErr = nil
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
	return testSAMEnrollmentClientRouterForLeaf("pve-leaf-a", nonce)
}

func testSAMEnrollmentClientRouterForLeaf(leaf, nonce string) *api.Router {
	claim := testSAMEnrollmentClaimResource(nonce)
	claim.Metadata.Name = leaf
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		panic(err)
	}
	claimSpec.LeafID = leaf
	claim.Spec = claimSpec
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: "routerd.net/v1alpha1", Kind: "Router"},
		Metadata: api.ObjectMeta{Name: leaf},
		Spec: api.RouterSpec{Resources: []api.Resource{
			claim,
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClient"},
				Metadata: api.ObjectMeta{Name: leaf},
				Spec: api.SAMEnrollmentClientSpec{
					ClaimRef:              "SAMEnrollmentClaim/" + leaf,
					BootstrapEndpoints:    []string{"http://10.30.0.10:65432"},
					StateTTLRefreshBefore: "10m",
					RetryBackoff:          api.SAMEnrollmentRetryBackoffSpec{Min: "10s", Max: "15m"},
				},
			},
		}},
	}
}

// fakeSAMEnrollmentReplica is a compact in-memory RR projection used only for
// the eight-leaf restart regression. It models the relevant production fact:
// an RR has no direct view of a leaf until that leaf's signed claim is accepted.
type fakeSAMEnrollmentReplica struct {
	now         time.Time
	leaves      []string
	accepted    map[string]bool
	submitCount int
	fetchCount  int
}

func newFakeSAMEnrollmentReplica(now time.Time, leaves, accepted []string) *fakeSAMEnrollmentReplica {
	result := &fakeSAMEnrollmentReplica{
		now:      now,
		leaves:   append([]string(nil), leaves...),
		accepted: make(map[string]bool, len(accepted)),
	}
	for _, leaf := range accepted {
		result.accepted[leaf] = true
	}
	return result
}

func (r *fakeSAMEnrollmentReplica) SubmitSAMEnrollmentClaim(_ context.Context, request controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
	r.submitCount++
	leaf := strings.TrimSpace(request.Claim.Metadata.Name)
	r.accepted[leaf] = true
	result := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/"+leaf, "SAMEnrollmentClaim/"+leaf, 1, r.now, r.now.Add(time.Hour))
	return &result, nil
}

func (r *fakeSAMEnrollmentReplica) GetSAMEnrollmentTopology(_ context.Context, request controlapi.SAMEnrollmentTopologyGetRequest) (*controlapi.SAMEnrollmentTopologyGetResult, error) {
	r.fetchCount++
	_, leaf, ok := strings.Cut(strings.TrimSpace(request.ClaimRef), "/")
	if !ok || leaf == "" || !r.accepted[leaf] {
		return nil, &controlapi.APIError{StatusCode: http.StatusBadRequest, Message: "bad request: accepted " + strings.TrimSpace(request.ClaimRef) + " " + controlapi.SAMEnrollmentTopologyIdentityAbsentMessage}
	}
	result := controlapi.NewSAMEnrollmentTopologyGetResult("pve-rrs", request.ClaimDigest, testSAMEnrollmentRRSetResource(), r.peerGroupFor(leaf))
	return &result, nil
}

func (r *fakeSAMEnrollmentReplica) peerGroupFor(self string) *api.Resource {
	nodes := make([]api.SAMNodeSpec, 0, len(r.leaves)-1)
	for index, leaf := range r.leaves {
		if leaf == self || !r.accepted[leaf] {
			continue
		}
		nodes = append(nodes, api.SAMNodeSpec{
			NodeRef:     leaf,
			SAMEndpoint: fmt.Sprintf("10.30.0.%d", index+21),
		})
	}
	if len(nodes) == 0 {
		return nil
	}
	return &api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "pve-direct-leaves"},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  "SAMEnrollmentPolicy/pve-wg-leaves",
			TransportFingerprint: "sha256:mesh",
			Nodes:                nodes,
		},
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
	setSAMEnrollmentClientDirectMeshForLeaf(t, router, "pve-leaf-a", directMesh)
}

func setSAMEnrollmentClientDirectMeshForLeaf(t *testing.T, router *api.Router, leaf string, directMesh bool) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != leaf {
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
	t.Fatalf("SAMEnrollmentClaim/%s not found", leaf)
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
