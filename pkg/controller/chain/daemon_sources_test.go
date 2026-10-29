// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRuntimeReloadOwnsDaemonEventReadersAndAcknowledgesExit(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "sources"}}
	ctx, cancel := context.WithCancel(t.Context())
	runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"isolated-source-runtime"}, DaemonSockets: map[string]string{"wan": filepath.Join(t.TempDir(), "missing.sock")}}}
	t.Cleanup(func() {
		cancel()
		wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := runner.Wait(wait); err != nil {
			t.Error(err)
		}
	})
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	withClient := *router
	withClient.Spec.Resources = []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "uplink"}, Spec: api.InterfaceSpec{IfName: "fixture0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "uplink"}},
	}
	if err := runner.ReloadRuntime(ctx, &withClient); err != nil {
		t.Fatal(err)
	}
	key := daemonEventSourceKey("routerd-dhcpv4-client", "wan")
	runner.daemonSourceMu.Lock()
	first, found := runner.daemonSources[key]
	runner.daemonSourceMu.Unlock()
	if !found || first.Spec.Socket != runner.Opts.DaemonSockets["wan"] {
		t.Fatalf("new generation did not establish current source: %+v found=%v", first, found)
	}
	unchangedSource := withClient
	unchangedSource.Metadata.Name = "new-generation-same-source"
	if err := runner.ReloadRuntime(ctx, &unchangedSource); err != nil {
		t.Fatal(err)
	}
	runner.daemonSourceMu.Lock()
	second := runner.daemonSources[key]
	runner.daemonSourceMu.Unlock()
	if second.Done != first.Done {
		t.Fatal("unchanged source lost its reader and in-memory daemon cursor")
	}
	if err := runner.ReloadRuntime(ctx, router); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Done:
	default:
		t.Fatal("source removal acknowledged before the old reader exited")
	}
	runner.daemonSourceMu.Lock()
	remaining := len(runner.daemonSources)
	runner.daemonSourceMu.Unlock()
	if remaining != 0 {
		t.Fatalf("removed source retained %d readers", remaining)
	}
	cancel()
	wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := runner.Wait(wait); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonEventSourceSocketChangeJoinsPreviousReader(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "check"}, Spec: api.HealthCheckSpec{Target: "127.0.0.1", Protocol: "icmp"}}}}}
	runner := &Runner{Bus: bus.New(), clientDaemonContext: ctx, daemonSourcesEnabled: true, Opts: Options{DaemonSockets: map[string]string{"check": filepath.Join(t.TempDir(), "old.sock")}}}
	defer runner.stopDaemonEventSources()
	if err := runner.reconcileDaemonEventSources(ctx, router, nil); err != nil {
		t.Fatal(err)
	}
	key := daemonEventSourceKey("routerd-healthcheck", "check")
	first := runner.daemonSources[key]
	runner.Opts.DaemonSockets["check"] = filepath.Join(t.TempDir(), "new.sock")
	if err := runner.reconcileDaemonEventSources(ctx, router, nil); err != nil {
		t.Fatal(err)
	}
	second := runner.daemonSources[key]
	select {
	case <-first.Done:
	default:
		t.Fatal("replacement was started before the old source acknowledged exit")
	}
	if second.Done == first.Done || second.Spec.Socket != runner.Opts.DaemonSockets["check"] {
		t.Fatalf("socket replacement retained old source: %+v", second.Spec)
	}
}
