// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestARPObserverUnknownPlanPreservesActualSupervisorOwner(t *testing.T) {
	for _, failure := range []string{"closed-database", "corrupt-active-payload"} {
		t.Run(failure, func(t *testing.T) {
			useSupervisedDaemonMarkerTestRoot(t)
			oldProcesses := supervisedDaemonProcesses
			t.Cleanup(func() { supervisedDaemonProcesses = oldProcesses })
			processReads := 0
			supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
				processReads++
				return nil, nil
			}
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			record := arpObserverIntentRecord(t, []dynamicconfig.ARPObserverIntent{{ResourceName: "mobility-arp-svnet1-observer-0", PoolRef: "svnet1", Prefix: "10.77.60.0/24", SourceType: "arp-observer", IfName: "eth1", EventInterface: "capture", Observe: true}}, time.Now().Add(time.Minute))
			if err := store.UpsertDynamicConfigPart(record); err != nil {
				t.Fatal(err)
			}
			router := &api.Router{}
			runner := &Runner{Router: router, Store: store, Opts: Options{SuperviseClientDaemons: true}}
			specs, err := runner.supervisedDaemonSpecs(router)
			if err != nil || len(specs) != 1 {
				t.Fatalf("actual typed plan not observed: %+v err=%v", specs, err)
			}
			spec := specs[0]
			spec.OwnerToken = "existing-owner"
			key := supervisedDaemonKey(spec.Binary, spec.ResourceName)
			done := make(chan struct{})
			canceled := false
			runner.clientDaemonStates = map[string]supervisedDaemonState{key: {Spec: spec, Cancel: func() { canceled = true; close(done) }, Done: done}}
			marker := supervisedDaemonMarker{Version: 1, Binary: spec.Binary, Resource: spec.ResourceName, SpecHash: supervisedDaemonSpecHash(spec), OwnerToken: spec.OwnerToken, PID: 536870911}
			if err := writeSupervisedDaemonMarker(marker); err != nil {
				t.Fatal(err)
			}
			if failure == "closed-database" {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				record.ARPObserverIntentsJSON = "{"
				if err := store.UpsertDynamicConfigPart(record); err != nil {
					t.Fatal(err)
				}
			}
			if err := runner.reconcileSupervisedClientDaemons(t.Context(), router, nil); err == nil {
				t.Fatal("unknown desired plan accepted as withdrawal")
			}
			markers, err := readSupervisedDaemonMarkers()
			if err != nil || canceled || processReads != 0 || len(runner.clientDaemonStates) != 1 || markers[key] != marker {
				t.Fatalf("unknown plan changed live owner: canceled=%v reads=%d states=%d marker=%+v err=%v", canceled, processReads, len(runner.clientDaemonStates), markers[key], err)
			}
			if failure == "closed-database" {
				store, err = routerstate.OpenSQLite(path)
				if err != nil {
					t.Fatal(err)
				}
				runner.Store = store
			}
			// A successful observation of an explicit empty plan still withdraws.
			record.ARPObserverIntentsJSON = "[]"
			if err := store.UpsertDynamicConfigPart(record); err != nil {
				t.Fatal(err)
			}
			if err := runner.reconcileSupervisedClientDaemons(t.Context(), router, nil); err != nil {
				t.Fatal(err)
			}
			markers, err = readSupervisedDaemonMarkers()
			if err != nil || !canceled || len(runner.clientDaemonStates) != 0 || len(markers) != 0 {
				t.Fatalf("known empty plan did not withdraw owner: canceled=%v states=%d markers=%+v err=%v", canceled, len(runner.clientDaemonStates), markers, err)
			}
		})
	}
}
