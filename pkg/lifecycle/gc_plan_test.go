// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

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
