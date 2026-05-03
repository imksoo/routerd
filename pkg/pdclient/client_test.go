package pdclient

import (
	"bytes"
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestClientSolicitRequestReply(t *testing.T) {
	now := time.Date(2026, 5, 2, 1, 0, 0, 0, time.UTC)
	transport := &memoryTransport{}
	xids := []uint32{0x010203, 0x010204}
	client, err := New(Config{
		Resource:   "wan-pd",
		Interface:  "wan0",
		ClientDUID: []byte{0, 3, 0, 1, 2, 0, 0, 0, 1, 3},
		IAID:       1,
		Now:        func() time.Time { return now },
		Transaction: func() (uint32, error) {
			xid := xids[0]
			xids = xids[1:]
			return xid, nil
		},
	}, transport)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if client.State != StateSoliciting {
		t.Fatalf("state = %s", client.State)
	}
	if got := transport.last().Message; got.Type != MessageSolicit || got.TransactionID != 0x010203 || got.Prefix.IsValid() {
		t.Fatalf("solicit = %+v", got)
	}

	advertise := mustPayload(t, Message{
		Type:          MessageAdvertise,
		TransactionID: 0x010203,
		ClientDUID:    client.Config.ClientDUID,
		ServerDUID:    []byte{0, 3, 0, 1, 2, 0, 0, 0, 0, 1},
		IAID:          1,
		T1:            7200,
		T2:            12600,
		Prefix:        netip.MustParsePrefix("2001:db8:1200:1240::/60"),
		Preferred:     14400,
		Valid:         14400,
	})
	if err := client.Handle(context.Background(), advertise); err != nil {
		t.Fatalf("handle advertise: %v", err)
	}
	request := transport.last().Message
	if client.State != StateRequesting || request.Type != MessageRequest || request.TransactionID != 0x010204 {
		t.Fatalf("request state/message = %s %+v", client.State, request)
	}
	if !bytes.Equal(request.ServerDUID, []byte{0, 3, 0, 1, 2, 0, 0, 0, 0, 1}) {
		t.Fatalf("request server DUID = %x", request.ServerDUID)
	}

	reply := mustPayload(t, Message{
		Type:          MessageReply,
		TransactionID: 0x010204,
		ClientDUID:    client.Config.ClientDUID,
		ServerDUID:    request.ServerDUID,
		IAID:          1,
		T1:            7200,
		T2:            12600,
		Prefix:        netip.MustParsePrefix("2001:db8:1200:1240::/60"),
		Preferred:     14400,
		Valid:         14400,
	})
	if err := client.Handle(context.Background(), reply); err != nil {
		t.Fatalf("handle reply: %v", err)
	}
	if client.State != StateBound {
		t.Fatalf("state = %s", client.State)
	}
	if client.Lease.Prefix.String() != "2001:db8:1200:1240::/60" || client.Lease.T1 != 2*time.Hour {
		t.Fatalf("lease = %+v", client.Lease)
	}
}

func TestClientRenewRebindExpire(t *testing.T) {
	now := time.Date(2026, 5, 2, 1, 0, 0, 0, time.UTC)
	transport := &memoryTransport{}
	client, err := New(Config{
		Resource:   "wan-pd",
		Interface:  "wan0",
		ClientDUID: []byte{0, 3, 0, 1, 2, 0, 0, 0, 1, 3},
		IAID:       1,
		Now:        func() time.Time { return now },
		Transaction: func() (uint32, error) {
			return 0x020304 + uint32(len(transport.sent)), nil
		},
	}, transport)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.State = StateBound
	client.Lease = Lease{
		Prefix:     netip.MustParsePrefix("2001:db8:1200:1240::/60"),
		ServerDUID: []byte{0, 3, 0, 1, 2, 0, 0, 0, 0, 1},
		IAID:       1,
		T1:         10 * time.Second,
		T2:         20 * time.Second,
		Preferred:  30 * time.Second,
		Valid:      40 * time.Second,
		AcquiredAt: now,
	}

	now = now.Add(10 * time.Second)
	if err := client.Tick(context.Background()); err != nil {
		t.Fatalf("tick renew: %v", err)
	}
	if client.State != StateRenewing || transport.last().Message.Type != MessageRenew {
		t.Fatalf("renew state/message = %s %+v", client.State, transport.last().Message)
	}

	now = client.Lease.AcquiredAt.Add(20 * time.Second)
	if err := client.Tick(context.Background()); err != nil {
		t.Fatalf("tick rebind: %v", err)
	}
	if client.State != StateRebinding || transport.last().Message.Type != MessageRebind {
		t.Fatalf("rebind state/message = %s %+v", client.State, transport.last().Message)
	}

	now = client.Lease.AcquiredAt.Add(40 * time.Second)
	if err := client.Tick(context.Background()); err != nil {
		t.Fatalf("tick expire: %v", err)
	}
	if client.State != StateExpired || client.Lease.Prefix.IsValid() {
		t.Fatalf("expired state/lease = %s %+v", client.State, client.Lease)
	}
}

func TestClientSnapshotIsDBFriendly(t *testing.T) {
	now := time.Date(2026, 5, 2, 1, 0, 0, 0, time.UTC)
	client, err := New(Config{
		Resource:   "wan-pd",
		Interface:  "wan0",
		ClientDUID: []byte{0, 3, 0, 1, 2, 0, 0, 0, 1, 3},
		IAID:       1,
		Now:        func() time.Time { return now },
	}, &memoryTransport{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.State = StateBound
	client.Lease = Lease{
		Prefix:     netip.MustParsePrefix("2001:db8:1200:1240::/60"),
		ServerDUID: []byte{0, 3, 0, 1, 2, 0, 0, 0, 0, 1},
		IAID:       1,
		T1:         10 * time.Second,
		T2:         20 * time.Second,
		Preferred:  30 * time.Second,
		Valid:      40 * time.Second,
		AcquiredAt: now,
	}

	snapshot := client.Snapshot()
	if snapshot.CurrentPrefix != "2001:db8:1200:1240::/60" || snapshot.ServerDUID != "00030001020000000001" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if !snapshot.RenewAt.Equal(now.Add(10*time.Second)) || !snapshot.ExpiresAt.Equal(now.Add(40*time.Second)) {
		t.Fatalf("snapshot times = %+v", snapshot)
	}

	restored, err := New(Config{
		Resource:   "wan-pd",
		Interface:  "wan0",
		ClientDUID: client.Config.ClientDUID,
	}, &memoryTransport{})
	if err != nil {
		t.Fatalf("new restored client: %v", err)
	}
	restored.Restore(snapshot)
	if restored.State != StateBound || restored.Lease.Prefix.String() != "2001:db8:1200:1240::/60" {
		t.Fatalf("restored = %s %+v", restored.State, restored.Lease)
	}
}

func mustPayload(t *testing.T, msg Message) []byte {
	t.Helper()
	payload, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("encode message: %v", err)
	}
	return payload
}

type memoryTransport struct {
	sent []OutboundPacket
}

func (m *memoryTransport) Send(_ context.Context, packet OutboundPacket) error {
	m.sent = append(m.sent, packet)
	return nil
}

func (m *memoryTransport) last() OutboundPacket {
	if len(m.sent) == 0 {
		return OutboundPacket{}
	}
	return m.sent[len(m.sent)-1]
}
