// SPDX-License-Identifier: BSD-3-Clause

package routeradvertisement

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mdlayher/ndp"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

func solicitation(t *testing.T, source netip.Addr, code int, body []byte) []byte {
	t.Helper()
	message := icmp.Message{Type: ipv6.ICMPTypeRouterSolicitation, Code: code, Body: &icmp.RawBody{Data: body}}
	packet, err := message.Marshal(icmp.IPv6PseudoHeader(source.AsSlice(), allRouters.AsSlice()))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestSolicitationValidationPreservesRFCOptionalInput(t *testing.T) {
	source := netip.MustParseAddr("2001:db8::2")
	cm := &ipv6.ControlMessage{HopLimit: 255, IfIndex: 7, Dst: allRouters.AsSlice()}
	// Nonzero reserved bytes and an option not defined for RS are ignored.
	packet := solicitation(t, source, 0, []byte{1, 2, 3, 4, 3, 1, 0, 0, 0, 0, 0, 0})
	if !ValidSolicitation(packet, cm, source, 7) {
		t.Fatal("valid global unicast RS with unrelated optional content discarded")
	}
	cm.HopLimit = 254
	if ValidSolicitation(packet, cm, source, 7) {
		t.Fatal("forwarded RS accepted")
	}
	cm.HopLimit = 255
	packet[2] ^= 1
	if ValidSolicitation(packet, cm, source, 7) {
		t.Fatal("bad checksum accepted")
	}
	if ValidSolicitation(solicitation(t, source, 1, make([]byte, 4)), cm, source, 7) {
		t.Fatal("nonzero ICMP code accepted")
	}
	unspecified := netip.IPv6Unspecified()
	if !ValidSolicitation(solicitation(t, unspecified, 0, make([]byte, 4)), cm, unspecified, 7) {
		t.Fatal("unspecified source without SLLA discarded")
	}
	if ValidSolicitation(solicitation(t, unspecified, 0, []byte{0, 0, 0, 0, 1, 1, 1, 2, 3, 4, 5, 6}), cm, unspecified, 7) {
		t.Fatal("unspecified source carrying SLLA accepted")
	}
	if ValidSolicitation(solicitation(t, source, 0, []byte{0, 0, 0, 0, 253, 0, 0, 0, 0, 0, 0, 0}), cm, source, 7) {
		t.Fatal("zero-length option accepted")
	}
}

type loopTestConn struct{ closed atomic.Bool }

func (c *loopTestConn) ReadRaw([]byte) (int, *ipv6.ControlMessage, netip.Addr, error) {
	time.Sleep(5 * time.Millisecond)
	return 0, nil, netip.Addr{}, os.ErrDeadlineExceeded
}
func (c *loopTestConn) WriteTo(ndp.Message, *ipv6.ControlMessage, netip.Addr) error { return nil }
func (c *loopTestConn) SetReadDeadline(time.Time) error                             { return nil }
func (c *loopTestConn) SetWriteDeadline(time.Time) error                            { return nil }
func (c *loopTestConn) Close() error                                                { c.closed.Store(true); return nil }

func TestLoopRateLimitsReceiptFailureAndJoinsFinalAdvertisement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &loopTestConn{}
	writes := make(chan time.Time, 4)
	finals := atomic.Int32{}
	attempts := 0
	loop := Loop{Conn: conn, Source: netip.MustParseAddr("fe80::1"), InterfaceIndex: 7, Transmit: func(_ time.Time, final bool) error {
		if final {
			finals.Add(1)
			return nil
		}
		attempts++
		writes <- time.Now()
		if attempts == 1 {
			return errors.New("wire succeeded, status commit failed")
		}
		cancel()
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	var first, second time.Time
	select {
	case first = <-writes:
	case <-time.After(2 * time.Second):
		t.Fatal("initial advertisement absent")
	}
	select {
	case second = <-writes:
	case <-time.After(5 * time.Second):
		t.Fatal("failed receipt did not retry")
	}
	if second.Sub(first) < MinDelay {
		t.Fatalf("receipt failure bypassed multicast rate limit: %v", second.Sub(first))
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not join final advertisement")
	}
	if finals.Load() != 1 || !conn.closed.Load() {
		t.Fatalf("final sends=%d closed=%t", finals.Load(), conn.closed.Load())
	}
}

func TestLoopInitialCadenceFitsShortRouterLifetime(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	conn := &loopTestConn{}
	times := make(chan time.Time, 3)
	wake := make(chan struct{}, 1)
	count := 0
	loop := Loop{Conn: conn, Source: netip.MustParseAddr("fe80::1"), InterfaceIndex: 7, Wake: wake, MaxInterval: func() time.Duration { return 4 * time.Second }, Transmit: func(_ time.Time, final bool) error {
		if final {
			return nil
		}
		count++
		times <- time.Now()
		if count == 3 {
			cancel()
		} else {
			wake <- struct{}{}
		}
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	var last time.Time
	for i := 0; i < 3; i++ {
		select {
		case now := <-times:
			if !last.IsZero() {
				gap := now.Sub(last)
				if gap < MinDelay || gap > 4500*time.Millisecond {
					t.Fatalf("initial/current interval %v outside 3..4s router lifetime", gap)
				}
			}
			last = now
		case <-time.After(5 * time.Second):
			t.Fatal("initial RA missed bounded cadence")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final advertisement did not join")
	}
}

func TestLoopRetiresUnavailableNativeSource(t *testing.T) {
	conn := &loopTestConn{}
	sends := 0
	err := (Loop{Conn: conn, Source: netip.MustParseAddr("fe80::1"), InterfaceIndex: 7, Transmit: func(time.Time, bool) error { sends++; return syscall.EADDRNOTAVAIL }}).Run(t.Context())
	if !errors.Is(err, syscall.EADDRNOTAVAIL) || sends != 1 || !conn.closed.Load() {
		t.Fatalf("stale native source not retired: sends=%d closed=%t err=%v", sends, conn.closed.Load(), err)
	}
}

func TestLinkLocalSourceRemainsIndependentOfReceiverBind(t *testing.T) {
	addresses := []net.Addr{
		&net.IPNet{IP: net.ParseIP("169.254.1.1"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("192.0.2.1"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
	}
	source, err := linkLocalSource(addresses, "lan0")
	if err != nil || source != netip.MustParseAddr("fe80::1%lan0") {
		t.Fatalf("explicit RA source = %s, error = %v", source, err)
	}
	if _, err := linkLocalSource(addresses[:3], "lan0"); err == nil {
		t.Fatal("missing link-local source became an unspecified transmit source")
	}
}

func TestSolicitationMulticastDestinationAndInterfaceSelection(t *testing.T) {
	source := netip.MustParseAddr("fe80::2")
	cm := &ipv6.ControlMessage{HopLimit: 255, IfIndex: 7, Dst: allRouters.AsSlice()}
	packet := solicitation(t, source, 0, make([]byte, 4))
	if !ValidSolicitation(packet, cm, source, 7) {
		t.Fatal("all-routers multicast solicitation rejected")
	}
	if ValidSolicitation(packet, cm, source, 8) {
		t.Fatal("wildcard receiver accepted a different interface's solicitation")
	}
}
