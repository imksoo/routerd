// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type GCActionType string

const (
	GCActionBackupState      GCActionType = "backupState"
	GCActionTeardownResource GCActionType = "teardownResource"
	GCActionDeleteStatus     GCActionType = "deleteStatus"
	GCActionRecordEvent      GCActionType = "recordEvent"
)

type GCPlan struct {
	ResourceTeardowns []routerstate.ObjectStatus
	StatusDeletes     []routerstate.ObjectStatus
	Actions           []GCAction
	BackupRequired    bool
}

type GCAction struct {
	Type        GCActionType
	Status      routerstate.ObjectStatus
	Reason      string
	Remediation string
	Label       string
}

func PlanResourceTeardownGC(desired map[string]bool, statuses []routerstate.ObjectStatus) GCPlan {
	var teardowns []routerstate.ObjectStatus
	for _, status := range statuses {
		if !resourceLifecycleObjectStatus(status) {
			continue
		}
		if desired[ObjectStatusID(status)] {
			continue
		}
		teardowns = append(teardowns, status)
	}
	sort.Slice(teardowns, func(i, j int) bool {
		return ObjectStatusID(teardowns[i]) < ObjectStatusID(teardowns[j])
	})
	plan := GCPlan{ResourceTeardowns: teardowns}
	if len(teardowns) > 0 {
		plan.BackupRequired = true
		plan.Actions = append(plan.Actions, GCAction{Type: GCActionBackupState, Reason: "resource teardown cleanup requires a state backup before deletion"})
		for _, status := range teardowns {
			plan.Actions = append(plan.Actions, GCAction{
				Type:   GCActionTeardownResource,
				Status: status,
				Label:  ObjectStatusID(status),
				Reason: "resource status is no longer desired and requires resource-specific teardown",
			})
		}
		plan.Actions = append(plan.Actions, GCAction{Type: GCActionRecordEvent, Reason: "ResourceTeardownCleanup"})
	}
	return plan
}

func PlanStatusGC(desired map[string]bool, statuses []routerstate.ObjectStatus) GCPlan {
	var deletes []routerstate.ObjectStatus
	for _, status := range statuses {
		if api.IsRemovedLegacyKind(status.Kind) {
			deletes = append(deletes, status)
			continue
		}
		if SyntheticObjectStatus(status) {
			continue
		}
		if !ConfigResourceObjectStatus(status) {
			continue
		}
		if resourceLifecycleObjectStatus(status) {
			continue
		}
		if desired[ObjectStatusID(status)] {
			continue
		}
		deletes = append(deletes, status)
	}
	sort.Slice(deletes, func(i, j int) bool {
		return ObjectStatusID(deletes[i]) < ObjectStatusID(deletes[j])
	})
	plan := GCPlan{StatusDeletes: deletes}
	if len(deletes) > 0 {
		plan.BackupRequired = true
		plan.Actions = append(plan.Actions, GCAction{Type: GCActionBackupState, Reason: "stale object status cleanup requires a state backup before deletion"})
		for _, status := range deletes {
			plan.Actions = append(plan.Actions, GCAction{Type: GCActionDeleteStatus, Status: status, Label: ObjectStatusID(status)})
		}
		plan.Actions = append(plan.Actions, GCAction{Type: GCActionRecordEvent, Reason: "StaleStateCleanup"})
	}
	return plan
}

func SyntheticObjectStatus(status routerstate.ObjectStatus) bool {
	if status.APIVersion == api.RouterAPIVersion {
		return true
	}
	switch status.Kind {
	case "ConntrackObserver", "ConntrackTuning":
		return true
	default:
		return false
	}
}

func ConfigResourceObjectStatus(status routerstate.ObjectStatus) bool {
	if strings.TrimSpace(status.Kind) == "" || strings.TrimSpace(status.Name) == "" {
		return false
	}
	if status.APIVersion == api.RouterAPIVersion {
		return false
	}
	apiVersion := APIVersionForKind(status.Kind)
	if apiVersion == "" {
		apiVersion = legacyStatusAPIVersionForKind(status.Kind)
	}
	return status.APIVersion == apiVersion
}

func resourceLifecycleObjectStatus(status routerstate.ObjectStatus) bool {
	if status.APIVersion == api.HybridAPIVersion && status.Kind == "TunnelInterface" {
		return true
	}
	switch {
	case status.APIVersion == api.NetAPIVersion && (status.Kind == "IPv4Route" || status.Kind == "IPv4StaticAddress" || status.Kind == "WireGuardInterface" || status.Kind == "WireGuardPeer"):
		return true
	case status.APIVersion == api.SystemAPIVersion && status.Kind == "Sysctl" && strings.HasPrefix(status.Name, "sam-proxy-arp-"):
		return true
	default:
		return false
	}
}

func ObjectStatusID(status routerstate.ObjectStatus) string {
	return ResourceKey(status.APIVersion, status.Kind, status.Name)
}

func legacyStatusAPIVersionForKind(kind string) string {
	switch kind {
	case "Bridge", "VXLANSegment", "IPv4StaticRoute", "IPv6StaticRoute":
		return api.NetAPIVersion
	case "ServiceUnit":
		return api.SystemAPIVersion
	default:
		return ""
	}
}
