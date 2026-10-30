// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func addressClient(t *testing.T, now *time.Time, mode RequestMode) (*Client, *memoryTransport) {
	t.Helper()
	c, transport := testClient(t, now)
	config := c.Config
	config.Mode = mode
	c, err := New(config, transport)
	if err != nil {
		t.Fatal(err)
	}
	return c, transport
}

func addressResponse(c *Client, kind uint8, addresses ...IAAddress) Message {
	return Message{Type: kind, TransactionID: c.prefixExchange.message.TransactionID,
		ClientDUID: c.Config.ClientDUID, ServerDUID: testServer,
		IANAs: []IANA{{IAID: c.Config.IAID, T1: 30, T2: 60, Addresses: addresses}}}
}

func testAddress(text string, preferred, valid uint32) IAAddress {
	return IAAddress{Address: netip.MustParseAddr(text), Preferred: preferred, Valid: valid}
}

func acquireAddresses(t *testing.T, c *Client, mixed bool) {
	t.Helper()
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []uint8{MessageAdvertise, MessageReply} {
		msg := addressResponse(c, kind, testAddress("2001:db8::123", 90, 120))
		if mixed {
			msg.IAPDs = []IAPD{{IAID: c.Config.IAID, T1: 45, T2: 75, Prefixes: []IAPrefix{prefix("2001:db8:10::/56", 90, 120)}}}
		}
		receive(t, c, msg)
	}
	if c.State != StateBound || len(c.Lease.Addresses) != 1 {
		t.Fatalf("IA_NA not acquired: state=%s lease=%+v", c.State, c.Lease)
	}
}

func TestAddressClientStandaloneAcquisitionExpiryRecoveryAndRelease(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	c, transport := addressClient(t, &now, RequestAddresses)
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if msg := transport.last().Message; len(msg.IAPDs) != 0 || len(msg.IANAs) != 1 || len(msg.IANAs[0].Addresses) != 0 {
		t.Fatalf("address-only Solicit contains prefix request or address hint: %+v", msg)
	}
	acquireAddresses(t, c, false)
	for _, step := range []struct {
		at    time.Duration
		state State
		type_ uint8
	}{{30 * time.Second, StateRenewing, MessageRenew}, {60 * time.Second, StateRebinding, MessageRebind}} {
		now = c.Lease.Addresses[0].AcquiredAt.Add(step.at)
		if err := c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		msg := transport.last().Message
		if c.State != step.state || msg.Type != step.type_ || len(msg.IAPDs) != 0 || len(msg.IANAs) != 1 || len(msg.IANAs[0].Addresses) != 1 {
			t.Fatalf("address lease transition: state=%s msg=%+v", c.State, msg)
		}
	}
	now = now.Add(60 * time.Second)
	if err := c.Tick(ctx); err != nil || c.State != StateExpired || len(c.Lease.Addresses) != 0 {
		t.Fatalf("expired address retained: state=%s lease=%+v err=%v", c.State, c.Lease, err)
	}
	if err := c.Tick(ctx); err != nil || c.State != StateSoliciting {
		t.Fatalf("no autonomous discovery after expiry: %s %v", c.State, err)
	}
	acquireAddresses(t, c, false)
	if err := c.Release(ctx); err != nil || c.State != StateReleased || len(c.Lease.Addresses) != 0 {
		t.Fatalf("release did not revoke local address: %s %v", c.State, err)
	}
	if msg := transport.last().Message; msg.Type != MessageRelease || len(msg.IANAs) != 1 || len(msg.IANAs[0].Addresses) != 1 {
		t.Fatalf("release omitted owned address: %+v", msg)
	}
}

func TestAddressClientCombinedRefreshDoesNotExtendMissingPrefix(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	c, transport := addressClient(t, &now, RequestPrefixAndAddress)
	acquireAddresses(t, c, true)
	originalPrefixExpiry := c.Lease.Prefixes[0].ExpiresAt()
	if c.Lease.RenewAt() != now.Add(30*time.Second) || c.Lease.RebindAt() != now.Add(60*time.Second) {
		t.Fatalf("combined lease did not choose earliest IA timers: %+v", c.Lease)
	}
	now = now.Add(30 * time.Second)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	renew := transport.last().Message
	if len(renew.IAPDs) != 1 || len(renew.IANAs) != 1 {
		t.Fatalf("combined Renew split associations: %+v", renew)
	}
	receive(t, c, addressResponse(c, MessageReply, testAddress("2001:db8::123", 180, 240)))
	if c.Lease.Prefixes[0].ExpiresAt() != originalPrefixExpiry {
		t.Fatal("address refresh extended unconfirmed prefix lifetime")
	}
	if !c.prefixExchange.active() {
		t.Fatal("missing PD answer silently completed Renew")
	}
	now = c.prefixExchange.retryAt
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if retry := transport.last().Message; retry.TransactionID != renew.TransactionID || len(retry.IANAs) != 1 || len(retry.IAPDs) != 1 {
		t.Fatalf("partial Reply lost bounded combined retry: %+v", retry)
	}
	now = originalPrefixExpiry
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.Lease.Prefixes) != 0 || len(c.Lease.Addresses) != 1 || c.State == StateExpired {
		t.Fatalf("prefix expiry destroyed still-valid address: %+v", c.Lease)
	}
}

func TestAddressClientPartialOfferRetriesMissingAddressInRenew(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	c, transport := addressClient(t, &now, RequestPrefixAndAddress)
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []uint8{MessageAdvertise, MessageReply} {
		msg := response(c, kind, prefix("2001:db8:10::/56", 90, 120))
		msg.IANAs = []IANA{{IAID: c.Config.IAID, StatusCode: StatusNoAddrsAvail}}
		receive(t, c, msg)
	}
	if c.State != StateBound || len(c.Lease.Prefixes) != 1 || len(c.Lease.Addresses) != 0 {
		t.Fatalf("partial server offer not usable: state=%s lease=%+v", c.State, c.Lease)
	}
	now = now.Add(30 * time.Second)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if msg := transport.last().Message; len(msg.IANAs) != 1 || len(msg.IANAs[0].Addresses) != 0 || len(msg.IAPDs) != 1 {
		t.Fatalf("missing address association never requested again: %+v", msg)
	}
	msg := addressResponse(c, MessageReply, testAddress("2001:db8::123", 90, 120))
	msg.IAPDs = response(c, MessageReply, prefix("2001:db8:10::/56", 90, 120)).IAPDs
	receive(t, c, msg)
	if len(c.Lease.Addresses) != 1 || len(c.Lease.Prefixes) != 1 || c.State != StateBound {
		t.Fatalf("missing association did not recover: %+v", c.Lease)
	}
}

func TestAddressClientWithdrawalKeepsPDAndRestoresIndependentExpiry(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	c, _ := addressClient(t, &now, RequestPrefixAndAddress)
	acquireAddresses(t, c, true)
	now = now.Add(10 * time.Second)
	before := c.Snapshot()
	restored, _ := addressClient(t, &now, RequestPrefixAndAddress)
	restored.Restore(before)
	if restored.State != StateBound || len(restored.Lease.Addresses) != 1 || restored.Lease.Addresses[0].ExpiresAt() != c.Lease.Addresses[0].ExpiresAt() {
		t.Fatalf("restore lost or extended address: %+v", restored.Lease)
	}
	if err := restored.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := addressResponse(restored, MessageReply, testAddress("2001:db8::123", 0, 0))
	msg.IAPDs = response(restored, MessageReply, prefix("2001:db8:10::/56", 90, 120)).IAPDs
	receive(t, restored, msg)
	if len(restored.Lease.Addresses) != 0 || len(restored.Lease.Prefixes) != 1 || restored.State != StateBound {
		t.Fatalf("address withdrawal damaged PD: %+v", restored.Lease)
	}
	addressOnly, _ := addressClient(t, &now, RequestAddresses)
	addressOnly.Restore(before)
	if addressOnly.State != StateIdle || len(addressOnly.Lease.Prefixes) != 0 || len(addressOnly.Lease.Addresses) != 0 {
		t.Fatal("changed request mode adopted another configuration's snapshot")
	}
}
