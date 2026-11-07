// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"github.com/imksoo/routerd/pkg/api"
	"testing"
)

func TestVRFRebootRecovery(t *testing.T) {
	for _, mode := range []string{"recreate", "removed", "foreign", "same-boot-namespace", "dry-run"} {
		t.Run(mode, func(t *testing.T) {
			owner, native := vrfTestOwner(t)
			native.scope = "linux:old-boot:100"
			if err := owner.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			old := vrfOneReceipt(t, owner)
			native.scope = "linux:new-boot:200"
			delete(native.links, "vrf101")
			port := native.links["vrf-port"]
			port.Master = 0
			native.links["vrf-port"] = port
			native.ops = nil
			switch mode {
			case "removed":
				owner.Router = &api.Router{}
			case "foreign":
				native.links["vrf101"] = vrfLink{Exists: true, Name: "vrf101", Kind: "vrf", Index: 99, Table: 101, Token: "foreign"}
			case "same-boot-namespace":
				native.scope = "linux:old-boot:200"
			case "dry-run":
				owner.DryRun = true
			}
			err := owner.Reconcile(t.Context())
			if mode == "foreign" || mode == "same-boot-namespace" {
				if err == nil || len(native.ops) != 0 {
					t.Fatalf("unsafe recovery: err=%v ops=%v", err, native.ops)
				}
				if mode == "same-boot-namespace" && !sameVRFReceipt(old, vrfOneReceipt(t, owner)) {
					t.Fatal("same-boot receipt retired")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "dry-run" {
				if len(native.ops) != 0 || !sameVRFReceipt(old, vrfOneReceipt(t, owner)) {
					t.Fatal("dry run changed ownership")
				}
				return
			}
			if mode == "removed" {
				records, err := owner.readReceipts()
				if err != nil || len(records) != 0 {
					t.Fatalf("old receipt retained: %v %v", records, err)
				}
				status, err := owner.Store.ReadObjectStatus(api.NetAPIVersion, "VRF", "tenant")
				if err != nil || status != nil {
					t.Fatalf("removed health remains: %v %v", status, err)
				}
				return
			}
			fresh := vrfOneReceipt(t, owner)
			if fresh.Scope != native.scope || fresh.Token == old.Token || !native.links["vrf101"].Up || native.links["vrf-port"].Master != fresh.Index {
				t.Fatalf("VRF not reacquired: %+v", fresh)
			}
			if err := owner.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
