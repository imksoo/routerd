// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestInspectionCarriesTypedPDLeaseWithoutEncodedStatusTransfer(t *testing.T) {
	now := time.Now().UTC()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease := routerstate.PDLease{CurrentPrefix: "2001:db8:1::/56", LastPrefix: "2001:db8:1::/56", LastReplyAt: now, ValidSeconds: 120, Phase: "Bound"}
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0"}}}}}
	views, err := serveInspectionResources(router, store, "DHCPv6PrefixDelegation/wan-pd", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].PDLease == nil || views[0].PDLease.CurrentPrefix != lease.CurrentPrefix || views[0].Status["observation"] != nil || views[0].Status["currentPrefix"] != nil {
		t.Fatalf("inspection lost type or duplicated lease: %+v", views)
	}
	// This is the actual control API serialization boundary, not an internal clone.
	payload, err := json.Marshal(controlapi.NewDescribeResult("DHCPv6PrefixDelegation/wan-pd", views[0]))
	if err != nil {
		t.Fatal(err)
	}
	var received controlapi.DescribeResult
	if err := json.Unmarshal(payload, &received); err != nil {
		t.Fatal(err)
	}
	if received.Resource.PDLease == nil || received.Resource.PDLease.ValidSeconds != 120 || !received.Resource.PDLease.LastReplyAt.Equal(now) {
		t.Fatalf("control API lost typed lifetime: %+v", received.Resource)
	}
}

// The source changes after the real producer captures its lease, as can happen
// when a configuration operation races a slow OS/file observation.
type sourceSwitchingPDStore struct {
	*routerstate.SQLiteStore
	next     routerstate.PDLeaseConfig
	switched bool
}

func (s *sourceSwitchingPDStore) ReadPDLease(name string) (routerstate.PDLease, bool, error) {
	lease, found, err := s.SQLiteStore.ReadPDLease(name)
	if err == nil && !s.switched {
		s.switched = true
		_, _, err = s.SQLiteStore.ConfigurePDLease(name, s.next)
	}
	return lease, found, err
}

func TestActualPDObservationRejectsConcurrentSourceSwitch(t *testing.T) {
	now := time.Now().UTC()
	raw, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	config := routerstate.PDLeaseConfig{Client: api.IPv6PDClientRouterd, Profile: api.IPv6PDProfileDefault, UplinkIfname: "wan0", ClientDUID: "01", IAID: 7, PrefixLength: 56}
	lease, _, err := raw.ConfigurePDLease("wan-pd", config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: "wan0", ClientDUID: "01", IAID: 7, State: pdclient.StateBound, UpdatedAt: now, CurrentPrefix: "2001:db8:1::/56", Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1::/56", AcquiredAt: now, Preferred: 60, Valid: 120}}}
	lease = routerstate.ObservePDLease(lease, snapshot, now)
	if err := raw.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	oldDir := pdClientLeaseDir
	pdClientLeaseDir = t.TempDir()
	defer func() { pdClientLeaseDir = oldDir }()
	if err := os.MkdirAll(filepath.Join(pdClientLeaseDir, "wan-pd"), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdClientLeaseDir, "wan-pd", "lease.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0", ClientDUID: "01", IAID: "7", PrefixLength: 56}}}}}
	newer := config
	newer.Client = api.IPv6PDClientNetworkd
	store := &sourceSwitchingPDStore{SQLiteStore: raw, next: newer}
	if err := recordObservedPrefixDelegationState(router, store, nil); !errors.Is(err, routerstate.ErrStalePDLeaseSource) {
		t.Fatalf("stale real producer accepted: %v", err)
	}
	current, _, err := raw.ReadPDLease("wan-pd")
	if err != nil || current.Client != api.IPv6PDClientNetworkd || current.CurrentPrefix != "" {
		t.Fatalf("old observation revived under new source: %+v %v", current, err)
	}
	// The raw typed boundary also refuses the same in-flight captured lease.
	if err := raw.SavePDLease("wan-pd", lease); !errors.Is(err, routerstate.ErrStalePDLeaseSource) {
		t.Fatalf("raw source guard: %v", err)
	}
}

func TestActualPDObservationRejectsExternalClientBeforeReadingManagedFile(t *testing.T) {
	now := time.Now().UTC()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := routerstate.PDLeaseConfig{Client: api.IPv6PDClientRouterd, UplinkIfname: "wan0", ClientDUID: "01", IAID: 7}
	lease, _, err := store.ConfigurePDLease("wan-pd", config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: "wan0", ClientDUID: "01", IAID: 7, State: pdclient.StateBound, UpdatedAt: now, CurrentPrefix: "2001:db8:1::/56", Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1::/56", AcquiredAt: now, Preferred: 60, Valid: 120}}}
	if err := store.SavePDLease("wan-pd", routerstate.ObservePDLease(lease, snapshot, now)); err != nil {
		t.Fatal(err)
	}
	oldDir := pdClientLeaseDir
	pdClientLeaseDir = t.TempDir()
	defer func() { pdClientLeaseDir = oldDir }()
	if err := os.MkdirAll(filepath.Join(pdClientLeaseDir, "wan-pd"), 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	if err := os.WriteFile(filepath.Join(pdClientLeaseDir, "wan-pd", "lease.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0", Client: api.IPv6PDClientNetworkd, ClientDUID: "01", IAID: "7"}}}}}
	if err := recordObservedPrefixDelegationState(router, store, bus.NewWithStore(store)); err == nil {
		t.Fatal("unsupported external client was silently admitted")
	}
	current, _, err := store.ReadPDLease("wan-pd")
	if err != nil || current.CurrentPrefix != snapshot.CurrentPrefix || current.Client != api.IPv6PDClientRouterd {
		t.Fatalf("rejected config altered canonical authority: %+v %v", current, err)
	}
	events, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed"})
	if err != nil || len(events) != 0 {
		t.Fatalf("rejected config published transition: %+v %v", events, err)
	}
}
