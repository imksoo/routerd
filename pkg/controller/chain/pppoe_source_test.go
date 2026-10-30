// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPPPoESourceRequiresCurrentSpecAndOwnedWorker(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	previousProcesses := supervisedDaemonProcesses
	t.Cleanup(func() { supervisedDaemonProcesses = previousProcesses })
	readFailure := false
	failure := errors.New("native process list unreadable")
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
		if readFailure {
			return nil, failure
		}
		return []supervisedDaemonProcess{{PID: 4242, Command: "/usr/local/sbin/routerd-pppoe-client daemon --resource wan --supervisor-owner current-owner"}}, nil
	}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ethernet"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.PPPoESessionSpec{Interface: "ethernet", Username: "subscriber", Password: "private"}},
	}}}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := &Runner{Router: router, Store: store, Opts: Options{SuperviseClientDaemons: true}}
	prepared, err := prepareRouterConfig(router, platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	runner.publishPreparedRouter(prepared)
	wanted := runner.clientDaemonSpecs(prepared.startup.Router())[0]
	wanted.OwnerToken = "current-owner"
	done := make(chan struct{})
	runner.clientDaemonStates = map[string]supervisedDaemonState{supervisedDaemonKey(wanted.Binary, wanted.ResourceName): {Spec: wanted, Done: done}}
	marker := supervisedDaemonMarker{Version: 1, Binary: wanted.Binary, Resource: wanted.ResourceName, SpecHash: supervisedDaemonSpecHash(wanted), OwnerToken: wanted.OwnerToken, PID: 4242}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	evented := eventedStore{Store: store, Router: router}
	source, ready, err := runner.currentPPPoESource(router, "wan", evented)
	if err != nil || !ready || source.SupervisorOwner != "current-owner" || source.Interface != "ens18" || source.Device != "ppp-wan" {
		t.Fatalf("current source not accepted: %+v ready=%v err=%v", source, ready, err)
	}
	readFailure = true
	if _, _, err := runner.currentPPPoESource(router, "wan", evented); !errors.Is(err, failure) {
		t.Fatalf("native read failure became missing worker: %v", err)
	}
	readFailure = false
	updated := *router
	updated.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	spec := updated.Spec.Resources[1].Spec.(api.PPPoESessionSpec)
	spec.Username = "new-subscriber"
	updated.Spec.Resources[1].Spec = spec
	prepared, err = prepareRouterConfig(&updated, platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	runner.publishPreparedRouter(prepared)
	if _, ready, err := runner.currentPPPoESource(router, "wan", evented); err != nil || ready {
		t.Fatalf("old operation accepted after configuration publication: ready=%v err=%v", ready, err)
	}
	if _, ready, err := runner.currentPPPoESource(&updated, "wan", evented); err != nil || ready {
		t.Fatalf("old supervisor hash/token accepted new configuration: ready=%v err=%v", ready, err)
	}
}
