// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

const (
	PhaseDisabled      = "Disabled"
	PhaseStandby       = "Standby"
	PhaseNotApplicable = "NotApplicable"
)

func healthCheckDisabled(spec api.HealthCheckSpec) bool {
	return !api.BoolDefault(spec.Enabled, true)
}

func pppoeSessionDisabled(spec api.PPPoESessionSpec) bool {
	return !api.BoolDefault(spec.Enabled, true)
}

func dsliteTunnelDisabled(spec api.DSLiteTunnelSpec) bool {
	return !api.BoolDefault(spec.Enabled, true)
}

func egressRoutePolicyCandidateDisabled(candidate api.EgressRoutePolicyCandidate) bool {
	return !api.BoolDefault(candidate.Enabled, true)
}

func dependencyUnavailablePhase(router *api.Router, store Store, dependencies []api.ResourceDependencySpec, standby bool) string {
	for _, dependency := range dependencies {
		kind, name, ok := resourcequery.SplitResource(dependency.Resource)
		if !ok {
			continue
		}
		phase := fmt.Sprint(store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)["phase"])
		if neutralDependencyPhase(phase) || specDisabled(router, kind, name) {
			if standby {
				return PhaseStandby
			}
			return PhaseNotApplicable
		}
	}
	if standby {
		return PhaseStandby
	}
	return "Pending"
}

func neutralDependencyPhase(phase string) bool {
	switch strings.TrimSpace(phase) {
	case PhaseDisabled, PhaseStandby, PhaseNotApplicable:
		return true
	default:
		return false
	}
}

func specDisabled(router *api.Router, kind string, name string) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != kind || resource.Metadata.Name != name {
			continue
		}
		switch kind {
		case "HealthCheck":
			spec, err := resource.HealthCheckSpec()
			return err == nil && healthCheckDisabled(spec)
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			return err == nil && pppoeSessionDisabled(spec)
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			return err == nil && dsliteTunnelDisabled(spec)
		default:
			return false
		}
	}
	return false
}

func standbyHealthcheckRoute(name string, spec api.IPv4RouteSpec) bool {
	if strings.Contains(strings.ToLower(name), "healthcheck") {
		return true
	}
	for _, dependency := range spec.DependsOn {
		if strings.Contains(strings.ToLower(dependency.Resource), "healthcheck") {
			return true
		}
	}
	return false
}
