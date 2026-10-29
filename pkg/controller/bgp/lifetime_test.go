// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"
)

type heldWatchServer struct {
	*fakeServer
	entered chan struct{}
	release chan struct{}
}

func (s *heldWatchServer) WatchEvent(ctx context.Context, _ *gobgpapi.WatchEventRequest, _ func(*gobgpapi.WatchEventResponse) error) error {
	close(s.entered)
	<-ctx.Done()
	<-s.release
	return ctx.Err()
}
func TestBGPStopWaitsForActualWatchExitBeforeClosingConnection(t *testing.T) {
	server := &heldWatchServer{fakeServer: &fakeServer{}, entered: make(chan struct{}), release: make(chan struct{})}
	controller := &Controller{Router: bgpRouter(), Store: mapStore{}, Server: server, started: true}
	done := controller.Start(t.Context())
	select {
	case <-server.entered:
	case <-time.After(time.Second):
		t.Fatal("watch did not start")
	}
	stopCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := controller.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked old watch Stop=%v", err)
	}
	select {
	case <-done:
		t.Fatal("old watch exit was fabricated")
	default:
	}
	if server.stops != 0 {
		t.Fatal("connection closed before the watch acknowledged exit")
	}
	close(server.release)
	if err := controller.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Stop succeeded before actual watch exit")
	}
	if server.stops != 1 || controller.Server != nil {
		t.Fatalf("RPC lifetime not closed once: stops=%d server=%v", server.stops, controller.Server)
	}
	if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "generation has stopped") {
		t.Fatalf("stopped actor admitted reconcile: %v", err)
	}
	if again := controller.Start(t.Context()); again != done {
		t.Fatal("stopped actor started another watch lifetime")
	}
}

type heldFIBSyncer struct {
	entered chan struct{}
	release chan struct{}
}

func (*heldFIBSyncer) KernelScope(context.Context) (string, error) {
	return "linux:fixture:network", nil
}

func (s *heldFIBSyncer) SyncBGP(ctx context.Context, _ FIBStore, _ []FIBRoute, _ FIBIntentReader) (FIBSyncResult, error) {
	close(s.entered)
	<-s.release
	return FIBSyncResult{}, ctx.Err()
}
func (*heldFIBSyncer) ClearBGP(context.Context, FIBStore) error { return nil }

func TestBGPStopFencesInFlightReconcileBeforeNextKernelOwner(t *testing.T) {
	fib := &heldFIBSyncer{entered: make(chan struct{}), release: make(chan struct{})}
	server := &fakeServer{}
	controller := &Controller{Router: bgpRouter(), Store: mapStore{}, Server: server, FIB: fib}
	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	finished := make(chan error, 1)
	go func() { finished <- controller.Reconcile(ownerCtx) }()
	select {
	case <-fib.entered:
	case <-time.After(time.Second):
		t.Fatal("native owner did not enter")
	}
	cancelOwner()
	stopCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := controller.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("in-flight native Stop=%v", err)
	}
	if server.stops != 0 {
		t.Fatal("Stop released the actor before its kernel mutation exited")
	}
	close(fib.release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled old reconcile=%v", err)
	}
	if err := controller.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if server.stops != 1 {
		t.Fatalf("RPC close count=%d", server.stops)
	}
}

func TestBGPStopClosesOneShotRPCWithoutStoppingHelperOrClearingFIB(t *testing.T) {
	server, fib := &fakeServer{}, &fakeFIB{}
	controller := &Controller{Server: server, FIB: fib, StopDaemon: func(context.Context) error { t.Fatal("generation teardown stopped the managed helper"); return nil }}
	if err := controller.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if server.stops != 1 || fib.clearCalls != 0 {
		t.Fatalf("one-shot teardown mixed owners: RPC=%d FIB=%d", server.stops, fib.clearCalls)
	}
}
