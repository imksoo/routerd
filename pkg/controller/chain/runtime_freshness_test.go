// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/conntrack"
	"github.com/imksoo/routerd/pkg/controller/conntrackobserver"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDHCPInformationGenerationUsesUpdatedRouterAndOwnership(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := pdAtomicReviewRouterWithPD("wan0", "01", "7")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Information"},
		Metadata: api.ObjectMeta{Name: "info", OwnerRefs: []api.OwnerRef{{Kind: "DHCPv6PrefixDelegation", Name: "wan-pd"}}},
		Spec:     api.DHCPv6InformationSpec{Interface: "wan0"},
	})
	lease, _ := (routerstate.PDLease{}).Configured(pdAtomicReviewConfig())
	snapshot := pdAtomicReviewSnapshot(time.Now().UTC())
	events := bus.NewWithStore(store)
	if err := RecordPDLeaseObservation(store, events, router, "wan-pd", routerstate.PDLeaseConfig{}, lease, &snapshot, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Store: store, Bus: events, Opts: Options{EnabledControllers: []string{"dhcpv6-information"}, DaemonSockets: map[string]string{"wan-pd": filepath.Join(t.TempDir(), "missing.sock")}}}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	next := router.Clone()
	next.Spec.Resources[len(next.Spec.Resources)-1].Metadata.OwnerRefs = append(next.Spec.Resources[len(next.Spec.Resources)-1].Metadata.OwnerRefs, api.OwnerRef{Kind: "Interface", Name: "new-owner"})
	if err := runner.UpdateRuntimeRouter(&next); err != nil {
		t.Fatal(err)
	}
	want := lifecycle.OwnerRefsStatusList(next.Spec.Resources[len(next.Spec.Resources)-1].Metadata.OwnerRefs)
	waitFreshRuntimeStatus(t, runner, "DHCPv6Information", "info", func(status map[string]any) bool {
		return status["phase"] == "Pending" && status["socket"] == runner.Opts.DaemonSockets["wan-pd"] && reflect.DeepEqual(status["ownerRefs"], want)
	})
}

func TestDHCPServerGenerationConsumesUpdatedSameShapeIntent(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	command := filepath.Join(dir, "dnsmasq")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "fixture0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{Interface: "lan", AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.150", LeaseTime: "1h"}, Gateway: "192.0.2.1", LeaseFile: filepath.Join(dir, "leases")}},
	}}}
	configPath := filepath.Join(dir, "dnsmasq.conf")
	runner := &Runner{Router: router, Store: store, Bus: bus.NewWithStore(store), Opts: Options{EnabledControllers: []string{"dhcpv6-server"}, DryRunDHCPv6: true, DnsmasqCommand: command, DnsmasqConfig: configPath, DnsmasqPID: filepath.Join(dir, "dnsmasq.pid")}}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	next := router.Clone()
	spec, _ := next.Spec.Resources[1].DHCPv4ServerSpec()
	spec.Gateway = "192.0.2.2"
	next.Spec.Resources[1].Spec = spec
	next.Spec.Resources[1].Metadata.OwnerRefs = []api.OwnerRef{{Kind: "Interface", Name: "new-owner"}}
	if err := runner.UpdateRuntimeRouter(&next); err != nil {
		t.Fatal(err)
	}
	want := lifecycle.OwnerRefsStatusList(next.Spec.Resources[1].Metadata.OwnerRefs)
	waitFreshRuntimeStatus(t, runner, "DHCPv4Server", "lan-v4", func(status map[string]any) bool {
		data, err := os.ReadFile(configPath)
		return err == nil && strings.Contains(string(data), "option:router,192.0.2.2") && !strings.Contains(string(data), "option:router,192.0.2.1") && reflect.DeepEqual(status["ownerRefs"], want)
	})
}

func TestConntrackFrameworkOwnerConsumesUpdatedFlowLogInput(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"}, Metadata: api.ObjectMeta{Name: "flows"}, Spec: api.TrafficFlowLogSpec{Enabled: true, Path: filepath.Join(dir, "old-flows.db")}}}}}
	runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"isolated"}}}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	observer := &conntrackobserver.Controller{Bus: runner.Bus, Snapshot: func(context.Context) (conntrack.Snapshot, error) { return conntrack.Snapshot{Count: 1, Max: 10}, nil }, Connections: func(_ context.Context, limit int) (*observe.ConnectionTable, error) {
		if limit != -1 {
			t.Fatalf("authoritative flow snapshot was truncated to %d", limit)
		}
		return &observe.ConnectionTable{}, nil
	}}
	controller := runner.conntrackObserverController(eventedStore{Store: store, Bus: runner.Bus, Router: router}, observer)
	if _, err := controller.PeriodicReconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := router.Clone()
	path := filepath.Join(dir, "current-flows.db")
	next.Spec.Resources[0].Spec = api.TrafficFlowLogSpec{Enabled: true, Path: path}
	next.Spec.Resources[0].Metadata.OwnerRefs = []api.OwnerRef{{Kind: "Interface", Name: "current-owner"}}
	if err := runner.UpdateRuntimeRouter(&next); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.PeriodicReconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "TrafficFlowLog", "flows")
	if err != nil || status["path"] != path || !reflect.DeepEqual(status["ownerRefs"], lifecycle.OwnerRefsStatusList(next.Spec.Resources[0].Metadata.OwnerRefs)) {
		t.Fatalf("existing observer retained old input/ownership: %+v %v", status, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("current flow-log intent was never consumed: %v", err)
	}
}

func waitFreshRuntimeStatus(t *testing.T, runner *Runner, kind, name string, matches func(map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "fixture"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation", Name: "wan-pd"}
		if err := runner.Bus.Publish(t.Context(), event); err != nil {
			t.Fatal(err)
		}
		status, err := runner.Store.(interface {
			ReadObjectStatus(string, string, string) (map[string]any, error)
		}).ReadObjectStatus(api.NetAPIVersion, kind, name)
		if err != nil {
			t.Fatal(err)
		}
		if matches(status) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("current input was not reconciled: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
