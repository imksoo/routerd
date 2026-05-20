// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestCARPConfigRendersFreeBSDIfconfigCommands(t *testing.T) {
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
					Priority:        150,
					Preempt:         &preempt,
					AdvertInterval:  "2s",
					Authentication:  "secret",
				},
			},
		},
	}}}
	config, err := CARPConfig(router, map[string]string{"lan": "vtnet1"})
	if err != nil {
		t.Fatalf("render CARP config: %v", err)
	}
	if !config.Preempt || config.PreemptSysctlValue() != "1" {
		t.Fatalf("preempt not reflected in config: %#v", config)
	}
	wantCommands := [][]string{{
		"vtnet1", "inet", "vhid", "50", "advbase", "2", "advskew", "104", "pass", "secret", "alias", "10.240.70.10/32",
	}}
	if !reflect.DeepEqual(config.IfconfigCommands(), wantCommands) {
		t.Fatalf("commands = %#v, want %#v", config.IfconfigCommands(), wantCommands)
	}
	lines := strings.Join(config.RCConfLines(), "\n")
	if !strings.Contains(lines, `ifconfig_vtnet1_alias0="inet vhid 50 advbase 2 advskew 104 pass secret alias 10.240.70.10/32"`) {
		t.Fatalf("rc.conf lines missing CARP alias:\n%s", lines)
	}
}

func TestCARPConfigRendersIPv6IfconfigCommands(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "api-vip-v6"},
			Spec: api.VirtualAddressSpec{Family: "ipv6",
				Interface: "lan",
				Address:   "fd00:1234::10/128",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 51, Priority: 150},
			},
		},
	}}}
	config, err := CARPConfig(router, map[string]string{"lan": "vtnet1"})
	if err != nil {
		t.Fatalf("render CARP config: %v", err)
	}
	wantCommands := [][]string{{
		"vtnet1", "inet6", "vhid", "51", "advbase", "1", "advskew", "104", "alias", "fd00:1234::10/128",
	}}
	if !reflect.DeepEqual(config.IfconfigCommands(), wantCommands) {
		t.Fatalf("commands = %#v, want %#v", config.IfconfigCommands(), wantCommands)
	}
	lines := strings.Join(config.RCConfLines(), "\n")
	if !strings.Contains(lines, `ifconfig_vtnet1_alias0="inet6 vhid 51 advbase 1 advskew 104 alias fd00:1234::10/128"`) {
		t.Fatalf("rc.conf lines missing CARP IPv6 alias:\n%s", lines)
	}
}

func TestCARPConfigOverridesPriority(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Priority: 150},
			},
		},
	}}}
	config, err := CARPConfigWithOptions(router, map[string]string{"lan": "vtnet1"}, CARPOptions{PriorityByResource: map[string]int{"k8s-api": 80}})
	if err != nil {
		t.Fatalf("render CARP config: %v", err)
	}
	if got := config.Interfaces[0].AdvSkew; got != 174 {
		t.Fatalf("advskew = %d, want 174", got)
	}
}
