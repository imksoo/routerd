// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

func TestSupervisedRemovedStatusRequiresAcknowledgedStop(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, kind := range []string{"DHCPv6PrefixDelegation", "DNSResolver"} {
		if err := store.SaveObjectStatus(api.NetAPIVersion, kind, "removed", map[string]any{"phase": "Pending"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePDLease("removed", state.PDLease{LastPrefix: "2001:db8:1200::/60", Phase: "Expired"}); err != nil {
		t.Fatal(err)
	}
	recordLANReceiptForTest(t, store, "lan", "lan0", "removed", "2001:db8:1200::1/64")
	marker := supervisedDaemonMarker{Version: 1, Binary: "routerd-dhcpv6-client", Resource: "removed", SpecHash: "hash", OwnerToken: "owner"}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	oldProcesses := supervisedDaemonProcesses
	t.Cleanup(func() { supervisedDaemonProcesses = oldProcesses })
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
		return nil, errors.New("process observation unavailable")
	}
	r := Runner{Router: &api.Router{}, Store: store, Opts: Options{SuperviseClientDaemons: true, SuperviseDNSResolvers: true}}
	if err := r.reconcileSupervisedClientDaemons(t.Context(), r.Router, nil); err == nil {
		t.Fatal("unobserved stop acknowledged")
	}
	for _, kind := range []string{"DHCPv6PrefixDelegation", "DNSResolver"} {
		want := "Pending"
		if kind == "DHCPv6PrefixDelegation" {
			want = "Expired"
		}
		if status := store.ObjectStatus(api.NetAPIVersion, kind, "removed"); status["phase"] != want {
			t.Fatalf("%s failure hidden: %v", kind, status)
		}
	}
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) { return nil, nil }
	if err := r.reconcileSupervisedClientDaemons(t.Context(), r.Router, nil); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"DHCPv6PrefixDelegation", "DNSResolver"} {
		if status := store.ObjectStatus(api.NetAPIVersion, kind, "removed"); status != nil {
			t.Errorf("stopped removed %s retains status: %v", kind, status)
		}
	}
	if _, found, err := store.ReadPDLease("removed"); err != nil || found {
		t.Fatalf("removed canonical PD observation survived: found=%v err=%v", found, err)
	}
	if receipt, err := delegatedReceipt(store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan")); err != nil || receipt == nil {
		t.Fatalf("supervisor removed LAN controller's native ownership: %v, %v", receipt, err)
	}
}

func TestSupervisedDisabledDeclarationRetainsStatus(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	store := mapStore{}
	router := &api.Router{}
	for _, kind := range []string{"DHCPv6PrefixDelegation", "DNSResolver"} {
		router.Spec.Resources = append(router.Spec.Resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "disabled"},
		})
		if err := store.SaveObjectStatus(api.NetAPIVersion, kind, "disabled", map[string]any{"phase": "Pending", "reason": "WhenFalse"}); err != nil {
			t.Fatal(err)
		}
	}
	r := Runner{Router: router, Store: store, Opts: Options{SuperviseClientDaemons: true, SuperviseDNSResolvers: true}}
	if err := r.reconcileSupervisedClientDaemons(t.Context(), &api.Router{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"DHCPv6PrefixDelegation", "DNSResolver"} {
		if status := store.ObjectStatus(api.NetAPIVersion, kind, "disabled"); status["reason"] != "WhenFalse" {
			t.Errorf("disabled declaration %s lost status: %v", kind, status)
		}
	}
}
