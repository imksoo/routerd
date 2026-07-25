// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestRunnerMobilityARPObserverDaemonSpecsFromOnPremL2Sources(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.EventGroupSpec{NodeName: "pve-rt08"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "svnet1"}, Spec: api.InterfaceSpec{IfName: "eth1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "svnet1"}, Spec: api.MobilityPoolSpec{
			Prefix:   "192.168.123.0/24",
			GroupRef: "home",
			Members: []api.MobilityPoolMember{
				{NodeRef: "pve-rt01", Site: "pve01", Role: "onprem"},
				{
					NodeRef: "pve-rt08",
					Site:    "pve08",
					Role:    "onprem",
					Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "svnet1"},
					OwnershipDiscovery: api.MobilityOwnershipDiscovery{
						Mode: "onprem-l2",
						Sources: []api.MobilityOwnershipDiscoverySource{
							{Type: "arp-observer", Interface: "svnet1"},
							{Type: "on-demand-arp", Interface: "svnet1", ProbeTimeout: "500ms", ProbeRetries: 2, ScanInterval: "1s", SourceAddressFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/svnet1-source", Field: "currentAddress"}},
							{Type: "pve-svnet", Interface: "svnet1", Network: "svnet1", Bridge: "vmbr123", ScanInterval: "3s"},
						},
					},
				},
			},
		}},
	}}}
	store := mapStore{api.NetAPIVersion + "/DHCPv4Client/svnet1-source": {"currentAddress": "192.168.123.134/24"}}
	runner := Runner{Router: router, Store: store}
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

func TestRunnerMobilityARPObserverDaemonSpecsIncludeSAMNodeSetMemberMACs(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.EventGroupSpec{NodeName: "pve-leaf-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "svnet1"}, Spec: api.InterfaceSpec{IfName: "eth1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
			{NodeRef: "pve-leaf-a", Site: "pve", Role: "onprem", MACAddresses: []string{"02:00:00:00:00:aa"}},
			{NodeRef: "aws-leaf-a", Site: "aws", Role: "cloud", MACAddresses: []string{"02:00:00:00:00:BB", "02:00:00:00:00:cc"}},
		}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "svnet1"}, Spec: api.MobilityPoolSpec{
			Prefix:   "192.168.123.0/24",
			GroupRef: "home",
			Members: []api.MobilityPoolMember{
				{
					NodeRef: "pve-leaf-a",
					Site:    "pve",
					Role:    "onprem",
					Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "svnet1"},
					OwnershipDiscovery: api.MobilityOwnershipDiscovery{
						Mode:    "onprem-l2",
						Sources: []api.MobilityOwnershipDiscoverySource{{Type: "arp-observer", Interface: "svnet1"}},
					},
				},
			},
		}},
	}}}
	runner := Runner{Router: router}
	specs := runner.mobilityARPObserverDaemonSpecs()
	if len(specs) != 1 {
		t.Fatalf("daemon specs = %#v, want one arp-observer spec", specs)
	}
	want := []string{"02:00:00:00:00:aa", "02:00:00:00:00:bb", "02:00:00:00:00:cc"}
	if got := specs[0].IgnoredSenderMACs; !stringSlicesEqual(got, want) {
		t.Fatalf("IgnoredSenderMACs = %#v, want %#v", got, want)
	}
}

func TestARPObserverDaemonArgsDoNotExposeIgnoredSenderMACFlag(t *testing.T) {
	spec := mobilityARPObserverDaemonSpec{
		ResourceName:      "mobility-svnet1-pve-arp-observer-eth1",
		PoolName:          "svnet1",
		Prefix:            "192.168.123.0/24",
		SourceType:        "arp-observer",
		IfName:            "eth1",
		EventInterface:    "eth1",
		Socket:            "/run/routerd/arp-observer/mobility-svnet1-pve-arp-observer-eth1.sock",
		EventFile:         "/var/lib/routerd/arp-observer/mobility-svnet1-pve-arp-observer-eth1/events.jsonl",
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

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.EventGroupSpec{NodeName: "pve-leaf-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "capture"}, Spec: api.InterfaceSpec{IfName: "eth1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "cloudedge"}, Spec: api.MobilityPoolSpec{
			Prefix:   "10.77.60.0/24",
			GroupRef: "home",
			Members: []api.MobilityPoolMember{{
				NodeRef: "pve-leaf-a",
				Site:    "pve",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "capture"},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode: "onprem-l2",
					Sources: []api.MobilityOwnershipDiscoverySource{
						{Type: "arp-observer", Interface: "capture"},
						{Type: "on-demand-arp", Interface: "capture", ScanInterval: "1s"},
					},
				},
			}},
		}},
	}}}
	runner := &Runner{Router: router}
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

	runner.Router = &api.Router{}
	runner.reconcileSupervisedDaemonSpecs(ctx, nil, runner.clientDaemonSpecs(runner.Router))
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
		ResourceName:      "mobility-svnet1-pve-arp-observer-eth1",
		Socket:            "/run/routerd/arp-observer/mobility-svnet1-pve-arp-observer-eth1.sock",
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
		ResourceName: "mobility-svnet1-pve-arp-observer-eth1",
		Socket:       "/run/routerd/arp-observer/mobility-svnet1-pve-arp-observer-eth1.sock",
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
		ResourceName: "mobility-svnet1-pve-arp-observer-eth1",
		Socket:       "/run/routerd/arp-observer/mobility-svnet1-pve-arp-observer-eth1.sock",
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
		ResourceName:      "mobility-svnet1-pve-arp-observer-eth1",
		Socket:            "/run/routerd/arp-observer/mobility-svnet1-pve-arp-observer-eth1.sock",
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
