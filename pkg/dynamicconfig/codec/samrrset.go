// SPDX-License-Identifier: BSD-3-Clause

package codec

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// FetchedSAMEnrollmentTopologyRecordOptions preserves the local persistence
// policy of an enrollment topology fetcher. Different fetchers intentionally
// have different lease and digest policies, but must construct the same
// source, owner, and resource shape before persisting the result.
type FetchedSAMEnrollmentTopologyRecordOptions struct {
	Name                              string
	Generation                        int64
	DefaultTTL                        time.Duration
	IncludeEmptyDirectivesActionPlans bool
	Digest                            func(dynamicconfig.DynamicConfigPart) string
}

// FetchedSAMEnrollmentTopologyRecord validates and persists one fetched
// enrollment topology as a single dynamic configuration part. The RR snapshot
// is always present; a policy-scoped direct peer group is optional. Keeping
// both resources in one part makes a refreshed direct topology an atomic
// replacement under the long-lived SAMRRSet/<name> source.
func FetchedSAMEnrollmentTopologyRecord(rrSet api.Resource, peerGroup *api.Resource, observedAt, expiresAt time.Time, options FetchedSAMEnrollmentTopologyRecordOptions) (routerstate.DynamicConfigPartRecord, error) {
	if rrSet.APIVersion != api.MobilityAPIVersion || rrSet.Kind != "SAMRRSet" || strings.TrimSpace(rrSet.Metadata.Name) == "" {
		return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched resource must be %s/SAMRRSet", api.MobilityAPIVersion)
	}
	rrSetSpec, err := rrSet.SAMRRSetSpec()
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	resources := []api.Resource{rrSet}
	if peerGroup != nil {
		if peerGroup.APIVersion != api.MobilityAPIVersion || peerGroup.Kind != "SAMPeerGroup" || strings.TrimSpace(peerGroup.Metadata.Name) == "" {
			return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched peer group must be %s/SAMPeerGroup", api.MobilityAPIVersion)
		}
		peerGroupSpec, err := peerGroup.SAMPeerGroupSpec()
		if err != nil {
			return routerstate.DynamicConfigPartRecord{}, err
		}
		if strings.TrimSpace(peerGroupSpec.EnrollmentPolicyRef) == "" {
			return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched SAMPeerGroup/%s must be enrollment-policy scoped", peerGroup.Metadata.Name)
		}
		if strings.TrimSpace(peerGroupSpec.TransportFingerprint) == "" {
			return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched SAMPeerGroup/%s transportFingerprint is required", peerGroup.Metadata.Name)
		}
		if strings.TrimSpace(peerGroupSpec.EnrollmentPolicyRef) != strings.TrimSpace(rrSetSpec.EnrollmentPolicyRef) {
			return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched SAMPeerGroup/%s enrollmentPolicyRef %q does not match SAMRRSet/%s enrollmentPolicyRef %q", peerGroup.Metadata.Name, peerGroupSpec.EnrollmentPolicyRef, rrSet.Metadata.Name, rrSetSpec.EnrollmentPolicyRef)
		}
		resources = append(resources, *peerGroup)
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if expiresAt.IsZero() {
		expiresAt = observedAt.Add(options.DefaultTTL)
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: options.Name,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "SAMRRSet",
				Name:       rrSet.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     "SAMRRSet/" + rrSet.Metadata.Name,
			Generation: options.Generation,
			ObservedAt: observedAt.UTC(),
			ExpiresAt:  expiresAt.UTC(),
			Resources:  resources,
		},
	}
	if options.IncludeEmptyDirectivesActionPlans {
		part.Spec.Directives = []dynamicconfig.DynamicConfigDirective{}
		part.Spec.ActionPlans = []dynamicconfig.ActionPlan{}
	}
	if options.Digest != nil {
		part.Spec.Digest = options.Digest(part)
	}
	return Encode(part)
}
