// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import "github.com/imksoo/routerd/pkg/api"

// ReferencesDSLiteTunnel reports whether an EgressRoutePolicy sends the named
// health check through a declared DS-Lite tunnel.  Renderers and controllers
// share this classification so daemon probes cannot silently use management
// routing when their DS-Lite binding is absent.
func ReferencesDSLiteTunnel(router *api.Router, healthCheck string) bool {
	if router == nil || healthCheck == "" {
		return false
	}
	tunnels := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "DSLiteTunnel" {
			tunnels[resource.Metadata.Name] = true
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := resource.EgressRoutePolicySpec()
		if err != nil {
			continue
		}
		for _, candidate := range spec.Candidates {
			if candidate.HealthCheck == healthCheck && (tunnels[candidate.EffectiveInterface()] || tunnels[candidate.Name]) {
				return true
			}
			for _, target := range candidate.Targets {
				if target.HealthCheck == healthCheck && (tunnels[target.EffectiveInterface()] || tunnels[target.Name]) {
					return true
				}
			}
		}
	}
	return false
}
