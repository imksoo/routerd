// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestValidateHealthBindingConflicts(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "health"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			testResource(api.NetAPIVersion, "Interface", "wan", api.InterfaceSpec{IfName: "wan0"}),
			testResource(api.NetAPIVersion, "Interface", "backup", api.InterfaceSpec{IfName: "wan1"}),
			testResource(api.NetAPIVersion, "HealthCheck", "check", api.HealthCheckSpec{Interface: "wan", Target: "192.0.2.1"}),
			testResource(api.NetAPIVersion, "EgressRoutePolicy", "policy", api.EgressRoutePolicySpec{Mode: "priority", Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "primary", Interface: "wan", GatewaySource: "none", Priority: 10, Table: 100, Mark: 256, HealthCheck: "check"},
			}}),
		}},
	}
	if err := ValidateForOS(router, platform.OSLinux); err != nil {
		t.Fatalf("consistent declared probe: %v", err)
	}
	policy, _ := router.Spec.Resources[3].EgressRoutePolicySpec()
	policy.Candidates = append(policy.Candidates, api.EgressRoutePolicyCandidate{Name: "backup", Interface: "backup", GatewaySource: "none", Priority: 20, Table: 101, Mark: 256, HealthCheck: "check"})
	router.Spec.Resources[3].Spec = policy
	if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "conflicting source interfaces") {
		t.Fatalf("conflicting declared path reached runtime: %v", err)
	}
	policy.Candidates[1].Interface = "wan"
	policy.Candidates[1].Mark = 257
	router.Spec.Resources[3].Spec = policy
	if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "conflicting marks") {
		t.Fatalf("conflicting declared mark reached runtime: %v", err)
	}
}
