// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package routeradvertisement

import (
	"bytes"
	"crypto/rand"
	"net"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

// The test sends an RS only to loopback. No RA is sent and no host interface,
// address or route is configured. Permission failures remain test failures.
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
