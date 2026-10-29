// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNetworkdDropinsAttachToGeneratedVirtualNetwork(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "wan", api.InterfaceSpec{IfName: "eth0"}),
		netResource("VXLANTunnel", "overlay", api.VXLANTunnelSpec{IfName: "vx10", VNI: 10, LocalAddress: "192.0.2.1", Peers: []string{"192.0.2.2"}}),
		netResource("Bridge", "lan", api.BridgeSpec{IfName: "br0", Members: []string{"overlay", "wan"}}),
		netResource("IPv6StaticRoute", "route", api.IPv6StaticRouteSpec{Interface: "lan", Destination: "2001:db8:1::/64", Via: "fe80::1"}),
		netResource("NTPClient", "ntp", api.NTPClientSpec{Interface: "overlay", Managed: true, Servers: []string{"192.0.2.123"}}),
	}}}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]bool{"10-netplan-eth0.network": true}
	for _, file := range files {
		if strings.HasSuffix(file.Path, ".network") {
			base[filepath.Base(file.Path)] = true
		}
	}
	for _, file := range files {
		dir := filepath.Base(filepath.Dir(file.Path))
		if strings.HasSuffix(dir, ".network.d") && !base[strings.TrimSuffix(dir, ".d")] {
			t.Fatalf("drop-in %s has no corresponding generated base network", file.Path)
		}
	}
}
