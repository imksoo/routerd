// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// Packet includes the IP source required by DHCP, including 0.0.0.0 before a
// lease is confirmed. The OS transport owns link delivery and socket lifetime.
type Packet struct {
	Payload     []byte
	Source      netip.Addr
	Destination netip.Addr
}

type Sender interface {
	Send(context.Context, Packet) error
}

// Client owns one DHCP exchange and its lease. Its caller serializes every
// operation, including snapshots and persistence, with transport effects.
type Client struct {
	cfg             Config
	sender          Sender
	state           State
	lease           Lease
	remembered      Lease
	clientID        []byte
	requested       netip.Addr
	xid             uint32
	selected        netip.Addr
	offered         netip.Addr
	startedAt       time.Time
	sentAt          time.Time
	retryAt         time.Time
	attempt         uint
	discoverSeconds uint16
}

func New(cfg Config, sender Sender) (*Client, error) {
	if len(cfg.HardwareAddr) != 6 {
		return nil, fmt.Errorf("DHCPv4 requires an Ethernet hardware address")
	}
	if sender == nil {
		return nil, fmt.Errorf("DHCPv4 packet sender is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.HardwareAddr = append(net.HardwareAddr(nil), cfg.HardwareAddr...)
	for _, value := range []string{cfg.Hostname, cfg.ClassID, cfg.ClientID} {
		if len(value) > 255 {
			return nil, fmt.Errorf("DHCPv4 configured option exceeds 255 bytes")
		}
	}
	if cfg.ClientID != "" && len(cfg.ClientID) < 2 {
		return nil, fmt.Errorf("DHCPv4 client identifier requires at least two bytes")
	}
	c := &Client{cfg: cfg, sender: sender, state: StateIdle}
	c.clientID = []byte(cfg.ClientID)
	if len(c.clientID) == 0 {
		c.clientID = append([]byte{1}, cfg.HardwareAddr...)
	}
	if cfg.RequestedAddress != "" {
		var err error
		c.requested, err = netip.ParseAddr(cfg.RequestedAddress)
		if err != nil || !usableIPv4(c.requested) {
			return nil, fmt.Errorf("invalid requested IPv4 address %q", cfg.RequestedAddress)
		}
	}
	return c, nil
}

func (c *Client) now() time.Time { return c.cfg.Now().UTC() }

func (c *Client) begin(state State) error {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Errorf("DHCP transaction ID: %w", err)
	}
	c.xid = binary.BigEndian.Uint32(raw[:])
	c.state, c.startedAt, c.sentAt, c.attempt = state, c.now(), time.Time{}, 0
	return nil
}

func (c *Client) Start(ctx context.Context) error {
	switch c.state {
	case StateDiscovering, StateRequesting, StateBound, StateRenewing, StateRebinding:
		return nil
	}
	if c.remembered.ValidAt(c.now()) {
		if err := c.begin(StateRequesting); err != nil {
			return err
		}
		c.offered, c.selected = c.remembered.Address, netip.Addr{}
		return c.transmit(ctx)
	}
	return c.discover(ctx)
}

func (c *Client) discover(ctx context.Context) error {
	c.lease, c.remembered = Lease{}, Lease{}
	c.offered, c.selected = netip.Addr{}, netip.Addr{}
	if err := c.begin(StateDiscovering); err != nil {
		return err
	}
	return c.transmit(ctx)
}

func (c *Client) Tick(ctx context.Context) error {
	now := c.now()
	if usableIPv4(c.lease.Address) && !c.lease.ValidAt(now) {
		// Withdraw authority before attempting any new network effect.
		c.lease, c.state, c.retryAt = Lease{}, StateExpired, now
	}
	switch c.state {
	case StateIdle, StateReleased:
		return nil
	case StateExpired:
		if !now.Before(c.retryAt) {
			return c.discover(ctx)
		}
	case StateBound, StateRenewing, StateRebinding:
		if rebind := c.lease.RebindAt(); !rebind.IsZero() && !now.Before(rebind) && c.state != StateRebinding {
			if c.state == StateBound {
				if err := c.begin(StateRebinding); err != nil {
					return err
				}
			} else {
				c.state, c.attempt = StateRebinding, 0
			}
			return c.transmit(ctx)
		}
		if renew := c.lease.RenewAt(); c.state == StateBound && !renew.IsZero() && !now.Before(renew) {
			return c.Renew(ctx)
		}
		if c.state != StateBound && !now.Before(c.retryAt) {
			return c.transmit(ctx)
		}
	case StateDiscovering, StateRequesting:
		if !now.Before(c.startedAt.Add(60 * time.Second)) {
			c.state, c.remembered, c.retryAt = StateExpired, Lease{}, now.Add(10*time.Second)
			return fmt.Errorf("DHCPv4 acquisition exchange timed out")
		}
		if !now.Before(c.retryAt) {
			return c.transmit(ctx)
		}
	}
	return nil
}

// NextDeadline keeps protocol timers ahead of the daemon's cancellation poll.
// In particular even a one-second lease gets both T1 and T2 processing.
func (c *Client) NextDeadline() time.Time {
	var next time.Time
	add := func(at time.Time) {
		if !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	switch c.state {
	case StateBound:
		add(c.lease.RenewAt())
		add(c.lease.RebindAt())
		add(c.lease.ExpiresAt())
	case StateRenewing:
		add(c.lease.RebindAt())
		add(c.lease.ExpiresAt())
		add(c.retryAt)
	case StateRebinding:
		add(c.lease.ExpiresAt())
		add(c.retryAt)
	case StateDiscovering, StateRequesting:
		add(c.startedAt.Add(60 * time.Second))
		add(c.retryAt)
	case StateExpired:
		add(c.retryAt)
	}
	return next
}

func (c *Client) Renew(ctx context.Context) error {
	if !c.lease.ValidAt(c.now()) {
		return fmt.Errorf("no unexpired DHCPv4 lease to renew")
	}
	if c.state == StateRenewing || c.state == StateRebinding {
		return nil
	}
	if err := c.begin(StateRenewing); err != nil {
		return err
	}
	return c.transmit(ctx)
}

func (c *Client) Release(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Relinquish local authority even when notifying the server fails. A
	// failed send is reported to the command caller and never revives use.
	defer func() { c.lease, c.remembered, c.state, c.retryAt = Lease{}, Lease{}, StateReleased, time.Time{} }()
	if c.lease.ValidAt(c.now()) {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return err
		}
		msg := Message{Op: 1, HType: 1, XID: binary.BigEndian.Uint32(raw[:]), CHAddr: c.cfg.HardwareAddr,
			CIAddr: c.lease.Address, Options: map[byte][]byte{OptionMessageType: {MessageRelease}, OptionClientID: c.clientID, OptionServerID: c.lease.ServerID.AsSlice()}}
		payload, err := Encode(msg)
		if err != nil {
			return err
		}
		if err := c.sender.Send(ctx, Packet{Payload: payload, Source: c.lease.Address, Destination: c.lease.ServerID}); err != nil {
			return err
		}
	}
	return nil
}

// Receive ignores packets outside the active exchange. Server identity is taken
// from option 54, not the relay's IP source. RFC 6842 permits legacy replies
// without client-id; an echoed client-id must match exactly.
func (c *Client) Receive(ctx context.Context, packet []byte) error {
	now := c.now()
	if usableIPv4(c.lease.Address) && !c.lease.ValidAt(now) {
		c.lease, c.state, c.retryAt = Lease{}, StateExpired, now
		return nil
	}
	if (c.state == StateDiscovering || c.state == StateRequesting) && !now.Before(c.startedAt.Add(60*time.Second)) {
		c.state, c.remembered, c.retryAt = StateExpired, Lease{}, now.Add(10*time.Second)
		return nil
	}
	msg, err := Decode(packet)
	if err != nil {
		return nil
	}
	if msg.Op != 2 || msg.HType != 1 || msg.XID != c.xid || !bytes.Equal(msg.CHAddr, c.cfg.HardwareAddr) {
		return nil
	}
	if id, present := msg.Options[OptionClientID]; present && !bytes.Equal(id, c.clientID) {
		return nil
	}
	server := optionAddr(msg.Options[OptionServerID])
	if !usableIPv4(server) {
		return nil
	}
	if c.state == StateDiscovering {
		if msg.MessageType() != MessageOffer || !usableIPv4(msg.YIAddr) {
			return nil
		}
		c.selected, c.offered, c.state, c.startedAt, c.attempt = server, msg.YIAddr, StateRequesting, now, 0
		c.sentAt = time.Time{}
		return c.transmit(ctx)
	}
	if c.state != StateRequesting && c.state != StateRenewing && c.state != StateRebinding {
		return nil
	}
	if (c.state == StateRequesting && c.selected.IsValid() && c.selected != server) || (c.state == StateRenewing && c.lease.ServerID != server) {
		return nil
	}
	switch msg.MessageType() {
	case MessageNAK:
		c.lease, c.remembered, c.state, c.retryAt = Lease{}, Lease{}, StateExpired, now.Add(time.Second)
		return fmt.Errorf("DHCPv4 server %s returned NAK", server)
	case MessageACK:
		address := c.lease.Address
		previous := c.lease
		if c.state == StateRequesting {
			address, previous = c.offered, c.remembered
		}
		if msg.YIAddr != address || c.sentAt.IsZero() {
			return nil
		}
		lease, err := LeaseFromACK(msg, previous, c.sentAt)
		if err != nil {
			return err
		}
		if !lease.ValidAt(now) {
			return fmt.Errorf("DHCPv4 ACK lease already expired")
		}
		c.lease, c.remembered, c.state, c.retryAt = lease, Lease{}, StateBound, time.Time{}
	}
	return nil
}

func (c *Client) transmit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := c.now()
	opts := map[byte][]byte{
		OptionMessageType: {MessageRequest}, OptionClientID: c.clientID,
		OptionParameterRequest: {OptionSubnetMask, OptionRouter, OptionDNSServer, OptionHostname, OptionDomainName, OptionMTU, OptionBroadcastAddress, OptionNTPServer, OptionTFTPServer, OptionBootfile, OptionLeaseTime, OptionT1, OptionT2},
	}
	if c.cfg.Hostname != "" {
		opts[OptionHostname] = []byte(c.cfg.Hostname)
	}
	if c.cfg.ClassID != "" {
		opts[OptionClassID] = []byte(c.cfg.ClassID)
	}
	msg := Message{Op: 1, HType: 1, XID: c.xid, CHAddr: c.cfg.HardwareAddr, Options: opts}
	destination, source := netip.MustParseAddr("255.255.255.255"), netip.IPv4Unspecified()
	elapsed := max(time.Duration(0), now.Sub(c.startedAt)) / time.Second
	msg.Seconds = uint16(min(elapsed, 65535))
	switch c.state {
	case StateDiscovering:
		c.discoverSeconds = msg.Seconds
		opts[OptionMessageType] = []byte{MessageDiscover}
		msg.Flags = 0x8000
		if c.requested.IsValid() {
			opts[OptionRequestedIP] = c.requested.AsSlice()
		}
	case StateRequesting:
		msg.Flags = 0x8000
		opts[OptionRequestedIP] = c.offered.AsSlice()
		if c.selected.IsValid() {
			msg.Seconds = c.discoverSeconds
			opts[OptionServerID] = c.selected.AsSlice()
		}
	case StateRenewing, StateRebinding:
		msg.CIAddr, source = c.lease.Address, c.lease.Address
		if c.state == StateRenewing {
			destination = c.lease.ServerID
		}
	default:
		return fmt.Errorf("no DHCP exchange in state %s", c.state)
	}
	payload, err := Encode(msg)
	if err != nil {
		return err
	}
	// Retain one xid and one conservative lease epoch across retransmissions.
	c.attempt++
	delay := time.Duration(4<<min(c.attempt-1, 4)) * time.Second
	if c.state == StateRenewing || c.state == StateRebinding {
		until := c.lease.RebindAt()
		if c.state == StateRebinding {
			until = c.lease.ExpiresAt()
		}
		if !until.IsZero() {
			delay = max(60*time.Second, until.Sub(now)/2)
		}
	}
	// Acquisition retries use the RFC's random +/- one-second backoff fuzz.
	if c.state == StateDiscovering || c.state == StateRequesting {
		var fuzz [2]byte
		if _, err := rand.Read(fuzz[:]); err != nil {
			return err
		}
		delay += time.Duration(binary.BigEndian.Uint16(fuzz[:]))*2*time.Second/65535 - time.Second
	}
	c.retryAt = now.Add(delay)
	if err := c.sender.Send(ctx, Packet{Payload: payload, Source: source, Destination: destination}); err != nil {
		return err
	}
	if c.sentAt.IsZero() && c.state != StateDiscovering {
		c.sentAt = now
	}
	return nil
}

func (c *Client) Snapshot() Snapshot {
	s := SnapshotFromLease(c.cfg.Resource, c.cfg.Interface, c.state, c.lease, c.now())
	s.HardwareAddress, s.ClientID = c.cfg.HardwareAddr.String(), hex.EncodeToString(c.clientID)
	return s
}

func (c *Client) Restore(s Snapshot) error {
	if s.Resource != c.cfg.Resource || s.Interface != c.cfg.Interface || s.HardwareAddress != c.cfg.HardwareAddr.String() || s.ClientID != hex.EncodeToString(c.clientID) {
		return fmt.Errorf("DHCPv4 saved lease belongs to a different client or interface")
	}
	c.lease, c.remembered, c.state, c.retryAt = Lease{}, Lease{}, StateIdle, time.Time{}
	c.selected, c.offered = netip.Addr{}, netip.Addr{}
	if s.State == StateReleased {
		return nil
	}
	if s.CurrentAddress != "" && (s.LeaseTimeSeconds <= 0 || s.LeaseTimeSeconds > 0xffffffff || s.Infinite != (s.LeaseTimeSeconds == 0xffffffff)) {
		return fmt.Errorf("invalid saved DHCPv4 lease lifetime")
	}
	lease := LeaseFromSnapshot(s)
	if !lease.ValidAt(c.now()) {
		c.state = StateExpired
		return nil
	}
	if !usableIPv4(lease.ServerID) || lease.PrefixLength < 0 || lease.PrefixLength > 32 {
		return fmt.Errorf("invalid saved DHCPv4 lease")
	}
	// INIT-REBOOT confirms reachability on the current link before publishing
	// authority. The remembered lease is never exposed as a current lease.
	c.remembered, c.state = lease, StateIdle
	return nil
}
