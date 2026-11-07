// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNativeCARPRejectsForeignAliasSharingVHID(t *testing.T) {
	for _, ownedDesired := range []bool{false, true} {
		t.Run(fmt.Sprintf("desired-present-owned=%t", ownedDesired), func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			controller := Controller{Router: vrrpRouter("vrrp"), Store: store, OperatingSystem: platform.OSFreeBSD}
			if ownedDesired {
				seedOwnedVIP(t, &controller, "vip", "10.240.70.10/32", 50)
			}
			before, err := controller.readNativeVIPJournal()
			if err != nil {
				t.Fatal(err)
			}
			// Keep the desired alias before the foreign alias: finding our own
			// address must not end observation of the shared VHID's other members.
			observed := "ens18: flags=... mtu 1500\n"
			if ownedDesired {
				observed += "\tinet 10.240.70.10 netmask 0xffffffff vhid 50\n"
			}
			observed += "\tinet 10.240.70.11 netmask 0xffffffff vhid 50\n\tcarp: MASTER vhid 50 advbase 1 advskew 104\n"
			mutations := 0
			controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ifconfig" {
					if len(args) == 1 && args[0] == "ens18" {
						return []byte(observed), nil
					}
					mutations++
					return nil, fmt.Errorf("unexpected native mutation: %v", args)
				}
				if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
					return []byte("0\n"), nil
				}
				if name == "kldload" {
					return nil, nil
				}
				t.Fatalf("unexpected native command %s %v", name, args)
				return nil, nil
			}
			if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "unowned alias") {
				t.Fatalf("foreign shared VHID accepted: %v", err)
			}
			after, err := controller.readNativeVIPJournal()
			if err != nil || !reflect.DeepEqual(before, after) || mutations != 0 {
				t.Fatalf("foreign group changed ownership/effects: before=%+v after=%+v mutations=%d err=%v", before, after, mutations, err)
			}
		})
	}
}

func TestNativeCARPKeepsAndWithdrawsOwnedAliasesSharingVHID(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := vrrpRouter("vrrp")
	alias := router.Spec.Resources[1]
	alias.Metadata.Name = "alias"
	spec := alias.Spec.(api.VirtualAddressSpec)
	spec.Address = "10.240.70.11/32"
	alias.Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, alias)
	controller := Controller{Router: router, Store: store, OperatingSystem: platform.OSFreeBSD}
	first := nativeVIP{Kind: "VirtualAddress", Name: "vip", IfName: "ens18", Address: "10.240.70.10/32", Backend: "carp", VHID: 50}
	second := first
	second.Name, second.Address = "alias", "10.240.70.11/32"
	if err := controller.saveNativeVIPJournal(nativeVIPJournal{Applied: []nativeVIP{first}, Pending: []nativeVIP{second}}); err != nil {
		t.Fatal(err)
	}
	secondPresent, deletes := true, 0
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "ifconfig" {
			if len(args) == 1 && args[0] == "ens18" {
				out := "ens18: flags=... mtu 1500\n\tinet 10.240.70.10 netmask 0xffffffff vhid 50\n"
				if secondPresent {
					out += "\tinet 10.240.70.11 netmask 0xffffffff vhid 50\n"
				}
				return []byte(out + "\tcarp: MASTER vhid 50 advbase 1 advskew 104\n"), nil
			}
			if strings.Join(args, " ") == "ens18 inet 10.240.70.11/32 -alias" {
				secondPresent = false
				deletes++
				return nil, nil
			}
		}
		if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
			return []byte("0\n"), nil
		}
		if name == "kldload" {
			return nil, nil
		}
		t.Fatalf("unexpected native mutation %s %v", name, args)
		return nil, nil
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	journal, err := controller.readNativeVIPJournal()
	if err != nil || len(journal.Pending) != 0 || len(journal.Applied) != 2 || deletes != 0 {
		t.Fatalf("owned group recovery failed: %+v deletes=%d err=%v", journal, deletes, err)
	}
	controller.Router = &api.Router{Spec: api.RouterSpec{Resources: router.Spec.Resources[:2]}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	journal, err = controller.readNativeVIPJournal()
	if err != nil || len(journal.Pending) != 0 || len(journal.Applied) != 1 || !containsVIP(journal.Applied, first) || secondPresent || deletes != 1 {
		t.Fatalf("exact group member withdrawal failed: %+v deletes=%d secondPresent=%v err=%v", journal, deletes, secondPresent, err)
	}
}
