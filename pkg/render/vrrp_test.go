// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestKeepalivedConfigRendersVRRPInstance(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP: api.VirtualIPv4VRRPSpec{
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

func TestKeepalivedConfigRendersIPv6VRRPInstance(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv6Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api-v6"},
			Spec: api.VirtualIPv6AddressSpec{
				Interface: "lan",
				Address:   "fd00:1234::10/128",
				Mode:      "vrrp",
				VRRP: api.VirtualIPv6VRRPSpec{
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
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10",
				Mode:      "vrrp",
				VRRP:      api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Priority: 150, Peers: []string{"10.240.70.3"}},
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
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP: api.VirtualIPv4VRRPSpec{
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
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP: api.VirtualIPv4VRRPSpec{
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
