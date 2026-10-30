// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// A worker snapshot may finish after its configured probe has changed. Keep the
// sample for diagnostics, but only the current configured probe can publish a
// current phase for dependency predicates and routing.
func (c DaemonStatusController) currentHealthSnapshot(observed daemonapi.ResourceStatus, now time.Time) (daemonapi.ResourceStatus, error) {
	current := false
	if c.Router != nil && observed.Resource.APIVersion == api.NetAPIVersion {
		for _, resource := range c.Router.Spec.Resources {
			if resource.APIVersion != api.NetAPIVersion || resource.Kind != "HealthCheck" || resource.Metadata.Name != observed.Resource.Name {
				continue
			}
			spec, err := resource.HealthCheckSpec()
			if err != nil {
				return observed, err
			}
			if !api.BoolDefault(spec.Enabled, true) {
				break
			}
			stateStore, _ := c.Store.(resourcequery.StateStore)
			eligible, err := resourcequery.ResourceWhenMatches(spec.When, stateStore)
			if err != nil {
				return observed, err
			}
			if !eligible {
				break
			}
			resolved, err := healthcheck.ResolveSpecWithStoreForResource(c.Router, c.Store, resource.Metadata.Name, spec)
			if err != nil {
				return observed, err
			}
			current = observed.Routing != nil && observed.Phase == observed.Routing.Phase && observed.Routing.Probe == resolved.Probe() && observed.Routing.Current(now)
			break
		}
	}
	if !current {
		observed.Phase = healthcheck.PhaseUnknown
		observed.Health = daemonapi.HealthUnknown
		observed.Conditions = []daemonapi.Condition{{Type: "Healthy", Status: daemonapi.ConditionUnknown, Reason: "ProbeObservationUnknown"}}
	}
	return observed, nil
}
