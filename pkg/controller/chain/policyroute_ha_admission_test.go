// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHAPromotionPreservesReplicatedMarksUntilVIPPublication(t *testing.T) {
	now := time.Now().UTC()
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/gateway": {"role": "master", "vipPublicationConfirmed": false, "lastRoleTransitionAt": now.Format(time.RFC3339Nano)},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "primary"}, Spec: api.HealthCheckSpec{Target: "192.0.2.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.EgressRoutePolicySpec{
			Mode: "priority", SourceCIDRs: []string{"198.18.30.0/24"}, Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "primary", Interface: "lo", Mark: 0x110, Table: 110, Weight: 120, HealthCheck: "primary", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}},
				{Name: "fallback", Interface: "lo", Mark: 0x113, Table: 113, Weight: 80},
			},
		}},
	}}}
	store[api.NetAPIVersion+"/HealthCheck/primary"] = healthProbeStatusForTest(t, router, store, "primary", "Unhealthy", now, 3)
	c := IPv4PolicyRouteController{Router: router, DeclaredRouter: router, Store: store, Now: func() time.Time { return now }, CommandOutput: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
	for _, tc := range []struct {
		role      string
		published bool
		preserve  bool
	}{
		{"backup", false, true},
		{"master", false, true},
		{"master", true, false},
	} {
		store[api.NetAPIVersion+"/VirtualAddress/gateway"]["role"] = tc.role
		store[api.NetAPIVersion+"/VirtualAddress/gateway"]["vipPublicationConfirmed"] = tc.published
		path := filepath.Join(t.TempDir(), "default.nft")
		if err := c.applyDefaultRoutePolicies(t.Context(), "nft", path); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		preserved := strings.Contains(string(data), "ct mark { 0x110, 0x113 } meta mark set ct mark")
		if preserved != tc.preserve {
			t.Errorf("role=%s publication=%t replicated mark preserved=%t\n%s", tc.role, tc.published, preserved, data)
		}
		if !strings.Contains(string(data), "ct mark 0x0 meta mark set 0x113") {
			t.Fatal("new flows lost the available fallback")
		}
	}
}
