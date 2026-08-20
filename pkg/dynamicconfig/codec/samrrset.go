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

// FetchedSAMRRSetRecordOptions preserves the local persistence policy of a
// SAMRRSet fetcher. Different fetchers intentionally have different lease and
// digest policies, but must construct the same source, owner, and resource
// shape before persisting the result.
type FetchedSAMRRSetRecordOptions struct {
	Name                              string
	Generation                        int64
	DefaultTTL                        time.Duration
	IncludeEmptyDirectivesActionPlans bool
	Digest                            func(dynamicconfig.DynamicConfigPart) string
}

// FetchedSAMRRSetRecord validates and persists one fetched SAMRRSet as a
// dynamic configuration part. Callers provide their explicit expiry, name,
// and digest policy so daemon and routerctl fetching retain their existing
// refresh semantics.
func FetchedSAMRRSetRecord(resource api.Resource, observedAt, expiresAt time.Time, options FetchedSAMRRSetRecordOptions) (routerstate.DynamicConfigPartRecord, error) {
	if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMRRSet" || strings.TrimSpace(resource.Metadata.Name) == "" {
		return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched resource must be %s/SAMRRSet", api.MobilityAPIVersion)
	}
	if _, err := resource.SAMRRSetSpec(); err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
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
				Name:       resource.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     "SAMRRSet/" + resource.Metadata.Name,
			Generation: options.Generation,
			ObservedAt: observedAt.UTC(),
			ExpiresAt:  expiresAt.UTC(),
			Resources:  []api.Resource{resource},
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
