// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestKeepalivedConfigRendersVRRPInstance(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{
					VirtualRouterID: 50,
					Priority:        150,
					Peers:           []string{"10.240.70.3"},
					AdvertInterval:  "2s",
				},
			},
		},
	}}}
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens18"})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"vrrp_instance k8s_api",
		"interface ens18",
		"virtual_router_id 50",
		"priority 150",
		"advert_int 2",
		"nopreempt",
		"10.240.70.3",
		"10.240.70.10/32",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("keepalived config missing %q:\n%s", want, got)
		}
	}
}

func TestKeepalivedConfigRendersSingleOwnerFailoverVMAC(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw"},
		Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "172.18.0.1/32", Mode: "vrrp",
			VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Peers: []string{"172.18.0.3"}, FailoverVMAC: &api.VirtualAddressVRRPFailoverVMACSpec{
				ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13",
			}}},
	}}}}
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens19", "wan": "eth0"})
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
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens19", "wan": "eth0"})
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
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens19", "wan": "ens18"})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	args := "--resource lan-gw-v4 --deferred-address 172.18.0.1/32 --deferred-interface ens19"
	for _, state := range []string{"notify_master", "notify_backup", "notify_fault", "notify_stop"} {
		if !strings.Contains(got, state+" \"/usr/local/sbin/routerd-vrrp-vmac ") || !strings.Contains(got, args) {
			t.Fatalf("keepalived config missing graceful %s hook:\n%s", state, got)
		}
	}
	if !strings.Contains(got, args+" --parent ens18 --interface wan-vmac --mac 02:00:5e:00:01:13") {
		t.Fatalf("graceful hook lost single VMAC arguments:\n%s", got)
	}
	if !strings.Contains(got, "  no_virtual_ipaddress\n") {
		t.Fatalf("graceful activation must keep the VIP out of keepalived:\n%s", got)
	}
	if strings.Contains(got, "virtual_ipaddress {") {
		t.Fatalf("graceful activation must not let keepalived publish the VIP:\n%s", got)
	}
}

func TestKeepalivedConfigRendersIPv6VRRPInstance(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api-v6"},
			Spec: api.VirtualAddressSpec{Family: "ipv6",
				Interface: "lan",
				Address:   "fd00:1234::10/128",
				Mode:      "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{
					VirtualRouterID: 51,
					Priority:        140,
					Peers:           []string{"fd00:1234::3"},
				},
			},
		},
	}}}
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens18"})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"vrrp_instance k8s_api_v6",
		"family inet6",
		"virtual_router_id 51",
		"fd00:1234::3",
		"fd00:1234::10/128",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("keepalived config missing %q:\n%s", want, got)
		}
	}
}

func TestKeepalivedConfigOverridesPriority(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Priority: 150, Peers: []string{"10.240.70.3"}},
			},
		},
	}}}
	data, err := KeepalivedConfigWithOptions(router, map[string]string{"lan": "ens18"}, KeepalivedOptions{PriorityByResource: map[string]int{"k8s-api": 80}})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	if got := string(data); !strings.Contains(got, "priority 80") {
		t.Fatalf("keepalived config did not use overridden priority:\n%s", got)
	}
}

func TestKeepalivedConfigRendersPreemptDelay(t *testing.T) {
	preempt := true
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{
					VirtualRouterID: 50,
					Preempt:         &preempt,
					PreemptDelay:    "5m",
					Peers:           []string{"10.240.70.3"},
				},
			},
		},
	}}}
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens18"})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "preempt_delay 300") || strings.Contains(got, "nopreempt") {
		t.Fatalf("keepalived config did not render preempt_delay correctly:\n%s", got)
	}
}

func TestKeepalivedConfigResolvesAuthenticationFromEnv(t *testing.T) {
	t.Setenv("ROUTERD_TEST_VRRP_AUTH", "secret")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{
					VirtualRouterID:    50,
					Peers:              []string{"10.240.70.3"},
					AuthenticationFrom: api.SecretValueSourceSpec{Env: "ROUTERD_TEST_VRRP_AUTH"},
				},
			},
		},
	}}}
	data, err := KeepalivedConfig(router, map[string]string{"lan": "ens18"})
	if err != nil {
		t.Fatalf("render keepalived config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "auth_pass secret") {
		t.Fatalf("keepalived config did not include env auth_pass:\n%s", got)
	}
}
