// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
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
	expiresAt := submittedSAMEnrollmentClaimExpiresAt(claim, observedAt)
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
	record, err := samEnrollmentClaimPartRecord(part)
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

func submittedSAMEnrollmentClaimExpiresAt(claim api.SAMEnrollmentClaimSpec, now time.Time) time.Time {
	if strings.TrimSpace(claim.ExpiresAt) != "" {
		if expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(claim.ExpiresAt)); err == nil {
			return expiresAt.UTC()
		}
		if expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(claim.ExpiresAt)); err == nil {
			return expiresAt.UTC()
		}
	}
	return now.UTC().Add(mobility.DefaultLeaseTTL)
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
	effective, _, err := dynamicconfig.BuildEffectiveConfig(*router, parts, policies, now.UTC())
	if err != nil {
		return err
	}
	return config.Validate(&effective)
}

func samEnrollmentDynamicPartsFromRecords(records []routerstate.DynamicConfigPartRecord, skipSource string) ([]dynamicconfig.DynamicConfigPart, error) {
	parts := make([]dynamicconfig.DynamicConfigPart, 0, len(records))
	for _, record := range records {
		if record.Source == skipSource {
			continue
		}
		var resources []api.Resource
		if strings.TrimSpace(record.ResourcesJSON) != "" {
			if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
				return nil, fmt.Errorf("%s generation %d resources: %w", record.Source, record.Generation, err)
			}
		}
		var directives []dynamicconfig.DynamicConfigDirective
		if strings.TrimSpace(record.DirectivesJSON) != "" {
			if err := json.Unmarshal([]byte(record.DirectivesJSON), &directives); err != nil {
				return nil, fmt.Errorf("%s generation %d directives: %w", record.Source, record.Generation, err)
			}
		}
		parts = append(parts, dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Metadata: api.ObjectMeta{
				Name: fmt.Sprintf("%s-%d", record.Source, record.Generation),
			},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     record.Source,
				Generation: record.Generation,
				ObservedAt: record.ObservedAt,
				ExpiresAt:  record.ExpiresAt,
				Digest:     record.Digest,
				Resources:  resources,
				Directives: directives,
			},
		})
	}
	return parts, nil
}

func samEnrollmentClaimPartRecord(part dynamicconfig.DynamicConfigPart) (routerstate.DynamicConfigPartRecord, error) {
	resources, err := json.Marshal(part.Spec.Resources)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	directives, err := json.Marshal(part.Spec.Directives)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	return routerstate.DynamicConfigPartRecord{
		Source:         part.Spec.Source,
		Generation:     part.Spec.Generation,
		ObservedAt:     part.Spec.ObservedAt,
		ExpiresAt:      part.Spec.ExpiresAt,
		Digest:         part.Spec.Digest,
		ResourcesJSON:  string(resources),
		DirectivesJSON: string(directives),
		Status:         "active",
	}, nil
}

func digestSAMEnrollmentClaimPart(part dynamicconfig.DynamicConfigPart) string {
	data, err := json.Marshal(part.Spec)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
