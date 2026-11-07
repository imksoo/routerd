// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/healthcheck"
)

// Fixtures carry the same typed sample as the real persistence boundary.
func fixtureHealthObservation(status map[string]any) (api.HealthObservation, error) {
	observation, _ := status["routing"].(api.HealthObservation)
	return observation, nil
}

func healthProbeStatusForTest(t *testing.T, router *api.Router, store Store, name, phase string, checkedAt time.Time, failures int) map[string]any {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "HealthCheck" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err != nil {
			t.Fatal(err)
		}
		bound, err := healthcheck.ResolveSpecWithStoreForResource(router, store, name, spec)
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"phase": phase, "routing": api.HealthObservation{Probe: bound.Probe(), Phase: phase, CheckedAt: checkedAt, ConsecutiveFailed: failures}}
	}
	t.Fatalf("HealthCheck/%s not declared", name)
	return nil
}

func (s *countingSAMStatusStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
func (s *dynamicRouteSAMStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
func (s *mobilityEffectStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
func (s *routeCleanupStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
func (s *syncMapStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
func (s mapStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
