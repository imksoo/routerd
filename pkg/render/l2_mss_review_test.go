// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNftablesL2TCPMSSClampRejectsImplicitMTU(t *testing.T) {
	r := l2MSSReviewRouter(0)
	_, err := NftablesL2TCPMSSClamp(r)
	if err == nil || !strings.Contains(err.Error(), "explicit mtu") {
		t.Fatalf("error=%v, want explicit mtu validation", err)
	}
}

func TestNftablesL2TCPMSSClampOwnedUsesPrivateIdentityAndNoFlush(t *testing.T) {
	r := l2MSSReviewRouter(1280)
	b, err := NftablesL2TCPMSSClampOwned(r, "routerd_l2_abc", "rules_abc", "forward_abc", "private-token")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"create table bridge routerd_l2_abc", "create chain bridge routerd_l2_abc forward_abc", NftablesL2MSSPrivateProofMarker + "private-token"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "flush table") || strings.Contains(s, "delete table") {
		t.Fatalf("unsafe table mutation:\n%s", s)
	}
}

func l2MSSReviewRouter(mtu int) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "eth0", MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 42, LocalAddress: "198.18.0.1", UnderlayInterface: "underlay", Bridge: "br", MTU: mtu, TCPMSSClamp: true}},
	}}}
}
