// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func filterRouterByWhen(router *api.Router, store routerstate.Store) *api.Router {
	return resourcequery.FilterRouterByWhen(router, store)
}

func appendPrefixDelegationStateWarnings(result *apply.Result, router *api.Router, store routerstate.Store) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		lease, _ := routerstate.PDLeaseFromStore(store, base)
		if lease.CurrentPrefix != "" {
			continue
		}
		msg := fmt.Sprintf("%s is not currently observable", res.ID())
		if lease.LastPrefix != "" {
			msg += "; last delegated prefix was " + lease.LastPrefix
		} else {
			msg += "; no delegated prefix has been recorded locally yet"
		}
		if lease.LastObservedAt != "" {
			msg += " observed at " + lease.LastObservedAt
		}
		msg += ". The OS DHCPv6 client must renew or reacquire PD before the upstream lease expires."
		result.Warnings = append(result.Warnings, msg)
	}
}

func appendUniqueOrphans(existing, additions []apply.OrphanedArtifact) []apply.OrphanedArtifact {
	seen := map[string]int{}
	for i, orphan := range existing {
		seen[orphan.Name+"/"+orphan.Remediation] = i
	}
	for _, orphan := range additions {
		id := orphan.Name + "/" + orphan.Remediation
		if index, ok := seen[id]; ok {
			if existing[index].Owner == "" && orphan.Owner != "" {
				existing[index] = orphan
			}
			continue
		}
		seen[id] = len(existing)
		existing = append(existing, orphan)
	}
	return existing
}

func recordLastAppliedPath(router *api.Router, store routerstate.Store, path string) error {
	if path == "" {
		return nil
	}
	applySourceStore, ok := store.(routerstate.ObjectApplySourceStore)
	if !ok {
		return nil
	}
	for _, res := range router.Spec.Resources {
		if err := applySourceStore.SaveObjectApplySource(res.APIVersion, res.Kind, res.Metadata.Name, path); err != nil {
			return err
		}
	}
	return nil
}

func controllerDefaultStatuses() []controlapi.ControllerStatus {
	names := []string{
		"address",
		"bgp",
		"dhcp-lease-sync",
		"dhcpv4client",
		"dhcpv6",
		"dns-resolver",
		"dslite",
		"firewall",
		"ingress",
		"kernel-module",
		"nat",
		"network-adoption",
		"package",
		"pppoesession",
		"route",
		"service-unit",
		"vrrp",
	}
	out := make([]controlapi.ControllerStatus, 0, len(names))
	for _, name := range names {
		out = append(out, controlapi.ControllerStatus{
			Name:          name,
			Mode:          "live",
			Reason:        controlapi.ControllerModeReasonLive,
			Message:       "controller is reconciling declared router state",
			ResourceKinds: controllerResourceKinds(name),
		})
	}
	return out
}
