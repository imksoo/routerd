// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestObserveHealthBindingRejectsConflictingPaths(t *testing.T) {
	check := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "check"}, Spec: api.HealthCheckSpec{Target: "192.0.2.1"}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		check,
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "paths"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{
			{Interface: "wan-a", Mark: 256, HealthCheck: "check"},
			{Interface: "wan-b", Mark: 256, HealthCheck: "check"},
		}}},
	}}}
	result := ResourceResult{Observed: map[string]string{}}
	(&Engine{}).observeHealthCheck(router, check, nil, nil, true, &result)
	if result.Phase != "Blocked" || len(result.Plan) != 0 || !strings.Contains(strings.Join(result.Warnings, " "), "conflicting source interfaces") {
		t.Fatalf("conflicting paths produced a runnable probe plan: %+v", result)
	}
}
