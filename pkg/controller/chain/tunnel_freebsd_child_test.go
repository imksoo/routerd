// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestTunnelFreeBSDParentPreservesAcquiredChildAddress(t *testing.T) {
	for _, tt := range []struct{ mode, name, scenario string }{
		{"ipip", "gif0", "unchanged"}, {"gre", "gre0", "unchanged"},
		{"ipip", "gif0", "first-address"}, {"ipip", "gif0", "change-address"},
		{"ipip", "gif0", "clear-address"}, {"ipip", "gif0", "delete-error"},
	} {
		t.Run(tt.mode+"/"+tt.scenario, func(t *testing.T) {
			spec := api.TunnelInterfaceSpec{Mode: tt.mode, Local: "198.18.89.1", Remote: "198.18.89.2", Address: "10.99.0.1/30", PeerAddress: "10.99.0.2", MTU: 1400, TrustedUnderlay: true}
			if tt.mode == "gre" {
				spec.Key = 42
			}
			oldAddress, oldPeer := spec.Address, spec.PeerAddress
			addresses := map[string]string{oldAddress: oldPeer, "192.0.2.99/32": "0.0.0.0"}
			previous := map[string]any{"phase": "Up", "interfaceOwned": true, "address": oldAddress, "peerAddress": oldPeer, "key": spec.Key, "observedAddress": oldAddress, "observedPeerAddress": oldPeer}
			switch tt.scenario {
			case "first-address":
				delete(addresses, oldAddress)
				previous["phase"] = "Pending"
				delete(previous, "observedAddress")
				delete(previous, "observedPeerAddress")
			case "change-address", "delete-error":
				spec.Address, spec.PeerAddress = "10.100.0.1/30", "10.100.0.2"
			case "clear-address":
				spec.Address, spec.PeerAddress = "", ""
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: tt.name}, Spec: spec},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "inside"}, Spec: api.IPv4StaticAddressSpec{Interface: tt.name, Address: "192.0.2.99/32"}},
			}}}
			store := mapStore{api.HybridAPIVersion + "/TunnelInterface/" + tt.name: previous}
			child := IPv4StaticAddressController{Store: store}
			if err := child.saveStaticAddress("inside", map[string]any{"phase": "Applied"}, &staticAddressReceipt{IfName: tt.name, IfIndex: 7, Scope: "freebsd:fixture", Address: "192.0.2.99/32"}); err != nil {
				t.Fatal(err)
			}
			failDelete := tt.scenario == "delete-error"
			implicitReplacement := false
			var effects [][]string
			parent := TunnelInterfaceController{Router: router, Store: store, OS: platform.OSFreeBSD,
				Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
					if name == "ifconfig" && slices.Equal(args, []string{tt.name}) {
						output := fmt.Sprintf("%s: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> metric 0 mtu 1400\n\ttunnel inet 198.18.89.1 --> 198.18.89.2\n", tt.name)
						for address, peer := range addresses {
							mask := "0xfffffffc"
							if strings.HasSuffix(address, "/32") {
								mask = "0xffffffff"
							}
							output += fmt.Sprintf("\tinet %s --> %s netmask %s\n", strings.Split(address, "/")[0], peer, mask)
						}
						return []byte(output), nil
					}
					effects = append(effects, append([]string{name}, args...))
					if name == "ifconfig" && len(args) >= 4 && args[0] == tt.name && args[1] == "inet" {
						if args[3] == "-alias" {
							if failDelete && args[2] == oldAddress {
								failDelete = false
								return nil, errors.New("injected parent address delete failure")
							}
							delete(addresses, args[2])
						} else {
							if len(args) != 5 || args[4] != "alias" {
								implicitReplacement = true
							}
							addresses[args[2]] = args[3]
						}
					}
					return nil, nil
				},
			}
			if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if tt.scenario == "delete-error" {
				status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", tt.name)
				if status["phase"] != "Error" || status["observedAddress"] != oldAddress || status["observedPeerAddress"] != oldPeer {
					t.Fatalf("failed parent withdrawal lost its confirmed address: %+v", status)
				}
				if err := parent.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if _, present := addresses["192.0.2.99/32"]; !present {
				t.Fatalf("FreeBSD %s parent removed its acknowledged child address: %v", tt.mode, effects)
			}
			if implicitReplacement {
				t.Fatalf("parent address update used implicit first-address replacement: %v", effects)
			}
			if spec.Address != oldAddress {
				if _, present := addresses[oldAddress]; present {
					t.Fatalf("old parent address survived its configuration change: %v", addresses)
				}
			}
			if spec.Address != "" {
				if peer, present := addresses[spec.Address]; !present || peer != spec.PeerAddress {
					t.Fatalf("parent address was not applied: %v", addresses)
				}
			}
			status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", tt.name)
			if status["phase"] != "Up" {
				t.Fatalf("parent failed: %+v", status)
			}
			if tt.scenario == "unchanged" && len(effects) != 0 {
				t.Fatalf("unchanged parent mutated addresses: %v", effects)
			}
			if tt.scenario == "clear-address" {
				if status["address"] != nil || status["observedAddress"] != nil || status["observedPeerAddress"] != nil {
					t.Fatalf("successful clear retained address provenance: %+v", status)
				}
				before := len(effects)
				if err := parent.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				if len(effects) != before {
					t.Fatalf("cleared parent repeated address mutation: %v", effects[before:])
				}
			}
		})
	}
}
