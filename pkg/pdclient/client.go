package pdclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net/netip"
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

type Config struct {
	Resource    string
	Interface   string
	ClientDUID  []byte
	IAID        uint32
	WantPrefix  int
	Now         func() time.Time
	Transaction func() (uint32, error)
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
	Config    Config
	Transport Transport
	State     State
	Lease     Lease

	lastTransaction uint32
	advertise       Message
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
	if config.IAID == 0 {
		config.IAID = 1
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
	now := c.now()
	if c.State == StateBound || c.State == StateRenewing || c.State == StateRebinding {
		if c.Lease.Valid > 0 && !now.Before(c.Lease.ExpiresAt()) {
			c.State = StateExpired
			c.Lease = Lease{}
			return nil
		}
		if c.State == StateBound && c.Lease.T1 > 0 && !now.Before(c.Lease.RenewAt()) {
			return c.send(ctx, StateRenewing, Message{Type: MessageRenew})
		}
		if (c.State == StateBound || c.State == StateRenewing) && c.Lease.T2 > 0 && !now.Before(c.Lease.RebindAt()) {
			return c.send(ctx, StateRebinding, Message{Type: MessageRebind})
		}
	}
	if c.State == StateIdle || c.State == StateExpired {
		return c.Start(ctx)
	}
	return nil
}

func (c *Client) Handle(ctx context.Context, payload []byte) error {
	msg, err := DecodeMessage(payload)
	if err != nil {
		return err
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
		return c.send(ctx, StateRequesting, Message{Type: MessageRequest, ServerDUID: msg.ServerDUID, Prefix: msg.Prefix})
	case (c.State == StateRequesting || c.State == StateRenewing || c.State == StateRebinding) && msg.Type == MessageReply:
		c.acceptReply(msg)
		return nil
	default:
		return nil
	}
}

func (c *Client) acceptReply(msg Message) {
	now := c.now()
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
	return c.Transport.Send(ctx, OutboundPacket{
		Interface: c.Config.Interface,
		Message:   msg,
		Payload:   payload,
	})
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

func seconds(value uint32) time.Duration {
	return time.Duration(value) * time.Second
}
