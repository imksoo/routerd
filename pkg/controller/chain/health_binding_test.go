// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHealthBindingConflictPreventsUnitEffectAndMarkBootstrap(t *testing.T) {
	spec := api.HealthCheckSpec{Target: "192.0.2.1"}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "check"}, Spec: spec},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "paths"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{
			{Interface: "wan-a", Mark: 256, HealthCheck: "check"},
			{Interface: "wan-b", Mark: 256, HealthCheck: "check"},
		}}},
	}}}
	path := filepath.Join(t.TempDir(), "check.service")
	commands := 0
	c := SystemdUnitController{Router: router}
	if _, _, err := c.applyHealthCheckSystemdUnit(t.Context(), path, "check.service", "check", spec, nil, func(context.Context, string, ...string) ([]byte, error) {
		commands++
		return nil, nil
	}); err == nil {
		t.Fatal("ambiguous probe reached systemd activation")
	}
	if commands != 0 {
		t.Fatalf("ambiguous probe issued %d native commands", commands)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ambiguous probe wrote unit artifact: %v", err)
	}
	policy := IPv4PolicyRouteController{Router: router}
	if policy.healthCheckUsesFwMark("check", 256) {
		t.Fatal("conflicting probe authorized a marked bootstrap route")
	}
}
