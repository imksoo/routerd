// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	bgpcontroller "github.com/imksoo/routerd/pkg/controller/bgp"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/manageddaemon"
	"github.com/imksoo/routerd/pkg/platform"
)

type removalFIB struct {
	stopped *bool
	clears  int
}

func (*removalFIB) SyncBGP(context.Context, []bgpcontroller.FIBRoute) (bgpcontroller.FIBSyncResult, error) {
	return bgpcontroller.FIBSyncResult{}, nil
}

func (f *removalFIB) ClearBGP(context.Context) error {
	if !*f.stopped {
		return errors.New("FIB clear preceded supervisor acknowledgment")
	}
	f.clears++
	return nil
}

func TestBGPHelperRemovalOrdersIntentStopAndFIBAcrossSupervisors(t *testing.T) {
	for _, supervisor := range []string{"systemd", "rc.d"} {
		t.Run(supervisor, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "applied.json")
			if err := bgpdaemon.WriteApplied(path, bgpdaemon.AppliedConfig{}); err != nil {
				t.Fatal(err)
			}
			stopped := false
			fib := &removalFIB{stopped: &stopped}
			controller := SystemdUnitController{BGPStatePath: path, BGPController: &bgpcontroller.Controller{Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib}}
			features := platform.Features{HasSystemd: supervisor == "systemd", HasRCD: supervisor == "rc.d"}
			failure := errors.New("supervisor stop failed")
			var got []string
			command := func(_ context.Context, name string, args ...string) ([]byte, error) {
				pending, err := bgpdaemon.RemovalPending(path)
				if err != nil || !pending {
					t.Fatal("stop preceded durable removal intent")
				}
				got = append([]string{name}, args...)
				if !stopped {
					return nil, failure
				}
				return nil, nil
			}
			if err := controller.cleanupBGPDaemon(context.Background(), platform.Defaults{}, features, command); !errors.Is(err, failure) {
				t.Fatalf("cleanup error = %v", err)
			}
			if fib.clears != 0 {
				t.Fatal("failed supervisor stop permitted FIB deletion")
			}
			stopped = true
			if err := controller.cleanupBGPDaemon(context.Background(), platform.Defaults{}, features, command); err != nil {
				t.Fatal(err)
			}
			want := []string{"systemctl", "disable", "--now", "routerd-bgp.service"}
			if supervisor == "rc.d" {
				want = []string{"service", "routerd_bgp", "onestop"}
			}
			if !reflect.DeepEqual(got, want) || fib.clears != 1 {
				t.Fatalf("stop=%#v clears=%d", got, fib.clears)
			}
			if err := controller.cleanupBGPDaemon(context.Background(), platform.Defaults{}, features, func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("completed removal repeated supervisor stop")
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBGPOnlyRunnerKeepsAcknowledgedRemovalDependency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := bgpdaemon.WriteApplied(path, bgpdaemon.AppliedConfig{}); err != nil {
		t.Fatal(err)
	}
	failure, stops := errors.New("supervisor refused stop"), 0
	runner := &Runner{Router: &api.Router{}, Store: mapStore{}, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"bgp"}, BGPStatePath: path, BGPDaemonStop: func(context.Context) error { stops++; return failure }}}
	controllers, _, err := runner.frameworkControllers(context.Background(), slog.Default(), eventedStore{Store: runner.Store, Router: runner.Router, Bus: runner.Bus}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 || controllers[0].Name() != "bgp" {
		t.Fatalf("selected controllers=%v", controllers)
	}
	if _, err := controllers[0].PeriodicReconcile(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("selected BGP removal error=%v", err)
	}
	pending, err := bgpdaemon.RemovalPending(path)
	if err != nil || !pending || stops != 1 {
		t.Fatalf("selected mode lost cleanup owner: pending=%v stops=%d error=%v", pending, stops, err)
	}
}

func TestBGPRemovalHonorsServiceAndKernelDryRunModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := bgpdaemon.WriteApplied(path, bgpdaemon.AppliedConfig{}); err != nil {
		t.Fatal(err)
	}
	stopped := false
	fib := &removalFIB{stopped: &stopped}
	actor := &bgpcontroller.Controller{Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib, DryRun: true}
	controller := SystemdUnitController{BGPStatePath: path, BGPController: actor}
	command := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("dry-run changed supervisor")
		return nil, nil
	}
	if err := controller.cleanupBGPDaemon(context.Background(), platform.Defaults{}, platform.Features{HasSystemd: true}, command); err == nil {
		t.Fatal("kernel dry-run permitted live helper removal")
	}
	controller.DryRun = true
	if err := controller.stopBGPDaemon(context.Background(), platform.Features{HasSystemd: true}, command); err == nil {
		t.Fatal("disabled service manager reported acknowledged stop")
	}
}
