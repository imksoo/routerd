// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestReferencesDSLiteTunnel(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "dslite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ip6tnl-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{{Interface: "dslite-a", HealthCheck: "dslite-health"}, {Interface: "wan", HealthCheck: "wan-health"}}}}}},
	}}}
	if !ReferencesDSLiteTunnel(router, "dslite-health") {
		t.Fatal("DS-Lite target was not detected")
	}
	if ReferencesDSLiteTunnel(router, "wan-health") {
		t.Fatal("non-DS-Lite target was detected")
	}
}
