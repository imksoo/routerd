// SPDX-License-Identifier: BSD-3-Clause

package codec

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFetchedSAMEnrollmentTopologyRecordPreservesFetcherPolicies(t *testing.T) {
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
	peerGroup := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "cloudedge-direct-leaves"},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  "SAMEnrollmentPolicy/cloudedge",
			TransportFingerprint: "sha256:mesh",
			Nodes: []api.SAMNodeSpec{{
				NodeRef:     "leaf-a",
				SAMEndpoint: "10.30.0.21",
			}},
		},
	}
	tests := []struct {
		name      string
		peerGroup *api.Resource
		options   FetchedSAMEnrollmentTopologyRecordOptions
		want      routerstate.DynamicConfigPartRecord
		wantPart  dynamicconfig.DynamicConfigPart
	}{
		{
			name:      "daemon topology",
			peerGroup: &peerGroup,
			options: FetchedSAMEnrollmentTopologyRecordOptions{
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
				ResourcesJSON:  `[{"apiVersion":"mobility.routerd.net/v1alpha1","kind":"SAMRRSet","metadata":{"name":"Cloud.Edge RR"},"spec":{"enrollmentPolicyRef":"SAMEnrollmentPolicy/cloudedge","nodes":[{"nodeRef":"rr-a","samEndpoint":"10.30.0.10","samEndpointFrom":{"resource":""},"wireGuard":{},"placement":{},"maintenance":{}}]}},{"apiVersion":"mobility.routerd.net/v1alpha1","kind":"SAMPeerGroup","metadata":{"name":"cloudedge-direct-leaves"},"spec":{"enrollmentPolicyRef":"SAMEnrollmentPolicy/cloudedge","transportFingerprint":"sha256:mesh","nodes":[{"nodeRef":"leaf-a","samEndpoint":"10.30.0.21","samEndpointFrom":{"resource":""},"wireGuard":{},"placement":{},"maintenance":{}}]}}]`,
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
					Resources:   []api.Resource{resource, peerGroup},
					Directives:  []dynamicconfig.DynamicConfigDirective{},
					ActionPlans: []dynamicconfig.ActionPlan{},
				},
			},
		},
		{
			name: "topology without direct peer group",
			options: FetchedSAMEnrollmentTopologyRecordOptions{
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
			got, err := FetchedSAMEnrollmentTopologyRecord(resource, tt.peerGroup, observedAt, time.Time{}, options)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("record = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestFetchedSAMEnrollmentTopologyRecordRejectsPeerGroupOutsideRRSetPolicy(t *testing.T) {
	rrSet := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "pve-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/pve-leaves",
			Nodes:               []api.SAMNodeSpec{{NodeRef: "rr-a", SAMEndpoint: "10.30.0.10"}},
		},
	}
	peerGroup := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "pve-direct-leaves"},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  "SAMEnrollmentPolicy/other-leaves",
			TransportFingerprint: "sha256:mesh",
		},
	}
	_, err := FetchedSAMEnrollmentTopologyRecord(rrSet, &peerGroup, time.Now(), time.Time{}, FetchedSAMEnrollmentTopologyRecordOptions{DefaultTTL: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("FetchedSAMEnrollmentTopologyRecord error = %v, want policy mismatch", err)
	}
}
