// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestValidateVXLANTCPMSSClampRequiresExplicitSafeMTU(t *testing.T) {
	for _, mtu := range []int{0, 1279} {
		r := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "eth0", MTU: 1500}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 42, LocalAddress: "198.18.0.1", UnderlayInterface: "underlay", Bridge: "br", MTU: mtu, TCPMSSClamp: true}},
		}}}
		err := Validate(r)
		if err == nil || !strings.Contains(err.Error(), "explicitly set to at least 1280") {
			t.Fatalf("mtu=%d error=%v", mtu, err)
		}
	}
}
