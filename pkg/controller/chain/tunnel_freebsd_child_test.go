// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestTunnelFreeBSDParentPreservesAcquiredChildAddress(t *testing.T) {
	for _, tt := range []struct{ mode, name string }{{"ipip", "gif0"}, {"gre", "gre0"}} {
		t.Run(tt.mode, func(t *testing.T) {
			spec := api.TunnelInterfaceSpec{Mode: tt.mode, Local: "198.18.89.1", Remote: "198.18.89.2", Address: "10.99.0.1/30", PeerAddress: "10.99.0.2", MTU: 1400, TrustedUnderlay: true}
			if tt.mode == "gre" {
				spec.Key = 42
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: tt.name}, Spec: spec},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "inside"}, Spec: api.IPv4StaticAddressSpec{Interface: tt.name, Address: "192.0.2.99/32"}},
			}}}
			store := mapStore{api.HybridAPIVersion + "/TunnelInterface/" + tt.name: {"phase": "Up", "interfaceOwned": true, "address": spec.Address, "peerAddress": spec.PeerAddress, "key": spec.Key, "observedAddress": spec.Address, "observedPeerAddress": spec.PeerAddress}}
			child := IPv4StaticAddressController{Store: store}
			if err := child.saveStaticAddress("inside", map[string]any{"phase": "Applied"}, &staticAddressReceipt{IfName: tt.name, IfIndex: 7, Scope: "freebsd:fixture", Address: "192.0.2.99/32"}); err != nil {
				t.Fatal(err)
			}
			childPresent := true
			var effects [][]string
			parent := TunnelInterfaceController{Router: router, Store: store, OS: platform.OSFreeBSD,
				Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
					if name == "ifconfig" && slices.Equal(args, []string{tt.name}) {
						output := fmt.Sprintf("%s: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> metric 0 mtu 1400\n\ttunnel inet 198.18.89.1 --> 198.18.89.2\n\tinet 10.99.0.1 --> 10.99.0.2 netmask 0xfffffffc\n", tt.name)
						if childPresent {
							output += "\tinet 192.0.2.99 --> 0.0.0.0 netmask 0xffffffff\n"
						}
						return []byte(output), nil
					}
					effects = append(effects, append([]string{name}, args...))
					if name == "ifconfig" && slices.Equal(args, []string{tt.name, "inet", "192.0.2.99/32", "-alias"}) {
						childPresent = false
					}
					return nil, nil
				},
			}
			if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !childPresent {
				t.Fatalf("unchanged FreeBSD %s parent removed its acknowledged child address: %v", tt.mode, effects)
			}
		})
	}
}
