// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func declineForTest(t *testing.T, c *Client, addresses ...netip.Addr) error {
	t.Helper()
	client, ok := any(c).(interface {
		Decline(context.Context, []netip.Addr) error
	})
	if !ok {
		t.Fatal("DHCPv6 client has no DAD failure/Decline path")
	}
	return client.Decline(t.Context(), addresses)
}

func TestDeclineWithdrawsOnlyConflictingAddress(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	c, transport := addressClient(t, &now, RequestPrefixAndAddress)
	acquireAddresses(t, c, true)
	bad := c.Lease.Addresses[0].Address
	good := c.Lease.Addresses[0]
	good.Address = netip.MustParseAddr("2001:db8::456")
	c.Lease.Addresses = append(c.Lease.Addresses, good)
	prefix := c.Lease.Prefixes[0]
	now = now.Add(5 * time.Second)
	if err := declineForTest(t, c, bad); err != nil {
		t.Fatal(err)
	}
	msg := transport.last().Message
	if msg.Type != 9 || len(msg.IAPDs) != 0 || len(msg.IANAs) != 1 || len(msg.IANAs[0].Addresses) != 1 || msg.IANAs[0].Addresses[0].Address != bad || msg.IANAs[0].Addresses[0].Valid != 0 || !bytes.Equal(msg.ServerDUID, testServer) {
		t.Fatalf("Decline must contain only the conflicting IA_NA address: %+v", msg)
	}
	if len(c.Lease.Addresses) != 1 || c.Lease.Addresses[0] != good || len(c.Lease.Prefixes) != 1 || c.Lease.Prefixes[0] != prefix {
		t.Fatalf("Decline changed a healthy binding or lifetime: %+v", c.Lease)
	}
	deadline := c.NextRetryAt()
	if err := declineForTest(t, c, bad); err != nil || !deadline.Equal(c.NextRetryAt()) {
		t.Fatalf("repeated DAD report reset retry ownership: %v", err)
	}
	// A foreign server cannot terminate the report or trigger a new lease exchange.
	reply := Message{Type: MessageReply, TransactionID: msg.TransactionID, ClientDUID: c.Config.ClientDUID, ServerDUID: []byte{9}, StatusCode: StatusNoBinding}
	receive(t, c, reply)
	if transport.last().Message.Type != 9 {
		t.Fatal("foreign Decline reply accepted")
	}
	reply.ServerDUID = testServer
	receive(t, c, reply)
	if transport.last().Message.Type != MessageRenew {
		t.Fatal("NoBinding must complete Decline and resume lease acquisition")
	}
	if c.Lease.Prefixes[0] != prefix || c.Lease.Addresses[0] != good {
		t.Fatal("Decline acknowledgment extended/released a healthy sibling")
	}
}

func TestDeclineRetryBoundAndAddressOnlyRecovery(t *testing.T) {
	now := time.Now().UTC()
	c, transport := addressClient(t, &now, RequestAddresses)
	acquireAddresses(t, c, false)
	if err := declineForTest(t, c, c.Lease.Addresses[0].Address); err != nil {
		t.Fatal(err)
	}
	if len(c.Snapshot().Addresses) != 0 {
		t.Fatal("DAD-failed address remains usable in snapshot")
	}
	for attempt := 1; attempt < 4; attempt++ {
		now = c.NextRetryAt()
		if err := c.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		if transport.last().Message.Type != 9 {
			t.Fatal("new acquisition replaced pending Decline")
		}
	}
	now = c.NextRetryAt()
	if err := c.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.State != StateSoliciting || transport.last().Message.Type != MessageSolicit {
		t.Fatalf("bounded Decline did not resume discovery: %s %+v", c.State, transport.last().Message)
	}
}

func TestDeclineValidatesWholeRequestBeforeChangingBindings(t *testing.T) {
	now := time.Now().UTC()
	c, _ := addressClient(t, &now, RequestAddresses)
	acquireAddresses(t, c, false)
	bad := c.Lease.Addresses[0]
	if err := declineForTest(t, c, bad.Address, netip.MustParseAddr("2001:db8::ffff")); err == nil {
		t.Fatal("foreign address report accepted")
	}
	if len(c.Lease.Addresses) != 1 || c.Lease.Addresses[0] != bad {
		t.Fatal("invalid report partially revoked authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	decliner := any(c).(interface {
		Decline(context.Context, []netip.Addr) error
	})
	if err := decliner.Decline(ctx, []netip.Addr{bad.Address}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	if len(c.Lease.Addresses) != 1 {
		t.Fatal("canceled report changed lease")
	}
}

func TestDeclineSendFailureStillRevokesAddressAndRetries(t *testing.T) {
	now := time.Now().UTC()
	c, transport := addressClient(t, &now, RequestPrefixAndAddress)
	acquireAddresses(t, c, true)
	transport.failure = errors.New("temporary send failure")
	if err := declineForTest(t, c, c.Lease.Addresses[0].Address); !errors.Is(err, ErrSend) {
		t.Fatalf("send failure lost: %v", err)
	}
	if len(c.Lease.Addresses) != 0 || len(c.Lease.Prefixes) != 1 {
		t.Fatal("send failure revived duplicate or revoked PD")
	}
	transport.failure = nil
	now = c.NextRetryAt()
	if err := c.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if transport.last().Message.Type != 9 {
		t.Fatal("failed Decline lost retry ownership")
	}
}
