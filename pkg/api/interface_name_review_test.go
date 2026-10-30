// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"runtime"
	"testing"
)

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

func TestResolveInterfaceIfNameMatchesDSLiteNativeName(t *testing.T) {
	for _, spec := range []DSLiteTunnelSpec{{Interface: "wan", TunnelName: "ds-owned"}, {Interface: "wan"}, {}} {
		router := &Router{Spec: RouterSpec{Resources: []Resource{{TypeMeta: TypeMeta{APIVersion: NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: ObjectMeta{Name: "ds"}, Spec: spec}}}}
		name := spec.TunnelName
		if name == "" {
			name = spec.Interface
		}
		if name == "" {
			name = "ds"
		}
		if got := ResolveInterfaceIfName(router, "ds"); got != DSLiteRuntimeIfName(name, runtime.GOOS) {
			t.Fatalf("alias differs from actual owner: %q", got)
		}
	}
	if got := DSLiteRuntimeIfName("ds-owned", "freebsd"); got != "gif919" {
		t.Fatalf("FreeBSD native naming changed: %s", got)
	}
	if got := DSLiteRuntimeIfName("gif40", "freebsd"); got != "gif40" {
		t.Fatalf("explicit gif name changed: %s", got)
	}
}
