package dhcp6recorder

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"

	"routerd/pkg/dhcp6control"
	routerstate "routerd/pkg/state"
)

func TestParseFrameAndApplyReply(t *testing.T) {
	srcMAC := mustMAC(t, "02:00:00:00:00:01")
	dstMAC := mustMAC(t, "02:00:00:00:01:03")
	frame, err := dhcp6control.BuildEthernetIPv6UDP(dhcp6control.PacketSpec{
		MessageType:       dhcp6control.MessageReply,
		TransactionID:     0x010203,
		SourceMAC:         srcMAC,
		DestinationMAC:    dstMAC,
		SourceIP:          netip.MustParseAddr("fe80::1"),
		DestinationIP:     netip.MustParseAddr("fe80::200:ff:fe00:103"),
		ClientDUID:        dhcp6control.DUIDLL(dstMAC),
		ServerDUID:        dhcp6control.DUIDLL(srcMAC),
		IAID:              1,
		T1:                7200,
		T2:                12600,
		Prefix:            netip.MustParsePrefix("2001:db8:1200:1240::/60"),
		PreferredLifetime: 14400,
		ValidLifetime:     14400,
		ReconfigureAccept: true,
	})
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}
	binary.BigEndian.PutUint16(frame[14+40:14+42], dhcp6ServerPort)
	binary.BigEndian.PutUint16(frame[14+42:14+44], dhcp6ClientPort)
	obs, ok, err := ParseFrame(frame)
	if err != nil || !ok {
		t.Fatalf("parse frame ok=%v err=%v", ok, err)
	}
	if obs.Direction != "received" || obs.SourceMAC != "02:00:00:00:00:01" || obs.DestinationPort != 546 {
		t.Fatalf("observation = %+v", obs)
	}
	obs.ObservedAt = time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	obs.Interface = "ens18"
	store := routerstate.NewJSON()
	if err := ApplyObservation(store, "wan-pd", obs); err != nil {
		t.Fatalf("apply observation: %v", err)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.CurrentPrefix != "2001:db8:1200:1240::/60" || lease.LastReplyAt == "" {
		t.Fatalf("lease = %+v", lease)
	}
	if len(lease.Transactions) != 1 {
		t.Fatalf("transactions = %+v", lease.Transactions)
	}
	tx := lease.Transactions[0]
	if tx.MessageType != "Reply" || tx.TransactionID != "010203" || tx.ValidLifetime != "14400" {
		t.Fatalf("transaction = %+v", tx)
	}
}

func TestRunSkipsNonDHCPv6Frames(t *testing.T) {
	source := &fakeSource{frames: [][]byte{{0, 1, 2}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seen int
	err := Run(ctx, source, func(Observation) { seen++ })
	if err == nil {
		t.Fatal("run should return source exhaustion error")
	}
	if seen != 0 {
		t.Fatalf("seen = %d", seen)
	}
}

type fakeSource struct {
	frames [][]byte
}

func (f *fakeSource) ReadFrame(context.Context) ([]byte, error) {
	if len(f.frames) == 0 {
		return nil, errDone{}
	}
	frame := f.frames[0]
	f.frames = f.frames[1:]
	return frame, nil
}

func (f *fakeSource) Close() error { return nil }

type errDone struct{}

func (errDone) Error() string { return "done" }

func mustMAC(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatalf("parse MAC %s: %v", value, err)
	}
	return mac
}
