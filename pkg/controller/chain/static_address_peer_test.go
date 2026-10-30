// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"net/netip"
	"path/filepath"
	"slices"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestStaticAddressPeerNativeDecode(t *testing.T) {
	for _, tt := range []struct {
		name, frame string
		valid       bool
	}{
		{"hex", "inet 198.51.100.1 --> 198.51.100.2 netmask 0xfffffffc\ninet 192.0.2.1 --> 198.51.100.2 netmask 0xffffff00", true},
		{"dotted", "inet 192.0.2.1 --> 198.51.100.2 netmask 255.255.255.0", true},
		{"cidr", "inet 192.0.2.1 --> 198.51.100.2/24", true},
		{"missing", "gif0: flags=8011<UP,POINTOPOINT> mtu 1400", false},
		{"ambiguous", "inet 198.51.100.1 --> 198.51.100.2 netmask 0xfffffffc\ninet 192.0.2.1 --> 198.51.100.9 netmask 0xffffff00", false},
		{"invalid-mask", "inet 192.0.2.1 --> 198.51.100.2 netmask 0xff00ff00", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			observed, err := parseFreeBSDStaticPeers([]byte(tt.frame))
			if !tt.valid {
				if err == nil {
					t.Fatalf("unusable peer observation was accepted: %+v", observed)
				}
				return
			}
			receipt := staticAddressReceipt{Address: "192.0.2.1/24", PeerAddress: "198.51.100.2"}
			if err != nil || observed.peerAddress() != receipt.PeerAddress || !observed.owns(receipt) {
				t.Fatalf("usable native P2P pair was lost: %+v %v", observed, err)
			}
		})
	}
}

func TestStaticAddressPeerAddCommand(t *testing.T) {
	receipt := staticAddressReceipt{IfName: "gif0", Address: "192.0.2.1/32", PeerAddress: "198.51.100.2"}
	name, args := staticAddressAddCommand(platform.OSFreeBSD, receipt)
	if name != "ifconfig" || !slices.Equal(args, []string{"gif0", "inet", "192.0.2.1/32", "198.51.100.2", "alias"}) {
		t.Fatalf("P2P alias omitted its observed peer or requested replacement: %s %v", name, args)
	}
	// Ordinary IPv4 aliases retain the existing OS command contract.
	receipt.PeerAddress = ""
	name, args = staticAddressAddCommand(platform.OSFreeBSD, receipt)
	if name != "ifconfig" || !slices.Equal(args, []string{"gif0", "inet", "192.0.2.1/32", "alias"}) {
		t.Fatalf("ordinary alias changed: %s %v", name, args)
	}
}

func TestStaticAddressPeerChangeDuringAcquisition(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	peer := netip.MustParseAddr("198.51.100.2")
	parent := netip.MustParsePrefix("198.51.100.1/30")
	store := &staticAddressFaultStore{SQLiteStore: db, afterPending: func() { peer = netip.MustParseAddr("198.51.100.9") }}
	writes := 0
	controller := IPv4StaticAddressController{Router: staticOwnerRouter(), Store: store,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			return staticAddressSnapshot{Index: 10, Scope: "fixture-kernel", Peer: peer, Addresses: []netip.Prefix{parent}, Peers: []staticAddressPeer{{Address: parent, Peer: peer}}}, nil
		},
		Command: func(context.Context, string, ...string) error { writes++; return nil },
	}
	if err := controller.Reconcile(t.Context()); err == nil || writes != 0 {
		t.Fatalf("peer changed during persistence, but acquisition ran: writes=%d err=%v", writes, err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil || status["nativeReceipt"] != nil || status["phase"] == "Applied" {
		t.Fatalf("aborted P2P acquisition retained authority: %+v %v", status, err)
	}
}

func TestStaticAddressPeerRestartPreservesForeignWithdrawal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	peer := netip.MustParseAddr("198.51.100.2")
	parent, child := netip.MustParsePrefix("198.51.100.1/30"), netip.MustParsePrefix("192.0.2.1/24")
	present := false
	controller := IPv4StaticAddressController{Router: staticOwnerRouter(), Store: db,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			snapshot := staticAddressSnapshot{Index: 10, Scope: "fixture-kernel", Peer: peer, Addresses: []netip.Prefix{parent}, Peers: []staticAddressPeer{{Address: parent, Peer: peer}}}
			if present {
				snapshot.Addresses = append(snapshot.Addresses, child)
				snapshot.Peers = append(snapshot.Peers, staticAddressPeer{Address: child, Peer: peer})
			}
			return snapshot, nil
		}, Command: func(context.Context, string, ...string) error { present = true; return nil },
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	receipt, decodeErr := staticReceipt(status)
	if err != nil || decodeErr != nil || receipt == nil || receipt.Pending || receipt.PeerAddress != peer.String() {
		t.Fatalf("restart lost the acquired native peer: %+v %v %v", receipt, err, decodeErr)
	}
	controller.Router = &api.Router{}
	controller.Store = &staticAddressFaultStore{SQLiteStore: db, afterPending: func() { peer = netip.MustParseAddr("198.51.100.9") }}
	writes := 0
	controller.Command = func(context.Context, string, ...string) error { writes++; present = false; return nil }
	if err := controller.Reconcile(t.Context()); err != nil || writes != 0 || !present {
		t.Fatalf("old receipt deleted the same local address with a foreign peer: present=%t writes=%d err=%v", present, writes, err)
	}
}
