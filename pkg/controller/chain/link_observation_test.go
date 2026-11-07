// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestLinkObservationFailureRetainsPriorAddresses(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveObjectStatus(api.NetAPIVersion, "Interface", "lan", map[string]any{"phase": "Up", "primaryIPv4": "192.0.2.1/24"}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("native addresses unreadable")
	controller := LinkController{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "test0"}}}}}, Store: db,
		Lookup: func(string) (*net.Interface, error) {
			return &net.Interface{Name: "test0", Index: 10, Flags: net.FlagUp}, nil
		},
		ReadAddresses: func(*net.Interface) ([]net.Addr, error) { return nil, failure },
	}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("required read failed without error: %v", err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "Interface", "lan")
	if err != nil || status["phase"] != "Error" || status["reason"] != "AddressObservationFailed" || status["primaryIPv4"] != "192.0.2.1/24" {
		t.Fatalf("unknown became empty/up: %#v %v", status, err)
	}
}

func TestLinkAdminUpUsesNativeFreeBSDCommand(t *testing.T) {
	name, args, err := linkAdminUpCommand(platform.OSFreeBSD, "vtnet1")
	if err != nil || name != "ifconfig" || strings.Join(args, " ") != "vtnet1 up" {
		t.Fatalf("FreeBSD command=%s %v %v", name, args, err)
	}
}

func TestLinkObservationRetiresRemovedDeclarationsOnly(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "retained"}, Spec: api.InterfaceSpec{IfName: "fake0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "inactive"}, Spec: api.InterfaceSpec{IfName: "inactive0"}},
	}}}
	for _, name := range []string{"removed", "inactive"} {
		if err := db.SaveObjectStatus(api.NetAPIVersion, "Interface", name, map[string]any{"phase": "Down", "reason": "InterfaceNotFound"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "removed", map[string]any{"phase": "Pending"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveObjectStatus("other.example/v1", "Interface", "removed", map[string]any{"phase": "Down"}); err != nil {
		t.Fatal(err)
	}
	effective := &api.Router{Spec: api.RouterSpec{Resources: declared.Spec.Resources[:1]}}
	controller := LinkController{Router: effective, DeclaredRouter: declared, Store: db, Lookup: func(string) (*net.Interface, error) {
		return &net.Interface{Name: "fake0", Index: 7, Flags: net.FlagUp}, nil
	}, ReadAddresses: func(*net.Interface) ([]net.Addr, error) { return nil, nil }}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := db.ObjectStatus(api.NetAPIVersion, "Interface", "removed"); len(got) != 0 {
		t.Fatalf("removed observation still affects router phase: %#v", got)
	}
	if got := db.ObjectStatus(api.NetAPIVersion, "Interface", "retained"); got["phase"] != "Up" {
		t.Fatalf("retained interface was not observed: %#v", got)
	}
	if got := db.ObjectStatus(api.NetAPIVersion, "Interface", "inactive"); got["phase"] != "Down" {
		t.Fatalf("conditionally inactive declaration was removed: %#v", got)
	}
	if len(db.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "removed")) == 0 || len(db.ObjectStatus("other.example/v1", "Interface", "removed")) == 0 {
		t.Fatal("observation cleanup removed another owner or API")
	}
}

type linkRemovalStore struct {
	*routerstate.SQLiteStore
	listFailure, deleteFailure error
}

func (s *linkRemovalStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	if s.listFailure != nil {
		return nil, s.listFailure
	}
	return s.SQLiteStore.ListObjectStatuses()
}
func (s *linkRemovalStore) DeleteObject(version, kind, name string) error {
	if s.deleteFailure != nil {
		return s.deleteFailure
	}
	return s.SQLiteStore.DeleteObject(version, kind, name)
}

func TestLinkRemovedObservationFailuresRemainRetryable(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveObjectStatus(api.NetAPIVersion, "Interface", "removed", map[string]any{"phase": "Down"}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("observation cleanup unavailable")
	store := &linkRemovalStore{SQLiteStore: db, listFailure: failure}
	c := LinkController{Router: &api.Router{}, Store: store}
	if err := c.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("list failure reported success: %v", err)
	}
	store.listFailure = nil
	store.deleteFailure = failure
	if err := c.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("delete failure reported success: %v", err)
	}
	if len(db.ObjectStatus(api.NetAPIVersion, "Interface", "removed")) == 0 {
		t.Fatal("failed deletion lost retry input")
	}
	store.deleteFailure = nil
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(db.ObjectStatus(api.NetAPIVersion, "Interface", "removed")) != 0 {
		t.Fatal("retry left removed observation")
	}
}
