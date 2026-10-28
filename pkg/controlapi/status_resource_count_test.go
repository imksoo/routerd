// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// This regression exercises both observation paths without host commands or
// network mutations. The count describes configured resources, while the
// effective graph must still omit role-gated resources and their dependents.
func TestStatusResourceCountAcrossObservationPaths(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "status-count"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 1053, Sources: []string{"default"}}}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"}, Metadata: api.ObjectMeta{Name: "conditional"}, Spec: api.DNSUpstreamSpec{Protocol: "udp", Address: "192.0.2.53", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"test-role": {Equals: "master"}}}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.DNSForwarderSpec{Resolver: "local", Match: []string{"."}, Upstreams: []string{"conditional"}}},
		}},
	}
	engine := &apply.Engine{Command: func(string, ...string) ([]byte, error) { return nil, nil }}
	startup, err := engine.Observe(router)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"backup", "master"} {
		t.Run(role, func(t *testing.T) {
			store := routerstate.NewMemory()
			store.Set("test-role", role, "fixture")
			effective := resourcequery.FilterRouterByWhen(router, store)
			wantEffective := 3
			if role == "backup" {
				wantEffective = 1
			}
			if len(effective.Spec.Resources) != wantEffective {
				t.Fatalf("effective resources = %d, want %d", len(effective.Spec.Resources), wantEffective)
			}
			applied, err := engine.ObserveEffective(effective, router)
			if err != nil {
				t.Fatal(err)
			}
			if len(applied.Resources) != wantEffective {
				t.Fatalf("observed effective resources = %d, want %d", len(applied.Resources), wantEffective)
			}
			for name, result := range map[string]*apply.Result{"startup": startup, "immediate-apply": applied} {
				got := NewStatus(result, router)
				if got.Status.ResourceCount != 3 {
					t.Errorf("%s: resourceCount = %d, want 3 declared resources", name, got.Status.ResourceCount)
				}
				if got.Status.Phase != result.Phase || got.Status.Generation != result.Generation || !got.Status.LastApplyTime.Equal(result.Timestamp) {
					t.Errorf("%s: observation metadata changed", name)
				}
			}
		})
	}
}

// Observation absence and failed attempts do not erase configured resources;
// synthetic plan entries do not add configured resources either.
func TestStatusResourceCountUsesCurrentDeclaration(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: make([]api.Resource, 2)}}
	for _, tt := range []struct {
		name   string
		result *apply.Result
		phase  string
	}{
		{name: "before-observation", phase: "Unknown"},
		{name: "failed-apply", result: &apply.Result{Phase: "Error", Generation: 12}, phase: "Error"},
		{name: "plan-with-derived-resource", result: &apply.Result{Phase: "Healthy", Resources: make([]apply.ResourceResult, 3)}, phase: "Healthy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status := NewStatus(tt.result, router)
			if status.Status.ResourceCount != 2 {
				t.Errorf("resourceCount = %d, want 2", status.Status.ResourceCount)
			}
			if status.Status.Phase != tt.phase {
				t.Errorf("phase = %q, want %q", status.Status.Phase, tt.phase)
			}
			if tt.result != nil && status.Status.Generation != tt.result.Generation {
				t.Error("generation changed")
			}
		})
	}
	previous := &apply.Result{Phase: "Healthy", Resources: make([]apply.ResourceResult, 2)}
	for _, count := range []int{1, 4, 0} {
		next := &api.Router{Spec: api.RouterSpec{Resources: make([]api.Resource, count)}}
		if got := NewStatus(previous, next).Status.ResourceCount; got != count {
			t.Errorf("changed configuration: resourceCount = %d, want %d", got, count)
		}
	}
	if got := NewStatus(previous, nil).Status.ResourceCount; got != 0 {
		t.Errorf("absent configuration: resourceCount = %d, want 0", got)
	}
}
