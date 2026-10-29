// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
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
	StateReleased   State = "released"
)

const (
	solicitInitialRetransmissionTimeout = time.Second
	solicitMaxRetransmissionTimeout     = time.Hour
	// Bound retained state even if a server continually adds long-lived prefixes.
	maxDelegatedPrefixes            = 2048
	infinity                        = time.Duration(0xffffffff) * time.Second
	informationFailureRetryDuration = time.Minute
)

// ErrSend leaves a scheduled exchange owned by the client; callers may keep
// ticking it after recording the failure. Encoding and cancellation errors do
// not use this sentinel.
var ErrSend = errors.New("DHCPv6 transport send failed")

var infiniteDeadline = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

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

// PrefixLease retains its own reception time. Updating another prefix in the
// association must not extend this prefix's preferred or valid lifetime.
type PrefixLease struct {
	Prefix     netip.Prefix
	Preferred  time.Duration
	Valid      time.Duration
	AcquiredAt time.Time
}

func (p PrefixLease) PreferredUntil() time.Time { return lifetimeDeadline(p.AcquiredAt, p.Preferred) }
func (p PrefixLease) ExpiresAt() time.Time      { return lifetimeDeadline(p.AcquiredAt, p.Valid) }

type Lease struct {
	ServerDUID    []byte
	IAID          uint32
	T1            time.Duration
	T2            time.Duration
	AcquiredAt    time.Time
	Prefixes      []PrefixLease
	CurrentPrefix netip.Prefix
}

func (l Lease) RenewAt() time.Time  { return lifetimeDeadline(l.AcquiredAt, l.T1) }
func (l Lease) RebindAt() time.Time { return lifetimeDeadline(l.AcquiredAt, l.T2) }

func lifetimeDeadline(start time.Time, duration time.Duration) time.Time {
	if duration == infinity {
		return infiniteDeadline
	}
	return start.Add(duration)
}

func remainingLifetime(start time.Time, duration time.Duration, now time.Time) uint32 {
	if duration == infinity {
		return 0xffffffff
	}
	left := max(time.Duration(0), start.Add(duration).Sub(now))
	return uint32(left / time.Second)
}

type Information struct {
	AFTRName     string
	DNSServers   []netip.Addr
	SNTPServers  []netip.Addr
	DomainSearch []string
	UpdatedAt    time.Time
	LastSentAt   time.Time
	RefreshAt    time.Time
	StatusCode   uint16
	LastError    string
}

type Transport interface {
	Send(context.Context, OutboundPacket) error
}
type OutboundPacket struct {
	Interface string
	Message   Message
	Payload   []byte
}

// Each exchange has one owner: the daemon's existing receive/tick loop. There
// are no retry goroutines, and starting an exchange replaces its old ownership.
type exchange struct {
	message          Message
	started          time.Time
	retryAt          time.Time
	timeout          time.Duration
	attempts         int
	failureUntil     time.Time
	restartDiscovery bool
}

func (e exchange) active() bool { return e.message.Type != 0 }

type Client struct {
	Config          Config
	Transport       Transport
	State           State
	Lease           Lease
	Info            Information
	prefixExchange  exchange
	infoExchange    exchange
	solicitMaxRT    time.Duration
	infoMaxRT       time.Duration
	lastError       string
	lastSentAt      time.Time
	lastMessageType uint8
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
	if len(config.ClientDUID) > 130 {
		return nil, fmt.Errorf("client DUID exceeds the RFC 9915 size limit")
	}
	if config.WantPrefix == 0 {
		config.WantPrefix = 60
	}
	if config.WantPrefix < 1 || config.WantPrefix > 128 {
		return nil, fmt.Errorf("prefix length must be between 1 and 128")
	}
	config.ClientDUID = append([]byte(nil), config.ClientDUID...)
	return &Client{Config: config, Transport: transport, State: StateIdle, solicitMaxRT: solicitMaxRetransmissionTimeout, infoMaxRT: time.Hour}, nil
}

func requestedOptions() []uint16 {
	return []uint16{optionSOLMaxRT, optionAFTRName, optionDNSServers, optionSNTPServers, optionDomainSearch}
}

func (c *Client) Start(ctx context.Context) error {
	ia := IAPD{IAID: c.Config.IAID, Prefixes: []IAPrefix{{Prefix: netip.PrefixFrom(netip.IPv6Unspecified(), c.Config.WantPrefix)}}}
	return c.send(ctx, StateSoliciting, Message{Type: MessageSolicit, IAPDs: []IAPD{ia}, ORO: requestedOptions()})
}

func (c *Client) Tick(ctx context.Context) error { return c.TickWithMargin(ctx, 0, 0) }

func (c *Client) TickWithMargin(ctx context.Context, renewMargin, rebindMargin time.Duration) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := c.now()
	if infoErr := c.tickInformation(ctx, now); infoErr != nil {
		defer func() { result = errors.Join(infoErr, result) }()
	}
	c.prunePrefixes(now)
	if c.State == StateBound || c.State == StateRenewing || c.State == StateRebinding {
		if len(c.Lease.Prefixes) == 0 {
			c.Lease = Lease{}
			c.State = StateExpired
			c.prefixExchange = exchange{}
			return nil
		}
		// A margin must never turn a zero server timer into immediate transmissions.
		rebindAt := maxTime(c.Lease.RebindAt().Add(-max(time.Duration(0), rebindMargin)), c.Lease.AcquiredAt.Add(time.Second))
		renewAt := maxTime(c.Lease.RenewAt().Add(-max(time.Duration(0), renewMargin)), c.Lease.AcquiredAt.Add(time.Second))
		if (c.State == StateBound || c.State == StateRenewing) && !now.Before(rebindAt) {
			return c.Rebind(ctx)
		}
		if c.State == StateBound && !now.Before(renewAt) {
			return c.Renew(ctx)
		}
	}
	e := &c.prefixExchange
	if e.active() && !now.Before(e.retryAt) {
		if e.restartDiscovery {
			return c.Start(ctx)
		}
		if !e.failureUntil.IsZero() && !now.Before(e.failureUntil) {
			switch c.State {
			case StateRenewing:
				return c.Rebind(ctx)
			case StateRebinding:
				// Stop retrying this failed exchange while keeping unexpired bindings.
				c.State = StateBound
				c.Lease.AcquiredAt = now
				c.Lease.T1 = time.Minute
				c.Lease.T2 = 2 * time.Minute
				c.prefixExchange = exchange{}
				return nil
			default:
				return c.Start(ctx)
			}
		}
		if c.State == StateRequesting && e.attempts >= 10 {
			return c.Start(ctx)
		}
		if e.message.Type == MessageRelease && e.attempts >= 4 {
			c.prefixExchange = exchange{}
			return nil
		}
		if e.message.Type == MessageRenew || e.message.Type == MessageRebind {
			e.message.IAPDs = []IAPD{c.leaseIA(now)}
		}
		return c.retransmit(ctx, e, now)
	}
	if c.State == StateIdle || c.State == StateExpired {
		return c.Start(ctx)
	}
	return nil
}

func maxTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

// NextRetryAt includes both exchanges, refresh, and lease deadlines. Receipt of
// unrelated traffic cannot postpone retransmissions in the daemon's tick loop.
func (c *Client) NextRetryAt() time.Time {
	var next time.Time
	add := func(at time.Time) {
		if !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	if c.prefixExchange.active() {
		add(c.prefixExchange.retryAt)
	}
	if c.infoExchange.active() {
		add(c.infoExchange.retryAt)
	} else {
		add(c.Info.RefreshAt)
	}
	if c.State == StateBound {
		add(c.Lease.RenewAt())
		add(c.Lease.RebindAt())
	}
	if c.State == StateRenewing {
		add(c.Lease.RebindAt())
	}
	for _, p := range c.Lease.Prefixes {
		add(p.ExpiresAt())
	}
	return next
}

func (c *Client) Renew(ctx context.Context) error {
	c.prunePrefixes(c.now())
	if len(c.Lease.Prefixes) == 0 {
		return fmt.Errorf("cannot renew without a lease")
	}
	return c.send(ctx, StateRenewing, Message{Type: MessageRenew, ServerDUID: c.Lease.ServerDUID, IAPDs: []IAPD{c.leaseIA(c.now())}, ORO: requestedOptions()})
}

func (c *Client) Rebind(ctx context.Context) error {
	c.prunePrefixes(c.now())
	if len(c.Lease.Prefixes) == 0 {
		return fmt.Errorf("cannot rebind without a lease")
	}
	return c.send(ctx, StateRebinding, Message{Type: MessageRebind, IAPDs: []IAPD{c.leaseIA(c.now())}, ORO: requestedOptions()})
}

// Release immediately withdraws the local association and retries delivery a
// finite number of times. Only an explicit Start can reacquire in this process.
func (c *Client) Release(ctx context.Context) error {
	c.prunePrefixes(c.now())
	if len(c.Lease.Prefixes) == 0 {
		return fmt.Errorf("cannot release without a lease")
	}
	err := c.send(ctx, StateReleased, Message{Type: MessageRelease, ServerDUID: c.Lease.ServerDUID, IAPDs: []IAPD{c.leaseIA(c.now())}})
	if err != nil && !errors.Is(err, ErrSend) {
		return err
	}
	c.Lease = Lease{}
	return err
}

// InfoRequest schedules the required randomized first transmission. A command
// repeated while pending keeps the existing transaction and retry deadline.
func (c *Client) InfoRequest(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.infoExchange.active() {
		return nil
	}
	if c.Transport == nil {
		return fmt.Errorf("transport is required")
	}
	xid, err := c.newTransaction()
	if err != nil {
		return err
	}
	msg := Message{Type: MessageInfoRequest, TransactionID: xid, ClientDUID: append([]byte(nil), c.Config.ClientDUID...), ORO: []uint16{optionINFMaxRT, optionInfoRefreshTime, optionAFTRName, optionDNSServers, optionSNTPServers, optionDomainSearch}}
	if _, err = EncodeMessage(msg); err != nil {
		return err
	}
	c.infoExchange = exchange{message: msg, retryAt: c.now().Add(time.Duration(c.random() * float64(time.Second)))}
	c.Info.RefreshAt = time.Time{}
	c.Info.StatusCode = 0
	c.Info.LastError = ""
	return nil
}

func (c *Client) tickInformation(ctx context.Context, now time.Time) error {
	if !c.infoExchange.active() {
		if !c.Info.RefreshAt.IsZero() && !now.Before(c.Info.RefreshAt) {
			return c.InfoRequest(ctx)
		}
		return nil
	}
	e := &c.infoExchange
	if now.Before(e.retryAt) {
		return nil
	}
	if !e.failureUntil.IsZero() && !now.Before(e.failureUntil) {
		c.infoExchange = exchange{}
		c.Info.LastError = "DHCPv6 Information-request failed after bounded retries"
		c.Info.RefreshAt = now.Add(24 * time.Hour)
		return nil
	}
	return c.retransmit(ctx, e, now)
}

func (c *Client) Handle(ctx context.Context, payload []byte) error {
	msg, err := DecodeMessageForIAID(payload, c.Config.IAID)
	if err != nil {
		return err
	}
	return c.HandleMessage(ctx, msg)
}

func (c *Client) HandleMessage(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(msg.ServerDUID) == 0 || len(msg.ServerDUID) > 130 || !bytes.Equal(msg.ClientDUID, c.Config.ClientDUID) {
		return nil
	}
	// A matching identifier does not revive an exchange whose complete lease
	// authority expired between the preceding tick and packet receipt.
	c.prunePrefixes(c.now())
	if (c.State == StateBound || c.State == StateRenewing || c.State == StateRebinding) && len(c.Lease.Prefixes) == 0 {
		c.Lease = Lease{}
		c.State = StateExpired
		c.prefixExchange = exchange{}
	}
	if c.infoExchange.active() && c.infoExchange.attempts > 0 && msg.Type == MessageReply && msg.TransactionID == c.infoExchange.message.TransactionID {
		c.acceptMaxRT(msg)
		if msg.StatusCode != StatusSuccess {
			c.Info.StatusCode = msg.StatusCode
			c.Info.LastError = fmt.Sprintf("DHCPv6 Information-request status %d", msg.StatusCode)
			if msg.StatusCode == StatusUnspecFail {
				c.boundFailure(&c.infoExchange, c.now())
			} else {
				c.infoExchange = exchange{}
				c.Info.RefreshAt = c.now().Add(24 * time.Hour)
			}
			return nil
		}
		c.acceptInformation(msg, true)
		c.infoExchange = exchange{}
		return nil
	}
	e := &c.prefixExchange
	if !e.active() || msg.TransactionID != e.message.TransactionID {
		return nil
	}
	if c.State == StateSoliciting {
		if msg.Type != MessageAdvertise {
			return nil
		}
	} else if msg.Type != MessageReply {
		return nil
	}
	if (c.State == StateRequesting || c.State == StateRenewing || c.State == StateReleased) && !bytes.Equal(msg.ServerDUID, e.message.ServerDUID) {
		return nil
	}
	c.acceptMaxRT(msg)
	if c.State == StateReleased && e.message.Type == MessageRelease {
		c.prefixExchange = exchange{}
		return nil
	}
	if msg.StatusCode == StatusUnspecFail {
		c.lastError = "DHCPv6 server reported UnspecFail"
		c.boundFailure(e, c.now())
		return nil
	}
	var ia *IAPD
	for i := range msg.IAPDs {
		if msg.IAPDs[i].IAID == c.Config.IAID {
			ia = &msg.IAPDs[i]
			break
		}
	}
	if c.State == StateRequesting && (msg.StatusCode == StatusNotOnLink || (ia != nil && ia.StatusCode == StatusNotOnLink)) {
		c.lastError = "DHCPv6 server reported NotOnLink"
		e.restartDiscovery = true
		return nil
	}
	if msg.StatusCode != StatusSuccess {
		c.lastError = fmt.Sprintf("DHCPv6 status %d", msg.StatusCode)
		return nil
	}
	// An absent or independently discarded IA does not revoke a binding or reset
	// the exchange. Other requested configuration remains independently usable.
	if c.State != StateSoliciting {
		c.acceptInformation(msg, false)
	}
	if ia == nil {
		return nil
	}
	if ia.T1 > 0 && ia.T2 > 0 && ia.T1 > ia.T2 {
		return nil
	}
	if (c.State == StateRenewing || c.State == StateRebinding) && ia.StatusCode == StatusNoBinding {
		return c.send(ctx, StateRequesting, Message{Type: MessageRequest, ServerDUID: msg.ServerDUID, IAPDs: []IAPD{c.leaseIA(c.now())}, ORO: requestedOptions()})
	}
	if ia.StatusCode != StatusSuccess {
		c.lastError = fmt.Sprintf("DHCPv6 IA_PD status %d", ia.StatusCode)
		if ia.StatusCode == StatusUnspecFail {
			c.boundFailure(e, c.now())
		}
		return nil
	}
	if c.State == StateSoliciting {
		var prefixes []IAPrefix
		for _, p := range ia.Prefixes {
			if usablePrefix(p) && p.Valid > 0 {
				prefixes = append(prefixes, IAPrefix{Prefix: p.Prefix})
			}
		}
		if len(prefixes) == 0 {
			return nil
		}
		return c.send(ctx, StateRequesting, Message{Type: MessageRequest, ServerDUID: msg.ServerDUID, IAPDs: []IAPD{{IAID: c.Config.IAID, Prefixes: prefixes}}, ORO: requestedOptions()})
	}
	if c.State == StateRequesting || c.State == StateRenewing || c.State == StateRebinding {
		c.acceptReply(msg.ServerDUID, *ia)
	}
	return nil
}

func usablePrefix(p IAPrefix) bool {
	return p.Prefix.IsValid() && p.Prefix.Addr().Is6() && !p.Prefix.Addr().Is4In6() && p.Preferred <= p.Valid && p.StatusCode == StatusSuccess
}

func (c *Client) acceptMaxRT(msg Message) {
	if msg.SOLMaxRT >= 60 && msg.SOLMaxRT <= 86400 {
		c.solicitMaxRT = seconds(msg.SOLMaxRT)
	}
	if msg.INFMaxRT >= 60 && msg.INFMaxRT <= 86400 {
		c.infoMaxRT = seconds(msg.INFMaxRT)
	}
}

func (c *Client) boundFailure(e *exchange, now time.Time) {
	if e.failureUntil.IsZero() {
		e.failureUntil = now.Add(informationFailureRetryDuration)
	}
	if e.retryAt.After(e.failureUntil) {
		e.retryAt = e.failureUntil
	}
}

func (c *Client) acceptInformation(msg Message, refresh bool) {
	c.Info.AFTRName = msg.AFTRName
	c.Info.DNSServers = append([]netip.Addr(nil), msg.DNSServers...)
	c.Info.SNTPServers = append([]netip.Addr(nil), msg.SNTPServers...)
	c.Info.DomainSearch = append([]string(nil), msg.DomainSearch...)
	c.Info.UpdatedAt = c.now()
	c.Info.StatusCode = 0
	c.Info.LastError = ""
	if refresh {
		value := uint32(86400)
		if msg.InfoRefreshTime != nil {
			value = max(uint32(600), *msg.InfoRefreshTime)
		}
		c.Info.RefreshAt = lifetimeDeadline(c.now(), seconds(value))
	}
}

func (c *Client) acceptReply(server []byte, ia IAPD) {
	now := c.now()
	c.lastError = ""
	c.prunePrefixes(now)
	byPrefix := make(map[netip.Prefix]PrefixLease, len(c.Lease.Prefixes)+len(ia.Prefixes))
	for _, p := range c.Lease.Prefixes {
		byPrefix[p.Prefix] = p
	}
	// Apply withdrawals first so an update at the storage bound can replace
	// revoked prefixes regardless of the server's option ordering.
	for _, p := range ia.Prefixes {
		if usablePrefix(p) && p.Valid == 0 {
			delete(byPrefix, p.Prefix.Masked())
		}
	}
	var selected netip.Prefix
	for _, p := range ia.Prefixes {
		if !usablePrefix(p) {
			continue
		}
		p.Prefix = p.Prefix.Masked()
		if p.Valid == 0 {
			continue
		}
		if _, exists := byPrefix[p.Prefix]; !exists && len(byPrefix) >= maxDelegatedPrefixes {
			c.lastError = "delegated prefix storage limit reached"
			continue
		}
		byPrefix[p.Prefix] = PrefixLease{Prefix: p.Prefix, Preferred: seconds(p.Preferred), Valid: seconds(p.Valid), AcquiredAt: now}
		if !selected.IsValid() && p.Preferred > 0 {
			selected = p.Prefix
		}
	}
	oldCurrent := c.Lease.CurrentPrefix
	c.Lease = Lease{ServerDUID: append([]byte(nil), server...), IAID: ia.IAID, AcquiredAt: now, CurrentPrefix: oldCurrent}
	for _, p := range byPrefix {
		c.Lease.Prefixes = append(c.Lease.Prefixes, p)
	}
	slices.SortFunc(c.Lease.Prefixes, func(a, b PrefixLease) int { return strings.Compare(a.Prefix.String(), b.Prefix.String()) })
	if selected.IsValid() {
		c.Lease.CurrentPrefix = selected
	}
	c.selectCurrent(now)
	c.Lease.T1, c.Lease.T2 = associationTimers(ia, c.Lease.Prefixes, now)
	c.prefixExchange = exchange{}
	if len(c.Lease.Prefixes) == 0 {
		c.State = StateExpired
		c.Lease = Lease{}
	} else {
		c.State = StateBound
	}
}

func associationTimers(ia IAPD, prefixes []PrefixLease, now time.Time) (time.Duration, time.Duration) {
	base := infinity
	for _, p := range prefixes {
		if p.Preferred > 0 && now.Before(p.PreferredUntil()) {
			base = min(base, p.PreferredUntil().Sub(now))
		}
	}
	if base == infinity {
		for _, p := range prefixes {
			base = min(base, p.ExpiresAt().Sub(now))
		}
	}
	if base <= 0 || base >= infinity {
		base = 24 * time.Hour
	}
	t1, t2 := seconds(ia.T1), seconds(ia.T2)
	if t2 == 0 {
		t2 = max(time.Second, max((base*4/5)/time.Second*time.Second, t1))
	}
	if t1 == 0 {
		t1 = max(time.Second, min(base/2, t2*5/8)/time.Second*time.Second)
	}
	return t1, t2
}

func (c *Client) prunePrefixes(now time.Time) {
	c.Lease.Prefixes = slices.DeleteFunc(c.Lease.Prefixes, func(p PrefixLease) bool { return !now.Before(p.ExpiresAt()) })
	c.selectCurrent(now)
}

func (c *Client) selectCurrent(now time.Time) {
	c.Lease.CurrentPrefix = currentPrefixAt(c.Lease.Prefixes, c.Lease.CurrentPrefix, now)
}

func currentPrefixAt(prefixes []PrefixLease, current netip.Prefix, now time.Time) netip.Prefix {
	var fallback, preferred netip.Prefix
	for _, p := range prefixes {
		if !now.Before(p.ExpiresAt()) {
			continue
		}
		if p.Prefix == current && now.Before(p.PreferredUntil()) {
			return current
		}
		if !preferred.IsValid() && now.Before(p.PreferredUntil()) {
			preferred = p.Prefix
		}
		if !fallback.IsValid() || p.Prefix == current {
			fallback = p.Prefix
		}
	}
	if preferred.IsValid() {
		return preferred
	}
	return fallback
}

func (c *Client) leaseIA(now time.Time) IAPD {
	ia := IAPD{IAID: c.Config.IAID}
	for _, p := range c.Lease.Prefixes {
		if now.Before(p.ExpiresAt()) {
			ia.Prefixes = append(ia.Prefixes, IAPrefix{Prefix: p.Prefix, Preferred: remainingLifetime(p.AcquiredAt, p.Preferred, now), Valid: remainingLifetime(p.AcquiredAt, p.Valid, now)})
		}
	}
	return ia
}

func (c *Client) send(ctx context.Context, next State, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Transport == nil {
		return fmt.Errorf("transport is required")
	}
	xid, err := c.newTransaction()
	if err != nil {
		return err
	}
	msg.TransactionID = xid
	msg.ClientDUID = append([]byte(nil), c.Config.ClientDUID...)
	msg.ServerDUID = append([]byte(nil), msg.ServerDUID...)
	payload, err := EncodeMessage(msg)
	if err != nil {
		return err
	}
	c.State = next
	c.prefixExchange = exchange{}
	if next == StateSoliciting || next == StateRequesting || next == StateRenewing || next == StateRebinding || (next == StateReleased && msg.Type == MessageRelease) {
		now := c.now()
		c.prefixExchange = exchange{message: msg, started: now, attempts: 1}
		c.scheduleRetry(&c.prefixExchange, now)
	}
	if err := c.Transport.Send(ctx, OutboundPacket{Interface: c.Config.Interface, Message: msg, Payload: payload}); err != nil {
		c.lastError = err.Error()
		return fmt.Errorf("%w: %w", ErrSend, err)
	}
	c.lastSentAt, c.lastMessageType = c.now(), msg.Type
	return nil
}

func (c *Client) retransmit(ctx context.Context, e *exchange, now time.Time) error {
	if e.attempts == 0 {
		e.started = now
	}
	elapsed := max(time.Duration(0), now.Sub(e.started)) / (10 * time.Millisecond)
	e.message.ElapsedTime = uint16(min(elapsed, 65535))
	payload, err := EncodeMessage(e.message)
	if err != nil {
		return err
	}
	e.attempts++
	c.scheduleRetry(e, now)
	err = c.Transport.Send(ctx, OutboundPacket{Interface: c.Config.Interface, Message: e.message, Payload: payload})
	if err != nil {
		if e.message.Type == MessageInfoRequest {
			c.Info.LastError = err.Error()
		} else {
			c.lastError = err.Error()
		}
		return fmt.Errorf("%w: %w", ErrSend, err)
	}
	if e.message.Type == MessageInfoRequest {
		c.Info.LastSentAt = now
	} else {
		c.lastSentAt, c.lastMessageType = now, e.message.Type
	}
	return nil
}

func (c *Client) scheduleRetry(e *exchange, now time.Time) {
	initial, maximum := 10*time.Second, 10*time.Minute
	switch e.message.Type {
	case MessageSolicit:
		initial, maximum = time.Second, c.solicitMaxRT
	case MessageRequest:
		initial, maximum = time.Second, 30*time.Second
	case MessageInfoRequest:
		initial, maximum = time.Second, c.infoMaxRT
	case MessageRelease:
		initial, maximum = time.Second, time.Duration(1<<63-1)
	}
	e.timeout = c.nextExchangeRetryTimeout(e.timeout, initial, maximum, e.message.Type == MessageSolicit)
	e.retryAt = now.Add(e.timeout)
	if !e.failureUntil.IsZero() && e.retryAt.After(e.failureUntil) {
		e.retryAt = e.failureUntil
	}
}

func (c *Client) nextExchangeRetryTimeout(previous, initial, maximum time.Duration, strictlyAboveInitial bool) time.Duration {
	random := c.random()
	if previous <= 0 {
		if strictlyAboveInitial {
			return initial + time.Duration((0.000001+random*0.099999)*float64(initial))
		}
		return time.Duration(float64(initial) * (0.9 + random*0.2))
	}
	factor := random*0.2 - 0.1
	next := time.Duration(float64(previous) * (2 + factor))
	if next > maximum {
		next = time.Duration(float64(maximum) * (1 + factor))
	}
	return next
}

func (c *Client) now() time.Time {
	if c.Config.Now != nil {
		return c.Config.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Client) newTransaction() (uint32, error) {
	xid, err := c.transaction()
	if err != nil {
		return 0, err
	}
	if xid > 0xffffff {
		return 0, fmt.Errorf("transaction ID must fit in 24 bits")
	}
	// Concurrent prefix and information exchanges must never share an identity.
	for (c.prefixExchange.active() && xid == c.prefixExchange.message.TransactionID) || (c.infoExchange.active() && xid == c.infoExchange.message.TransactionID) {
		xid = (xid + 1) & 0xffffff
	}
	return xid, nil
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
		if value < 0 {
			return 0
		}
		if value >= 1 {
			return 1
		}
		return value
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return .5
	}
	value := uint64(raw[0])<<56 | uint64(raw[1])<<48 | uint64(raw[2])<<40 | uint64(raw[3])<<32 | uint64(raw[4])<<24 | uint64(raw[5])<<16 | uint64(raw[6])<<8 | uint64(raw[7])
	return float64(value>>11) / (1 << 53)
}

func seconds(value uint32) time.Duration { return time.Duration(value) * time.Second }
