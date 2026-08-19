// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func arpObserverIntentRecord(t *testing.T, intents []dynamicconfig.ARPObserverIntent, expiresAt time.Time) routerstate.DynamicConfigPartRecord {
	t.Helper()
	raw, err := json.Marshal(intents)
	if err != nil {
		t.Fatalf("marshal ARP observer intents: %v", err)
	}
	now := time.Now().UTC()
	if expiresAt.IsZero() || !expiresAt.After(now) || expiresAt.Sub(now) > 10*time.Minute {
		expiresAt = now.Add(5 * time.Minute)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:                 "MobilityPool/svnet1/node/pve-rt08/arp-observer",
		Generation:             1,
		ObservedAt:             now,
		ExpiresAt:              expiresAt,
		Digest:                 "arp-observer-test",
		ARPObserverIntentsJSON: string(raw),
		Status:                 "active",
	}
}

func TestRunnerMobilityARPObserverDaemonSpecsFromTypedIntents(t *testing.T) {
	intents := []dynamicconfig.ARPObserverIntent{
		{ResourceName: "mobility-arp-svnet1-observer-0", PoolRef: "svnet1", Prefix: "192.168.123.0/24", SourceType: "arp-observer", IfName: "eth1", EventInterface: "svnet1", Observe: true},
		{ResourceName: "mobility-arp-svnet1-demand-1", PoolRef: "svnet1", Prefix: "192.168.123.0/24", SourceType: "on-demand-arp", IfName: "eth1", EventInterface: "svnet1", SourceAddress: "192.168.123.134", OnDemand: true, ProbeTimeout: "500ms", ProbeRetries: 2, ScanInterval: "1s"},
		{ResourceName: "mobility-arp-svnet1-pve-2", PoolRef: "svnet1", Prefix: "192.168.123.0/24", SourceType: "pve-svnet", IfName: "eth1", EventInterface: "svnet1", Network: "svnet1", Bridge: "vmbr123", Observe: true, ScanInterval: "3s"},
	}
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{arpObserverIntentRecord(t, intents, time.Now().Add(time.Hour))}}
	runner := Runner{Store: store}
	specs := runner.mobilityARPObserverDaemonSpecs()
	if len(specs) != 3 {
		t.Fatalf("daemon specs = %d, want 3: %#v", len(specs), specs)
	}
	byType := map[string]mobilityARPObserverDaemonSpec{}
	for _, spec := range specs {
		byType[spec.SourceType] = spec
		if spec.IfName != "eth1" {
			t.Fatalf("%s IfName = %q, want eth1", spec.SourceType, spec.IfName)
		}
		if spec.EventInterface != "svnet1" {
			t.Fatalf("%s EventInterface = %q, want svnet1", spec.SourceType, spec.EventInterface)
		}
	}
	if !byType["arp-observer"].Observe || byType["arp-observer"].OnDemand {
		t.Fatalf("arp-observer spec = %#v, want observe only", byType["arp-observer"])
	}
	if !byType["on-demand-arp"].OnDemand || byType["on-demand-arp"].Observe {
		t.Fatalf("on-demand-arp spec = %#v, want on-demand only", byType["on-demand-arp"])
	}
	if got := byType["on-demand-arp"].SourceAddress; got != "192.168.123.134" {
		t.Fatalf("on-demand source address = %q, want DHCP status address without prefix", got)
	}
	if byType["on-demand-arp"].ProbeTimeout != "500ms" || byType["on-demand-arp"].ProbeRetries != 2 || byType["on-demand-arp"].ScanInterval != "1s" {
		t.Fatalf("on-demand probe settings = %#v", byType["on-demand-arp"])
	}
	if byType["pve-svnet"].Network != "svnet1" || byType["pve-svnet"].Bridge != "vmbr123" || byType["pve-svnet"].ScanInterval != "3s" {
		t.Fatalf("pve-svnet metadata = %#v", byType["pve-svnet"])
	}
}

func TestRunnerMobilityARPObserverDaemonSpecsPreserveTypedIgnoredSenderMACs(t *testing.T) {
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{arpObserverIntentRecord(t, []dynamicconfig.ARPObserverIntent{{
		ResourceName:   "mobility-arp-svnet1-observer-0",
		PoolRef:        "svnet1",
		Prefix:         "192.168.123.0/24",
		SourceType:     "arp-observer",
		IfName:         "eth1",
		EventInterface: "svnet1",
		Observe:        true,
		IgnoredSenderMACs: []string{
			"02:00:00:00:00:aa",
			"02:00:00:00:00:bb",
			"02:00:00:00:00:cc",
		},
	}}, time.Now().Add(time.Hour))}}
	runner := Runner{Store: store}
	specs := runner.mobilityARPObserverDaemonSpecs()
	if len(specs) != 1 {
		t.Fatalf("daemon specs = %#v, want one arp-observer spec", specs)
	}
	want := []string{"02:00:00:00:00:aa", "02:00:00:00:00:bb", "02:00:00:00:00:cc"}
	if got := specs[0].IgnoredSenderMACs; !stringSlicesEqual(got, want) {
		t.Fatalf("IgnoredSenderMACs = %#v, want %#v", got, want)
	}
}

func TestRunnerMobilityARPObserverDaemonSpecsRejectsWrongSourceOrPool(t *testing.T) {
	now := time.Now().UTC()
	intent := dynamicconfig.ARPObserverIntent{
		ResourceName: "mobility-arp-svnet1-observer-0", PoolRef: "svnet1", Prefix: "192.168.123.0/24",
		SourceType: "arp-observer", IfName: "eth1", EventInterface: "svnet1", Observe: true,
	}
	mainSource := arpObserverIntentRecord(t, []dynamicconfig.ARPObserverIntent{intent}, now.Add(time.Hour))
	mainSource.Source = "MobilityPool/svnet1/node/pve-rt08"
	wrongPool := arpObserverIntentRecord(t, []dynamicconfig.ARPObserverIntent{intent}, now.Add(time.Hour))
	wrongPool.Source = "MobilityPool/other/node/pve-rt08/arp-observer"
	runner := Runner{Store: &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{mainSource, wrongPool}}}
	if specs := runner.mobilityARPObserverDaemonSpecs(); len(specs) != 0 {
		t.Fatalf("untrusted ARP observer records produced daemon specs: %#v", specs)
	}
}

func TestARPObserverDaemonArgsDoNotExposeIgnoredSenderMACFlag(t *testing.T) {
	spec := mobilityARPObserverDaemonSpec{
		ResourceName:      "mobility-arp-svnet1-observer-0",
		PoolName:          "svnet1",
		Prefix:            "192.168.123.0/24",
		SourceType:        "arp-observer",
		IfName:            "eth1",
		EventInterface:    "eth1",
		Socket:            "/run/routerd/arp-observer/mobility-arp-svnet1-observer-0.sock",
		EventFile:         "/var/lib/routerd/arp-observer/mobility-arp-svnet1-observer-0/events.jsonl",
		Observe:           true,
		IgnoredSenderMACs: []string{"02:00:00:00:00:aa"},
	}
	args := arpObserverDaemonArgs(spec)
	for i, arg := range args {
		if arg == "--ignore-sender-mac" {
			t.Fatalf("args[%d] exposed --ignore-sender-mac: %#v", i, args)
		}
	}
}

func TestARPObserverDaemonsUseSupervisedOwnerTokenLifecycle(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	oldProcesses, oldReady := supervisedDaemonProcesses, supervisedDaemonSocketReady
	t.Cleanup(func() {
		supervisedDaemonProcesses, supervisedDaemonSocketReady = oldProcesses, oldReady
	})
	supervisedDaemonProcesses = func() []supervisedDaemonProcess { return nil }
	supervisedDaemonSocketReady = func(string) bool { return false }

	router := &api.Router{}
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{arpObserverIntentRecord(t, []dynamicconfig.ARPObserverIntent{
		{ResourceName: "mobility-arp-cloudedge-observer-0", PoolRef: "svnet1", Prefix: "10.77.60.0/24", SourceType: "arp-observer", IfName: "eth1", EventInterface: "capture", Observe: true},
		{ResourceName: "mobility-arp-cloudedge-demand-1", PoolRef: "svnet1", Prefix: "10.77.60.0/24", SourceType: "on-demand-arp", IfName: "eth1", EventInterface: "capture", SourceAddress: "10.77.60.1", OnDemand: true, ScanInterval: "1s"},
	}, time.Now().Add(time.Hour))}}
	runner := &Runner{Router: router, Store: store}
	specs := runner.clientDaemonSpecs(router)
	if len(specs) != 2 {
		t.Fatalf("client daemon specs = %#v, want two ARP observers", specs)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.reconcileSupervisedDaemonSpecs(ctx, nil, specs)

	markers, err := readSupervisedDaemonMarkers()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		key := supervisedDaemonKey(spec.Binary, spec.ResourceName)
		state, ok := runner.clientDaemonStates[key]
		if !ok || state.Spec.OwnerToken == "" {
			t.Fatalf("%s state = %#v, want non-empty owner token", key, state)
		}
		marker, ok := markers[key]
		if !ok || marker.OwnerToken != state.Spec.OwnerToken || marker.SpecHash != supervisedDaemonSpecHash(state.Spec) {
			t.Fatalf("%s marker = %#v, state = %#v", key, marker, state)
		}
	}

	store.records = []routerstate.DynamicConfigPartRecord{arpObserverIntentRecord(t, nil, time.Now().Add(time.Hour))}
	runner.reconcileSupervisedDaemonSpecs(ctx, nil, runner.clientDaemonSpecs(router))
	markers, err = readSupervisedDaemonMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 0 || len(runner.clientDaemonStates) != 0 {
		t.Fatalf("deleted ARP observers retained ownership: markers=%#v states=%#v", markers, runner.clientDaemonStates)
	}
}

func TestRunnerSyncsARPObserverIgnoredSenderMACsOnDriftOnly(t *testing.T) {
	spec := mobilityARPObserverDaemonSpec{
		ResourceName:      "mobility-arp-svnet1-observer-0",
		Socket:            "/run/routerd/arp-observer/mobility-arp-svnet1-observer-0.sock",
		IgnoredSenderMACs: []string{"02:00:00:00:00:aa", "02:00:00:00:00:bb"},
	}
	pusher := &fakeARPObserverCommandPusher{
		statuses: []daemonapi.DaemonStatus{{
			Observed: map[string]string{"ignoredSenderMACsConfigured": "true", "ignoredSenderMACs": "02:00:00:00:00:aa"},
		}, {
			Observed: map[string]string{"ignoredSenderMACsConfigured": "true", "ignoredSenderMACs": "02:00:00:00:00:aa,02:00:00:00:00:bb"},
		}},
	}
	runner := Runner{ARPObserverCommands: pusher}

	if err := runner.syncARPObserverIgnoredSenderMACs(context.Background(), spec); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(pusher.sets) != 1 || !reflect.DeepEqual(pusher.sets[0], spec.IgnoredSenderMACs) {
		t.Fatalf("sets after drift = %#v, want %#v", pusher.sets, spec.IgnoredSenderMACs)
	}
	if err := runner.syncARPObserverIgnoredSenderMACs(context.Background(), spec); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(pusher.sets) != 1 {
		t.Fatalf("sets after no-op sync = %#v, want one push", pusher.sets)
	}
}

func TestRunnerSyncsARPObserverIgnoredSenderMACsPushesEmptySetBeforeReady(t *testing.T) {
	spec := mobilityARPObserverDaemonSpec{
		ResourceName: "mobility-arp-svnet1-observer-0",
		Socket:       "/run/routerd/arp-observer/mobility-arp-svnet1-observer-0.sock",
	}
	pusher := &fakeARPObserverCommandPusher{
		statuses: []daemonapi.DaemonStatus{{
			Observed: map[string]string{"ignoredSenderMACsConfigured": "false"},
		}, {
			Observed: map[string]string{"ignoredSenderMACsConfigured": "true"},
		}},
	}
	runner := Runner{ARPObserverCommands: pusher}

	if err := runner.syncARPObserverIgnoredSenderMACs(context.Background(), spec); err != nil {
		t.Fatalf("initial empty sync: %v", err)
	}
	if len(pusher.sets) != 1 || len(pusher.sets[0]) != 0 {
		t.Fatalf("sets after uninitialized empty sync = %#v, want one empty push", pusher.sets)
	}
	if err := runner.syncARPObserverIgnoredSenderMACs(context.Background(), spec); err != nil {
		t.Fatalf("initialized empty sync: %v", err)
	}
	if len(pusher.sets) != 1 {
		t.Fatalf("sets after initialized no-op sync = %#v, want one push", pusher.sets)
	}
}

func TestRunnerSyncsARPObserverIgnoredSenderMACsRepushesAfterObserverReset(t *testing.T) {
	spec := mobilityARPObserverDaemonSpec{
		ResourceName: "mobility-arp-svnet1-observer-0",
		Socket:       "/run/routerd/arp-observer/mobility-arp-svnet1-observer-0.sock",
	}
	pusher := &fakeARPObserverCommandPusher{
		statuses: []daemonapi.DaemonStatus{{
			Observed: map[string]string{"ignoredSenderMACsConfigured": "true"},
		}, {
			Observed: map[string]string{"ignoredSenderMACsConfigured": "false"},
		}},
	}
	runner := Runner{ARPObserverCommands: pusher}

	if err := runner.syncARPObserverIgnoredSenderMACs(context.Background(), spec); err != nil {
		t.Fatalf("sync before reset: %v", err)
	}
	if len(pusher.sets) != 0 {
		t.Fatalf("sets before reset = %#v, want no-op", pusher.sets)
	}
	if err := runner.syncARPObserverIgnoredSenderMACs(context.Background(), spec); err != nil {
		t.Fatalf("sync after reset: %v", err)
	}
	if len(pusher.sets) != 1 || len(pusher.sets[0]) != 0 {
		t.Fatalf("sets after reset = %#v, want one empty push", pusher.sets)
	}
}

func TestRunnerDoesNotMarkARPObserverReadyBeforeInitialIgnoredSenderMACPush(t *testing.T) {
	spec := mobilityARPObserverDaemonSpec{
		ResourceName:      "mobility-arp-svnet1-observer-0",
		Socket:            "/run/routerd/arp-observer/mobility-arp-svnet1-observer-0.sock",
		IgnoredSenderMACs: []string{"02:00:00:00:00:aa"},
	}
	pusher := &fakeARPObserverCommandPusher{setErr: context.DeadlineExceeded}
	runner := Runner{ARPObserverCommands: pusher}

	if err := runner.waitForARPObserverInitialSync(context.Background(), spec); err == nil {
		t.Fatal("waitForARPObserverInitialSync succeeded before initial ignore-set push completed")
	}
	if runner.arpObserverReady(spec.ResourceName) {
		t.Fatal("observer marked ready before initial ignore-set push completed")
	}
	pusher.setErr = nil
	if err := runner.waitForARPObserverInitialSync(context.Background(), spec); err != nil {
		t.Fatalf("waitForARPObserverInitialSync after push: %v", err)
	}
	if !runner.arpObserverReady(spec.ResourceName) {
		t.Fatal("observer not marked ready after initial ignore-set push completed")
	}
}

type fakeARPObserverCommandPusher struct {
	statuses []daemonapi.DaemonStatus
	sets     [][]string
	setErr   error
}

func (f *fakeARPObserverCommandPusher) Status(_ context.Context, _ string) (daemonapi.DaemonStatus, error) {
	if len(f.statuses) == 0 {
		return daemonapi.DaemonStatus{}, nil
	}
	status := f.statuses[0]
	f.statuses = f.statuses[1:]
	return status, nil
}

func (f *fakeARPObserverCommandPusher) SetIgnoredSenderMACs(_ context.Context, _ string, macs []string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.sets = append(f.sets, append([]string(nil), macs...))
	return nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
