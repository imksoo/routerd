// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const samEnrollmentClaimDynamicGeneration = int64(1)

type samEnrollmentClaimStore interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	RecordSAMEnrollmentRevokedIdentity(claimSource, identityDigest string, revokedAt time.Time) error
	HasSAMEnrollmentRevokedIdentity(identityDigest string) (bool, error)
}

func submitSAMEnrollmentClaim(router *api.Router, store samEnrollmentClaimStore, req controlapi.SAMEnrollmentClaimSubmitRequest, now time.Time) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: router config unavailable", controlapi.ErrBadRequest)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: state store unavailable", controlapi.ErrBadRequest)
	}
	claimResource, claim, err := normalizeSubmittedSAMEnrollmentClaim(req.Claim)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	if claim.Revoked {
		return nil, fmt.Errorf("%w: submitted SAMEnrollmentClaim must not be revoked", controlapi.ErrBadRequest)
	}
	source := "SAMEnrollmentClaim/" + claimResource.Metadata.Name
	observedAt := now.UTC()
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		return nil, err
	}
	identityDigest := samenrollment.ClientIdentityDigest(claim)
	identityRevoked, err := store.HasSAMEnrollmentRevokedIdentity(identityDigest)
	if err != nil {
		return nil, err
	}
	if !identityRevoked {
		if legacySource, found := revokedSAMEnrollmentClaimIdentitySource(records, identityDigest); found {
			// Upgrade-safe lazy backfill: older releases stored only a revoked
			// dynamic claim row, so preserve its client identity before accepting
			// any future request that might replace that row.
			if err := store.RecordSAMEnrollmentRevokedIdentity(legacySource, identityDigest, observedAt); err != nil {
				return nil, err
			}
			identityRevoked = true
		}
	}
	if identityRevoked {
		return nil, fmt.Errorf("%w: %s was revoked; use a new client identity (advance spec.joinNonce for join-token enrollment)", controlapi.ErrBadRequest, source)
	}
	if revokedClaim, revoked := revokedSubmittedSAMEnrollmentClaim(records, source); revoked && !submittedSAMEnrollmentClaimRotatesIdentity(revokedClaim, claim) {
		return nil, fmt.Errorf("%w: %s was revoked; use a new client identity (advance spec.joinNonce for join-token enrollment)", controlapi.ErrBadRequest, source)
	} else if revoked {
		// Preserve the old identity before this successful re-enrollment
		// replaces the current dynamic claim row. Without this ledger, a
		// captured pre-revoke client identity could be replayed after the new
		// identity becomes active.
		if err := store.RecordSAMEnrollmentRevokedIdentity(source, samenrollment.ClientIdentityDigest(revokedClaim), observedAt); err != nil {
			return nil, err
		}
	}
	policy, err := submittedSAMEnrollmentClaimPolicy(router, store, source, claim, observedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	if claim.DirectMesh && strings.TrimSpace(policy.DirectMesh.PeerGroupRef) == "" {
		return nil, fmt.Errorf("%w: %s spec.directMesh requires %s spec.directMesh.peerGroupRef", controlapi.ErrBadRequest, claimResource.ID(), claim.PolicyRef)
	}
	if err := validateSubmittedSAMEnrollmentClaimJoinToken(claimResource.ID(), policy, claim); err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	expiresAt, err := submittedSAMEnrollmentClaimExpiresAt(claim, policy, observedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: "sam-enrollment-claim-" + claimResource.Metadata.Name,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "SAMEnrollmentClaim",
				Name:       claimResource.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     source,
			Generation: samEnrollmentClaimDynamicGeneration,
			ObservedAt: observedAt,
			ExpiresAt:  expiresAt,
			Resources:  []api.Resource{claimResource},
		},
	}
	part.Spec.Digest = digestSAMEnrollmentClaimPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return nil, err
	}
	if err := validateSubmittedSAMEnrollmentClaim(router, store, source, part, observedAt); err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		return nil, err
	}
	result := controlapi.NewSAMEnrollmentClaimSubmitResult(source, source, samEnrollmentClaimDynamicGeneration, observedAt, expiresAt)
	return &result, nil
}

func getSAMEnrollmentTopologyForAcceptedClaim(router *api.Router, store samEnrollmentClaimStore, req controlapi.SAMEnrollmentTopologyGetRequest, now time.Time) (*controlapi.SAMEnrollmentTopologyGetResult, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: router config unavailable", controlapi.ErrBadRequest)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: state store unavailable", controlapi.ErrBadRequest)
	}
	rrSetName := strings.TrimSpace(req.Name)
	if rrSetName == "" {
		return nil, fmt.Errorf("%w: SAM enrollment topology name is required", controlapi.ErrBadRequest)
	}
	claimName, err := samEnrollmentClaimNameFromRef(req.ClaimRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	claimSource := "SAMEnrollmentClaim/" + claimName
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		return nil, err
	}
	identityDigest := strings.TrimSpace(req.ClaimIdentityDigest)
	identityRevoked := false
	if identityDigest != "" {
		identityRevoked, err = store.HasSAMEnrollmentRevokedIdentity(identityDigest)
		if err != nil {
			return nil, err
		}
		if !identityRevoked {
			if legacySource, found := revokedSAMEnrollmentClaimIdentitySource(records, identityDigest); found {
				// Preserve a pre-table tombstone when a new client first asks for
				// it, before another admission can replace the dynamic record.
				if err := store.RecordSAMEnrollmentRevokedIdentity(legacySource, identityDigest, now.UTC()); err != nil {
					return nil, err
				}
				identityRevoked = true
			}
		}
	}
	if identityRevoked {
		return nil, fmt.Errorf("%w: accepted %s is revoked", controlapi.ErrBadRequest, claimSource)
	}
	_, acceptedClaim, ok := activeSubmittedSAMEnrollmentClaimResource(records, claimSource, now.UTC())
	if !ok {
		if identityDigest == "" && submittedSAMEnrollmentClaimWasExplicitlyRevoked(records, claimSource) {
			return nil, fmt.Errorf("%w: accepted %s is revoked", controlapi.ErrBadRequest, claimSource)
		}
		return nil, samEnrollmentTopologyClaimIdentityAbsentError(req, claimSource)
	}
	if identityDigest != "" && samenrollment.ClientIdentityDigest(acceptedClaim) != identityDigest {
		// A client with a deliberately rotated identity must reach Submit rather
		// than treating an older active same-name claim as proof of direct
		// topology. Keep this distinct from an empty admission store: a periodic
		// direct refresh must not overwrite a newer active identity.
		return nil, samEnrollmentTopologyClaimIdentityMismatchError(claimSource)
	}
	acceptedClaimDigest := samenrollment.ClaimDigest(acceptedClaim)
	claimDigestMatches := strings.TrimSpace(req.ClaimDigest) != "" && strings.TrimSpace(req.ClaimDigest) == acceptedClaimDigest
	parts, err := samEnrollmentDynamicPartsFromRecords(records, "")
	if err != nil {
		return nil, err
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		return nil, err
	}
	effective, _, err := dynamicconfig.BuildEffectiveConfig(*router, parts, policies, now.UTC())
	if err != nil {
		return nil, err
	}
	claimResource, claim, ok, err := findSAMEnrollmentClaimResource(&effective, claimName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: accepted %s not found in effective config", controlapi.ErrBadRequest, claimSource)
	}
	if samenrollment.ClaimDigest(claim) != acceptedClaimDigest {
		return nil, fmt.Errorf("%w: accepted %s does not match its effective claim", controlapi.ErrBadRequest, claimSource)
	}
	wantRRSetRef := "SAMRRSet/" + rrSetName
	if strings.TrimSpace(claim.RRSetRef) != wantRRSetRef {
		return nil, fmt.Errorf("%w: %s references %s, not %s", controlapi.ErrBadRequest, claimResource.ID(), claim.RRSetRef, wantRRSetRef)
	}
	policyName, err := samEnrollmentPolicyNameFromRef(claim.PolicyRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	policy, ok, err := findSAMEnrollmentPolicy(&effective, policyName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s not found", controlapi.ErrBadRequest, claim.PolicyRef)
	}
	if strings.TrimSpace(policy.RRSetRef) != wantRRSetRef {
		return nil, fmt.Errorf("%w: %s references %s, not %s", controlapi.ErrBadRequest, claim.PolicyRef, policy.RRSetRef, wantRRSetRef)
	}
	rrSetResource, peerGroup, err := projectSAMEnrollmentTopologyForEnrollment(&effective, claim, policyName, policy, rrSetName, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	// The RR topology is safe as a compatibility fallback, but a direct peer
	// group must be tied to the exact active client claim. Older clients that
	// do not send a digest and clients talking to a stale RR therefore keep
	// renewing their RR path without being handed a high-preference direct
	// path.  The result always echoes the RR's active digest so a new client
	// can make the same fail-closed decision.
	if !claimDigestMatches {
		peerGroup = nil
	}
	result := controlapi.NewSAMEnrollmentTopologyGetResult(rrSetName, acceptedClaimDigest, rrSetResource, peerGroup)
	return &result, nil
}

// samEnrollmentTopologyClaimIdentityAbsentError preserves the legacy response
// for callers that do not send an identity digest. An identity-aware client
// can safely distinguish a current RR's empty admission store from an older
// RR that ignored the identity query and returned its ambiguous legacy
// not-found response. Only the former may trigger automatic direct-claim
// re-admission.
func samEnrollmentTopologyClaimIdentityAbsentError(req controlapi.SAMEnrollmentTopologyGetRequest, claimSource string) error {
	if strings.TrimSpace(req.ClaimIdentityDigest) != "" {
		return fmt.Errorf("%w: accepted %s %s", controlapi.ErrBadRequest, claimSource, controlapi.SAMEnrollmentTopologyIdentityAbsentMessage)
	}
	return fmt.Errorf("%w: accepted %s not found", controlapi.ErrBadRequest, claimSource)
}

func samEnrollmentTopologyClaimIdentityMismatchError(claimSource string) error {
	return fmt.Errorf("%w: accepted %s %s", controlapi.ErrBadRequest, claimSource, controlapi.SAMEnrollmentTopologyIdentityMismatchMessage)
}

func revokeSAMEnrollmentClaim(router *api.Router, store samEnrollmentClaimStore, req controlapi.SAMEnrollmentClaimRevokeRequest, now time.Time) (*controlapi.SAMEnrollmentClaimRevokeResult, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: router config unavailable", controlapi.ErrBadRequest)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: state store unavailable", controlapi.ErrBadRequest)
	}
	claimName, err := samEnrollmentClaimNameFromRef(req.Name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	source := "SAMEnrollmentClaim/" + claimName
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		return nil, err
	}
	claimResource, claim, ok := activeSubmittedSAMEnrollmentClaimResource(records, source, now.UTC())
	if !ok {
		return nil, fmt.Errorf("%w: active %s not found", controlapi.ErrBadRequest, source)
	}
	observedAt := now.UTC()
	claim.Revoked = true
	claim.ExpiresAt = observedAt.Format(time.RFC3339)
	claimResource.Spec = claim
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: "sam-enrollment-claim-" + claimResource.Metadata.Name,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "SAMEnrollmentClaim",
				Name:       claimResource.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     source,
			Generation: samEnrollmentClaimDynamicGeneration,
			ObservedAt: observedAt,
			ExpiresAt:  observedAt,
			Resources:  []api.Resource{claimResource},
		},
	}
	part.Spec.Digest = digestSAMEnrollmentClaimPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return nil, err
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		return nil, err
	}
	// The active claim row is replaced on a later re-enrollment. Preserve the
	// revoked client identity separately so that an old request cannot roll the
	// claim back after a successful rotation.
	if err := store.RecordSAMEnrollmentRevokedIdentity(source, samenrollment.ClientIdentityDigest(claim), observedAt); err != nil {
		return nil, err
	}
	result := controlapi.NewSAMEnrollmentClaimRevokeResult(source, source, samEnrollmentClaimDynamicGeneration, observedAt, observedAt, strings.TrimSpace(req.Reason))
	return &result, nil
}

func normalizeSubmittedSAMEnrollmentClaim(resource api.Resource) (api.Resource, api.SAMEnrollmentClaimSpec, error) {
	if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" {
		return api.Resource{}, api.SAMEnrollmentClaimSpec{}, fmt.Errorf("claim must be %s/SAMEnrollmentClaim", api.MobilityAPIVersion)
	}
	resource.Metadata.Name = strings.TrimSpace(resource.Metadata.Name)
	if resource.Metadata.Name == "" {
		return api.Resource{}, api.SAMEnrollmentClaimSpec{}, fmt.Errorf("claim metadata.name is required")
	}
	claim, err := resource.SAMEnrollmentClaimSpec()
	if err != nil {
		return api.Resource{}, api.SAMEnrollmentClaimSpec{}, err
	}
	resource.Spec = claim
	return resource, claim, nil
}

func samEnrollmentClaimNameFromRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("claim query parameter is required")
	}
	if !strings.Contains(ref, "/") {
		if ref == "" {
			return "", fmt.Errorf("claim query parameter is required")
		}
		return ref, nil
	}
	kind, name, ok := strings.Cut(ref, "/")
	if !ok || kind != "SAMEnrollmentClaim" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("claim must reference SAMEnrollmentClaim/<name>")
	}
	return strings.TrimSpace(name), nil
}

func samEnrollmentPolicyNameFromRef(ref string) (string, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMEnrollmentPolicy" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("policyRef must reference SAMEnrollmentPolicy/<name>")
	}
	return strings.TrimSpace(name), nil
}

func activeSubmittedSAMEnrollmentClaimResource(records []routerstate.DynamicConfigPartRecord, source string, now time.Time) (api.Resource, api.SAMEnrollmentClaimSpec, bool) {
	claimName, err := samEnrollmentClaimNameFromRef(source)
	if err != nil {
		return api.Resource{}, api.SAMEnrollmentClaimSpec{}, false
	}
	for _, record := range records {
		if record.Source != source || record.EffectiveStatus(now.UTC()) != "active" {
			continue
		}
		if strings.TrimSpace(record.ResourcesJSON) == "" {
			continue
		}
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
			continue
		}
		for _, resource := range resources {
			if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentClaim" && strings.TrimSpace(resource.Metadata.Name) == claimName {
				claim, err := resource.SAMEnrollmentClaimSpec()
				if err != nil || claim.Revoked {
					continue
				}
				return resource, claim, true
			}
		}
	}
	return api.Resource{}, api.SAMEnrollmentClaimSpec{}, false
}

// submittedSAMEnrollmentClaimWasExplicitlyRevoked distinguishes an operator
// revocation from a reflector restart that simply lost its volatile claim
// store. The client must retain its RR fallback for a revocation rather than
// treating it as a recovery signal and silently re-admitting the claim.
func submittedSAMEnrollmentClaimWasExplicitlyRevoked(records []routerstate.DynamicConfigPartRecord, source string) bool {
	_, found := revokedSubmittedSAMEnrollmentClaim(records, source)
	return found
}

func revokedSubmittedSAMEnrollmentClaim(records []routerstate.DynamicConfigPartRecord, source string) (api.SAMEnrollmentClaimSpec, bool) {
	claimName, err := samEnrollmentClaimNameFromRef(source)
	if err != nil {
		return api.SAMEnrollmentClaimSpec{}, false
	}
	for _, record := range records {
		if record.Source != source || strings.TrimSpace(record.ResourcesJSON) == "" {
			continue
		}
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
			continue
		}
		for _, resource := range resources {
			if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || strings.TrimSpace(resource.Metadata.Name) != claimName {
				continue
			}
			claim, err := resource.SAMEnrollmentClaimSpec()
			if err == nil && claim.Revoked {
				return claim, true
			}
		}
	}
	return api.SAMEnrollmentClaimSpec{}, false
}

// revokedSAMEnrollmentClaimIdentitySource recognizes tombstones produced
// before the dedicated revocation table existed. Match the client identity
// across every enrollment source, not only the mutable resource name, so an
// old request cannot bypass an upgrade-era tombstone by changing metadata.name.
func revokedSAMEnrollmentClaimIdentitySource(records []routerstate.DynamicConfigPartRecord, identityDigest string) (string, bool) {
	identityDigest = strings.TrimSpace(identityDigest)
	if identityDigest == "" {
		return "", false
	}
	for _, record := range records {
		claimName, err := samEnrollmentClaimNameFromRef(record.Source)
		if err != nil || strings.TrimSpace(record.ResourcesJSON) == "" {
			continue
		}
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
			continue
		}
		for _, resource := range resources {
			if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || strings.TrimSpace(resource.Metadata.Name) != claimName {
				continue
			}
			claim, err := resource.SAMEnrollmentClaimSpec()
			if err == nil && claim.Revoked && samenrollment.ClientIdentityDigest(claim) == identityDigest {
				return record.Source, true
			}
		}
	}
	return "", false
}

// submittedSAMEnrollmentClaimRotatesIdentity permits intentional re-enrollment
// after an operator revokes a claim, but never lets the old claim overwrite a
// revocation tombstone. Join-token policies already require a nonce; for an
// older nonce-less policy, compare only the client-authored identity. ClaimDigest
// deliberately includes RR-owned expiry and revocation state, so it is not a
// valid replay boundary here.
func submittedSAMEnrollmentClaimRotatesIdentity(revoked, submitted api.SAMEnrollmentClaimSpec) bool {
	previousNonce := strings.TrimSpace(revoked.JoinNonce)
	nextNonce := strings.TrimSpace(submitted.JoinNonce)
	switch {
	case previousNonce != "" && nextNonce != "":
		// A token-authenticated claim must advance its replay nonce; changing
		// some other signed field is not a substitute for that boundary.
		return previousNonce != nextNonce
	case previousNonce != "" && nextNonce == "":
		// Never permit a token-era claim to downgrade to nonce-less admission.
		return false
	case previousNonce == "" && nextNonce != "":
		// A legacy nonce-less claim can deliberately move onto the current
		// nonce-based identity format after operator revocation.
		return true
	}
	return samenrollment.ClientIdentityDigest(revoked) != samenrollment.ClientIdentityDigest(submitted)
}

func findSAMEnrollmentClaimResource(router *api.Router, name string) (api.Resource, api.SAMEnrollmentClaimSpec, bool, error) {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			return api.Resource{}, api.SAMEnrollmentClaimSpec{}, false, err
		}
		return resource, spec, true, nil
	}
	return api.Resource{}, api.SAMEnrollmentClaimSpec{}, false, nil
}

func findSAMEnrollmentPolicy(router *api.Router, name string) (api.SAMEnrollmentPolicySpec, bool, error) {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.SAMEnrollmentPolicySpec()
		if err != nil {
			return api.SAMEnrollmentPolicySpec{}, false, err
		}
		return spec, true, nil
	}
	return api.SAMEnrollmentPolicySpec{}, false, nil
}

// projectSAMEnrollmentTopologyForEnrollment serializes the selected RR nodes
// and, for a direct-mesh admitted leaf, the policy-scoped direct leaf topology
// across the enrollment boundary. Both resources are deliberately runtime-only
// snapshots built from the effective configuration after admission.
func projectSAMEnrollmentTopologyForEnrollment(router *api.Router, claim api.SAMEnrollmentClaimSpec, policyName string, policy api.SAMEnrollmentPolicySpec, rrSetName string, now time.Time) (api.Resource, *api.Resource, error) {
	rrSet, err := projectSAMRRSetForEnrollment(router, policyName, policy, rrSetName)
	if err != nil {
		return api.Resource{}, nil, err
	}
	if !claim.DirectMesh || strings.TrimSpace(policy.DirectMesh.PeerGroupRef) == "" {
		return rrSet, nil, nil
	}
	peerGroup, err := projectSAMEnrollmentDirectPeerGroupForEnrollment(router, claim, policy, now)
	if err != nil {
		return api.Resource{}, nil, err
	}
	return rrSet, &peerGroup, nil
}

// projectSAMRRSetForEnrollment serializes the selected RR nodes across the
// enrollment boundary. SAMRRSet itself is deliberately runtime-only: static
// identity/topology lives in the policy's SAMNodeSet, and the result is scoped
// to the already admitted claim's policy.
func projectSAMRRSetForEnrollment(router *api.Router, policyName string, policy api.SAMEnrollmentPolicySpec, rrSetName string) (api.Resource, error) {
	ref := strings.TrimSpace(policy.RRNodeSetRef)
	if ref == "" {
		return api.Resource{}, fmt.Errorf("SAMEnrollmentPolicy/%s spec.rrNodeSetRef is required to project %s", policyName, rrSetName)
	}
	nodeSet, found, err := api.LookupSAMNodeSet(router, ref, "SAMEnrollmentPolicy/"+policyName+" spec.rrNodeSetRef")
	if err != nil {
		return api.Resource{}, err
	}
	if !found {
		return api.Resource{}, fmt.Errorf("SAMEnrollmentPolicy/%s spec.rrNodeSetRef references missing %s", policyName, ref)
	}
	nodes := make([]api.SAMNodeSpec, 0, len(nodeSet.Nodes))
	for _, node := range nodeSet.Nodes {
		if !node.RouteReflector {
			continue
		}
		nodes = append(nodes, cloneSAMRRSetNode(node))
	}
	if len(nodes) == 0 {
		return api.Resource{}, fmt.Errorf("SAMEnrollmentPolicy/%s spec.rrNodeSetRef %s has no route-reflector nodes", policyName, ref)
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: rrSetName},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/" + policyName,
			Nodes:               nodes,
		},
	}, nil
}

func projectSAMEnrollmentDirectPeerGroupForEnrollment(router *api.Router, claim api.SAMEnrollmentClaimSpec, policy api.SAMEnrollmentPolicySpec, now time.Time) (api.Resource, error) {
	peerGroupName, err := samEnrollmentDirectPeerGroupName(policy.DirectMesh.PeerGroupRef)
	if err != nil {
		return api.Resource{}, err
	}
	transport, found, err := findSAMTransportProfile(router, policy.TransportProfileRef)
	if err != nil {
		return api.Resource{}, err
	}
	if !found {
		return api.Resource{}, fmt.Errorf("%s references missing %s", claim.PolicyRef, policy.TransportProfileRef)
	}
	active, err := samenrollment.ActiveClaims(router.Spec.Resources, claim.PolicyRef, policy, now.UTC())
	if err != nil {
		return api.Resource{}, err
	}
	directTopology, _ := samenrollment.ActiveDirectMeshTopology(active, policy, claim.LeafID, strings.EqualFold(strings.TrimSpace(transport.Encryption), "wireguard"))
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: peerGroupName},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  strings.TrimSpace(claim.PolicyRef),
			TransportFingerprint: mobilityconfig.SAMTransportMeshFingerprint(transport),
			Nodes:                directTopology.Nodes.Nodes,
			OwnedPrefixesByNode:  directTopology.OwnedPrefixesByNode,
		},
	}, nil
}

func samEnrollmentDirectPeerGroupName(ref string) (string, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMPeerGroup" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("spec.directMesh.peerGroupRef must reference SAMPeerGroup/<name>")
	}
	return strings.TrimSpace(name), nil
}

func findSAMTransportProfile(router *api.Router, ref string) (api.SAMTransportProfileSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMTransportProfile" || strings.TrimSpace(name) == "" {
		return api.SAMTransportProfileSpec{}, false, fmt.Errorf("transportProfileRef must reference SAMTransportProfile/<name>")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMTransportProfileSpec()
		if err != nil {
			return api.SAMTransportProfileSpec{}, true, err
		}
		return spec, true, nil
	}
	return api.SAMTransportProfileSpec{}, false, nil
}

func cloneSAMRRSetNode(node api.SAMNodeSpec) api.SAMNodeSpec {
	clone := node
	clone.MACAddresses = append([]string(nil), node.MACAddresses...)
	clone.WireGuard.AllowedIPs = append([]string(nil), node.WireGuard.AllowedIPs...)
	return clone
}

func submittedSAMEnrollmentClaimPolicy(router *api.Router, store samEnrollmentClaimStore, replaceSource string, claim api.SAMEnrollmentClaimSpec, now time.Time) (api.SAMEnrollmentPolicySpec, error) {
	policyName, err := samEnrollmentPolicyNameFromRef(claim.PolicyRef)
	if err != nil {
		return api.SAMEnrollmentPolicySpec{}, err
	}
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		return api.SAMEnrollmentPolicySpec{}, err
	}
	parts, err := samEnrollmentDynamicPartsFromRecords(records, replaceSource)
	if err != nil {
		return api.SAMEnrollmentPolicySpec{}, err
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		return api.SAMEnrollmentPolicySpec{}, err
	}
	effective, _, err := dynamicconfig.BuildEffectiveConfig(*router, parts, policies, now.UTC())
	if err != nil {
		return api.SAMEnrollmentPolicySpec{}, err
	}
	policy, ok, err := findSAMEnrollmentPolicy(&effective, policyName)
	if err != nil {
		return api.SAMEnrollmentPolicySpec{}, err
	}
	if !ok {
		return api.SAMEnrollmentPolicySpec{}, fmt.Errorf("%s not found", claim.PolicyRef)
	}
	return policy, nil
}

func validateSubmittedSAMEnrollmentClaimJoinToken(resourceID string, policy api.SAMEnrollmentPolicySpec, claim api.SAMEnrollmentClaimSpec) error {
	if strings.TrimSpace(policy.JoinTokenFrom.File) == "" && strings.TrimSpace(policy.JoinTokenFrom.Env) == "" {
		return nil
	}
	if strings.TrimSpace(claim.JoinNonce) == "" {
		return fmt.Errorf("%s spec.joinNonce is required by %s joinTokenFrom", resourceID, claim.PolicyRef)
	}
	if strings.TrimSpace(claim.JoinTimestamp) == "" {
		return fmt.Errorf("%s spec.joinTimestamp is required by %s joinTokenFrom", resourceID, claim.PolicyRef)
	}
	if strings.TrimSpace(claim.JoinHMAC) == "" {
		return fmt.Errorf("%s spec.joinHMAC is required by %s joinTokenFrom", resourceID, claim.PolicyRef)
	}
	secret, err := readSubmittedSAMEnrollmentJoinSecret(policy.JoinTokenFrom)
	if err != nil {
		return fmt.Errorf("%s spec.policyRef joinTokenFrom: %w", resourceID, err)
	}
	want := samenrollment.JoinHMAC(secret, claim)
	if !hmac.Equal([]byte(want), []byte(strings.TrimSpace(claim.JoinHMAC))) {
		return fmt.Errorf("%s spec.joinHMAC does not match %s joinTokenFrom", resourceID, claim.PolicyRef)
	}
	return nil
}

func readSubmittedSAMEnrollmentJoinSecret(source api.SecretValueSourceSpec) ([]byte, error) {
	var value string
	switch {
	case strings.TrimSpace(source.File) != "":
		data, err := os.ReadFile(strings.TrimSpace(source.File))
		if err != nil {
			return nil, err
		}
		value = string(data)
	case strings.TrimSpace(source.Env) != "":
		found, ok := os.LookupEnv(strings.TrimSpace(source.Env))
		if !ok {
			return nil, fmt.Errorf("environment variable %s is not set", strings.TrimSpace(source.Env))
		}
		value = found
	default:
		return nil, fmt.Errorf("secret source is not configured")
	}
	value = strings.TrimSpace(value)
	if source.Base64 {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, err
		}
		return decoded, nil
	}
	return []byte(value), nil
}

func submittedSAMEnrollmentClaimExpiresAt(claim api.SAMEnrollmentClaimSpec, policy api.SAMEnrollmentPolicySpec, now time.Time) (time.Time, error) {
	if strings.TrimSpace(claim.ExpiresAt) != "" {
		if expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(claim.ExpiresAt)); err == nil {
			return expiresAt.UTC(), nil
		}
		if expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(claim.ExpiresAt)); err == nil {
			return expiresAt.UTC(), nil
		}
		return time.Time{}, fmt.Errorf("spec.expiresAt must be an RFC3339 timestamp")
	}
	if strings.TrimSpace(policy.TTL) != "" {
		ttl, err := time.ParseDuration(strings.TrimSpace(policy.TTL))
		if err != nil {
			return time.Time{}, fmt.Errorf("%s spec.ttl must be a duration: %w", claim.PolicyRef, err)
		}
		return now.UTC().Add(ttl), nil
	}
	return now.UTC().Add(mobility.DefaultLeaseTTL), nil
}

func validateSubmittedSAMEnrollmentClaim(router *api.Router, store samEnrollmentClaimStore, replaceSource string, part dynamicconfig.DynamicConfigPart, now time.Time) error {
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		return err
	}
	parts, err := samEnrollmentDynamicPartsFromRecords(records, replaceSource)
	if err != nil {
		return err
	}
	parts = append(parts, part)
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		return err
	}
	_, _, err = dynamicconfig.BuildEffectiveConfig(*router, parts, policies, now.UTC())
	if err != nil {
		return err
	}
	return nil
}

func samEnrollmentDynamicPartsFromRecords(records []routerstate.DynamicConfigPartRecord, skipSource string) ([]dynamicconfig.DynamicConfigPart, error) {
	kept := make([]routerstate.DynamicConfigPartRecord, 0, len(records))
	for _, record := range records {
		if record.Source == skipSource {
			continue
		}
		kept = append(kept, record)
	}
	return codec.DecodeAll(kept)
}

func digestSAMEnrollmentClaimPart(part dynamicconfig.DynamicConfigPart) string {
	data, err := json.Marshal(part.Spec)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
