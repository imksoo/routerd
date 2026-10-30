// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func TestDnsmasqConfigDoesNotOwnStaticRouterAdvertisements(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcp"}, Spec: api.DHCPv4ServerSpec{Interface: "lan", LeaseFile: filepath.Join(dir, "state", "dnsmasq.leases"), AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.10", End: "192.0.2.20"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8:1::123/64"}},
	}}}
	path := filepath.Join(dir, "dnsmasq.conf")
	if _, _, err := writeDnsmasqConfig(router, mapStore{}, path, filepath.Join(dir, "dnsmasq.pid"), 0, nil); err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offline, _, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range [][]byte{live, offline} {
		if strings.Contains(string(output), "enable-ra") || strings.Contains(string(output), "ra-param=") || strings.Contains(string(output), "ra-only") {
			t.Fatalf("dnsmasq retained competing RA authority:\n%s", output)
		}
	}
}
