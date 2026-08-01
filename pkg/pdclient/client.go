// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
)

type State string

const (
	StateIdle       State = "idle"
	StateSoliciting State = "soliciting"
	StateRequesting State = "requesting"
	StateBound      State = "bound"
	StateRenewing   State = "renewing"
	StateRebinding  State = "rebinding"
	StateExpired    State = "expired"
)

const (
	solicitInitialRetransmissionTimeout = time.Second
	solicitMaxRetransmissionTimeout     = time.Hour
)

type Config struct {
	Resource    string
	Interface   string
	ClientDUID  []byte
	IAID        uint32
	WantPrefix  int
	Now         func() time.Time
	Transaction func() (uint32, error)
	Random      func() float64
}

type Lease struct {
	Prefix     netip.Prefix
	ServerDUID []byte
	IAID       uint32
	T1         time.Duration
	T2         time.Duration
	Preferred  time.Duration
	Valid      time.Duration
	AcquiredAt time.Time
	RenewedAt  time.Time
}

// PreviousPrefix records a delegated prefix which is no longer current but
// remains valid until ExpiresAt.  Multiple entries are required because an
// upstream server may move a client through several prefixes before their
// valid lifetimes expire.
type PreviousPrefix struct {
	Prefix    netip.Prefix
	ExpiresAt time.Time
}

type Information struct {
	AFTRName     string
	DNSServers   []netip.Addr
	SNTPServers  []netip.Addr
	DomainSearch []string
	UpdatedAt    time.Time
}

func (l Lease) RenewAt() time.Time {
	return l.AcquiredAt.Add(l.T1)
}

func (l Lease) RebindAt() time.Time {
	return l.AcquiredAt.Add(l.T2)
}

func (l Lease) ExpiresAt() time.Time {
	return l.AcquiredAt.Add(l.Valid)
}

type Transport interface {
	Send(ctx context.Context, packet OutboundPacket) error
}

type OutboundPacket struct {
	Interface string
	Message   Message
	Payload   []byte
}

type Client struct {
	Config           Config
	Transport        Transport
	State            State
	Lease            Lease
	PreviousPrefixes []PreviousPrefix
	Info             Information

	lastTransaction     uint32
	lastInfoTransaction uint32
	advertise           Message
	lastMessage         Message
	solicitRetryAt      time.Time
	solicitRetryTimeout time.Duration
}

func New(config Config, transport Transport) (*Client, error) {
	if config.Resource == "" {
		return nil, fmt.Errorf("resource is required")
	}
	if config.Interface == "" {
		return nil, fmt.Errorf("interface is required")
	}
	if len(config.ClientDUID) == 0 {
		return nil, fmt.Errorf("client DUID is required")
	}
	if config.WantPrefix == 0 {
		config.WantPrefix = 60
	}
	return &Client{Config: config, Transport: transport, State: StateIdle}, nil
}

func (c *Client) Start(ctx context.Context) error {
	return c.send(ctx, StateSoliciting, Message{Type: MessageSolicit})
}

func (c *Client) Tick(ctx context.Context) error {
	return c.TickWithMargin(ctx, 0, 0)
}

func (c *Client) TickWithMargin(ctx context.Context, renewMargin, rebindMargin time.Duration) error {
	now := c.now()
	c.prunePreviousPrefixes(now)
	if c.State == StateSoliciting && !c.solicitRetryAt.IsZero() && !now.Before(c.solicitRetryAt) {
		return c.retransmitSolicit(ctx, now)
	}
	if c.State == StateBound || c.State == StateRenewing || c.State == StateRebinding {
		if c.Lease.Valid > 0 && !now.Before(c.Lease.ExpiresAt()) {
			c.State = StateExpired
			c.Lease = Lease{}
			return nil
		}
		if c.State == StateBound && c.Lease.T1 > 0 && !now.Before(c.Lease.RenewAt().Add(-renewMargin)) {
			return c.Renew(ctx)
		}
		if (c.State == StateBound || c.State == StateRenewing) && c.Lease.T2 > 0 && !now.Before(c.Lease.RebindAt().Add(-rebindMargin)) {
			return c.Rebind(ctx)
		}
	}
	if c.State == StateIdle || c.State == StateExpired {
		return c.Start(ctx)
	}
	return nil
}

// NextSolicitRetryAt returns the next scheduled Solicit retransmission. The
// transaction ID remains unchanged for every retransmission in the exchange.
func (c *Client) NextSolicitRetryAt() time.Time {
	if c.State != StateSoliciting {
		return time.Time{}
	}
	return c.solicitRetryAt
}

func (c *Client) Renew(ctx context.Context) error {
	if !c.Lease.Prefix.IsValid() {
		return fmt.Errorf("cannot renew without a lease")
	}
	return c.send(ctx, StateRenewing, Message{Type: MessageRenew})
}

func (c *Client) Rebind(ctx context.Context) error {
	if !c.Lease.Prefix.IsValid() {
		return fmt.Errorf("cannot rebind without a lease")
	}
	return c.send(ctx, StateRebinding, Message{Type: MessageRebind})
}

func (c *Client) Release(ctx context.Context) error {
	if !c.Lease.Prefix.IsValid() {
		return fmt.Errorf("cannot release without a lease")
	}
	if err := c.send(ctx, StateIdle, Message{Type: MessageRelease}); err != nil {
		return err
	}
	c.Lease = Lease{}
	c.PreviousPrefixes = nil
	return nil
}

func (c *Client) InfoRequest(ctx context.Context) error {
	if c.Transport == nil {
		return fmt.Errorf("transport is required")
	}
	xid, err := c.transaction()
	if err != nil {
		return err
	}
	msg := Message{
		Type:          MessageInfoRequest,
		TransactionID: xid,
		ClientDUID:    append([]byte(nil), c.Config.ClientDUID...),
		ORO: []uint16{
			optionAFTRName,
			optionDNSServers,
			optionSNTPServers,
			optionDomainSearch,
		},
	}
	payload, err := EncodeMessage(msg)
	if err != nil {
		return err
	}
	c.lastInfoTransaction = xid
	return c.Transport.Send(ctx, OutboundPacket{
		Interface: c.Config.Interface,
		Message:   msg,
		Payload:   payload,
	})
}

func (c *Client) Handle(ctx context.Context, payload []byte) error {
	msg, err := DecodeMessage(payload)
	if err != nil {
		return err
	}
	return c.HandleMessage(ctx, msg)
}

func (c *Client) HandleMessage(ctx context.Context, msg Message) error {
	if msg.TransactionID == c.lastInfoTransaction && msg.Type == MessageReply {
		if len(msg.ClientDUID) > 0 && !bytes.Equal(msg.ClientDUID, c.Config.ClientDUID) {
			return nil
		}
		c.acceptInformation(msg)
		return nil
	}
	if msg.TransactionID != c.lastTransaction {
		return nil
	}
	if len(msg.ServerDUID) == 0 {
		return nil
	}
	if len(msg.ClientDUID) > 0 && !bytes.Equal(msg.ClientDUID, c.Config.ClientDUID) {
		return nil
	}

	switch {
	case c.State == StateSoliciting && msg.Type == MessageAdvertise:
		c.advertise = msg
		return c.send(ctx, StateRequesting, Message{
			Type:       MessageRequest,
			ServerDUID: msg.ServerDUID,
			Prefix:     msg.Prefix,
			T1:         msg.T1,
			T2:         msg.T2,
			Preferred:  msg.Preferred,
			Valid:      msg.Valid,
		})
	case (c.State == StateRequesting || c.State == StateRenewing || c.State == StateRebinding) && msg.Type == MessageReply:
		c.acceptReply(msg)
		return nil
	default:
		return nil
	}
}

func (c *Client) acceptInformation(msg Message) {
	c.Info = Information{
		AFTRName:     msg.AFTRName,
		DNSServers:   append([]netip.Addr(nil), msg.DNSServers...),
		SNTPServers:  append([]netip.Addr(nil), msg.SNTPServers...),
		DomainSearch: append([]string(nil), msg.DomainSearch...),
		UpdatedAt:    c.now(),
	}
}

func (c *Client) acceptReply(msg Message) {
	now := c.now()
	c.rememberPreviousPrefix(msg.Prefix, now)
	t1 := seconds(msg.T1)
	t2 := seconds(msg.T2)
	valid := seconds(msg.Valid)
	preferred := seconds(msg.Preferred)
	c.Lease = Lease{
		Prefix:     msg.Prefix,
		ServerDUID: append([]byte(nil), msg.ServerDUID...),
		IAID:       msg.IAID,
		T1:         t1,
		T2:         t2,
		Preferred:  preferred,
		Valid:      valid,
		AcquiredAt: now,
		RenewedAt:  now,
	}
	c.State = StateBound
}

func (c *Client) rememberPreviousPrefix(next netip.Prefix, now time.Time) {
	byPrefix := make(map[netip.Prefix]time.Time, len(c.PreviousPrefixes)+1)
	for _, previous := range c.PreviousPrefixes {
		if !previous.Prefix.IsValid() || previous.Prefix == next || !now.Before(previous.ExpiresAt) {
			continue
		}
		byPrefix[previous.Prefix] = previous.ExpiresAt
	}
	if c.Lease.Prefix.IsValid() && c.Lease.Prefix != next {
		expiresAt := c.Lease.ExpiresAt()
		if now.Before(expiresAt) {
			byPrefix[c.Lease.Prefix] = expiresAt
		}
	}
	c.setPreviousPrefixes(byPrefix)
}

func (c *Client) prunePreviousPrefixes(now time.Time) {
	byPrefix := make(map[netip.Prefix]time.Time, len(c.PreviousPrefixes))
	for _, previous := range c.PreviousPrefixes {
		if !previous.Prefix.IsValid() || previous.Prefix == c.Lease.Prefix || !now.Before(previous.ExpiresAt) {
			continue
		}
		byPrefix[previous.Prefix] = previous.ExpiresAt
	}
	c.setPreviousPrefixes(byPrefix)
}

func (c *Client) setPreviousPrefixes(byPrefix map[netip.Prefix]time.Time) {
	prefixes := make([]netip.Prefix, 0, len(byPrefix))
	for prefix := range byPrefix {
		prefixes = append(prefixes, prefix)
	}
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		return strings.Compare(a.String(), b.String())
	})
	c.PreviousPrefixes = make([]PreviousPrefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		c.PreviousPrefixes = append(c.PreviousPrefixes, PreviousPrefix{Prefix: prefix, ExpiresAt: byPrefix[prefix]})
	}
}

func (c *Client) send(ctx context.Context, next State, msg Message) error {
	if c.Transport == nil {
		return fmt.Errorf("transport is required")
	}
	xid, err := c.transaction()
	if err != nil {
		return err
	}
	msg.TransactionID = xid
	msg.ClientDUID = append([]byte(nil), c.Config.ClientDUID...)
	msg.IAID = c.Config.IAID
	if msg.ServerDUID == nil && (msg.Type == MessageRenew || msg.Type == MessageRelease) {
		msg.ServerDUID = append([]byte(nil), c.Lease.ServerDUID...)
	}
	if !msg.Prefix.IsValid() && (msg.Type == MessageRenew || msg.Type == MessageRebind || msg.Type == MessageRelease) {
		msg.Prefix = c.Lease.Prefix
	}
	if msg.Type == MessageRenew || msg.Type == MessageRebind || msg.Type == MessageRelease {
		msg.T1 = uint32(c.Lease.T1 / time.Second)
		msg.T2 = uint32(c.Lease.T2 / time.Second)
		msg.Preferred = uint32(c.Lease.Preferred / time.Second)
		msg.Valid = uint32(c.Lease.Valid / time.Second)
	}
	payload, err := EncodeMessage(msg)
	if err != nil {
		return err
	}
	c.lastTransaction = xid
	c.State = next
	c.lastMessage = msg
	if next == StateSoliciting {
		c.solicitRetryTimeout = c.nextSolicitRetryTimeout(0)
		c.solicitRetryAt = c.now().Add(c.solicitRetryTimeout)
	} else {
		c.clearSolicitRetry()
	}
	return c.sendMessage(ctx, msg, payload)
}

func (c *Client) retransmitSolicit(ctx context.Context, now time.Time) error {
	if c.State != StateSoliciting || c.lastMessage.Type != MessageSolicit || c.lastMessage.TransactionID != c.lastTransaction {
		return nil
	}
	payload, err := EncodeMessage(c.lastMessage)
	if err != nil {
		return err
	}
	if err := c.sendMessage(ctx, c.lastMessage, payload); err != nil {
		return err
	}
	c.solicitRetryTimeout = c.nextSolicitRetryTimeout(c.solicitRetryTimeout)
	c.solicitRetryAt = now.Add(c.solicitRetryTimeout)
	return nil
}

func (c *Client) sendMessage(ctx context.Context, msg Message, payload []byte) error {
	return c.Transport.Send(ctx, OutboundPacket{
		Interface: c.Config.Interface,
		Message:   msg,
		Payload:   payload,
	})
}

func (c *Client) clearSolicitRetry() {
	c.solicitRetryAt = time.Time{}
	c.solicitRetryTimeout = 0
}

// nextSolicitRetryTimeout follows the RFC 8415 retransmission shape: a
// randomized initial timeout, exponential growth, and SOL_MAX_RT as the
// upper bound before the permitted +/-10 percent randomization.
func (c *Client) nextSolicitRetryTimeout(previous time.Duration) time.Duration {
	random := c.random()
	if previous <= 0 {
		// RFC 8415 requires the first Solicit RT to be strictly greater than
		// SOL_TIMEOUT. Keep the random component in the (0, 0.1] range.
		return solicitInitialRetransmissionTimeout + time.Duration((0.000001+random*0.099999)*float64(solicitInitialRetransmissionTimeout))
	}
	factor := random*0.2 - 0.1
	next := time.Duration(float64(previous) * (2 + factor))
	if next > solicitMaxRetransmissionTimeout {
		next = time.Duration(float64(solicitMaxRetransmissionTimeout) * (1 + factor))
	}
	return next
}

func (c *Client) now() time.Time {
	if c.Config.Now != nil {
		return c.Config.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Client) transaction() (uint32, error) {
	if c.Config.Transaction != nil {
		return c.Config.Transaction()
	}
	var raw [3]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0, err
	}
	return uint32(raw[0])<<16 | uint32(raw[1])<<8 | uint32(raw[2]), nil
}

func (c *Client) random() float64 {
	if c.Config.Random != nil {
		value := c.Config.Random()
		switch {
		case value < 0:
			return 0
		case value >= 1:
			return 1
		default:
			return value
		}
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0.5
	}
	value := uint64(raw[0])<<56 | uint64(raw[1])<<48 | uint64(raw[2])<<40 | uint64(raw[3])<<32 |
		uint64(raw[4])<<24 | uint64(raw[5])<<16 | uint64(raw[6])<<8 | uint64(raw[7])
	return float64(value>>11) / (1 << 53)
}

func seconds(value uint32) time.Duration {
	return time.Duration(value) * time.Second
}
