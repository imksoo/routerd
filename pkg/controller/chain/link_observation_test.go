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
