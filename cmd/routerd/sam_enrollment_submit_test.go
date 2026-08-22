// SPDX-License-Identifier: BSD-3-Clause

package main

import (
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

	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, controlapi.SAMEnrollmentTopologyGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, now); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a not found") {
		t.Fatalf("pre-submit getSAMEnrollmentTopologyForAcceptedClaim error = %v, want accepted claim required", err)
	}
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatalf("submitSAMEnrollmentClaim: %v", err)
	}
	result, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, controlapi.SAMEnrollmentTopologyGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, now)
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
	for _, claim := range []api.Resource{requester, peer, nonDirect} {
		if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
			t.Fatalf("submit %s: %v", claim.Metadata.Name, err)
		}
	}

	result, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, controlapi.SAMEnrollmentTopologyGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, now)
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
	if len(group.Nodes) != 1 || group.Nodes[0].NodeRef != "pve-leaf-c" {
		t.Fatalf("direct nodes = %#v, want only opted-in remote leaf", group.Nodes)
	}
	if group.Nodes[0].SAMEndpoint != "10.31.0.23" || group.Nodes[0].WireGuard.PublicKey != "PVE_LEAF_C_WIREGUARD_PUBLIC_KEY" {
		t.Fatalf("direct node material = %#v", group.Nodes[0])
	}
	if got := group.OwnedPrefixesByNode["pve-leaf-c"]; len(got) != 2 || got[0] != "10.77.70.19/32" || got[1] != "10.77.70.23/32" {
		t.Fatalf("direct node signed owned prefixes = %#v", group.OwnedPrefixesByNode)
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
	result, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, controlapi.SAMEnrollmentTopologyGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, now)
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
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
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
	if _, err := getSAMEnrollmentTopologyForAcceptedClaim(router, store, controlapi.SAMEnrollmentTopologyGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, revokeAt); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a not found") {
		t.Fatalf("post-revoke getSAMEnrollmentTopologyForAcceptedClaim error = %v, want accepted claim required", err)
	}
	records, err := store.GetDynamicConfigPartsBySource("SAMEnrollmentClaim/pve-leaf-a")
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(records) != 1 || records[0].EffectiveStatus(revokeAt) != "expired" || !strings.Contains(records[0].ResourcesJSON, `"revoked":true`) {
		t.Fatalf("records = %#v", records)
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
