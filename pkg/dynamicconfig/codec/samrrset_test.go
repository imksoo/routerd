// SPDX-License-Identifier: BSD-3-Clause

package codec

import (
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

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
