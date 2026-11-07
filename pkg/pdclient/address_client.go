// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"time"
)

const maxAssignedAddresses = 2048

func (c *Client) wantsPrefixes() bool  { return c.Config.Mode != RequestAddresses }
func (c *Client) wantsAddresses() bool { return c.Config.Mode != RequestPrefixDelegation }
func (c *Client) hasBindings() bool {
	return len(c.Lease.Prefixes) > 0 || len(c.Lease.Addresses) > 0
}

// Each IA retains its own timer reception time. Receiving a partial Reply must
// not postpone the unanswered association's renew/rebind deadline.
func (l Lease) associationDeadline(rebind bool, margin time.Duration) time.Time {
	var result time.Time
	add := func(acquired time.Time, t1, t2 time.Duration) {
		timer := t1
		if rebind {
			timer = t2
		}
		at := maxTime(lifetimeDeadline(acquired, timer).Add(-max(time.Duration(0), margin)), acquired.Add(time.Second))
		if result.IsZero() || at.Before(result) {
			result = at
		}
	}
	if len(l.Prefixes) > 0 {
		add(l.AcquiredAt, l.T1, l.T2)
	}
	if len(l.Addresses) > 0 {
		add(l.AddressAcquiredAt, l.AddressT1, l.AddressT2)
	}
	return result
}

func (c *Client) setLeaseAssociations(msg *Message, now time.Time) {
	if c.wantsPrefixes() {
		msg.IAPDs = []IAPD{c.leaseIA(now)}
	}
	if c.wantsAddresses() {
		ia := IANA{IAID: c.Config.IAID}
		for _, a := range c.Lease.Addresses {
			if now.Before(a.ExpiresAt()) {
				// RFC 9915 21.6: clients send zero address lifetimes.
				ia.Addresses = append(ia.Addresses, IAAddress{Address: a.Address})
			}
		}
		msg.IANAs = []IANA{ia}
	}
}

func usableAddress(a IAAddress) bool {
	return a.Address.Is6() && !a.Address.Is4In6() && a.Address.Zone() == "" &&
		!a.Address.IsUnspecified() && !a.Address.IsMulticast() && !a.Address.IsLinkLocalUnicast() &&
		a.Preferred <= a.Valid && a.StatusCode == StatusSuccess
}

func (c *Client) handleAssociations(ctx context.Context, msg Message) error {
	e := &c.leaseExchange
	var pd *IAPD
	if c.wantsPrefixes() {
		for i := range msg.IAPDs {
			ia := &msg.IAPDs[i]
			if ia.IAID == c.Config.IAID && (ia.T1 == 0 || ia.T2 == 0 || ia.T1 <= ia.T2) {
				pd = ia
				break
			}
		}
	}
	var na *IANA
	if c.wantsAddresses() {
		for i := range msg.IANAs {
			ia := &msg.IANAs[i]
			if ia.IAID == c.Config.IAID && (ia.T1 == 0 || ia.T2 == 0 || ia.T1 <= ia.T2) {
				na = ia
				break
			}
		}
	}
	if e.message.Type == MessageRequest && (msg.StatusCode == StatusNotOnLink || (pd != nil && pd.StatusCode == StatusNotOnLink) || (na != nil && na.StatusCode == StatusNotOnLink)) {
		c.lastError = "DHCPv6 server reported NotOnLink"
		e.restartDiscovery = true
		return nil
	}
	if msg.StatusCode != StatusSuccess {
		c.lastError = fmt.Sprintf("DHCPv6 status %d", msg.StatusCode)
		return nil
	}
	if e.message.Type == MessageSolicit {
		request := Message{Type: MessageRequest, ServerDUID: msg.ServerDUID, ORO: requestedOptions()}
		offered := false
		if c.wantsPrefixes() {
			ia := IAPD{IAID: c.Config.IAID}
			if pd != nil && pd.StatusCode == StatusSuccess {
				for _, p := range pd.Prefixes {
					if usablePrefix(p) && p.Valid > 0 {
						ia.Prefixes = append(ia.Prefixes, IAPrefix{Prefix: p.Prefix})
					}
				}
			}
			offered = len(ia.Prefixes) > 0
			request.IAPDs = []IAPD{ia}
		}
		if c.wantsAddresses() {
			ia := IANA{IAID: c.Config.IAID}
			if na != nil && na.StatusCode == StatusSuccess {
				for _, a := range na.Addresses {
					if usableAddress(a) && a.Valid > 0 {
						ia.Addresses = append(ia.Addresses, IAAddress{Address: a.Address})
					}
				}
			}
			offered = offered || len(ia.Addresses) > 0
			request.IANAs = []IANA{ia}
		}
		if offered {
			return c.send(ctx, StateRequesting, request)
		}
		return nil
	}

	c.acceptInformation(msg, false)
	c.lastError = ""
	pdDone, naDone := !c.wantsPrefixes(), !c.wantsAddresses()
	accepted := false
	noBinding := false
	if pd != nil {
		if pd.StatusCode == StatusSuccess {
			c.acceptPrefixReply(*pd)
			pdDone, accepted = true, true
		} else {
			c.lastError = fmt.Sprintf("DHCPv6 IA_PD status %d", pd.StatusCode)
			pdDone = pd.StatusCode == StatusNoPrefixAvail && len(c.Lease.Prefixes) == 0
			noBinding = pd.StatusCode == StatusNoBinding
			if pd.StatusCode == StatusUnspecFail {
				c.boundFailure(e, c.now())
			}
		}
	}
	if na != nil {
		if na.StatusCode == StatusSuccess {
			c.acceptAddressReply(*na)
			naDone, accepted = true, true
		} else {
			c.lastError = fmt.Sprintf("DHCPv6 IA_NA status %d", na.StatusCode)
			naDone = na.StatusCode == StatusNoAddrsAvail && len(c.Lease.Addresses) == 0
			noBinding = noBinding || na.StatusCode == StatusNoBinding
			if na.StatusCode == StatusUnspecFail {
				c.boundFailure(e, c.now())
			}
		}
	}
	if accepted {
		c.Lease.ServerDUID = append([]byte(nil), msg.ServerDUID...)
		c.Lease.IAID = c.Config.IAID
	}
	if noBinding && (e.message.Type == MessageRenew || e.message.Type == MessageRebind) {
		request := Message{Type: MessageRequest, ServerDUID: msg.ServerDUID, ORO: requestedOptions()}
		c.setLeaseAssociations(&request, c.now())
		return c.send(ctx, StateRequesting, request)
	}
	if !accepted {
		return nil
	}
	if !c.hasBindings() {
		c.State = StateExpired
		c.Lease = Lease{}
		c.leaseExchange = exchange{}
	} else if pdDone && naDone {
		c.State = StateBound
		c.leaseExchange = exchange{}
	} else if c.State == StateRequesting {
		// A usable partial acquisition is available immediately. Keep the
		// unanswered exchange's original identity/backoff until it completes.
		c.State = StateBound
	}
	return nil
}

func (c *Client) acceptAddressReply(ia IANA) {
	now := c.now()
	byAddress := make(map[netip.Addr]AddressLease, len(c.Lease.Addresses)+len(ia.Addresses))
	for _, a := range c.Lease.Addresses {
		if now.Before(a.ExpiresAt()) {
			byAddress[a.Address] = a
		}
	}
	for _, a := range ia.Addresses {
		if usableAddress(a) && a.Valid == 0 {
			delete(byAddress, a.Address)
		}
	}
	for _, a := range ia.Addresses {
		if !usableAddress(a) || a.Valid == 0 {
			continue
		}
		if _, exists := byAddress[a.Address]; !exists && len(byAddress) >= maxAssignedAddresses {
			c.lastError = "assigned address storage limit reached"
			continue
		}
		byAddress[a.Address] = AddressLease{Address: a.Address, Preferred: seconds(a.Preferred), Valid: seconds(a.Valid), AcquiredAt: now}
	}
	c.Lease.Addresses = nil
	for _, a := range byAddress {
		c.Lease.Addresses = append(c.Lease.Addresses, a)
	}
	slices.SortFunc(c.Lease.Addresses, func(a, b AddressLease) int { return a.Address.Compare(b.Address) })
	base := infinity
	for _, a := range c.Lease.Addresses {
		if now.Before(a.PreferredUntil()) {
			base = min(base, a.PreferredUntil().Sub(now))
		}
	}
	if base == infinity {
		for _, a := range c.Lease.Addresses {
			base = min(base, a.ExpiresAt().Sub(now))
		}
	}
	c.Lease.AddressAcquiredAt = now
	c.Lease.AddressT1, c.Lease.AddressT2 = resolveAssociationTimers(ia.T1, ia.T2, base)
}
