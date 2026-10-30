// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestVRFForeignParentCannotAcquireStaticAddress(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.VRFSpec{IfName: "vrf-foreign", RouteTable: 101}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/24"}},
	}}}
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: router, Store: store,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observed := staticAddressSnapshot{Index: 31, Scope: "vrf-fixture-kernel"}
			if present {
				observed.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observed, nil
		},
		Command: func(context.Context, string, ...string) error { writes++; present = true; return nil },
	}
	err = child.Reconcile(t.Context())
	if writes != 0 || present {
		t.Fatalf("unacquired VRF parent admitted a child address: writes=%d err=%v", writes, err)
	}
}
