// SPDX-License-Identifier: BSD-3-Clause

package api

import "testing"

func TestResolveInterfaceIfNameCoversVRFAndVXLANIntent(t *testing.T) {
	router := &Router{Spec: RouterSpec{Resources: []Resource{
		{TypeMeta: TypeMeta{APIVersion: NetAPIVersion, Kind: "VRF"}, Metadata: ObjectMeta{Name: "tenant"}, Spec: VRFSpec{IfName: "vrf42", RouteTable: 42}},
		{TypeMeta: TypeMeta{APIVersion: NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: ObjectMeta{Name: "stretch"}, Spec: VXLANTunnelSpec{IfName: "vx42", VNI: 42}},
	}}}
	for name, want := range map[string]string{"tenant": "vrf42", "stretch": "vx42", "eth0": "eth0"} {
		if got := ResolveInterfaceIfName(router, name); got != want {
			t.Fatalf("%s resolved %q instead of native %q", name, got, want)
		}
	}
}
