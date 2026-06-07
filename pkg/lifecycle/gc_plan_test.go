// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPlanArtifactOrphansUsesDesiredLedgerAndHostInventory(t *testing.T) {
	owner := api.NetAPIVersion + "/PPPoESession/old"
	desired := []resource.Artifact{{
		Kind:  "file",
		Name:  "/etc/ppp/peers/routerd-keep",
		Owner: api.NetAPIVersion + "/PPPoESession/keep",
	}}
	ledger := []resource.Artifact{
		{
			Kind:  "file",
			Name:  "/etc/ppp/peers/routerd-keep",
			Owner: api.NetAPIVersion + "/PPPoESession/keep",
		},
		{
			Kind:  "file",
			Name:  "/etc/ppp/peers/routerd-old",
			Owner: owner,
		},
		{
			Kind:  "file",
			Name:  "/etc/ppp/chap-secrets",
			Owner: owner,
		},
		{
			Kind:  "unix.socket",
			Name:  "/run/routerd/pppoe-client/missing.sock",
			Owner: owner,
		},
	}
	host := []resource.Artifact{
		{
			Kind:  "file",
			Name:  "/etc/ppp/peers/routerd-old",
			Owner: owner,
		},
		{
			Kind:  "file",
			Name:  "/etc/ppp/chap-secrets",
			Owner: owner,
		},
	}
	plan := PlanArtifactOrphans(GCPlanInput{DesiredArtifacts: desired, LedgerArtifacts: ledger, HostArtifacts: host})
	if len(plan.ArtifactRemovals) != 1 {
		t.Fatalf("artifact removals = %+v, want one cleanup-eligible host-backed orphan", plan.ArtifactRemovals)
	}
	removal := plan.ArtifactRemovals[0]
	if removal.Artifact.Name != "/etc/ppp/peers/routerd-old" || removal.Remediation != "delete file /etc/ppp/peers/routerd-old" {
		t.Fatalf("removal = %+v", removal)
	}
	if got := actionTypes(plan.Actions); !reflect.DeepEqual(got, []GCActionType{GCActionRemoveArtifact, GCActionForgetLedger}) {
		t.Fatalf("actions = %#v", got)
	}
}

func TestPlanDeleteTargetGCKeepsLedgerForgetDryRunnable(t *testing.T) {
	owner := api.NetAPIVersion + "/DHCPv6PrefixDelegation/wan-pd"
	other := api.NetAPIVersion + "/DHCPv6PrefixDelegation/other"
	plan := PlanDeleteTargetGC(GCPlanInput{
		LedgerArtifacts: []resource.Artifact{
			{Kind: "file", Name: "/tmp/routerd-wan", Owner: owner},
			{Kind: "file", Name: "/tmp/routerd-other", Owner: other},
		},
		TargetOwnerIDs: map[string]bool{owner: true},
	})
	if len(plan.ArtifactRemovals) != 1 || plan.ArtifactRemovals[0].Artifact.Name != "/tmp/routerd-wan" {
		t.Fatalf("artifact removals = %+v, want only target owner", plan.ArtifactRemovals)
	}
	if len(plan.LedgerForgets) != 1 || plan.LedgerForgets[0].Name != "/tmp/routerd-wan" {
		t.Fatalf("ledger forgets = %+v, want only target owner", plan.LedgerForgets)
	}
	if got := actionTypes(plan.Actions); !reflect.DeepEqual(got, []GCActionType{GCActionRemoveArtifact, GCActionForgetLedger}) {
		t.Fatalf("actions = %#v", got)
	}
}

func TestPlanStatusGCUsesDesiredSetAndSyntheticAllowlist(t *testing.T) {
	statuses := []routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "wan"},
		{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode", Name: "old"},
		{APIVersion: api.NetAPIVersion, Kind: "ConntrackObserver", Name: "default"},
		{APIVersion: api.RouterAPIVersion, Kind: "Inventory", Name: "host"},
		{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface", Name: "legacy"},
		{APIVersion: api.NetAPIVersion, Kind: "Bridge", Name: "old-br"},
	}
	desired := map[string]bool{api.NetAPIVersion + "/Interface/wan": true}
	plan := PlanStatusGC(desired, statuses)
	got := objectStatusIDs(plan.StatusDeletes)
	want := []string{
		api.NetAPIVersion + "/Bridge/old-br",
		api.NetAPIVersion + "/PPPoEInterface/legacy",
		api.NetAPIVersion + "/TailscaleNode/old",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status deletes = %#v, want %#v", got, want)
	}
	if !plan.BackupRequired {
		t.Fatal("BackupRequired = false, want true")
	}
	if got := actionTypes(plan.Actions); !reflect.DeepEqual(got, []GCActionType{GCActionBackupState, GCActionDeleteStatus, GCActionDeleteStatus, GCActionDeleteStatus, GCActionRecordEvent}) {
		t.Fatalf("actions = %#v", got)
	}
}

func actionTypes(actions []GCAction) []GCActionType {
	out := make([]GCActionType, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.Type)
	}
	return out
}

func objectStatusIDs(statuses []routerstate.ObjectStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, ObjectStatusID(status))
	}
	return out
}
