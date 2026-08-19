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
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const samEnrollmentClaimDynamicGeneration = int64(1)

type samEnrollmentClaimStore interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
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
	policy, err := submittedSAMEnrollmentClaimPolicy(router, store, source, claim, observedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
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

func getSAMRRSetForAcceptedClaim(router *api.Router, store samEnrollmentClaimStore, req controlapi.SAMRRSetGetRequest, now time.Time) (*controlapi.SAMRRSetGetResult, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: router config unavailable", controlapi.ErrBadRequest)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: state store unavailable", controlapi.ErrBadRequest)
	}
	rrSetName := strings.TrimSpace(req.Name)
	if rrSetName == "" {
		return nil, fmt.Errorf("%w: SAMRRSet name is required", controlapi.ErrBadRequest)
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
	if !hasActiveSubmittedSAMEnrollmentClaim(records, claimSource, now.UTC()) {
		return nil, fmt.Errorf("%w: accepted %s not found", controlapi.ErrBadRequest, claimSource)
	}
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
	rrSetResource, err := projectSAMRRSetForEnrollment(router, policyName, policy, rrSetName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", controlapi.ErrBadRequest, err)
	}
	result := controlapi.NewSAMRRSetGetResult(rrSetName, rrSetResource)
	return &result, nil
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

func hasActiveSubmittedSAMEnrollmentClaim(records []routerstate.DynamicConfigPartRecord, source string, now time.Time) bool {
	_, _, ok := activeSubmittedSAMEnrollmentClaimResource(records, source, now)
	return ok
}

func activeSubmittedSAMEnrollmentClaimResource(records []routerstate.DynamicConfigPartRecord, source string, now time.Time) (api.Resource, api.SAMEnrollmentClaimSpec, bool) {
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
			if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentClaim" {
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
