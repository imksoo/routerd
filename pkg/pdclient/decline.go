// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"time"
)

// Decline reports DAD failures without releasing healthy addresses or prefixes
// (RFC 9915 18.2.8). One bounded exchange owns the report. A new batch waits
// until it finishes; repeating the pending batch never resets its deadline.
func (c *Client) Decline(ctx context.Context, addresses []netip.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(addresses) == 0 || len(addresses) > maxAssignedAddresses {
		return fmt.Errorf("Decline requires 1..%d assigned addresses", maxAssignedAddresses)
	}
	if c.declineExchange.active() {
		pending := c.declineExchange.message.IANAs[0].Addresses
		for _, address := range addresses {
			if !slices.ContainsFunc(pending, func(a IAAddress) bool { return a.Address == address }) {
				return fmt.Errorf("another DHCPv6 Decline is pending")
			}
		}
		return nil
	}
	if c.Transport == nil {
		return fmt.Errorf("transport is required")
	}
	now := c.now()
	bad := make(map[netip.Addr]bool, len(addresses))
	ia := IANA{IAID: c.Config.IAID}
	for _, address := range addresses {
		if !slices.ContainsFunc(c.Lease.Addresses, func(a AddressLease) bool { return a.Address == address && now.Before(a.ExpiresAt()) }) {
			return fmt.Errorf("Decline address %s has no current binding", address)
		}
		if !bad[address] {
			bad[address] = true
			ia.Addresses = append(ia.Addresses, IAAddress{Address: address})
		}
	}
	xid, err := c.newTransaction()
	if err != nil {
		return err
	}
	msg := Message{Type: MessageDecline, TransactionID: xid, ClientDUID: append([]byte(nil), c.Config.ClientDUID...), ServerDUID: append([]byte(nil), c.Lease.ServerDUID...), IANAs: []IANA{ia}}
	if len(msg.ServerDUID) == 0 {
		return fmt.Errorf("Decline has no allocating server identity")
	}
	if _, err := EncodeMessage(msg); err != nil {
		return err
	}
	c.declineExchange = exchange{message: msg}
	kept := make([]AddressLease, 0, len(c.Lease.Addresses))
	for _, address := range c.Lease.Addresses {
		if !bad[address.Address] {
			kept = append(kept, address)
		}
	}
	c.Lease.Addresses = kept
	// Any pending Renew/Rebind is refreshed from live associations on retry.
	return c.retransmit(ctx, &c.declineExchange, now)
}

func (c *Client) tickDecline(ctx context.Context, now time.Time) error {
	if !c.declineExchange.active() || now.Before(c.declineExchange.retryAt) {
		return nil
	}
	if c.declineExchange.attempts >= 4 {
		return c.finishDecline(ctx)
	}
	return c.retransmit(ctx, &c.declineExchange, now)
}

func (c *Client) receiveDeclineReply(ctx context.Context, msg Message) (bool, error) {
	e := c.declineExchange
	if !e.active() || msg.Type != MessageReply || msg.TransactionID != e.message.TransactionID {
		return false, nil
	}
	if !bytes.Equal(msg.ServerDUID, e.message.ServerDUID) {
		return true, nil
	}
	// A Reply terminates this exchange, including NoBinding after a lost Reply.
	// Reply lease options cannot reintroduce the address whose DAD failed.
	return true, c.finishDecline(ctx)
}

func (c *Client) finishDecline(ctx context.Context) error {
	c.declineExchange = exchange{}
	c.pruneBindings(c.now())
	if c.State == StateReleased {
		return nil
	}
	if c.hasBindings() {
		return c.Renew(ctx)
	}
	return c.Start(ctx)
}
