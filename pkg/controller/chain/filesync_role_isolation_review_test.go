// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	vrrpcontroller "github.com/imksoo/routerd/pkg/controller/vrrp"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Native command results are fixtures; the production VRRP observer, condition
// filter, lease role projection and actual local copy subprocess are exercised.
// No host network or service command is executed.
func TestLeaseSyncObservedRoleSurvivesUnrelatedDisabledVRRP(t *testing.T) {
	for _, unrelated := range []bool{false, true} {
		t.Run(fmt.Sprintf("unrelated_disabled_vrrp=%t", unrelated), func(t *testing.T) {
			dir := t.TempDir()
			lease, copied := filepath.Join(dir, "leases"), filepath.Join(dir, "received")
			content := []byte("fixture lease\n")
			if err := os.WriteFile(lease, content, 0600); err != nil {
				t.Fatal(err)
			}
			command := filepath.Join(dir, "copy")
			if err := os.WriteFile(command, []byte("#!/bin/sh\nexec cp -- "+fileSyncShellQuote(lease)+" "+fileSyncShellQuote(copied)+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			store, err := routerstate.OpenSQLite(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.Set("unused-enabled", "no", "fixture"); err != nil {
				t.Fatal(err)
			}
			declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "active"}, Spec: api.VirtualAddressSpec{Interface: "lan", Address: "192.0.2.1/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Priority: 200}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "unused.invalid"}}, Command: command, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/active.status.role": {Equals: "master"}}}}},
			}}}
			if unrelated {
				declared.Spec.Resources = append(declared.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "unused"}, Spec: api.VirtualAddressSpec{Interface: "lan", Address: "192.0.2.2/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 19, Priority: 200}, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"unused-enabled": {Equals: "yes"}}}}})
			}
			selected, err := resourcequery.FilterRouterByResolvedWhen(declared, store)
			if err != nil {
				t.Fatal(err)
			}
			native := vrrpcontroller.Controller{Router: selected, Store: store, OperatingSystem: platform.OSLinux, ConfigPath: filepath.Join(dir, "keepalived.conf"), IP: "ip", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ip" && strings.Join(args, " ") == "-4 addr show dev ens18" {
					return []byte("2: ens18 inet 192.0.2.1/32 scope global ens18\n"), nil
				}
				return []byte("ok"), nil
			}}
			var proof atomic.Bool
			if err := reconcileLeaseSyncVRRPRoles(t.Context(), &native, &proof, declared); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "VirtualAddress", "active")
			if err != nil || status["role"] != "master" {
				t.Fatalf("active native observation: %v %v", status, err)
			}
			selected, err = resourcequery.FilterRouterByResolvedWhen(declared, store)
			if err != nil {
				t.Fatal(err)
			}
			current := FileSyncController{Router: selected, Store: leaseSyncRoleStore{eventedStore: eventedStore{Store: store, Router: declared}, rolesObserved: &proof}}
			if err := current.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(copied)
			if err != nil || string(got) != string(content) {
				status, _ := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "leases")
				t.Fatalf("observed active MASTER lost independent lease sync: copy=%q err=%v roleProof=%t status=%v", got, err, proof.Load(), status)
			}
		})
	}
}
