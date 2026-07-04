// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
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
