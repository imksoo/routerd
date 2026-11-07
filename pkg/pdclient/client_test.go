// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"
	"time"
)

var testServer = []byte{0, 3, 0, 1, 2, 0, 0, 0, 0, 2}

func testClient(t *testing.T, now *time.Time) (*Client, *memoryTransport) {
	t.Helper()
	xid := uint32(100)
	transport := &memoryTransport{}
	c, err := New(Config{Resource: "wan-pd", Interface: "wan0", ClientDUID: []byte{0, 3, 0, 1, 2, 0, 0, 0, 0, 1}, IAID: 7, Now: func() time.Time { return *now }, Random: func() float64 { return .5 }, Transaction: func() (uint32, error) { xid++; return xid, nil }}, transport)
	if err != nil {
		t.Fatal(err)
	}
	return c, transport
}

func prefix(text string, preferred, valid uint32) IAPrefix {
	return IAPrefix{Prefix: netip.MustParsePrefix(text), Preferred: preferred, Valid: valid}
}

func response(c *Client, kind uint8, entries ...IAPrefix) Message {
	return Message{Type: kind, TransactionID: c.leaseExchange.message.TransactionID, ClientDUID: c.Config.ClientDUID, ServerDUID: testServer, IAPDs: []IAPD{{IAID: c.Config.IAID, T1: 30, T2: 60, Prefixes: entries}}}
}

func mustPayload(t *testing.T, msg Message) []byte {
	t.Helper()
	payload, err := EncodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func receive(t *testing.T, c *Client, msg Message) {
	t.Helper()
	if err := c.Handle(context.Background(), mustPayload(t, msg)); err != nil {
		t.Fatal(err)
	}
}

func acquire(t *testing.T, c *Client, entries ...IAPrefix) {
	t.Helper()
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	receive(t, c, response(c, MessageAdvertise, entries...))
	if c.State != StateRequesting {
		t.Fatalf("Advertise not selected: %s", c.State)
	}
	receive(t, c, response(c, MessageReply, entries...))
	if c.State != StateBound {
		t.Fatalf("Reply not bound: %s", c.State)
	}
}

func TestAcquiresAllPrefixesAndKeepsExchangeIdentityAfterRequestLoss(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	solicit := transport.last().Message
	if solicit.IAPDs[0].Prefixes[0].Prefix.String() != "::/60" {
		t.Fatalf("Solicit hint: %+v", solicit)
	}
	if !c.NextRetryAt().After(now.Add(time.Second)) {
		t.Fatal("first Solicit retry must exceed one second")
	}
	entries := []IAPrefix{prefix("2001:db8:1::/56", 90, 120), prefix("2001:db8:2::/56", 100, 180)}
	receive(t, c, response(c, MessageAdvertise, entries...))
	request := transport.last().Message
	if request.Type != MessageRequest || len(request.IAPDs) != 1 || len(request.IAPDs[0].Prefixes) != 2 || !bytes.Equal(request.ServerDUID, testServer) {
		t.Fatalf("Request lost association content: %+v", request)
	}
	now = c.NextRetryAt()
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	retry, err := DecodeMessage(transport.last().Payload)
	if err != nil {
		t.Fatal(err)
	}
	if retry.TransactionID != request.TransactionID || retry.ElapsedTime == 0 {
		t.Fatalf("retry identity/elapsed: %+v", retry)
	}
	reply := response(c, MessageReply, entries...)
	wrongServer := reply
	wrongServer.ServerDUID = []byte{9}
	receive(t, c, wrongServer)
	wrongClient := reply
	wrongClient.ClientDUID = []byte{9}
	receive(t, c, wrongClient)
	stale := reply
	stale.TransactionID = solicit.TransactionID
	receive(t, c, stale)
	if c.State != StateRequesting {
		t.Fatal("foreign/stale Reply changed state")
	}
	other := IAPD{IAID: 8, Prefixes: []IAPrefix{prefix("2001:db8:8::/56", 90, 120)}}
	reply.IAPDs = append([]IAPD{other}, reply.IAPDs...)
	receive(t, c, reply)
	if c.State != StateBound || len(c.Lease.Prefixes) != 2 || c.Lease.T1 != 30*time.Second {
		t.Fatalf("association: %+v", c.Lease)
	}
	if !c.leaseExchange.retryAt.IsZero() {
		t.Fatal("completed Request retained retry ownership")
	}
}

func TestPrefixUpdatesRevocationsOmissionsRecoveryAndRenewPayload(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	first := prefix("2001:db8:1::/56", 90, 120)
	second := prefix("2001:db8:2::/56", 100, 180)
	acquire(t, c, first, second)
	original := c.Lease.Prefixes[1]
	now = now.Add(10 * time.Second)
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	third := prefix("2001:db8:3::/56", 200, 240)
	receive(t, c, response(c, MessageReply, prefix("2001:db8:1::/56", 0, 0), third))
	if len(c.Lease.Prefixes) != 2 || c.Lease.Prefixes[0] != original {
		t.Fatalf("omitted lease changed or explicit revocation retained: %+v", c.Lease.Prefixes)
	}
	snapshot := c.Snapshot()
	if snapshot.CurrentPrefix != third.Prefix.String() || len(snapshot.PreviousPrefixes) != 1 || !snapshot.PreviousPrefixes[0].ExpiresAt.Equal(original.ExpiresAt()) {
		t.Fatalf("LAN projection: %+v", snapshot)
	}
	restored, _ := testClient(t, &now)
	restored.Restore(snapshot)
	if len(restored.Lease.Prefixes) != 2 || restored.Lease.Prefixes[0] != original {
		t.Fatalf("recovery lost prefix ownership/lifetime: %+v", restored.Lease)
	}
	now = now.Add(10 * time.Second)
	if err := restored.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	ia := restored.leaseExchange.message.IAPDs[0]
	if len(ia.Prefixes) != 2 || ia.Prefixes[0].Valid != 160 || ia.Prefixes[1].Valid != 230 || ia.Prefixes[0].Preferred != 80 {
		t.Fatalf("Renew must include all remaining lifetimes: %+v", ia)
	}
	// Revoke the selected prefix while the omitted older prefix remains usable.
	receive(t, restored, response(restored, MessageReply, prefix("2001:db8:3::/56", 0, 0)))
	if restored.State != StateBound || restored.Snapshot().CurrentPrefix != second.Prefix.String() {
		t.Fatalf("one revocation discarded whole IA: %+v", restored.Snapshot())
	}
	now = original.ExpiresAt()
	if err := restored.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restored.State != StateExpired || len(restored.Lease.Prefixes) != 0 {
		t.Fatal("final expired prefix remained usable")
	}
	if len(transport.sent) < 3 {
		t.Fatal("real transport boundary was not exercised")
	}
}

func TestRenewRebindRetransmitOnlyUnexpiredPrefixes(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 15, 20), prefix("2001:db8:2::/56", 120, 180))
	now = now.Add(10 * time.Second)
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	renewXID := transport.last().Message.TransactionID
	now = c.NextRetryAt()
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := transport.last().Message
	if got.Type != MessageRenew || got.TransactionID != renewXID || len(got.IAPDs[0].Prefixes) != 1 || got.IAPDs[0].Prefixes[0].Prefix.String() != "2001:db8:2::/56" {
		t.Fatalf("expired prefix retransmitted: %+v", got)
	}
	now = c.Lease.RebindAt()
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got = transport.last().Message
	if got.Type != MessageRebind || len(got.ServerDUID) != 0 || got.TransactionID == renewXID {
		t.Fatalf("Rebind identity: %+v", got)
	}
	if c.Snapshot().CurrentPrefix != "2001:db8:2::/56" {
		t.Fatal("surviving prefix not selected")
	}
}

func TestNoBindingRequestsAgainWithoutDroppingValidLeases(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120), prefix("2001:db8:2::/56", 120, 180))
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := response(c, MessageReply)
	msg.IAPDs[0].StatusCode = StatusNoBinding
	receive(t, c, msg)
	request := transport.last().Message
	if c.State != StateRequesting || request.Type != MessageRequest || len(request.IAPDs[0].Prefixes) != 2 || len(c.Lease.Prefixes) != 2 {
		t.Fatalf("NoBinding recovery: %s %+v", c.State, request)
	}
	receive(t, c, response(c, MessageReply, prefix("2001:db8:1::/56", 90, 120)))
	if c.State != StateBound || len(c.Lease.Prefixes) != 2 {
		t.Fatal("Request recovery lost omitted lease")
	}
}

func TestMissingAndFailedAssociationDoNotRevokeLeasesOrRestartRetry(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, _ := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120))
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	retry := c.NextRetryAt()
	msg := response(c, MessageReply)
	msg.IAPDs = nil
	msg.DNSServers = []netip.Addr{netip.MustParseAddr("2001:db8::53")}
	receive(t, c, msg)
	if c.State != StateRenewing || len(c.Lease.Prefixes) != 1 || !retry.Equal(c.NextRetryAt()) || len(c.Info.DNSServers) != 1 {
		t.Fatal("absent IA discarded usable state or independent information")
	}
	msg.IAPDs = []IAPD{{IAID: 7, StatusCode: StatusNoPrefixAvail}}
	receive(t, c, msg)
	if c.State != StateRenewing || len(c.Lease.Prefixes) != 1 || !retry.Equal(c.NextRetryAt()) {
		t.Fatal("NoPrefixAvail revoked an omitted lease or restarted retry")
	}
}

func TestZeroTimersAndInfinityDoNotCreateImmediateRetryLoop(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 0xffffffff, 0xffffffff))
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := response(c, MessageReply, prefix("2001:db8:1::/56", 0xffffffff, 0xffffffff))
	msg.IAPDs[0].T1 = 0
	msg.IAPDs[0].T2 = 0
	receive(t, c, msg)
	count := len(transport.sent)
	if err := c.TickWithMargin(context.Background(), time.Hour, time.Hour); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != count || c.Lease.T1 <= 0 || c.Lease.T2 < c.Lease.T1 {
		t.Fatal("zero timers caused immediate transmit")
	}
	now = now.AddDate(200, 0, 0)
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transport.last().Message.IAPDs[0].Prefixes[0].Valid != 0xffffffff {
		t.Fatal("infinite lease aged as finite")
	}
}

func TestInformationRequestLossRetryStatusAndRefresh(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120))
	if err := c.InfoRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	infoXID := c.infoExchange.message.TransactionID
	if infoXID == c.leaseExchange.message.TransactionID {
		t.Fatal("exchanges share transaction")
	}
	count := len(transport.sent)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != count {
		t.Fatal("Information-request ignored initial random delay")
	}
	now = c.infoExchange.retryAt
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := transport.last().Message
	if first.Type != MessageInfoRequest || len(first.IAPDs) != 0 || !slicesContain(first.ORO, optionInfoRefreshTime) || !slicesContain(first.ORO, optionINFMaxRT) {
		t.Fatalf("Information-request: %+v", first)
	}
	deadline := c.infoExchange.retryAt
	if err := c.InfoRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !deadline.Equal(c.infoExchange.retryAt) {
		t.Fatal("duplicate command restarted pending exchange")
	}
	now = deadline
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	retry := transport.last().Message
	if retry.TransactionID != infoXID || retry.ElapsedTime == 0 {
		t.Fatalf("information retry identity: %+v", retry)
	}
	refresh := uint32(2)
	reply := Message{Type: MessageReply, TransactionID: infoXID, ClientDUID: c.Config.ClientDUID, ServerDUID: testServer, DNSServers: []netip.Addr{netip.MustParseAddr("2001:db8::53")}, AFTRName: "aftr.example", InfoRefreshTime: &refresh}
	receive(t, c, reply)
	if c.infoExchange.active() || c.Info.AFTRName != "aftr.example" || !c.Info.RefreshAt.Equal(now.Add(600*time.Second)) || c.State != StateBound {
		t.Fatalf("information success: %+v", c.Info)
	}
	if err := c.InfoRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = c.infoExchange.retryAt
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reply.TransactionID = c.infoExchange.message.TransactionID
	reply.AFTRName = ""
	reply.DNSServers = nil
	reply.InfoRefreshTime = nil
	receive(t, c, reply)
	if c.Info.AFTRName != "" || len(c.Info.DNSServers) != 0 || !c.Info.RefreshAt.Equal(now.Add(24*time.Hour)) {
		t.Fatal("omitted requested options were not withdrawn or default refresh missing")
	}
	now = c.Info.RefreshAt
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !c.infoExchange.active() || c.infoExchange.attempts != 0 || c.infoExchange.retryAt.Before(now) || c.infoExchange.retryAt.After(now.Add(time.Second)) {
		t.Fatal("refresh did not schedule a delayed fresh exchange")
	}
	now = c.infoExchange.retryAt
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.infoExchange.attempts != 1 || !c.Info.LastSentAt.Equal(now) {
		t.Fatal("refresh was not sent by the existing tick owner")
	}
}

func TestInformationUnspecFailRetriesAreBoundedAndPreservePriorData(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 900, 1200))
	c.Info.AFTRName = "old.example"
	if err := c.InfoRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = c.infoExchange.retryAt
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := Message{Type: MessageReply, TransactionID: c.infoExchange.message.TransactionID, ClientDUID: c.Config.ClientDUID, ServerDUID: testServer, StatusCode: StatusUnspecFail, INFMaxRT: 60}
	receive(t, c, msg)
	until := c.infoExchange.failureUntil
	count := len(transport.sent)
	receive(t, c, msg)
	if !until.Equal(c.infoExchange.failureUntil) || len(transport.sent) != count || c.Info.AFTRName != "old.example" {
		t.Fatal("failure reply restarted or immediately retried exchange")
	}
	// Drive only the owned information exchange; prefix refresh remains independent.
	for c.infoExchange.active() {
		now = c.infoExchange.retryAt
		if err := c.tickInformation(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		if now.After(until) {
			t.Fatal("UnspecFail retry duration exceeded bound")
		}
	}
	if c.Info.LastError == "" || c.Info.StatusCode != StatusUnspecFail || c.Info.AFTRName != "old.example" || c.State != StateBound {
		t.Fatalf("failure did not preserve evidence/state: %+v", c.Info)
	}
}

func TestSnapshotRecoveryUsesCompletePrefixRecordsAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, _ := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120), prefix("2001:db8:2::/56", 120, 180))
	snapshot := c.Snapshot()
	for _, state := range []State{StateBound, StateRenewing, StateRebinding} {
		restored, _ := testClient(t, &now)
		snapshot.State = state
		restored.Restore(snapshot)
		if restored.State != state || len(restored.Lease.Prefixes) != 2 {
			t.Fatalf("failed recovery of %s: %+v", state, restored.Lease)
		}
	}
	snapshot.State = StateBound
	for _, mutate := range []func(*Snapshot){func(s *Snapshot) { s.ClientDUID = "99" }, func(s *Snapshot) { s.IAID++ }, func(s *Snapshot) { s.Interface = "other" }, func(s *Snapshot) { s.Resource = "other" }, func(s *Snapshot) { s.Prefixes = nil }} {
		changed := snapshot
		mutate(&changed)
		restored, _ := testClient(t, &now)
		restored.Restore(changed)
		if restored.State != StateIdle || len(restored.Lease.Prefixes) != 0 {
			t.Fatal("foreign or obsolete snapshot restored")
		}
	}
	now = now.Add(3 * time.Minute)
	restored, _ := testClient(t, &now)
	restored.Restore(snapshot)
	if restored.State != StateIdle {
		t.Fatal("expired association restored")
	}
}

func TestSendFailureRetainsRetryDeadlineAndRequestRetriesAreFinite(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	transport.failure = errors.New("link unavailable")
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("send failure hidden")
	}
	deadline := c.NextRetryAt()
	if !deadline.After(now) {
		t.Fatal("send failure creates busy loop")
	}
	transport.failure = nil
	receive(t, c, response(c, MessageAdvertise, prefix("2001:db8:1::/56", 90, 120)))
	xid := c.leaseExchange.message.TransactionID
	for i := 0; i < 10; i++ {
		now = c.NextRetryAt()
		if err := c.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if c.State != StateSoliciting || c.leaseExchange.message.TransactionID == xid {
		t.Fatal("exhausted Request retained old exchange")
	}
}

func TestDecodePreservesIndependentPrefixesStatusAndConfiguration(t *testing.T) {
	msg := Message{Type: MessageReply, TransactionID: 1, ClientDUID: []byte{1}, ServerDUID: []byte{2}, IAPDs: []IAPD{{IAID: 7, T1: 10, T2: 20, Prefixes: []IAPrefix{prefix("2001:db8:1::/56", 100, 10), prefix("2001:db8:2::/56", 30, 60), prefix("2001:db8:3::/56", 0, 0)}}}, DNSServers: []netip.Addr{netip.MustParseAddr("2001:db8::53")}}
	payload := mustPayload(t, msg)
	got, err := DecodeMessageForIAID(payload, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IAPDs) != 1 || len(got.IAPDs[0].Prefixes) != 2 || got.IAPDs[0].Prefixes[1].Valid != 0 || len(got.DNSServers) != 1 {
		t.Fatalf("independent prefix rejected: %+v", got)
	}
	// Corrupt only the IA timers; independent DNS still survives.
	msg.IAPDs[0].T1 = 30
	got, err = DecodeMessage(mustPayload(t, msg))
	if err != nil || len(got.IAPDs) != 0 || len(got.DNSServers) != 1 {
		t.Fatalf("invalid IA escaped its scope: %+v %v", got, err)
	}
	msg.IAPDs = []IAPD{{IAID: 7, StatusCode: StatusNoBinding}}
	got, err = DecodeMessageForIAID(mustPayload(t, msg), 7)
	if err != nil || len(got.IAPDs) != 1 || got.IAPDs[0].StatusCode != StatusNoBinding {
		t.Fatalf("zero-prefix status lost: %+v %v", got, err)
	}
	// Nested framing damage discards only its independent IA.
	malformed := make([]byte, 13)
	binary.BigEndian.PutUint32(malformed, 8)
	payload = appendOption(mustPayload(t, msg), optionIAPD, malformed)
	got, err = DecodeMessage(payload)
	if err != nil || len(got.IAPDs) != 1 || len(got.DNSServers) != 1 {
		t.Fatalf("malformed unrelated IA poisoned payload: %+v %v", got, err)
	}
	if _, err := DecodeMessage(payload[:len(payload)-1]); err == nil {
		t.Fatal("truncated outer option accepted")
	}
}

func slicesContain(values []uint16, want uint16) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type memoryTransport struct {
	sent    []OutboundPacket
	failure error
}

func (m *memoryTransport) Send(_ context.Context, packet OutboundPacket) error {
	m.sent = append(m.sent, packet)
	return m.failure
}
func (m *memoryTransport) last() OutboundPacket {
	if len(m.sent) == 0 {
		return OutboundPacket{}
	}
	return m.sent[len(m.sent)-1]
}

func TestReleaseWithdrawsEveryPrefixAndStopsAfterFiniteRetries(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120), prefix("2001:db8:2::/56", 120, 180))
	transport.failure = errors.New("network unavailable")
	if err := c.Release(context.Background()); !errors.Is(err, ErrSend) {
		t.Fatalf("send failure hidden: %v", err)
	}
	release := transport.last().Message
	if c.State != StateReleased || len(c.Lease.Prefixes) != 0 || release.Type != MessageRelease || len(release.IAPDs[0].Prefixes) != 2 {
		t.Fatal("Release did not withdraw complete local association")
	}
	transport.failure = nil
	for i := 0; i < 3; i++ {
		now = c.NextRetryAt()
		if err := c.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		packet := transport.last().Message
		if packet.Type != MessageRelease || packet.TransactionID != release.TransactionID {
			t.Fatalf("Release retry changed identity or reacquired: %+v", packet)
		}
	}
	now = c.NextRetryAt()
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.leaseExchange.active() {
		t.Fatal("Release exceeded finite retry count")
	}
	count := len(transport.sent)
	now = now.Add(time.Hour)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != count || c.State != StateReleased {
		t.Fatal("released association automatically reacquired")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.State != StateSoliciting {
		t.Fatal("explicit Start did not reacquire")
	}
}

func TestReleaseReplyCompletesRegardlessOfStatusAndChecksSelectedServer(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, _ := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120))
	if err := c.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := response(c, MessageReply)
	msg.IAPDs = nil
	msg.StatusCode = StatusUnspecFail
	msg.ServerDUID = []byte{9}
	receive(t, c, msg)
	if !c.leaseExchange.active() {
		t.Fatal("unselected server completed Release")
	}
	msg.ServerDUID = testServer
	receive(t, c, msg)
	if c.leaseExchange.active() || c.State != StateReleased {
		t.Fatal("valid Release Reply failed to complete")
	}
}

func TestLateRenewReplyCannotReviveExpiredAssociationButInformationStillCompletes(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, _ := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120))
	if err := c.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	late := response(c, MessageReply, prefix("2001:db8:1::/56", 90, 120))
	if err := c.InfoRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = c.infoExchange.retryAt
	if err := c.tickInformation(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	infoXID := c.infoExchange.message.TransactionID
	now = now.Add(121 * time.Second)
	receive(t, c, late)
	if c.State != StateExpired || c.leaseExchange.active() || len(c.Lease.Prefixes) != 0 {
		t.Fatal("late matching Renew Reply revived expired authority")
	}
	receive(t, c, Message{Type: MessageReply, TransactionID: infoXID, ClientDUID: c.Config.ClientDUID, ServerDUID: testServer, AFTRName: "aftr.example"})
	if c.Info.AFTRName != "aftr.example" || c.infoExchange.active() || c.State != StateExpired {
		t.Fatal("independent information exchange was lost with expired prefix")
	}
}

func TestSnapshotDerivesLiveAuthorityWithoutMutatingTheOwner(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, _ := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 10, 20), prefix("2001:db8:2::/56", 120, 180))
	oldCurrent := c.Lease.CurrentPrefix
	now = now.Add(21 * time.Second)
	s := c.Snapshot()
	if s.State != StateBound || s.CurrentPrefix != "2001:db8:2::/56" || len(s.Prefixes) != 1 || len(s.PreviousPrefixes) != 0 {
		t.Fatalf("snapshot exposed expired primary: %+v", s)
	}
	if c.Lease.CurrentPrefix != oldCurrent || len(c.Lease.Prefixes) != 2 {
		t.Fatal("read-only snapshot mutated protocol owner")
	}
	now = now.Add(160 * time.Second)
	s = c.Snapshot()
	if s.State != StateExpired || s.CurrentPrefix != "" || len(s.Prefixes) != 0 {
		t.Fatalf("snapshot claimed expired lease ready: %+v", s)
	}
}

func TestInformationSendFailureDoesNotDelayPrefixExpiry(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c, transport := testClient(t, &now)
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120))
	if err := c.InfoRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	transport.failure = errors.New("network unreachable")
	now = now.Add(121 * time.Second)
	if err := c.Tick(context.Background()); !errors.Is(err, ErrSend) {
		t.Fatalf("information failure evidence missing: %v", err)
	}
	if c.State != StateExpired || len(c.Lease.Prefixes) != 0 || c.leaseExchange.active() {
		t.Fatal("independent information send blocked prefix expiry")
	}
}
