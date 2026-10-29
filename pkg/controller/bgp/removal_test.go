// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/manageddaemon"
)

func writeRemovalState(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := bgpdaemon.WriteApplied(path, bgpdaemon.AppliedConfig{Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1"}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBGPRemovalRetriesFailedEffects(t *testing.T) {
	failure := errors.New("removal interrupted")
	for _, stage := range []string{"supervisor", "fib"} {
		t.Run(stage, func(t *testing.T) {
			path := writeRemovalState(t)
			fib := &fakeFIB{routes: []FIBRoute{{Prefix: "10.0.0.0/24", RetainOnMissing: true}}}
			controller := Controller{Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib}
			stopCalls := 0
			stop := func(context.Context) error {
				stopCalls++
				pending, err := bgpdaemon.RemovalPending(path)
				if err != nil || !pending {
					t.Fatal("supervisor called before durable removal intent")
				}
				if stage == "supervisor" {
					return failure
				}
				return nil
			}
			if stage == "fib" {
				fib.clearErr = failure
			}
			if err := controller.Remove(context.Background(), stop); !errors.Is(err, failure) {
				t.Fatalf("removal error = %v, want preserved effect failure", err)
			}
			pending, err := bgpdaemon.RemovalPending(path)
			if err != nil || !pending {
				t.Fatal("failed removal lost restart fence")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("failed removal discarded daemon checkpoint")
			}
			if stage == "supervisor" && fib.clearCalls != 0 {
				t.Fatal("FIB changed before acknowledged stop")
			}
			fib.clearErr = nil
			if err := controller.Remove(context.Background(), func(context.Context) error { stopCalls++; return nil }); err != nil {
				t.Fatal(err)
			}
			pending, err = bgpdaemon.RemovalPending(path)
			if err != nil || pending || len(fib.routes) != 0 || stopCalls != 2 {
				t.Fatal("retry did not finish removal")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("old restart state still present: %v", err)
			}
		})
	}
}

func TestBGPRemovalResumesAcrossControllerGeneration(t *testing.T) {
	path := writeRemovalState(t)
	if err := bgpdaemon.BeginRemoval(path); err != nil {
		t.Fatal(err)
	}
	fib := &fakeFIB{}
	controller := Controller{Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib}
	if err := controller.Remove(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if fib.clearCalls != 1 {
		t.Fatalf("restart FIB clears = %d", fib.clearCalls)
	}
	if err := controller.Remove(context.Background(), func(context.Context) error { t.Fatal("completed removal repeated supervisor stop"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestBGPRemovalDryRunHasNoEffects(t *testing.T) {
	path := writeRemovalState(t)
	controller := Controller{Daemon: manageddaemon.Spec{StatePath: path}, FIB: &fakeFIB{}, DryRun: true}
	if err := controller.Remove(context.Background(), func(context.Context) error { t.Fatal("dry-run stopped daemon"); return nil }); err != nil {
		t.Fatal(err)
	}
	pending, err := bgpdaemon.RemovalPending(path)
	if err != nil || pending {
		t.Fatal("dry-run created removal intent")
	}
}

func TestBGPRemovalFencesWatchAndActiveReconcileUntilRetry(t *testing.T) {
	path := writeRemovalState(t)
	fib := &fakeFIB{clearErr: errors.New("FIB interrupted")}
	controller := Controller{Router: bgpRouter(), Store: mapStore{}, Server: &fakeServer{}, FIB: fib, started: true, Daemon: manageddaemon.Spec{StatePath: path}}
	if err := controller.Remove(context.Background(), func(context.Context) error { return nil }); err == nil {
		t.Fatal("expected FIB failure")
	}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("active reconcile bypassed pending removal")
	}
	controller.mu.Lock()
	err := controller.observeAndSyncFromWatchLocked(context.Background())
	controller.mu.Unlock()
	if err != nil || len(fib.history) != 0 {
		t.Fatal("watch restored routes during pending removal")
	}
	fib.clearErr = nil
	if err := controller.Remove(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	controller.mu.Lock()
	err = controller.observeAndSyncFromWatchLocked(context.Background())
	controller.mu.Unlock()
	if err != nil || len(fib.history) != 0 {
		t.Fatal("waiting watch restored routes after removal")
	}
}

func TestBGPRemovalManagedHelperWithoutCheckpointStopsOnce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "applied.json")
	helper := filepath.Join(root, "routerd-bgp.service")
	if err := os.WriteFile(helper, []byte("managed helper"), 0600); err != nil {
		t.Fatal(err)
	}
	fib, stops := &fakeFIB{}, 0
	controller := Controller{Daemon: manageddaemon.Spec{StatePath: path}, ManagedHelperPath: helper, FIB: fib}
	stop := func(context.Context) error { stops++; return nil }
	for range 2 {
		if err := controller.Remove(context.Background(), stop); err != nil {
			t.Fatal(err)
		}
	}
	if stops != 1 || fib.clearCalls != 1 {
		t.Fatalf("completed helper removal repeated: stops=%d clear=%d", stops, fib.clearCalls)
	}
}

func TestBGPRemovalRetriesUncertainCompletionWithoutFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	fib := &fakeFIB{}
	controller := Controller{Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib, removal: removalInProgress}
	pending, err := controller.PendingRemoval()
	if err != nil || !pending {
		t.Fatal("uncertain completion lost in-memory retry")
	}
	if err := controller.Remove(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	pending, err = controller.PendingRemoval()
	if err != nil || pending || fib.clearCalls != 1 {
		t.Fatal("uncertain completion did not finish retry")
	}
}

func TestBGPRemovalWithoutSupervisorPreservesAuthority(t *testing.T) {
	path := writeRemovalState(t)
	fib := &fakeFIB{}
	controller := Controller{Router: &api.Router{}, Store: mapStore{}, Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("missing supervisor fabricated completed removal")
	}
	pending, err := bgpdaemon.RemovalPending(path)
	if err != nil || pending || fib.clearCalls != 0 {
		t.Fatal("unavailable supervisor mutated removal intent or FIB")
	}
}
