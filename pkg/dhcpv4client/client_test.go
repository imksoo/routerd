// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

type packetRecorder struct {
	packets []Packet
	err     error
}

func (s *packetRecorder) Send(_ context.Context, p Packet) error {
	s.packets = append(s.packets, p)
	return s.err
}
func (s *packetRecorder) last(t *testing.T) Message {
	t.Helper()
	m, err := Decode(s.packets[len(s.packets)-1].Payload)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func testClient(t *testing.T) (*Client, *packetRecorder, *time.Time) {
	t.Helper()
	now := time.Unix(1800000000, 0).UTC()
	sender := &packetRecorder{}
	c, err := New(Config{Resource: "wan", Interface: "eth0", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 255}, RequestedAddress: "192.0.2.99", Now: func() time.Time { return now }}, sender)
	if err != nil {
		t.Fatal(err)
	}
	return c, sender, &now
}
func reply(t *testing.T, request Message, kind byte, server, address string, seconds uint32) []byte {
	t.Helper()
	lifetime := make([]byte, 4)
	binary.BigEndian.PutUint32(lifetime, seconds)
	msg := Message{Op: 2, HType: 1, XID: request.XID, CHAddr: request.CHAddr, Options: map[byte][]byte{OptionMessageType: {kind}, OptionServerID: netip.MustParseAddr(server).AsSlice()}}
	if address != "" {
		msg.YIAddr = netip.MustParseAddr(address)
	}
	if kind == MessageACK {
		msg.Options[OptionLeaseTime], msg.Options[OptionSubnetMask] = lifetime, []byte{255, 255, 255, 0}
	}
	payload, err := Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
func acquire(t *testing.T, c *Client, s *packetRecorder, seconds uint32) {
	t.Helper()
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Receive(ctx, reply(t, s.last(t), MessageOffer, "192.0.2.1", "192.0.2.10", seconds)); err != nil {
		t.Fatal(err)
	}
	if err := c.Receive(ctx, reply(t, s.last(t), MessageACK, "192.0.2.1", "192.0.2.10", seconds)); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().State != StateBound {
		t.Fatalf("not bound: %+v", c.Snapshot())
	}
}

func TestClientAcquireRenewRebindExpire(t *testing.T) {
	c, sender, now := testClient(t)
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	discover := sender.last(t)
	if discover.MessageType() != MessageDiscover || discover.CIAddr != netip.IPv4Unspecified() || sender.packets[0].Source != netip.IPv4Unspecified() {
		t.Fatal("discover did not use an unconfigured source")
	}
	if err := c.Receive(ctx, reply(t, discover, MessageACK, "192.0.2.1", "192.0.2.10", 120)); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().State != StateDiscovering {
		t.Fatal("bare ACK was accepted before selecting an offer")
	}
	if err := c.Receive(ctx, reply(t, discover, MessageOffer, "192.0.2.1", "192.0.2.10", 120)); err != nil {
		t.Fatal(err)
	}
	request := sender.last(t)
	if got := optionAddr(request.Options[OptionRequestedIP]); got.String() != "192.0.2.10" {
		t.Fatalf("selected address overwritten by configuration: %s", got)
	}
	if request.XID != discover.XID || optionAddr(request.Options[OptionServerID]).String() != "192.0.2.1" {
		t.Fatal("selected request lost exchange/server identity")
	}
	*now = now.Add(10 * time.Second)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if retry := sender.last(t); retry.MessageType() != MessageRequest || retry.XID != request.XID {
		t.Fatal("request retry reverted to discover or changed xid")
	}
	if err := c.Receive(ctx, reply(t, request, MessageACK, "192.0.2.1", "192.0.2.10", 120)); err != nil {
		t.Fatal(err)
	}
	lease := c.Snapshot()
	if lease.AcquiredAt != now.Add(-10*time.Second) {
		t.Fatal("ACK receipt extended the request's lease epoch")
	}
	*now = lease.RenewAt
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	renew := sender.last(t)
	if c.Snapshot().State != StateRenewing || c.Snapshot().CurrentAddress == "" || renew.CIAddr.String() != "192.0.2.10" || sender.packets[len(sender.packets)-1].Destination.String() != "192.0.2.1" || len(renew.Options[OptionServerID]) != 0 || len(renew.Options[OptionRequestedIP]) != 0 {
		t.Fatalf("incorrect renewal: %+v %+v", renew, c.Snapshot())
	}
	*now = lease.RebindAt
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	rebind := sender.last(t)
	if c.Snapshot().State != StateRebinding || rebind.CIAddr.String() != "192.0.2.10" || sender.packets[len(sender.packets)-1].Destination.String() != "255.255.255.255" || len(rebind.Options[OptionServerID]) != 0 {
		t.Fatal("renewal did not become a correct broadcast rebind")
	}
	*now = lease.ExpiresAt
	if s := c.Snapshot(); s.CurrentAddress != "" || s.State != StateExpired {
		t.Fatalf("expired snapshot retained authority: %+v", s)
	}
	if err := c.Receive(ctx, reply(t, rebind, MessageACK, "192.0.2.1", "192.0.2.10", 3600)); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().CurrentAddress != "" {
		t.Fatal("late ACK revived expired exchange")
	}
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().State != StateDiscovering || sender.last(t).XID == rebind.XID {
		t.Fatal("expiry did not start a fresh exchange")
	}
}

func TestClientReplyIdentityAndRebindServer(t *testing.T) {
	c, sender, now := testClient(t)
	acquire(t, c, sender, 120)
	*now = c.lease.RenewAt()
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	renew := sender.last(t)
	for _, alter := range []func(*Message){
		func(m *Message) { m.Op = 1 }, func(m *Message) { m.XID++ }, func(m *Message) { m.CHAddr = net.HardwareAddr{2, 0, 0, 0, 0, 2} },
		func(m *Message) { m.Options[OptionServerID] = netip.MustParseAddr("192.0.2.2").AsSlice() }, func(m *Message) { m.Options[OptionClientID] = []byte("other-client") },
	} {
		msg, _ := Decode(reply(t, renew, MessageACK, "192.0.2.1", "192.0.2.10", 120))
		alter(&msg)
		payload, _ := Encode(msg)
		if err := c.Receive(context.Background(), payload); err != nil {
			t.Fatal(err)
		}
		if c.Snapshot().State != StateRenewing {
			t.Fatalf("unrelated reply changed state: %+v", msg)
		}
	}
	*now = c.lease.RebindAt()
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Receive(context.Background(), reply(t, sender.last(t), MessageACK, "192.0.2.2", "192.0.2.10", 3600)); err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.State != StateBound || s.ServerID != "192.0.2.2" {
		t.Fatalf("rebind did not accept new server: %+v", s)
	}
}

func TestClientNAKReleaseAndFailedSend(t *testing.T) {
	c, sender, now := testClient(t)
	acquire(t, c, sender, 120)
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Receive(context.Background(), reply(t, sender.last(t), MessageNAK, "192.0.2.1", "", 0)); err == nil {
		t.Fatal("NAK not reported")
	}
	if s := c.Snapshot(); s.State != StateExpired || s.CurrentAddress != "" {
		t.Fatalf("NAK retained lease: %+v", s)
	}
	*now = now.Add(time.Second)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Receive(context.Background(), reply(t, sender.last(t), MessageOffer, "192.0.2.1", "192.0.2.10", 120)); err != nil {
		t.Fatal(err)
	}
	if err := c.Receive(context.Background(), reply(t, sender.last(t), MessageACK, "192.0.2.1", "192.0.2.10", 120)); err != nil {
		t.Fatal(err)
	}
	sender.err = errors.New("unreachable")
	if err := c.Release(context.Background()); err == nil {
		t.Fatal("failed release reported success")
	}
	if s := c.Snapshot(); s.State != StateReleased || s.CurrentAddress != "" {
		t.Fatal("failed server notification revived released authority")
	}
	release := sender.last(t)
	if release.MessageType() != MessageRelease || release.CIAddr.String() != "192.0.2.10" || optionAddr(release.Options[OptionServerID]).String() != "192.0.2.1" || len(release.Options[OptionParameterRequest]) != 0 || len(release.Options[OptionRequestedIP]) != 0 {
		t.Fatalf("incorrect release: %+v", release)
	}
	count := len(sender.packets)
	*now = now.Add(24 * time.Hour)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.State != StateReleased || s.CurrentAddress != "" || len(sender.packets) != count {
		t.Fatalf("released client reacquired: %+v", s)
	}
	sender.err = nil
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().State != StateDiscovering || sender.last(t).MessageType() != MessageDiscover {
		t.Fatal("explicit Start did not reactivate a released client")
	}
}

func TestClientRestoreConfirmsIdentityAndLease(t *testing.T) {
	c, sender, now := testClient(t)
	acquire(t, c, sender, 3600)
	saved := c.Snapshot()
	restarted, transport, _ := testClient(t)
	if err := restarted.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if restarted.Snapshot().CurrentAddress != "" {
		t.Fatal("restored lease published before INIT-REBOOT confirmation")
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := transport.last(t)
	if request.MessageType() != MessageRequest || optionAddr(request.Options[OptionRequestedIP]).String() != "192.0.2.10" || len(request.Options[OptionServerID]) != 0 || !request.CIAddr.IsUnspecified() {
		t.Fatalf("incorrect INIT-REBOOT: %+v", request)
	}
	if err := restarted.Receive(context.Background(), reply(t, request, MessageACK, "192.0.2.2", "192.0.2.10", 3600)); err != nil {
		t.Fatal(err)
	}
	if restarted.Snapshot().State != StateBound {
		t.Fatal("INIT-REBOOT ACK was not bound")
	}
	saved.Interface = "other"
	if err := restarted.Restore(saved); err == nil {
		t.Fatal("restored lease from another interface")
	}
	saved = c.Snapshot()
	*now = saved.ExpiresAt
	if err := c.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().CurrentAddress != "" {
		t.Fatal("expired restored lease retained authority")
	}
}

func TestACKTimersAndOptionalOptionBounds(t *testing.T) {
	c, sender, _ := testClient(t)
	acquire(t, c, sender, 3600)
	request := sender.last(t)
	ack, _ := Decode(reply(t, request, MessageACK, "192.0.2.1", "192.0.2.10", 3600))
	ack.Options[OptionDNSServer] = []byte{192, 0, 2, 53, 1}
	ack.Options[OptionT1] = []byte{255, 255, 255, 255}
	ack.Options[OptionT2] = []byte{255, 255, 255, 255}
	lease, err := LeaseFromACK(ack, Lease{}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(lease.DNSServers) != 0 || lease.T1 != 1800*time.Second || lease.T2 != 3150*time.Second {
		t.Fatalf("optional normalization failed: %+v", lease)
	}
	delete(ack.Options, OptionLeaseTime)
	if _, err := LeaseFromACK(ack, Lease{}, time.Unix(1, 0)); err == nil {
		t.Fatal("invented lease lifetime for incomplete ACK")
	}
}

func TestClientInfiniteLeaseDoesNotExpireOrScheduleDefaultRenewal(t *testing.T) {
	c, sender, now := testClient(t)
	acquire(t, c, sender, 0xffffffff)
	count := len(sender.packets)
	*now = now.Add(200 * 365 * 24 * time.Hour)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.State != StateBound || s.CurrentAddress == "" || !s.ExpiresAt.IsZero() || !s.RenewAt.IsZero() || len(sender.packets) != count {
		t.Fatalf("infinite lease was treated as finite: %+v", s)
	}
}

func TestClientStartKeepsActiveLeaseAndExchange(t *testing.T) {
	c, sender, _ := testClient(t)
	acquire(t, c, sender, 3600)
	count := len(sender.packets)
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().State != StateBound || c.Snapshot().CurrentAddress != "192.0.2.10" || len(sender.packets) != count {
		t.Fatal("repeated Start restarted an active lease")
	}
}
