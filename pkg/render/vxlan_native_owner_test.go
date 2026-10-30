// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func TestFreeBSDVXLANTunnelDefaultRetainsNativeOwner(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "underlay", api.InterfaceSpec{IfName: "em0"}),
		netResource("VXLANTunnel", "overlay", api.VXLANTunnelSpec{IfName: "vx10", VNI: 10, LocalAddress: "192.0.2.1", Peers: []string{"192.0.2.2"}, UnderlayInterface: "underlay"}),
	}}}
	rendered, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("default FreeBSD tunnel rejected: %v", err)
	}
	if strings.Contains(string(rendered.RCConf), "ifconfig_vx10") || strings.Contains(string(rendered.RCConf), "vxlanid") {
		t.Fatal("boot renderer created native tunnel")
	}
	for name, body := range rendered.RCDScripts {
		if strings.Contains(name, "vxlan") || strings.Contains(string(body), "vxlanid") {
			t.Fatalf("duplicate VXLAN service: %s", name)
		}
	}
	spec, _ := router.Spec.Resources[1].VXLANTunnelSpec()
	spec.OuterDF = "inherit"
	router.Spec.Resources[1].Spec = spec
	if _, err := FreeBSD(router); err == nil || !strings.Contains(err.Error(), "outer DF") {
		t.Fatalf("unsupported explicit FreeBSD DF silently accepted: %v", err)
	}
}

func TestVXLANNetworkdPreservesNativeStateWithoutAnActivationOwner(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "underlay", api.InterfaceSpec{IfName: "eth0"}),
		netResource("VXLANTunnel", "overlay", api.VXLANTunnelSpec{IfName: "vx10", VNI: 10, LocalAddress: "192.0.2.1", Peers: []string{"192.0.2.2"}, UnderlayInterface: "underlay"}),
	}}}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatal(err)
	}
	file := findNetworkdTestFile(files, "31-routerd-vx10.network")
	if !strings.Contains(string(file.Data), "ActivationPolicy=manual") || !strings.Contains(string(file.Data), "KeepConfiguration=yes") {
		t.Fatalf("networkd may drive native tunnel state: %s", file.Data)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Path, ".netdev") || strings.Contains(string(file.Data), "[BridgeFDB]") || strings.Contains(string(file.Data), "Bridge=") {
			t.Fatalf("competing native writer: %s", file.Path)
		}
	}
}
