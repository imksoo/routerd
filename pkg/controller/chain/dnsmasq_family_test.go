// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// Exercise the file writer used by apply and continuous reconciliation, with
// temporary data paths. This proves selection and stable regeneration, not
// live packet forwarding or daemon restart behavior.
func TestWriteDnsmasqConfigIPv6OnlyListener(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		shared, relay, pendingV4 bool
		exclude                  bool
	}{
		{name: "split DHCP families", exclude: true},
		{name: "shared interface alias", shared: true},
		{name: "IPv4 relay on IPv6 interface", relay: true},
		{name: "pending IPv4 scope on shared interface", shared: true, pendingV4: true, exclude: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			v4name := "lan-parent"
			if tc.shared {
				v4name = "lan-vrrp"
			}
			v4 := api.DHCPv4ServerSpec{Interface: "lan", AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}, LeaseFile: filepath.Join(dir, "leases")}
			if tc.pendingV4 {
				v4.GatewayFrom = api.StatusValueSourceSpec{Resource: "VirtualAddress/not-ready", Field: "address"}
			}
			resources := []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: v4name}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.InterfaceSpec{IfName: "lan-vrrp"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "v4"}, Spec: v4},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.DHCPv6ServerSpec{Interface: "v6", Mode: "stateless"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "v6", MTU: 1454}},
			}
			if tc.relay {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"v6"}, Upstream: "192.0.2.1"}})
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: resources}}
			path := filepath.Join(dir, "dnsmasq.conf")
			pid := filepath.Join(dir, "dnsmasq.pid")
			if _, _, err := writeDnsmasqConfig(router, mapStore{}, path, pid, 0, nil); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			want := 0
			if tc.exclude {
				want = 1
			}
			if count := strings.Count(got, "no-dhcpv4-interface=lan-vrrp\n"); count != want {
				t.Fatalf("IPv4 exclusion count = %d, want %d:\n%s", count, want, got)
			}
			for _, forbidden := range []string{"no-dhcp-interface=", "no-dhcpv6-interface=", "no-dhcpv4-interface=lan-parent"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("unexpected exclusion %q:\n%s", forbidden, got)
				}
			}
			if !strings.Contains(got, "constructor:lan-vrrp,ra-stateless") || !strings.Contains(got, "ra-param=lan-vrrp,mtu:1454,0") {
				t.Fatalf("IPv6 service was lost:\n%s", got)
			}
			changed, reload, err := writeDnsmasqConfig(router, mapStore{}, path, pid, 0, nil)
			if err != nil || changed || reload {
				t.Fatalf("unchanged write: changed=%v reload=%v err=%v", changed, reload, err)
			}
		})
	}
}
