// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestKeepalivedConfigRendersSingleOwnerFailoverVMAC(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw"},
		Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "172.18.0.1/32", Mode: "vrrp",
			VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Peers: []string{"172.18.0.3"}, FailoverVMAC: &api.VirtualAddressVRRPFailoverVMACSpec{
				ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13",
			}}},
	}}}}
	data, err := KeepalivedConfigWithOptions(router, map[string]string{"lan": "ens19", "wan": "eth0"}, KeepalivedOptions{})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"vrrp_instance lan_gw",
		"notify_master \"/usr/local/sbin/routerd-vrrp-vmac activate --parent eth0 --interface wan-vmac --mac 02:00:5e:00:01:13\"",
		"notify_backup \"/usr/local/sbin/routerd-vrrp-vmac deactivate --parent eth0 --interface wan-vmac --mac 02:00:5e:00:01:13\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("keepalived config missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "vrrp_instance ") != 1 {
		t.Fatalf("expected one VRRP state machine:\n%s", got)
	}
}

func TestKeepalivedConfigRendersWANAndLANFailoverVMACsInOneVRRPTransition(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw"},
		Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "172.18.0.1/32", Mode: "vrrp",
			VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Peers: []string{"172.18.0.3"},
				FailoverVMAC: &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13"},
				AdditionalFailoverVMACs: []api.VirtualAddressVRRPFailoverVMACSpec{{
					ParentInterface: "lan", Interface: "lan-vrrp", MACAddress: "02:00:5e:00:01:12", LinkLocalAddress: "fe80::5eff:fe00:112", WithdrawRouterAdvertisement: true,
				}},
			},
		},
	}}}}
	data, err := KeepalivedConfigWithOptions(router, map[string]string{"lan": "ens19", "wan": "eth0"}, KeepalivedOptions{})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	wantArgs := "--vmac eth0,wan-vmac,02:00:5e:00:01:13,,false --vmac ens19,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true"
	for _, state := range []string{"notify_master \"/usr/local/sbin/routerd-vrrp-vmac activate ", "notify_backup \"/usr/local/sbin/routerd-vrrp-vmac deactivate ", "notify_fault \"/usr/local/sbin/routerd-vrrp-vmac deactivate ", "notify_stop \"/usr/local/sbin/routerd-vrrp-vmac deactivate "} {
		if !strings.Contains(got, state+wantArgs+"\"") {
			t.Fatalf("keepalived config missing %q:\n%s", state+wantArgs, got)
		}
	}
	if strings.Count(got, "vrrp_instance ") != 1 {
		t.Fatalf("WAN and LAN must share one VRRP state machine:\n%s", got)
	}
}

func TestKeepalivedConfigDefersGracefulVRRPAddressUntilRouterdReadiness(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw-v4"},
		Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "172.18.0.1/32", Mode: "vrrp",
			VRRP: api.VirtualAddressVRRPSpec{
				VirtualRouterID: 18,
				Peers:           []string{"172.18.0.3"},
				FailoverVMAC:    &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13"},
				GracefulActivation: &api.VirtualAddressVRRPGracefulActivationSpec{ReadyWhen: api.ResourceWhenSpec{
					State: map[string]api.StateMatchSpec{"DSLiteTunnel/dslite-a.phase": {Equals: "Up"}},
				}},
			},
		},
	}}}}
	data, err := KeepalivedConfigWithOptions(router, map[string]string{"lan": "ens19", "wan": "ens18"}, KeepalivedOptions{})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	args := "--resource lan-gw-v4 --deferred-address 172.18.0.1/32 --deferred-interface ens19"
	for _, state := range []string{"notify_master", "notify_backup", "notify_fault", "notify_stop"} {
		matches := 0
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), state+" ") {
				matches++
				if !strings.Contains(line, state+" \"/usr/local/sbin/routerd-vrrp-vmac ") || !strings.Contains(line, args+" --parent ens18 --interface wan-vmac --mac 02:00:5e:00:01:13") {
					t.Fatalf("incomplete graceful %s hook: %s", state, line)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("graceful %s hook count = %d", state, matches)
		}
	}
	if !strings.Contains(got, "  no_virtual_ipaddress\n") {
		t.Fatalf("graceful activation must keep the VIP out of keepalived:\n%s", got)
	}
	if strings.Contains(got, "virtual_ipaddress {") {
		t.Fatalf("graceful activation must not let keepalived publish the VIP:\n%s", got)
	}
}
