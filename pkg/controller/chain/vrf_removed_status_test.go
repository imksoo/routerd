// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"github.com/imksoo/routerd/pkg/api"
	"testing"
)

func TestVRFRemovedStatusClearedAfterWithdrawal(t *testing.T) {
	owner, native := vrfTestOwner(t)
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner.Router = &api.Router{}
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.links["vrf101"].Exists {
		t.Fatal("native VRF not withdrawn")
	}
	status, err := owner.Store.ReadObjectStatus(api.NetAPIVersion, "VRF", "tenant")
	if err != nil || status != nil {
		t.Fatalf("removed VRF pollutes empty router health: %+v %v", status, err)
	}
}

func TestVRFRemovedTerminalStatusRecovery(t *testing.T) {
	owner, native := vrfTestOwner(t)
	owner.Router = &api.Router{}
	if err := owner.Store.SaveObjectStatus(api.NetAPIVersion, "VRF", "tenant", map[string]any{"phase": "Absent", "reason": "OwnedGenerationWithdrawn"}); err != nil {
		t.Fatal(err)
	}
	owner.DryRun = true
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := owner.Store.ReadObjectStatus(api.NetAPIVersion, "VRF", "tenant")
	if err != nil || status == nil {
		t.Fatalf("dry run changed terminal status: %+v %v", status, err)
	}
	owner.DryRun = false
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err = owner.Store.ReadObjectStatus(api.NetAPIVersion, "VRF", "tenant")
	if err != nil || status != nil || len(native.ops) != 0 {
		t.Fatalf("terminal recovery failed or mutated kernel: %+v %v ops=%v", status, err, native.ops)
	}
}

func TestVRFInactiveDeclarationRetainsTerminalStatus(t *testing.T) {
	owner, _ := vrfTestOwner(t)
	owner.DeclaredRouter = owner.Router
	owner.Router = &api.Router{}
	if err := owner.Store.SaveObjectStatus(api.NetAPIVersion, "VRF", "tenant", map[string]any{"phase": "Absent", "reason": "OwnedGenerationWithdrawn"}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := owner.Store.ReadObjectStatus(api.NetAPIVersion, "VRF", "tenant")
	if err != nil || status == nil {
		t.Fatalf("inactive declared resource lost status: %+v %v", status, err)
	}
}
