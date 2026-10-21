// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Requirement: live and offline generation must agree, and a stable generation
// must not request another dnsmasq restart. All writes remain in t.TempDir.
func TestDnsmasqIPv4OnlyListenerConfig(t *testing.T) {
	for _, tc := range []struct {
		name            string
		shared, standby bool
	}{
		{name: "split families"}, {name: "shared physical interface", shared: true}, {name: "standby services suppressed", standby: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v6Name := "lan-vrrp"
			if tc.shared {
				v6Name = "ens19"
			}
			r := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.InterfaceSpec{IfName: v6Name}},
			}}}
			if !tc.standby {
				r.Spec.Resources = append(r.Spec.Resources,
					api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "v4"}, Spec: api.DHCPv4ServerSpec{Interface: "lan", AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}}},
					api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.DHCPv6ServerSpec{Interface: "v6", Mode: "stateless", DNSSource: "static", DNSServers: []string{"fd00:1234::1"}}},
					api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "v6", ValidLifetime: "7200"}},
				)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "dnsmasq.conf")
			pid := filepath.Join(dir, "dnsmasq.pid")
			changed, _, err := writeDnsmasqConfig(r, mapStore{}, path, pid, 0, nil)
			if err != nil || !changed {
				t.Fatalf("initial write: changed=%v err=%v", changed, err)
			}
			live, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			offline, _, err := render.DnsmasqConfig(r, render.DnsmasqRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"live": live, "offline": offline} {
				n := strings.Count(string(data), "no-dhcpv6-interface=ens19\n")
				want := 0
				if !tc.shared && !tc.standby {
					want = 1
				}
				if n != want || strings.Contains(string(data), "no-dhcpv6-interface=lan-vrrp") {
					t.Fatalf("%s: wrong exclusions: %s", name, data)
				}
			}
			changed, reload, err := writeDnsmasqConfig(r, mapStore{}, path, pid, 0, nil)
			if err != nil || changed || reload {
				t.Fatalf("stable config requested change: %v %v %v", changed, reload, err)
			}
		})
	}
}
