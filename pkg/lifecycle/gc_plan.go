// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type GCActionType string

const (
	GCActionBackupState    GCActionType = "backupState"
	GCActionRemoveArtifact GCActionType = "removeArtifact"
	GCActionForgetLedger   GCActionType = "forgetLedger"
	GCActionDeleteStatus   GCActionType = "deleteStatus"
	GCActionRecordEvent    GCActionType = "recordEvent"
)

type GCPlanInput struct {
	DesiredArtifacts       []resource.Artifact
	LedgerArtifacts        []resource.Artifact
	HostArtifacts          []resource.Artifact
	ObjectStatuses         []routerstate.ObjectStatus
	DesiredObjectStatusIDs map[string]bool
	TargetOwnerIDs         map[string]bool
}

type GCPlan struct {
	ArtifactRemovals []GCArtifactRemoval
	LedgerForgets    []resource.Artifact
	StatusDeletes    []routerstate.ObjectStatus
	Actions          []GCAction
	BackupRequired   bool
}

type GCArtifactRemoval struct {
	Artifact    resource.Artifact
	Reason      string
	Remediation string
}

type GCAction struct {
	Type        GCActionType
	Artifact    resource.Artifact
	Status      routerstate.ObjectStatus
	Reason      string
	Remediation string
	Label       string
}

func PlanGC(input GCPlanInput) GCPlan {
	plan := PlanArtifactOrphans(input)
	statusPlan := PlanStatusGC(input.DesiredObjectStatusIDs, input.ObjectStatuses)
	plan.StatusDeletes = append(plan.StatusDeletes, statusPlan.StatusDeletes...)
	if len(statusPlan.StatusDeletes) > 0 {
		plan.BackupRequired = true
		plan.Actions = append(plan.Actions, GCAction{Type: GCActionBackupState, Reason: "stale object status cleanup requires a state backup before deletion"})
		for _, status := range statusPlan.StatusDeletes {
			plan.Actions = append(plan.Actions, GCAction{Type: GCActionDeleteStatus, Status: status, Label: ObjectStatusID(status)})
		}
		plan.Actions = append(plan.Actions, GCAction{Type: GCActionRecordEvent, Reason: "StaleStateCleanup"})
	}
	return plan
}

func PlanArtifactOrphans(input GCPlanInput) GCPlan {
	desiredIDs := map[string]bool{}
	for _, artifact := range input.DesiredArtifacts {
		for _, id := range gcArtifactIdentityAliases(artifact) {
			desiredIDs[id] = true
		}
	}
	actualByID := map[string]resource.Artifact{}
	for _, artifact := range input.HostArtifacts {
		for _, id := range gcArtifactIdentityAliases(artifact) {
			actualByID[id] = artifact
		}
	}
	var removals []GCArtifactRemoval
	var forgets []resource.Artifact
	seen := map[string]bool{}
	for _, owned := range input.LedgerArtifacts {
		id := owned.Identity()
		if seen[id] || desiredIDs[id] {
			continue
		}
		seen[id] = true
		actual, ok := actualByID[id]
		if !ok {
			continue
		}
		artifact := mergeArtifactAttributesForGC(owned, actual)
		if !ArtifactCleanupEligible(artifact) {
			continue
		}
		removals = append(removals, GCArtifactRemoval{
			Artifact:    artifact,
			Reason:      "local ownership ledger records this artifact but no current resource owns it",
			Remediation: ArtifactCleanupRemediation(artifact),
		})
		forgets = append(forgets, artifact)
	}
	sortArtifactRemovals(removals)
	sort.SliceStable(forgets, func(i, j int) bool {
		return artifactCleanupSortKey(forgets[i]) < artifactCleanupSortKey(forgets[j])
	})
	return planFromArtifactRemovals(removals, forgets)
}

func gcArtifactIdentityAliases(artifact resource.Artifact) []string {
	ids := []string{artifact.Identity()}
	if artifact.Kind == "nft.table" {
		if name := artifact.Attributes["name"]; name != "" {
			legacyID := artifact.Kind + "/" + name
			if legacyID != ids[0] {
				ids = append(ids, legacyID)
			}
		}
	}
	return ids
}

func PlanDeleteTargetGC(input GCPlanInput) GCPlan {
	if len(input.TargetOwnerIDs) == 0 {
		return GCPlan{}
	}
	var removals []GCArtifactRemoval
	var forgets []resource.Artifact
	for _, artifact := range input.LedgerArtifacts {
		if !input.TargetOwnerIDs[artifact.Owner] {
			continue
		}
		removals = append(removals, GCArtifactRemoval{
			Artifact:    artifact,
			Reason:      "resource delete requested removal of owned artifact",
			Remediation: ArtifactCleanupRemediation(artifact),
		})
		forgets = append(forgets, artifact)
	}
	sortArtifactRemovals(removals)
	sort.SliceStable(forgets, func(i, j int) bool {
		return artifactCleanupSortKey(forgets[i]) < artifactCleanupSortKey(forgets[j])
	})
	return planFromArtifactRemovals(removals, forgets)
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

func ObjectStatusID(status routerstate.ObjectStatus) string {
	return OwnerKey(status.APIVersion, status.Kind, status.Name)
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

func planFromArtifactRemovals(removals []GCArtifactRemoval, forgets []resource.Artifact) GCPlan {
	plan := GCPlan{ArtifactRemovals: removals, LedgerForgets: forgets}
	for _, removal := range removals {
		plan.Actions = append(plan.Actions, GCAction{
			Type:        GCActionRemoveArtifact,
			Artifact:    removal.Artifact,
			Reason:      removal.Reason,
			Remediation: removal.Remediation,
			Label:       LabelForArtifact(removal.Artifact),
		})
	}
	for _, artifact := range forgets {
		plan.Actions = append(plan.Actions, GCAction{
			Type:     GCActionForgetLedger,
			Artifact: artifact,
			Label:    LabelForArtifact(artifact),
		})
	}
	return plan
}

func sortArtifactRemovals(removals []GCArtifactRemoval) {
	sort.SliceStable(removals, func(i, j int) bool {
		return artifactCleanupSortKey(removals[i].Artifact) < artifactCleanupSortKey(removals[j].Artifact)
	})
}

func artifactCleanupSortKey(artifact resource.Artifact) string {
	return strings.Join([]string{
		paddedInt(ArtifactCleanupPriority(artifact)),
		artifact.Kind,
		artifact.Name,
		artifact.Owner,
	}, "\x00")
}

func paddedInt(value int) string {
	if value < 0 {
		value = 0
	}
	if value > 999999 {
		value = 999999
	}
	return string(rune('0'+(value/100000)%10)) +
		string(rune('0'+(value/10000)%10)) +
		string(rune('0'+(value/1000)%10)) +
		string(rune('0'+(value/100)%10)) +
		string(rune('0'+(value/10)%10)) +
		string(rune('0'+value%10))
}

func mergeArtifactAttributesForGC(owned, actual resource.Artifact) resource.Artifact {
	merged := owned
	attrs := map[string]string{}
	for key, value := range actual.Attributes {
		attrs[key] = value
	}
	for key, value := range owned.Attributes {
		attrs[key] = value
	}
	merged.Attributes = attrs
	return merged
}
