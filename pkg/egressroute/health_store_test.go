// SPDX-License-Identifier: BSD-3-Clause
package egressroute

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/healthcheck"
)

func (s mapStore) HealthObservation(name string) (api.HealthObservation, error) {
	observation, _ := s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name)["routing"].(api.HealthObservation)
	return observation, nil
}

func healthCheckResource(name string, spec api.HealthCheckSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     spec,
	}
}

func storeHealthObservation(t *testing.T, router *api.Router, store mapStore, name, phase string, checkedAt time.Time, failed int) {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "HealthCheck" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err != nil {
			t.Fatal(err)
		}
		spec, err = healthcheck.ResolveSpecWithStoreForResource(router, store, name, spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", name, map[string]any{
			"routing": api.HealthObservation{Probe: spec.Probe(), Phase: phase, CheckedAt: checkedAt, ConsecutiveFailed: failed},
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("HealthCheck/%s is not declared", name)
}
