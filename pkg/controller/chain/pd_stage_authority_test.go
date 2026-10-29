// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/pdclient"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPDStageFileCannotReviveCanonicalRelease(t *testing.T) {
	now := time.Now().UTC()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", ClientDUID: "01", PrefixLength: 56}},
	}}}
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: "wan0", ClientDUID: "01", IAID: 1, State: pdclient.StateBound, CurrentPrefix: "2001:db8:1::/56", UpdatedAt: now, Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1::/56", AcquiredAt: now, Preferred: 60, Valid: 120}}}
	path := filepath.Join(t.TempDir(), "lease.json")
	data, _ := json.Marshal(snapshot)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := LANAddressController{Router: router, DeclaredRouter: router, Store: store, Now: func() time.Time { return now }, PDLeaseSnapshotPath: func(string) string { return path }}
	if _, fresh := controller.freshStagedPDSnapshot("wan-pd"); !fresh {
		t.Fatal("unobserved valid peer snapshot must support cold staging")
	}
	released := snapshot
	released.State = pdclient.StateReleased
	released.CurrentPrefix = ""
	released.Prefixes = nil
	released.UpdatedAt = now.Add(time.Second)
	if err := store.SavePDLease("wan-pd", routerstate.ObservePDLease(routerstate.PDLease{}, released, now)); err != nil {
		t.Fatal(err)
	}
	if _, fresh := controller.freshStagedPDSnapshot("wan-pd"); fresh {
		t.Fatal("older bound file revived canonical Release")
	}
	snapshot.UpdatedAt = released.UpdatedAt
	data, _ = json.Marshal(snapshot)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, fresh := controller.freshStagedPDSnapshot("wan-pd"); fresh {
		t.Fatal("same-epoch conflicting file replaced canonical authority")
	}
	snapshot.UpdatedAt = released.UpdatedAt.Add(time.Second)
	data, _ = json.Marshal(snapshot)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, fresh := controller.freshStagedPDSnapshot("wan-pd"); !fresh {
		t.Fatal("newer valid peer acquisition must remain usable")
	}
}
