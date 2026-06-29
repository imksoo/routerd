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
	if want := now.Add(24 * time.Hour); !result.ExpiresAt.Equal(want) {
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
	claimSpec.ExpiresAt = "2026-06-29T00:01:00Z"
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

func TestGetSAMRRSetForAcceptedClaimReturnsOnlyClaimRRSet(t *testing.T) {
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

	if _, err := getSAMRRSetForAcceptedClaim(router, store, controlapi.SAMRRSetGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, now); err == nil || !strings.Contains(err.Error(), "accepted SAMEnrollmentClaim/pve-leaf-a not found") {
		t.Fatalf("pre-submit getSAMRRSetForAcceptedClaim error = %v, want accepted claim required", err)
	}
	if _, err := submitSAMEnrollmentClaim(router, store, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim}, now); err != nil {
		t.Fatalf("submitSAMEnrollmentClaim: %v", err)
	}
	result, err := getSAMRRSetForAcceptedClaim(router, store, controlapi.SAMRRSetGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"}, now)
	if err != nil {
		t.Fatalf("getSAMRRSetForAcceptedClaim: %v", err)
	}
	if result.RRSet.APIVersion != api.MobilityAPIVersion || result.RRSet.Kind != "SAMRRSet" || result.RRSet.Metadata.Name != "pve-rrs" {
		t.Fatalf("rrset result = %#v", result.RRSet)
	}
	spec, err := result.RRSet.SAMRRSetSpec()
	if err != nil {
		t.Fatalf("rrset spec: %v", err)
	}
	if len(spec.Members) != 1 || spec.Members[0].NodeRef != "pve-rr" {
		t.Fatalf("rrset members = %#v", spec.Members)
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

func hasSubmitTestResource(router *api.Router, apiVersion, kind, name string) bool {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return true
		}
	}
	return false
}
