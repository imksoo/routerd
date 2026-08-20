// SPDX-License-Identifier: BSD-3-Clause

package codec

import (
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFetchedSAMRRSetRecordPreservesFetcherPolicies(t *testing.T) {
	observedAt := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.FixedZone("plus-nine", 9*60*60))
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "Cloud.Edge RR"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/cloudedge",
			Nodes: []api.SAMNodeSpec{{
				NodeRef:     "rr-a",
				SAMEndpoint: "10.30.0.10",
			}},
		},
	}
	tests := []struct {
		name     string
		options  FetchedSAMRRSetRecordOptions
		want     routerstate.DynamicConfigPartRecord
		wantPart dynamicconfig.DynamicConfigPart
	}{
		{
			name: "daemon",
			options: FetchedSAMRRSetRecordOptions{
				Name:                              "fetched-sam-rrset-cloud-edge-rr",
				Generation:                        1,
				DefaultTTL:                        5 * time.Minute,
				IncludeEmptyDirectivesActionPlans: true,
			},
			want: routerstate.DynamicConfigPartRecord{
				Source:         "SAMRRSet/Cloud.Edge RR",
				Generation:     1,
				ObservedAt:     observedAt.UTC(),
				ExpiresAt:      observedAt.Add(5 * time.Minute).UTC(),
				Digest:         "sha256:daemon",
				ResourcesJSON:  `[{"apiVersion":"mobility.routerd.net/v1alpha1","kind":"SAMRRSet","metadata":{"name":"Cloud.Edge RR"},"spec":{"enrollmentPolicyRef":"SAMEnrollmentPolicy/cloudedge","nodes":[{"nodeRef":"rr-a","samEndpoint":"10.30.0.10","samEndpointFrom":{"resource":""},"wireGuard":{},"placement":{},"maintenance":{}}]}}]`,
				DirectivesJSON: "[]",
				Status:         "active",
			},
			wantPart: dynamicconfig.DynamicConfigPart{
				TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
				Metadata: api.ObjectMeta{Name: "fetched-sam-rrset-cloud-edge-rr", OwnerRefs: []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet", Name: "Cloud.Edge RR"}}},
				Spec: dynamicconfig.DynamicConfigPartSpec{
					Source:      "SAMRRSet/Cloud.Edge RR",
					Generation:  1,
					ObservedAt:  observedAt.UTC(),
					ExpiresAt:   observedAt.Add(5 * time.Minute).UTC(),
					Resources:   []api.Resource{resource},
					Directives:  []dynamicconfig.DynamicConfigDirective{},
					ActionPlans: []dynamicconfig.ActionPlan{},
				},
			},
		},
		{
			name: "routerctl",
			options: FetchedSAMRRSetRecordOptions{
				Name:       "fetched-sam-rrset-Cloud.Edge RR",
				Generation: 1,
				DefaultTTL: 24 * time.Hour,
			},
			want: routerstate.DynamicConfigPartRecord{
				Source:         "SAMRRSet/Cloud.Edge RR",
				Generation:     1,
				ObservedAt:     observedAt.UTC(),
				ExpiresAt:      observedAt.Add(24 * time.Hour).UTC(),
				Digest:         "sha256:routerctl",
				ResourcesJSON:  `[{"apiVersion":"mobility.routerd.net/v1alpha1","kind":"SAMRRSet","metadata":{"name":"Cloud.Edge RR"},"spec":{"enrollmentPolicyRef":"SAMEnrollmentPolicy/cloudedge","nodes":[{"nodeRef":"rr-a","samEndpoint":"10.30.0.10","samEndpointFrom":{"resource":""},"wireGuard":{},"placement":{},"maintenance":{}}]}}]`,
				DirectivesJSON: "null",
				Status:         "active",
			},
			wantPart: dynamicconfig.DynamicConfigPart{
				TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
				Metadata: api.ObjectMeta{Name: "fetched-sam-rrset-Cloud.Edge RR", OwnerRefs: []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet", Name: "Cloud.Edge RR"}}},
				Spec: dynamicconfig.DynamicConfigPartSpec{
					Source:     "SAMRRSet/Cloud.Edge RR",
					Generation: 1,
					ObservedAt: observedAt.UTC(),
					ExpiresAt:  observedAt.Add(24 * time.Hour).UTC(),
					Resources:  []api.Resource{resource},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := tt.options
			options.Digest = func(part dynamicconfig.DynamicConfigPart) string {
				if !reflect.DeepEqual(part, tt.wantPart) {
					t.Fatalf("part = %#v, want %#v", part, tt.wantPart)
				}
				return tt.want.Digest
			}
			got, err := FetchedSAMRRSetRecord(resource, observedAt, time.Time{}, options)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("record = %#v, want %#v", got, tt.want)
			}
		})
	}
}
