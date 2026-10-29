// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

var runCleanupCommand = runLogged

func filterRouterByWhen(router *api.Router, store resourcequery.StateStore) (*api.Router, error) {
	return resourcequery.FilterRouterByWhen(router, store)
}

func appendPrefixDelegationStateWarnings(result *apply.Result, router *api.Router, store resourcequery.StateStore) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		reader, ok := store.(routerstate.PDLeaseReader)
		if !ok {
			result.Warnings = append(result.Warnings, res.ID()+": PD observation store unavailable")
			continue
		}
		lease, _, err := reader.ReadPDLease(res.Metadata.Name)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: read PD observation: %v", res.ID(), err))
			continue
		}
		lease = lease.Live(store.Now())
		if lease.CurrentPrefix != "" {
			continue
		}
		msg := fmt.Sprintf("%s is not currently observable", res.ID())
		if lease.LastPrefix != "" {
			msg += "; last delegated prefix was " + lease.LastPrefix
		} else {
			msg += "; no delegated prefix has been recorded locally yet"
		}
		if !lease.LastObservedAt.IsZero() {
			msg += " observed at " + lease.LastObservedAt.Format(time.RFC3339)
		}
		msg += ". The OS DHCPv6 client must renew or reacquire PD before the upstream lease expires."
		result.Warnings = append(result.Warnings, msg)
	}
}

func appendLedgerOwnedOrphans(result *apply.Result, effective, declared *api.Router, ledgerPath string, transient bool) error {
	if ledgerPath == "" {
		return nil
	}
	ledger, err := loadPlanLedger(ledgerPath, transient)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	engine := apply.New()
	orphans, _, err := engine.LedgerOwnedOrphansEffective(effective, declared, ledger)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	result.Orphans = appendUniqueOrphans(result.Orphans, orphans)
	result.Warnings = append(result.Warnings, fmt.Sprintf("%d ledger-owned orphaned artifacts found", len(orphans)))
	if result.Phase == "Healthy" {
		result.Phase = "Drifted"
	}
	return nil
}

func loadPlanLedger(path string, transient bool) (resource.Ledger, error) {
	if !transient {
		return resource.LoadLedger(path)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return resource.NewLedger(), nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return resource.NewLedger(), nil
	} else if err != nil {
		return nil, err
	}
	return resource.OpenSQLiteLedgerReadOnly(path)
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

func cleanupLedgerOwnedOrphans(router *api.Router, ledgerPath string) ([]string, error) {
	return cleanupLedgerOwnedOrphansMatching(router, ledgerPath, func(resource.Artifact) bool { return true })
}

func cleanupLedgerOwnedOrphansMatching(router *api.Router, ledgerPath string, match func(resource.Artifact) bool) ([]string, error) {
	if ledgerPath == "" {
		return nil, nil
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ledger.Close() }()
	engine := apply.New()
	plan, err := engine.LedgerOwnedOrphanPlan(router, ledger)
	if err != nil {
		return nil, err
	}
	var removed []string
	var removedArtifacts []resource.Artifact
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionRemoveArtifact {
			continue
		}
		artifact := action.Artifact
		if match != nil && !match(artifact) {
			continue
		}
		label, err := cleanupLedgerOwnedArtifact(artifact)
		if err != nil {
			return removed, err
		}
		if label == "" {
			continue
		}
		removed = append(removed, label)
		removedArtifacts = append(removedArtifacts, artifact)
	}
	if len(removedArtifacts) > 0 {
		if err := ledger.Forget(removedArtifacts); err != nil {
			return removed, err
		}

	}
	return removed, nil
}

func cleanupLedgerOwnedArtifact(artifact resource.Artifact) (string, error) {
	return lifecycle.CleanupArtifact(routerdArtifactTeardownExecutor{}, artifact)
}

type routerdArtifactTeardownExecutor struct{}

func (routerdArtifactTeardownExecutor) Features() platform.Features {
	return platformFeatures
}

func (routerdArtifactTeardownExecutor) Run(name string, args ...string) error {
	return runCleanupCommand(name, args...)
}

func (routerdArtifactTeardownExecutor) Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (routerdArtifactTeardownExecutor) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (routerdArtifactTeardownExecutor) DeleteIPv4FwmarkRule(priority, mark, table int) error {
	return deleteIPv4FwmarkRule(ipv4FwmarkRule{Priority: priority, Mark: mark, Table: table})
}

func (routerdArtifactTeardownExecutor) FlushIPv4RouteTable(table int) error {
	return flushIPv4RouteTable(table)
}

func recordLastAppliedPath(router *api.Router, store resourcequery.StateStore, path string) error {
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
