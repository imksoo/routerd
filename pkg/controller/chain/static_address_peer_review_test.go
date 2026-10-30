// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestStaticPeerReceiptReplacementSurvivesPendingAbort(t *testing.T) {
	for _, operation := range []string{"acquire", "withdraw"} {
		for _, change := range []string{"peer changed", "cancelled"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				peer := netip.MustParseAddr("198.51.100.2")
				parent := netip.MustParsePrefix("198.51.100.1/30")
				child := netip.MustParsePrefix("192.0.2.1/24")
				present := operation == "withdraw"
				original := staticAddressReceipt{IfName: "test0", IfIndex: 10, Scope: "fixture-kernel", Address: child.String(), PeerAddress: peer.String()}
				if present {
					if err := db.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address", map[string]any{"phase": "Applied", "nativeReceipt": original}); err != nil {
						t.Fatal(err)
					}
				}
				replacement := original
				replacement.PeerAddress = "198.51.100.9"
				injected := false
				store := &staticAddressFaultStore{SQLiteStore: db}
				store.afterPending = func() {
					store.afterPending = nil
					injected = true
					// A different invocation owns the same local address with a
					// different peer before the old pending write returns.
					peer = netip.MustParseAddr(replacement.PeerAddress)
					present = true
					if err := db.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address", map[string]any{"phase": "Applied", "nativeReceipt": replacement}); err != nil {
						t.Fatal(err)
					}
					if change == "cancelled" {
						cancel()
					}
				}
				writes := 0
				controller := IPv4StaticAddressController{Router: staticOwnerRouter(), Store: store,
					ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
						snapshot := staticAddressSnapshot{Index: 10, Scope: "fixture-kernel", Peer: peer, Addresses: []netip.Prefix{parent}, Peers: []staticAddressPeer{{Address: parent, Peer: peer}}}
						if present {
							snapshot.Addresses = append(snapshot.Addresses, child)
							snapshot.Peers = append(snapshot.Peers, staticAddressPeer{Address: child, Peer: peer})
						}
						return snapshot, nil
					},
					Command: func(context.Context, string, ...string) error { writes++; return nil },
				}
				if operation == "withdraw" {
					controller.Router = &api.Router{}
				}
				reconcileErr := controller.Reconcile(ctx)
				status, readErr := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
				actual, decodeErr := staticReceipt(status)
				if !injected || reconcileErr == nil || writes != 0 || !present || status["phase"] != "Applied" || readErr != nil || decodeErr != nil || actual == nil || *actual != replacement {
					t.Fatalf("stale operation replaced another peer receipt: injected=%v err=%v writes=%d nativePresent=%v actual=%+v expected=%+v readErr=%v decodeErr=%v", injected, reconcileErr, writes, present, actual, replacement, readErr, decodeErr)
				}
			})
		}
	}
}
