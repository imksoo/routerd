// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
	state "github.com/imksoo/routerd/pkg/state"
)

func TestRAConflictWithdrawsOldIntentAndKeepsIndependentInterface(t *testing.T) {
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resource := func(name, iface, prefix string, mflag bool) api.Resource {
		return api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.IPv6RouterAdvertisementSpec{Interface: iface, Prefix: prefix, MFlag: mflag}}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource("old", "ra-bad", "2001:db8:1::/64", false), resource("good", "ra-good", "2001:db8:2::/64", false)}}}
	sockets := map[string]*raCapturedSocket{"ra-bad": {}, "ra-good": {}}
	service := NewRouterAdvertisementService(t.Context(), false)
	service.scope = func(context.Context) (string, error) { return "ra-test-scope", nil }
	service.open = func(ifname string) (ra.Endpoint, error) {
		return ra.Endpoint{Conn: sockets[ifname], Source: netip.MustParseAddr("fe80::1"), Index: 7, MTU: 1500}, nil
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err := service.Reconcile(t.Context(), router, router, store); err != nil {
		t.Fatal(err)
	}
	changed := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource("new", "ra-bad", "2001:db8:3::/64", false), resource("conflict", "ra-bad", "2001:db8:4::/64", true), router.Spec.Resources[1]}}}
	var conflict *raPlanConflicts
	if err := service.Reconcile(t.Context(), changed, changed, store); !errors.As(err, &conflict) {
		t.Fatalf("conflicting intent was not reported: %v", err)
	}
	service.mu.Lock()
	bad := append([]byte(nil), sockets["ra-bad"].packets[len(sockets["ra-bad"].packets)-1]...)
	good := append([]byte(nil), sockets["ra-good"].packets[len(sockets["ra-good"].packets)-1]...)
	_, badActive := service.plans["ra-bad"]
	_, goodActive := service.plans["ra-good"]
	service.mu.Unlock()
	prefixes := raWirePrefixes(t, bad)
	if badActive || !goodActive || len(prefixes) != 1 || prefixes["2001:db8:1::/64"] != (raWirePrefix{}) || binary.BigEndian.Uint16(bad[6:8]) != 0 {
		t.Fatalf("old intent not exactly withdrawn: active=%t/%t prefixes=%+v header=%x", badActive, goodActive, prefixes, bad[:16])
	}
	if raWirePrefixes(t, good)["2001:db8:2::/64"].valid == 0 {
		t.Fatal("independent interface withdrawn by unrelated conflict")
	}
	journal, err := readRAJournal(store)
	if err != nil {
		t.Fatal(err)
	}
	if journal["ra-bad"].Interface != "" || journal["ra-good"].Interface == "" {
		t.Fatalf("incorrect withdrawal receipt commit: %+v", journal)
	}
	for _, name := range []string{"new", "conflict"} {
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", name)
		if err != nil || status["phase"] != "Error" || status["reason"] != "AdvertisementFailed" {
			t.Fatalf("conflict result missing %s: %+v %v", name, status, err)
		}
	}
	if status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "old"); err != nil || status != nil {
		t.Fatalf("removed result retained: %+v %v", status, err)
	}
}
