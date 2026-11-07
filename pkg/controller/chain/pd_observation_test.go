// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestTypedPDObservationDrivesLANAndReleaseWithdrawal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0", ClientDUID: "01", IAID: "7"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-address"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "1", AddressSuffix: "::1"}},
	}}}
	if _, _, err := store.ConfigurePDLease("wan-pd", routerstate.PDLeaseConfig{Client: "routerd", UplinkIfname: "wan0", ClientDUID: "01", IAID: 7}); err != nil {
		t.Fatal(err)
	}
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: "wan0", ClientDUID: "01", IAID: 7, State: pdclient.StateBound, CurrentPrefix: "2001:db8:1200::/60", UpdatedAt: now, LeaseUpdatedAt: now, Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1200::/60", AcquiredAt: now, Preferred: 3600, Valid: 7200}, {Prefix: "2001:db8:1240::/60", AcquiredAt: now, Preferred: 0, Valid: 1800}}}
	if err := store.SavePDLeaseSnapshot("wan-pd", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "lan", map[string]any{"phase": "Up", "ifname": "lo"}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected address delete failure")
	failDelete := false
	controller := LANAddressController{Router: router, DeclaredRouter: router, Store: store, Now: func() time.Time { return now },
		Command: func(_ context.Context, _ string, args ...string) error {
			line := strings.Join(args, " ")
			if failDelete && (strings.Contains(line, "del") || strings.Contains(line, "-alias")) {
				return failure
			}
			return nil
		}}
	present := installLANIOFixture(&controller)
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if status["address"] != "2001:db8:1200:1::1/64" || status["phase"] != "Applied" || len(previousDelegatedAddressesFromStatus(status)) != 1 {
		t.Fatalf("typed observation did not drive LAN: %+v", status)
	}
	if len(store.Variables()) != 0 {
		t.Fatal("PD observation was carried as encoded state variable")
	}
	snapshot.State = pdclient.StateReleased
	snapshot.CurrentPrefix = ""
	snapshot.Prefixes = nil
	snapshot.UpdatedAt = now.Add(time.Second)
	if err := store.SavePDLeaseSnapshot("wan-pd", snapshot); err != nil {
		t.Fatal(err)
	}
	failDelete = true
	if err := controller.reconcile(context.Background(), "wan-pd"); !errors.Is(err, failure) {
		t.Fatalf("withdraw failure was hidden: %v", err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, readErr := delegatedReceipt(status)
	if readErr != nil || receipt == nil || !receipt.has("2001:db8:1200:1::1/64") || status["phase"] != "Pending" || status["reason"] != "AddressWithdrawing" {
		t.Fatalf("failed withdrawal lost exact ownership: %+v err=%v", status, readErr)
	}
	failDelete = false
	if err := controller.reconcile(t.Context(), "wan-pd"); err == nil {
		t.Fatal("pending present native tuple was adopted after failed withdrawal")
	}
	delete(present, "2001:db8:1200:1::1/64") // explicit external absence resolves the fence.
	if err := controller.reconcile(context.Background(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if status["phase"] != "Pending" || status["address"] != nil || status["reason"] != "PDLeaseStale" {
		t.Fatalf("Release did not finish withdrawal: %+v", status)
	}
	if phase := store.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd")["phase"]; phase != daemonapi.ResourcePhaseIdle {
		t.Fatalf("Release status still claimed bound: %v", phase)
	}
}
