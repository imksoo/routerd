// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func unicastSourceRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "node"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.18.30.3/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "198.18.30.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 130, Priority: 100, Peers: []string{"198.18.30.2"}}}},
	}}}
}

func TestKeepalivedUnicastPinsUniqueNodeAddressInsteadOfSharedVIP(t *testing.T) {
	router := unicastSourceRouter()
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.IPv4StaticAddressSpec{Interface: "wan", Address: "198.18.31.3/24"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "conditional"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.18.30.9/32", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}}},
	)
	data, err := KeepalivedConfig(router, map[string]string{"lan": "eth0", "wan": "wan0"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "  unicast_src_ip 198.18.30.3\n") {
		t.Fatalf("unicast receive socket can bind the shared VIP instead of node address:\n%s", text)
	}
	if strings.Contains(text, "unicast_src_ip 198.18.30.1") {
		t.Fatal("shared VIP selected as election source")
	}
}

func TestKeepalivedUnicastDoesNotInventSourceWithoutUniqueStaticNodeAddress(t *testing.T) {
	for _, scenario := range []string{"none", "multiple", "multicast"} {
		t.Run(scenario, func(t *testing.T) {
			r := unicastSourceRouter()
			switch scenario {
			case "none":
				r.Spec.Resources = r.Spec.Resources[1:]
			case "multiple":
				r.Spec.Resources = append(r.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "second"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.18.30.4/24"}})
			case "multicast":
				spec, _ := r.Spec.Resources[1].VirtualAddressSpec()
				spec.VRRP.Peers = nil
				r.Spec.Resources[1].Spec = spec
			}
			data, err := KeepalivedConfig(r, map[string]string{"lan": "eth0"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "unicast_src_ip") {
				t.Fatalf("invented source for %s:\n%s", scenario, data)
			}
		})
	}
}

func TestKeepalivedUnicastSelectsPeerSubnetWithLegacyLANAddress(t *testing.T) {
	router := unicastSourceRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "legacy-lan"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.18.29.3/24"}})
	data, err := KeepalivedConfig(router, map[string]string{"lan": "eth0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "  unicast_src_ip 198.18.30.3\n") {
		t.Fatalf("legacy LAN subnet prevented election on the node address used by its peer:\n%s", data)
	}
}
