// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func TestSubmitSAMEnrollmentClaimPersistsValidatedDynamicClaim(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	result, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now)
	if err != nil {
		t.Fatalf("submitSAMEnrollmentClaim: %v", err)
	}
	if !result.Accepted || result.DynamicSource != "SAMEnrollmentClaim/pve-leaf-a" || result.ClaimRef != "SAMEnrollmentClaim/pve-leaf-a" {
		t.Fatalf("result = %#v", result)
	}
	if want := now.Add(8760 * time.Hour); !result.ExpiresAt.Equal(want) {
		t.Fatalf("result ExpiresAt = %s, want policy ttl expiry %s", result.ExpiresAt, want)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 || !strings.Contains(records[0].ResourcesJSON, `"pve-leaf-a"`) {
		t.Fatalf("records = %#v", records)
	}
	parts, err := samEnrollmentDynamicPartsFromRecords(records, "")
	if err != nil {
		t.Fatalf("parts from records: %v", err)
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		t.Fatalf("ExtractDynamicOverridePolicies: %v", err)
	}
	effective, _, err := dynamicconfig.BuildEffectiveConfig(*router, parts, policies, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if !hasSubmitTestResource(&effective, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-a") {
		t.Fatalf("effective config missing submitted claim")
	}
}

func TestSubmitSAMEnrollmentClaimRejectsPolicyViolation(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-fou-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-b")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.TunnelAddress = "10.244.10.22/32"
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	_, err = submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now)
	if err == nil || !strings.Contains(err.Error(), "outside SAMEnrollmentPolicy/pve-fou-leaves spec.tunnelAddressPrefixes") {
		t.Fatalf("submitSAMEnrollmentClaim error = %v, want tunnel policy rejection", err)
	}
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("ListDynamicConfigParts: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("dynamic records = %#v, want none after rejection", records)
	}
}

func TestSubmitSAMEnrollmentClaimRejectsMissingJoinSecret(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", filepath.Join(t.TempDir(), "missing-join-token"))
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	_, err = submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now)
	if err == nil || !strings.Contains(err.Error(), "missing-join-token") {
		t.Fatalf("submitSAMEnrollmentClaim error = %v, want missing join secret rejection", err)
	}
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("ListDynamicConfigParts: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("dynamic records = %#v, want none after rejection", records)
	}
}

func TestSubmitSAMEnrollmentClaimRejectsExpiresAtBeyondPolicyTTL(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.ExpiresAt = "2027-06-29T00:01:00Z"
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	_, err = submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now)
	if err == nil || !strings.Contains(err.Error(), "exceeds SAMEnrollmentPolicy/pve-wg-leaves ttl window") {
		t.Fatalf("submitSAMEnrollmentClaim error = %v, want policy ttl rejection", err)
	}
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("ListDynamicConfigParts: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("dynamic records = %#v, want none after rejection", records)
	}
}

func TestGetSAMEnrollmentTopologyForAcceptedClaimReturnsRRSet(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, claim), now); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a "+controlapi.SAMEnrollmentTopologyIdentityAbsentMessage) {
		t.Fatalf("pre-submit getSAMEnrollmentTopologyForAcceptedClaim error = %v, want accepted claim required", err)
	}
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatalf("submitSAMEnrollmentClaim: %v", err)
	}
	result, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, claim), now)
	if err != nil {
		t.Fatalf("getSAMEnrollmentTopologyForAcceptedClaim: %v", err)
	}
	if result.RRSet.APIVersion != api.MobilityAPIVersion || result.RRSet.Kind != "SAMRRSet" || result.RRSet.Metadata.Name != "pve-rrs" {
		t.Fatalf("rrset result = %#v", result.RRSet)
	}
	spec, err := result.RRSet.SAMRRSetSpec()
	if err != nil {
		t.Fatalf("rrset spec: %v", err)
	}
	if len(spec.Nodes) != 2 || spec.Nodes[0].NodeRef != "pve-rr-a" || spec.Nodes[1].NodeRef != "pve-rr-b" {
		t.Fatalf("rrset nodes = %#v", spec.Nodes)
	}
	if result.PeerGroup != nil {
		t.Fatalf("peer group = %#v, want nil for non-direct claim", result.PeerGroup)
	}
}

func TestGetSAMEnrollmentTopologyForAcceptedDirectClaimReturnsPolicyScopedPeerGroup(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	setSubmitTestDirectMeshPeerGroup(t, router, "pve-wg-leaves", "SAMPeerGroup/pve-direct-leaves")

	requester := loadSubmitTestClaim(t, "pve-leaf-a")
	requesterSpec, err := requester.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("requester spec: %v", err)
	}
	requesterSpec.DirectMesh = true
	requesterSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), requesterSpec)
	requester.Spec = requesterSpec

	peer := loadSubmitTestClaim(t, "pve-leaf-c")
	peerSpec, err := peer.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("peer spec: %v", err)
	}
	peerSpec.DirectMesh = true
	peerSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), peerSpec)
	peer.Spec = peerSpec

	// This is the rollout state that matters in practice: an accepted leaf has
	// opted into direct mesh but has not captured any mobility address yet. It
	// still needs to be present in the RR-projected peer group so its direct
	// session can be established before ownership appears.
	emptyPeer := peer
	emptyPeer.Metadata.Name = "pve-leaf-empty"
	emptySpec := peerSpec
	emptySpec.LeafID = "pve-leaf-empty"
	emptySpec.JoinNonce = "pve-leaf-empty-0001"
	emptySpec.TunnelAddress = "10.255.10.26/32"
	emptySpec.Endpoint = "10.31.0.26"
	emptySpec.WireGuard.PublicKey = "PVE_LEAF_EMPTY_WIREGUARD_PUBLIC_KEY"
	emptySpec.WireGuard.Endpoint = "10.30.0.26:51820"
	emptySpec.WireGuard.AllowedIPs = []string{"10.31.0.26/32"}
	emptySpec.Mobility.OwnedAddresses = nil
	emptySpec.BGP.RouterID = "10.255.10.26"
	emptySpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), emptySpec)
	emptyPeer.Spec = emptySpec

	nonDirect := peer
	nonDirect.Metadata.Name = "pve-leaf-non-direct"
	nonDirectSpec := peerSpec
	nonDirectSpec.LeafID = "pve-leaf-non-direct"
	nonDirectSpec.JoinNonce = "pve-leaf-non-direct-0001"
	nonDirectSpec.TunnelAddress = "10.255.10.25/32"
	nonDirectSpec.Endpoint = "10.31.0.25"
	nonDirectSpec.WireGuard.PublicKey = "PVE_LEAF_NON_DIRECT_WIREGUARD_PUBLIC_KEY"
	nonDirectSpec.WireGuard.Endpoint = "10.30.0.25:51820"
	nonDirectSpec.WireGuard.AllowedIPs = []string{"10.31.0.25/32"}
	nonDirectSpec.Mobility.OwnedAddresses = []string{"10.77.70.25/32", "10.77.70.18/32"}
	nonDirectSpec.BGP.RouterID = "10.255.10.25"
	nonDirectSpec.DirectMesh = false
	nonDirectSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), nonDirectSpec)
	nonDirect.Spec = nonDirectSpec

	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()
	for _, claim := range []api.Resource{requester, peer, emptyPeer, nonDirect} {
		if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
			t.Fatalf("submit %s: %v", claim.Metadata.Name, err)
		}
	}

	oldRequest := submitTestTopologyRequest(t, requester)
	result, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, oldRequest, now)
	if err != nil {
		t.Fatalf("getSAMEnrollmentTopologyForAcceptedClaim: %v", err)
	}
	if result.RRSet.Kind != "SAMRRSet" || result.RRSet.Metadata.Name != "pve-rrs" {
		t.Fatalf("rrset = %#v", result.RRSet)
	}
	if result.PeerGroup == nil || result.PeerGroup.Kind != "SAMPeerGroup" || result.PeerGroup.Metadata.Name != "pve-direct-leaves" {
		t.Fatalf("peer group = %#v", result.PeerGroup)
	}
	group, err := result.PeerGroup.SAMPeerGroupSpec()
	if err != nil {
		t.Fatalf("peer group spec: %v", err)
	}
	if group.EnrollmentPolicyRef != requesterSpec.PolicyRef {
		t.Fatalf("peer group policy = %q, want %q", group.EnrollmentPolicyRef, requesterSpec.PolicyRef)
	}
	transport, found, err := findSAMTransportProfile(router, "SAMTransportProfile/pve-rr-a-wg")
	if err != nil || !found {
		t.Fatalf("find transport profile: found=%v err=%v", found, err)
	}
	if group.TransportFingerprint != mobilityconfig.SAMTransportMeshFingerprint(transport) {
		t.Fatalf("transport fingerprint = %q", group.TransportFingerprint)
	}
	if len(group.Nodes) != 2 {
		t.Fatalf("direct nodes = %#v, want owned and empty opted-in remote leaves", group.Nodes)
	}
	nodes := map[string]api.SAMNodeSpec{}
	for _, node := range group.Nodes {
		nodes[node.NodeRef] = node
	}
	ownedNode, ownedFound := nodes["pve-leaf-c"]
	emptyNode, emptyFound := nodes["pve-leaf-empty"]
	if !ownedFound || !emptyFound || ownedNode.SAMEndpoint != "10.31.0.23" || ownedNode.WireGuard.PublicKey != "PVE_LEAF_C_WIREGUARD_PUBLIC_KEY" ||
		emptyNode.SAMEndpoint != "10.31.0.26" || emptyNode.WireGuard.PublicKey != "PVE_LEAF_EMPTY_WIREGUARD_PUBLIC_KEY" {
		t.Fatalf("direct node material = %#v", group.Nodes)
	}
	if got := group.OwnedPrefixesByNode["pve-leaf-c"]; len(got) != 2 || got[0] != "10.77.70.19/32" || got[1] != "10.77.70.23/32" {
		t.Fatalf("direct node signed owned prefixes = %#v", group.OwnedPrefixesByNode)
	}
	if got := group.OwnedPrefixesByNode["pve-leaf-empty"]; len(got) != 0 {
		t.Fatalf("empty direct leaf received invented owned prefixes: %#v", group.OwnedPrefixesByNode)
	}

	// A claim rotation retains the same resource name. An identity-aware
	// client must be told that its old identity is not accepted, so it can
	// submit the deliberate rotation rather than treating a same-name claim as
	// authority for direct topology. A legacy client remains RR-only.
	rotated := requesterSpec
	rotated.JoinNonce = "pve-leaf-a-rotated-0001"
	rotated.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), rotated)
	requester.Spec = rotated
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: requester}, now); err != nil {
		t.Fatalf("submit rotated requester: %v", err)
	}
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, oldRequest, now); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a "+controlapi.SAMEnrollmentTopologyIdentityMismatchMessage) {
		t.Fatalf("stale identity topology error = %v, want active identity mismatch", err)
	}
	legacyRequest := oldRequest
	legacyRequest.ClaimDigest = ""
	legacyRequest.ClaimIdentityDigest = ""
	legacyResult, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, legacyRequest, now)
	if err != nil {
		t.Fatalf("get legacy claim topology: %v", err)
	}
	if legacyResult.RRSet.Kind != "SAMRRSet" || legacyResult.PeerGroup != nil || legacyResult.ClaimDigest != samenrollment.ClaimDigest(rotated) {
		t.Fatalf("legacy claim topology = %#v, want attested RR fallback only", legacyResult)
	}
	currentResult, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, requester), now)
	if err != nil {
		t.Fatalf("get rotated claim topology: %v", err)
	}
	if currentResult.PeerGroup == nil {
		t.Fatalf("current claim topology = %#v, want direct peer group", currentResult)
	}
}

func TestSubmitSAMEnrollmentClaimRejectsDirectMeshWithoutPolicyPeerGroup(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	spec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	spec.DirectMesh = true
	spec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), spec)
	claim.Spec = spec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	_, err = submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now)
	if err == nil || !strings.Contains(err.Error(), "directMesh.peerGroupRef") {
		t.Fatalf("submitSAMEnrollmentClaim error = %v, want direct mesh policy rejection", err)
	}
}

func TestGetSAMEnrollmentTopologyPolicyDirectMeshDisableKeepsRRFallback(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	setSubmitTestDirectMeshPeerGroup(t, router, "pve-wg-leaves", "SAMPeerGroup/pve-direct-leaves")
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.DirectMesh = true
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatalf("submit direct claim: %v", err)
	}

	// Removing the optional accelerator must not strand already accepted
	// direct claims. New direct submissions remain rejected at submit time,
	// while existing claims receive their RR topology without a peer group.
	setSubmitTestDirectMeshPeerGroup(t, router, "pve-wg-leaves", "")
	result, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, claim), now)
	if err != nil {
		t.Fatalf("get topology after direct disable: %v", err)
	}
	if result.RRSet.Kind != "SAMRRSet" || result.PeerGroup != nil {
		t.Fatalf("topology after direct disable = %#v, want RRSet-only fallback", result)
	}
}

func TestRevokeSAMEnrollmentClaimExpiresAcceptedClaim(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatalf("submitSAMEnrollmentClaim: %v", err)
	}
	revokeAt := now.Add(time.Minute)
	result, err := revokeSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimRevokeRequest{Name: "pve-leaf-a", Reason: "rotate"}, revokeAt)
	if err != nil {
		t.Fatalf("revokeSAMEnrollmentClaim: %v", err)
	}
	if !result.Revoked || result.ClaimRef != "SAMEnrollmentClaim/pve-leaf-a" || !result.ExpiresAt.Equal(revokeAt) {
		t.Fatalf("revoke result = %#v", result)
	}
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, claim), revokeAt); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a is revoked") {
		t.Fatalf("post-revoke getSAMEnrollmentTopologyForAcceptedClaim error = %v, want explicit revocation", err)
	}
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, revokeAt.Add(time.Second)); err == nil || !strings.Contains(err.Error(), "was revoked; use a new client identity") {
		t.Fatalf("replay revoked submit error = %v, want new identity required", err)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 || records[0].EffectiveStatus(revokeAt) != "expired" || !strings.Contains(records[0].ResourcesJSON, `"revoked":true`) {
		t.Fatalf("records = %#v", records)
	}
	rotated := claim
	rotatedSpec, err := rotated.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("rotated claim spec: %v", err)
	}
	rotatedSpec.JoinNonce = "pve-leaf-a-rotated-0001"
	rotatedSpec.JoinTimestamp = revokeAt.Add(2 * time.Minute).Format(time.RFC3339)
	rotatedSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), rotatedSpec)
	rotated.Spec = rotatedSpec
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: rotated}, revokeAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("rotated submitSAMEnrollmentClaim: %v", err)
	}
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, rotated), revokeAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("rotated getSAMEnrollmentTopologyForAcceptedClaim: %v", err)
	}
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, claim), revokeAt.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a is revoked") {
		t.Fatalf("revoked identity against rotated active claim error = %v, want explicit revocation", err)
	}
	fresh := rotated
	freshSpec, err := fresh.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("fresh claim spec: %v", err)
	}
	freshSpec.JoinNonce = "pve-leaf-a-rotated-0002"
	freshSpec.JoinTimestamp = revokeAt.Add(3 * time.Minute).Format(time.RFC3339)
	freshSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), freshSpec)
	fresh.Spec = freshSpec
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, fresh), revokeAt.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a "+controlapi.SAMEnrollmentTopologyIdentityMismatchMessage) {
		t.Fatalf("fresh replacement identity error = %v, want active identity mismatch before submit", err)
	}
	// The new active row replaced the revoked row, but the old client identity
	// must remain denied. Otherwise a captured nonce-A claim could roll a
	// successfully re-enrolled nonce-B leaf back during its old lease window.
	if err := store.Close(); err != nil {
		t.Fatalf("close state before replay check: %v", err)
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen state before replay check: %v", err)
	}
	defer store.Close()
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, revokeAt.Add(3*time.Minute)); err == nil || !strings.Contains(err.Error(), "was revoked; use a new client identity") {
		t.Fatalf("old claim after rotation error = %v, want durable replay rejection", err)
	}
	renamed := claim
	renamed.Metadata.Name = "pve-leaf-a-replay"
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: renamed}, revokeAt.Add(3*time.Minute)); err == nil || !strings.Contains(err.Error(), "was revoked; use a new client identity") {
		t.Fatalf("renamed old claim after rotation error = %v, want global identity replay rejection", err)
	}
	identityRevoked, err := store.HasSAMEnrollmentRevokedIdentity(samenrollment.ClientIdentityDigest(claimSpec))
	if err != nil {
		t.Fatalf("HasSAMEnrollmentRevokedIdentity: %v", err)
	}
	if !identityRevoked {
		t.Fatal("missing durable revoked identity after state reopen")
	}
}

func TestRevokeSAMEnrollmentClaimRejectsNonceLessReplay(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	clearSubmitTestJoinToken(t, router, "pve-wg-leaves")
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.JoinNonce = ""
	claimSpec.JoinTimestamp = ""
	claimSpec.JoinHMAC = ""
	claim.Spec = claimSpec
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatalf("submit nonce-less claim: %v", err)
	}
	if _, err := revokeSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimRevokeRequest{Name: "pve-leaf-a", Reason: "retire"}, now.Add(time.Minute)); err != nil {
		t.Fatalf("revoke nonce-less claim: %v", err)
	}
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "was revoked; use a new client identity") {
		t.Fatalf("nonce-less replay error = %v, want revoked identity rejection", err)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 || !strings.Contains(records[0].ResourcesJSON, `"revoked":true`) {
		t.Fatalf("nonce-less replay replaced revoked record: %#v", records)
	}
	rotated := claim
	rotatedSpec, err := rotated.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("rotated nonce-less claim spec: %v", err)
	}
	rotatedSpec.JoinNonce = "pve-leaf-a-reenroll-0001"
	rotatedSpec.JoinTimestamp = now.Add(3 * time.Minute).Format(time.RFC3339)
	rotated.Spec = rotatedSpec
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: rotated}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("nonce-added re-enrollment: %v", err)
	}
}

func TestSAMEnrollmentLegacyRevocationTombstoneBackfillsIdentity(t *testing.T) {
	now := time.Date(2026, 8, 22, 21, 30, 0, 0, time.UTC)
	router := loadSubmitTestRouter(t)
	secretFile := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretFile, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("write join token: %v", err)
	}
	setSubmitTestJoinToken(t, router, "pve-wg-leaves", secretFile)
	claim := loadSubmitTestClaim(t, "pve-leaf-a")
	claimSpec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	claimSpec.JoinHMAC = samenrollment.JoinHMAC([]byte("test-join-token"), claimSpec)
	claim.Spec = claimSpec

	legacy := claim
	legacySpec := claimSpec
	legacySpec.Revoked = true
	legacySpec.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
	legacy.Spec = legacySpec
	resources, err := json.Marshal([]api.Resource{legacy})
	if err != nil {
		t.Fatalf("marshal legacy revoked claim: %v", err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:        "SAMEnrollmentClaim/pve-leaf-a",
		Generation:    1,
		ObservedAt:    now.Add(-2 * time.Minute),
		ExpiresAt:     now.Add(-time.Minute),
		Digest:        "sha256:legacy-tombstone",
		ResourcesJSON: string(resources),
	}); err != nil {
		t.Fatalf("UpsertDynamicConfigPart legacy tombstone: %v", err)
	}
	identity := samenrollment.ClientIdentityDigest(claimSpec)
	if found, err := store.HasSAMEnrollmentRevokedIdentity(identity); err != nil || found {
		t.Fatalf("pre-upgrade identity ledger found=%t err=%v, want absent", found, err)
	}
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, submitTestTopologyRequest(t, claim), now); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a is revoked") {
		t.Fatalf("legacy topology error = %v, want explicit revocation", err)
	}
	if found, err := store.HasSAMEnrollmentRevokedIdentity(identity); err != nil || !found {
		t.Fatalf("legacy identity ledger found=%t err=%v, want durable backfill", found, err)
	}
	renamed := claim
	renamed.Metadata.Name = "pve-leaf-a-replay"
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: renamed}, now); err == nil || !strings.Contains(err.Error(), "was revoked; use a new client identity") {
		t.Fatalf("renamed legacy replay error = %v, want global identity rejection", err)
	}
}

func TestActiveSubmittedSAMEnrollmentClaimRequiresSourceClaimName(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 1, 0, 0, time.UTC)
	wrongClaim := loadSubmitTestClaim(t, "pve-leaf-a")
	wrongClaim.Metadata.Name = "pve-leaf-b"
	resources, err := json.Marshal([]api.Resource{wrongClaim})
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	records := []routerstate.DynamicConfigPartRecord{{
		Source:        "SAMEnrollmentClaim/pve-leaf-a",
		ObservedAt:    now,
		ExpiresAt:     now.Add(time.Hour),
		ResourcesJSON: string(resources),
	}}
	if _, _, ok := activeSubmittedSAMEnrollmentClaimResource(records, "SAMEnrollmentClaim/pve-leaf-a", now); ok {
		t.Fatal("wrong-name claim in a corrupted source record was accepted")
	}
}

func loadSubmitTestRouter(t *testing.T) *api.Router {
	t.Helper()
	router, err := config.Load(filepath.Join("..", "..", "examples", "pve-minimal-rr.yaml"))
	if err != nil {
		t.Fatalf("load pve-minimal-rr.yaml: %v", err)
	}
	if hasSubmitTestResource(router, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-a") ||
		hasSubmitTestResource(router, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-b") {
		t.Fatalf("pve-minimal-rr base must not contain seeded claims")
	}
	return router
}

func submitTestTopologyRequest(t *testing.T, claim api.Resource) controlapi.SAMEnrollmentTopologyGetRequest {
	t.Helper()
	spec, err := claim.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	return controlapi.SAMEnrollmentTopologyGetRequest{
		Name:                "pve-rrs",
		ClaimRef:            "SAMEnrollmentClaim/" + claim.Metadata.Name,
		ClaimDigest:         samenrollment.ClaimDigest(spec),
		ClaimIdentityDigest: samenrollment.ClientIdentityDigest(spec),
	}
}

func loadSubmitTestClaim(t *testing.T, name string) api.Resource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "pve-minimal-rr-claims-seed.yaml"))
	if err != nil {
		t.Fatalf("read claim seed: %v", err)
	}
	var seed api.Router
	if err := yaml.Unmarshal(data, &seed); err != nil {
		t.Fatalf("parse claim seed: %v", err)
	}
	for _, resource := range seed.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentClaim" && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("missing seed claim %s", name)
	return api.Resource{}
}

func setSubmitTestJoinToken(t *testing.T, router *api.Router, policyName, secretFile string) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != policyName {
			continue
		}
		spec, err := resource.SAMEnrollmentPolicySpec()
		if err != nil {
			t.Fatalf("policy spec: %v", err)
		}
		spec.JoinTokenFrom.File = secretFile
		router.Spec.Resources[i].Spec = spec
		return
	}
	t.Fatalf("missing policy %s", policyName)
}

func clearSubmitTestJoinToken(t *testing.T, router *api.Router, policyName string) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != policyName {
			continue
		}
		spec, err := resource.SAMEnrollmentPolicySpec()
		if err != nil {
			t.Fatalf("policy spec: %v", err)
		}
		spec.JoinTokenFrom = api.SecretValueSourceSpec{}
		router.Spec.Resources[i].Spec = spec
		return
	}
	t.Fatalf("missing policy %s", policyName)
}

func setSubmitTestDirectMeshPeerGroup(t *testing.T, router *api.Router, policyName, peerGroupRef string) {
	t.Helper()
	for i, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != policyName {
			continue
		}
		spec, err := resource.SAMEnrollmentPolicySpec()
		if err != nil {
			t.Fatalf("policy spec: %v", err)
		}
		spec.DirectMesh.PeerGroupRef = peerGroupRef
		router.Spec.Resources[i].Spec = spec
		return
	}
	t.Fatalf("missing policy %s", policyName)
}

func hasSubmitTestResource(router *api.Router, apiVersion, kind, name string) bool {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return true
		}
	}
	return false
}
