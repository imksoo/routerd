// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const maxDaemonProbeAttempts = 8

func daemonEgressProbe(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, target string) ([]controlapi.ProbeCheck, error) {
	return serveProbeChecks(ctx, router, store, "egress", target)
}

func daemonEgressProbeWithCurrent(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, target string, current probeCurrentRouter) ([]controlapi.ProbeCheck, error) {
	checks, err := resourcePhaseProbe(router, store, "EgressRoutePolicy", target)
	if err != nil {
		return nil, err
	}
	if router == nil {
		return checks, nil
	}
	attempts := 0
	selected, _ := selectProbeResources(router.Spec.Resources, "EgressRoutePolicy", target)
	for _, resource := range selected {
		policy, err := resource.EgressRoutePolicySpec()
		if err != nil {
			return nil, err
		}
		for index, candidate := range policy.Candidates {
			if index >= 32 {
				return append(checks, unobservedProbe("remaining egress candidates", "candidate preparation limit reached; remaining candidates are unprobed"), egressProbeScope()), nil
			}
			if candidate.Enabled != nil && !*candidate.Enabled {
				continue
			}
			if resourcequery.ResourceWhenPresent(candidate.When) {
				if store == nil {
					checks = append(checks, unobservedProbe(resource.ID()+" candidate/"+candidate.Name, "candidate activation is unobserved"))
					continue
				}
				active, err := resourcequery.ResourceWhenMatches(candidate.When, store)
				if err != nil {
					return nil, err
				}
				if !active {
					checks = append(checks, unobservedProbe(resource.ID()+" candidate/"+candidate.Name, "candidate activation is not currently resolved true"))
					continue
				}
			}
			found := false
			for index := -1; index < len(candidate.Targets); index++ {
				if attempts >= maxDaemonProbeAttempts || len(checks) >= 24 || ctx.Err() != nil {
					if ctx.Err() == context.Canceled {
						return nil, ctx.Err()
					}
					return append(checks, unobservedProbe("remaining egress targets", "probe attempt or observation limit reached; remaining configured targets are unprobed"), egressProbeScope()), nil
				}
				ref := candidate.HealthCheck
				if index >= 0 {
					ref = candidate.Targets[index].HealthCheck
				}
				if ref == "" {
					continue
				}
				found = true
				name := resource.Kind + "/" + resource.Metadata.Name + " candidate/" + candidate.Name + " HealthCheck/" + ref
				if attempts >= maxDaemonProbeAttempts {
					checks = append(checks, unobservedProbe(name, "probe attempt limit reached"))
					continue
				}
				if ctx.Err() != nil {
					checks = append(checks, unobservedProbe(name, "request budget exhausted before this attempt"))
					continue
				}
				var declared *api.Resource
				for i := range router.Spec.Resources {
					if router.Spec.Resources[i].APIVersion == api.NetAPIVersion && router.Spec.Resources[i].Kind == "HealthCheck" && router.Spec.Resources[i].Metadata.Name == ref {
						declared = &router.Spec.Resources[i]
						break
					}
				}
				if declared == nil {
					checks = append(checks, unobservedProbe(name, "configured HealthCheck resource is unavailable"))
					continue
				}
				spec, err := declared.HealthCheckSpec()
				if err != nil {
					return nil, err
				}
				if spec.Enabled != nil && !*spec.Enabled {
					checks = append(checks, unobservedProbe(name, "configured HealthCheck is disabled"))
					continue
				}
				if strings.TrimSpace(spec.Target) == "" || spec.TargetSource != "" && spec.TargetSource != "static" && spec.TargetSource != "auto" {
					checks = append(checks, unobservedProbe(name, "no current literal/static probe target; dynamic target resolution is unavailable"))
					continue
				}
				spec, err = healthcheck.ResolveSpecWithStoreForResource(router, store, ref, spec)
				if err != nil {
					return nil, err
				}
				if spec.SourceInterface != "" {
					if _, err := net.InterfaceByName(spec.SourceInterface); err != nil {
						checks = append(checks, unobservedProbe(name, "native source interface cannot be observed: "+err.Error()))
						continue
					}
				}
				attempts++
				eligible, err := daemonEgressAttemptCurrent(ctx, current, store, resource.Metadata.Name, candidate.Name, index, ref, spec)
				if err != nil {
					return nil, err
				}
				if !eligible {
					return append(checks, unobservedProbe(name, "current egress target or source changed before this attempt"), egressProbeScope()), nil
				}
				timeout := 2 * time.Second
				if configured, err := time.ParseDuration(spec.Timeout); err == nil && configured > 0 && configured < timeout {
					timeout = configured
				}
				attemptCtx, cancel := context.WithTimeout(ctx, timeout)
				started := time.Now().UTC()
				result := healthcheck.Probe(attemptCtx, spec)
				cancel()
				if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
					return nil, ctx.Err()
				}
				actualTarget := spec.Target
				if spec.Probe().Protocol != "icmp" {
					actualTarget = net.JoinHostPort(spec.Target, fmt.Sprint(spec.Probe().Port))
				}
				check := controlapi.ProbeCheck{Name: name, Kind: "communication", Protocol: spec.Probe().Protocol, Target: actualTarget, ObservedAt: started, DurationMilliseconds: time.Since(started).Milliseconds(), FailureKind: result.FailureKind, BindingInterface: spec.SourceInterface, BindingMark: spec.FwMark, Status: "fail", Detail: result.Message}
				if ctx.Err() == nil {
					eligible, err = daemonEgressAttemptCurrent(ctx, current, store, resource.Metadata.Name, candidate.Name, index, ref, spec)
					if err != nil {
						return nil, err
					}
					if !eligible {
						check.Status, check.FailureKind, check.Detail = "warn", "stale_source", "unobserved: current egress target or source changed during this exchange; remaining attempts are unprobed"
						return append(checks, check, egressProbeScope()), nil
					}
				}
				if result.OK {
					check.Status = "pass"
				} else if result.FailureKind == healthcheck.FailureKindPermission || result.FailureKind == healthcheck.FailureKindOther {
					check.Status = "warn"
					check.Detail = "unobserved: probe could not establish communication: " + result.Message
				}
				checks = append(checks, check)
			}
			if !found {
				checks = append(checks, unobservedProbe(resource.ID()+" candidate/"+candidate.Name, "candidate has no configured HealthCheck target"))
			}
		}
	}
	checks = append(checks, egressProbeScope())
	return checks, nil
}

func unobservedProbe(name, detail string) controlapi.ProbeCheck {
	return controlapi.ProbeCheck{Name: name, Status: "warn", Kind: "scope", Detail: "unobserved: " + detail}
}

func egressProbeScope() controlapi.ProbeCheck {
	return controlapi.ProbeCheck{Name: "probe scope", Status: "warn", Kind: "scope", Detail: "router-originated configured HealthCheck attempts do not establish forwarded client traffic, NAT or failover"}
}
