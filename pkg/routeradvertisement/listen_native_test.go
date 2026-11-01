// SPDX-License-Identifier: BSD-3-Clause
//go:build (linux || freebsd) && routerd_native

package routeradvertisement

import (
	"bytes"
	"crypto/rand"
	"net"
	"net/netip"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/mdlayher/ndp"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

// The test sends an RS only to loopback. No RA is sent and no host interface,
// address or route is configured. Enable with -tags routerd_native in the
// privileged native gate; permission failures remain test failures there.
func TestSolicitationWildcardReceiverOnActualSocket(t *testing.T) {
	name := "lo"
	if runtime.GOOS == "freebsd" {
		name = "lo0"
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := listenSolicitations(ifi)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	// ::1 is distinct from the link-local source used for RA transmissions.
	// Reception of this destination exercises the actual wildcard listener.
	source := netip.IPv6Loopback()
	sender, err := icmp.ListenPacket("ip6:ipv6-icmp", source.String())
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	conn := sender.IPv6PacketConn()
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetChecksum(true, 2); err != nil {
		t.Fatal(err)
	}
	reserved := make([]byte, 4)
	if _, err := rand.Read(reserved); err != nil {
		t.Fatal(err)
	}
	packet, err := (&icmp.Message{Type: ipv6.ICMPTypeRouterSolicitation, Body: &icmp.RawBody{Data: reserved}}).Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.WriteTo(packet, &ipv6.ControlMessage{IfIndex: ifi.Index, Src: source.AsSlice(), HopLimit: 255}, &net.IPAddr{IP: source.AsSlice()}); err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 1500)
	for {
		n, cm, from, err := receiver.ReadRaw(frame)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(packet) || !bytes.Equal(frame[4:n], reserved) {
			continue
		}
		if cm == nil {
			t.Fatal("actual wildcard receive omitted control metadata")
		}
		destination, ok := netip.AddrFromSlice(cm.Dst)
		if !ok || destination != source || from.WithZone("") != source || !ValidSolicitation(frame[:n], cm, from, ifi.Index) {
			t.Fatalf("actual wildcard receive lost RS identity: source=%s control=%+v packet=%x", from, cm, frame[:n])
		}
		return
	}
}

// TestAdvertisementWriteAcrossVeth verifies the actual multicast transmit path.
// It requires the caller to create the veth pair inside an isolated netns.
func TestAdvertisementWriteAcrossVeth(t *testing.T) {
	routerName, peerName := os.Getenv("ROUTERD_RA_NATIVE_ROUTER_IF"), os.Getenv("ROUTERD_RA_NATIVE_PEER_IF")
	if routerName == "" || peerName == "" {
		t.Skip("set ROUTERD_RA_NATIVE_ROUTER_IF and ROUTERD_RA_NATIVE_PEER_IF in an isolated netns")
	}
	router, err := Listen(routerName)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Conn.Close()
	peer, err := net.InterfaceByName(peerName)
	if err != nil {
		t.Fatal(err)
	}
	receiver, _, err := ndp.Listen(peer, ndp.Unspecified)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	if err := receiver.SetControlMessage(ipv6.FlagHopLimit, true); err != nil {
		t.Fatal(err)
	}
	if err := receiver.JoinGroup(allNodes); err != nil {
		t.Fatal(err)
	}
	if err := receiver.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := router.Write(&ndp.RouterAdvertisement{RouterLifetime: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	for {
		message, control, source, err := receiver.ReadFrom()
		if err != nil {
			t.Fatal(err)
		}
		advertisement, ok := message.(*ndp.RouterAdvertisement)
		if !ok {
			continue
		}
		if advertisement.RouterLifetime != 30*time.Second || control == nil || control.HopLimit != 255 || !source.IsLinkLocalUnicast() {
			t.Fatalf("RA envelope=%+v control=%+v source=%s", advertisement, control, source)
		}
		return
	}
}
