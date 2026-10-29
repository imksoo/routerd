// SPDX-License-Identifier: BSD-3-Clause

// Package routeradvertisement owns periodic and solicited IPv6 advertisements.
// The caller supplies a fresh advertisement at send time, so a lease deadline
// is never restarted by a configuration reload or by an RS.
package routeradvertisement

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"syscall"
	"time"

	"github.com/mdlayher/ndp"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

var allRouters = netip.MustParseAddr("ff02::2")
var allNodes = netip.MustParseAddr("ff02::1")

const MinDelay = 3 * time.Second

type Connection interface {
	ReadRaw([]byte) (int, *ipv6.ControlMessage, netip.Addr, error)
	WriteTo(ndp.Message, *ipv6.ControlMessage, netip.Addr) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

type Endpoint struct {
	Conn            Connection
	Source          netip.Addr
	Index           int
	MTU             int
	HardwareAddress net.HardwareAddr
}

type Loop struct {
	Conn           Connection
	InterfaceIndex int
	Source         netip.Addr
	// Transmit owns the source read, pending receipt, wire effect, and result
	// commit as one serialized operation. final requests zero router lifetime.
	Transmit    func(now time.Time, final bool) error
	MaxInterval func() time.Duration
	// Wake is bounded/coalesced; configuration changes do not bypass rate limits.
	Wake   <-chan struct{}
	Report func(error)
}

// Listen binds an ICMPv6 socket to the interface's link-local source, joins the
// all-routers group for RS, and asks the kernel to calculate ICMPv6 checksums.
func Listen(ifname string) (Endpoint, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return Endpoint{}, err
	}
	c, source, err := ndp.Listen(ifi, ndp.LinkLocal)
	if err != nil {
		return Endpoint{}, err
	}
	if !source.IsLinkLocalUnicast() {
		c.Close()
		return Endpoint{}, fmt.Errorf("RA source on %s is not link-local", ifname)
	}
	if err := c.SetControlMessage(ipv6.FlagHopLimit|ipv6.FlagDst|ipv6.FlagInterface, true); err != nil {
		c.Close()
		return Endpoint{}, err
	}
	filter := new(ipv6.ICMPFilter)
	filter.SetAll(true)
	filter.Accept(ipv6.ICMPTypeRouterSolicitation)
	if err := c.SetICMPFilter(filter); err != nil {
		c.Close()
		return Endpoint{}, err
	}
	if err := c.JoinGroup(allRouters); err != nil {
		c.Close()
		return Endpoint{}, err
	}
	return Endpoint{Conn: c, Source: source, Index: ifi.Index, MTU: ifi.MTU, HardwareAddress: ifi.HardwareAddr}, nil
}

func (l Loop) Run(ctx context.Context) error {
	if l.Conn == nil || l.Transmit == nil || !l.Source.IsLinkLocalUnicast() || l.InterfaceIndex <= 0 {
		return fmt.Errorf("incomplete RA loop")
	}
	defer l.Conn.Close()
	// Wake blocked receive without closing the socket needed for the final RA.
	stop := context.AfterFunc(ctx, func() { _ = l.Conn.SetReadDeadline(time.Now()) })
	defer stop()
	next := time.Now().Add(randomDelay(500 * time.Millisecond))
	var last time.Time
	initial := 0
	buffer := make([]byte, 65535)
	for ctx.Err() == nil {
		select {
		case <-l.Wake:
			due := time.Now().Add(randomDelay(500 * time.Millisecond))
			if due.Before(next) {
				next = due
			}
		default:
		}
		if floor := last.Add(MinDelay); !last.IsZero() && next.Before(floor) {
			next = floor
		}
		if now := time.Now(); !now.Before(next) {
			if err := l.Transmit(now, false); err != nil {
				if errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.ENODEV) || errors.Is(err, syscall.ENXIO) {
					return err
				}

				// The wire effect may have succeeded before its receipt/status
				// commit failed. Conservatively rate-limit every attempt.
				last = time.Now()
				if l.Report != nil {
					l.Report(err)
				}
				next = last.Add(MinDelay)
				continue
			}
			last = time.Now()
			initial++
			maximum := 16 * time.Second
			if l.MaxInterval != nil {
				maximum = l.MaxInterval()
			}
			if maximum < 4*time.Second || maximum > 1800*time.Second {
				return fmt.Errorf("MaxRtrAdvInterval outside RFC4861 bounds")
			}
			// RFC4861 6.2.4: first three intervals <=16s. The normal
			// interval also respects a shorter declared router lifetime.
			if initial < 3 && maximum > 16*time.Second {
				maximum = 16 * time.Second
			}
			minimum := MinDelay
			next = last.Add(minimum + randomDelay(maximum-minimum))
		}
		// A short receive deadline also observes coalesced configuration wakes.
		deadline := next
		if poll := time.Now().Add(250 * time.Millisecond); poll.Before(deadline) {
			deadline = poll
		}
		if err := l.Conn.SetReadDeadline(deadline); err != nil {
			return err
		}
		n, cm, source, err := l.Conn.ReadRaw(buffer)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				continue
			}
			return err
		}
		if ValidSolicitation(buffer[:n], cm, source, l.InterfaceIndex) {
			due := time.Now().Add(randomDelay(500 * time.Millisecond))
			if !last.IsZero() && due.Before(last.Add(MinDelay)) {
				due = last.Add(MinDelay).Add(randomDelay(500 * time.Millisecond))
			}
			if due.Before(next) {
				next = due
			}
		}
	}
	// RFC4861 6.2.5 permits a small number (at most three) of final
	// advertisements. One avoids extending shutdown with an independent retry
	// owner. The caller retains its withdrawal receipt on a send failure.
	if wait := time.Until(last.Add(MinDelay)); !last.IsZero() && wait > 0 {
		time.Sleep(wait)
	}
	return l.Transmit(time.Now(), true)
}

func (e Endpoint) Write(advertisement *ndp.RouterAdvertisement) error {
	packet := *advertisement
	packet.Options = append([]ndp.Option(nil), advertisement.Options...)
	if len(e.HardwareAddress) > 0 {
		packet.Options = append(packet.Options, &ndp.LinkLayerAddress{Direction: ndp.Source, Addr: e.HardwareAddress})
	}
	wire, err := ndp.MarshalMessage(&packet)
	if err != nil {
		return err
	}
	// ND must not depend on IPv6 fragmentation. Use the IPv6 minimum MTU
	// even on jumbo links, and fail honestly instead of truncating options.
	if len(wire) > min(e.MTU, 1280)-40 {
		return fmt.Errorf("RA options exceed the IPv6 link packet budget")
	}
	if err := e.Conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	return e.Conn.WriteTo(&packet, &ipv6.ControlMessage{IfIndex: e.Index, Src: e.Source.WithZone("").AsSlice(), HopLimit: 255}, allNodes)
}

func randomDelay(max time.Duration) time.Duration { return time.Duration(rand.Int64N(int64(max) + 1)) }

// ValidSolicitation applies RFC4861 6.1.1. Unknown and out-of-context option
// contents are ignored, while every option length remains bounded. In
// particular, RS source may be any interface unicast address, not only a LLA.
func ValidSolicitation(packet []byte, cm *ipv6.ControlMessage, source netip.Addr, ifindex int) bool {
	if len(packet) < 8 || packet[0] != 133 || packet[1] != 0 || cm == nil || cm.HopLimit != 255 || cm.IfIndex != ifindex || !source.Is6() || source.Is4In6() || source.IsMulticast() {
		return false
	}
	destination, ok := netip.AddrFromSlice(cm.Dst)
	if !ok || !destination.Is6() {
		return false
	}
	// The socket owns the selected interface; destination is used in the
	// checksum rather than inventing additional RS receive rejection rules.
	pseudo := icmp.IPv6PseudoHeader(source.WithZone("").AsSlice(), destination.AsSlice())
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(packet)))
	var sum uint32
	for _, bytes := range [][]byte{pseudo, packet} {
		for len(bytes) > 1 {
			sum += uint32(binary.BigEndian.Uint16(bytes))
			bytes = bytes[2:]
		}
		if len(bytes) == 1 {
			sum += uint32(bytes[0]) << 8
		}
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	if uint16(sum) != 0xffff {
		return false
	}
	for offset := 8; offset < len(packet); {
		if offset+2 > len(packet) || packet[offset+1] == 0 {
			return false
		}
		size := int(packet[offset+1]) * 8
		if size > len(packet)-offset {
			return false
		}
		if source.IsUnspecified() && packet[offset] == 1 {
			return false
		}
		offset += size
	}
	return true
}
