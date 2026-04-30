package dhcp6control

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
)

func TestBuildRequestPacket(t *testing.T) {
	srcMAC := mustMAC(t, "02:00:00:00:01:03")
	dstMAC := mustMAC(t, "02:00:00:00:00:01")
	spec := PacketSpec{
		MessageType:       MessageRequest,
		TransactionID:     0x123456,
		SourceMAC:         srcMAC,
		DestinationMAC:    dstMAC,
		SourceIP:          netip.MustParseAddr("fe80::200:ff:fe00:103"),
		DestinationIP:     netip.MustParseAddr("ff02::1:2"),
		ClientDUID:        DUIDLL(srcMAC),
		ServerDUID:        DUIDLL(dstMAC),
		IAID:              1,
		T1:                7200,
		T2:                12600,
		Prefix:            netip.MustParsePrefix("2001:db8:1200:1240::/60"),
		PreferredLifetime: 14400,
		ValidLifetime:     14400,
		ElapsedTime:       0,
		ORO:               []uint16{23},
		ReconfigureAccept: true,
	}
	frame, err := BuildEthernetIPv6UDP(spec)
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}
	if !bytes.Equal(frame[0:6], dstMAC) || !bytes.Equal(frame[6:12], srcMAC) {
		t.Fatalf("ethernet header mismatch")
	}
	if got := binary.BigEndian.Uint16(frame[12:14]); got != etherTypeIPv6 {
		t.Fatalf("ethertype = %#x", got)
	}
	payload := frame[14+ipv6HeaderLength+udpHeaderLength:]
	wantPrefix := []byte{
		MessageRequest, 0x12, 0x34, 0x56,
		0x00, 0x01, 0x00, 0x0a, 0x00, 0x03, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0x01, 0x03,
		0x00, 0x02, 0x00, 0x0a, 0x00, 0x03, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0x00, 0x01,
	}
	if !bytes.HasPrefix(payload, wantPrefix) {
		t.Fatalf("dhcpv6 payload prefix mismatch:\n got % x\nwant % x", payload[:len(wantPrefix)], wantPrefix)
	}
	if !bytes.Contains(payload, []byte{0x00, byte(optionIAPD), 0x00, 0x29}) {
		t.Fatalf("payload missing IA_PD option: % x", payload)
	}
	if !bytes.Contains(payload, []byte{0x00, byte(optionReconfAccept), 0x00, 0x00}) {
		t.Fatalf("payload missing Reconfigure Accept: % x", payload)
	}
	if checksum := binary.BigEndian.Uint16(frame[14+ipv6HeaderLength+6 : 14+ipv6HeaderLength+8]); checksum == 0 {
		t.Fatalf("UDP checksum was not populated")
	}
}

func TestControllerSendRequestUpdatesLease(t *testing.T) {
	sender := &fakeSender{}
	store := routerstate.NewJSON()
	now := time.Date(2026, 4, 30, 9, 30, 0, 0, time.UTC)
	controller := Controller{
		Sender:        sender,
		Now:           func() time.Time { return now },
		TransactionID: func() (uint32, error) { return 0x010203, nil },
	}
	srcMAC := mustMAC(t, "02:00:00:00:01:03")
	dstMAC := mustMAC(t, "02:00:00:00:00:01")
	in := SendInput{
		Resource: ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation", Name: "wan-pd"},
		Spec:     api.IPv6PrefixDelegationSpec{PriorPrefix: "2001:db8:1200:1240::/60", ServerID: "00030001020000000001", IAID: "1"},
		Lease:    routerstate.PDLease{},
		Identity: Identity{
			InterfaceName:  "ens18",
			SourceMAC:      srcMAC,
			DestinationMAC: dstMAC,
			SourceIP:       netip.MustParseAddr("fe80::200:ff:fe00:103"),
			DestinationIP:  netip.MustParseAddr("ff02::1:2"),
			ClientDUID:     DUIDLL(srcMAC),
			ServerDUID:     DUIDLL(dstMAC),
		},
	}
	if err := controller.SendRequest(context.Background(), store, in); err != nil {
		t.Fatalf("send request: %v", err)
	}
	if sender.ifname != "ens18" || len(sender.frame) == 0 {
		t.Fatalf("sender did not capture frame")
	}
	payload := dhcpPayload(sender.frame)
	if got := payload[0:4]; !bytes.Equal(got, []byte{MessageRequest, 0x01, 0x02, 0x03}) {
		t.Fatalf("unexpected DHCPv6 header: % x", got)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease was not written")
	}
	if lease.PriorPrefix != "2001:db8:1200:1240::/60" || lease.LastRequestAt != now.Format(time.RFC3339) {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestControllerSendRenewUsesFreshXIDServerLifetimesAndReconfigureAccept(t *testing.T) {
	sender := &multiSender{}
	store := routerstate.NewJSON()
	xids := []uint32{0x010203, 0x040506}
	controller := Controller{
		Sender: sender,
		TransactionID: func() (uint32, error) {
			xid := xids[0]
			xids = xids[1:]
			return xid, nil
		},
	}
	srcMAC := mustMAC(t, "02:00:00:00:01:03")
	dstMAC := mustMAC(t, "02:00:00:00:00:01")
	in := SendInput{
		Resource: ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation", Name: "wan-pd"},
		Lease: routerstate.PDLease{
			PriorPrefix: "2001:db8:1200:1240::/60",
			IAID:        "1",
			T1:          "7200",
			T2:          "12600",
			PLTime:      "14400",
			VLTime:      "14400",
		},
		Identity: Identity{
			InterfaceName:  "ens18",
			SourceMAC:      srcMAC,
			DestinationMAC: dstMAC,
			SourceIP:       netip.MustParseAddr("fe80::200:ff:fe00:103"),
			DestinationIP:  netip.MustParseAddr("ff02::1:2"),
			ClientDUID:     DUIDLL(srcMAC),
			ServerDUID:     DUIDLL(dstMAC),
		},
	}
	if err := controller.SendRenew(context.Background(), store, in); err != nil {
		t.Fatalf("send first renew: %v", err)
	}
	if err := controller.SendRenew(context.Background(), store, in); err != nil {
		t.Fatalf("send second renew: %v", err)
	}
	if len(sender.frames) != 2 {
		t.Fatalf("captured %d frames, want 2", len(sender.frames))
	}
	first := dhcpPayload(sender.frames[0])
	second := dhcpPayload(sender.frames[1])
	if bytes.Equal(first[1:4], second[1:4]) {
		t.Fatalf("transaction ID was reused: % x", first[1:4])
	}
	assertIAPD(t, first, 1, 7200, 12600, 14400, 14400)
	if !hasOption(first, optionReconfAccept) {
		t.Fatalf("renew payload missing Reconfigure Accept: % x", first)
	}
}

func TestControllerSendReleaseUsesZeroLifetimesWithoutReconfigureAccept(t *testing.T) {
	sender := &fakeSender{}
	controller := Controller{
		Sender:        sender,
		TransactionID: func() (uint32, error) { return 0x010203, nil },
	}
	srcMAC := mustMAC(t, "02:00:00:00:01:03")
	dstMAC := mustMAC(t, "02:00:00:00:00:01")
	in := SendInput{
		Resource: ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation", Name: "wan-pd"},
		Lease: routerstate.PDLease{
			PriorPrefix: "2001:db8:1200:1240::/60",
			IAID:        "1",
			T1:          "7200",
			T2:          "12600",
			PLTime:      "14400",
			VLTime:      "14400",
		},
		Identity: Identity{
			InterfaceName:  "ens18",
			SourceMAC:      srcMAC,
			DestinationMAC: dstMAC,
			SourceIP:       netip.MustParseAddr("fe80::200:ff:fe00:103"),
			DestinationIP:  netip.MustParseAddr("ff02::1:2"),
			ClientDUID:     DUIDLL(srcMAC),
			ServerDUID:     DUIDLL(dstMAC),
		},
	}
	if err := controller.SendRelease(context.Background(), nil, in); err != nil {
		t.Fatalf("send release: %v", err)
	}
	payload := dhcpPayload(sender.frame)
	assertIAPD(t, payload, 1, 0, 0, 0, 0)
	if hasOption(payload, optionReconfAccept) {
		t.Fatalf("release payload unexpectedly included Reconfigure Accept: % x", payload)
	}
}

type fakeSender struct {
	ifname string
	frame  []byte
}

func (f *fakeSender) SendFrame(_ context.Context, ifname string, frame []byte) error {
	f.ifname = ifname
	f.frame = append([]byte(nil), frame...)
	return nil
}

type multiSender struct {
	frames [][]byte
}

func (m *multiSender) SendFrame(_ context.Context, _ string, frame []byte) error {
	m.frames = append(m.frames, append([]byte(nil), frame...))
	return nil
}

func dhcpPayload(frame []byte) []byte {
	return frame[14+ipv6HeaderLength+udpHeaderLength:]
}

func hasOption(payload []byte, code uint16) bool {
	return optionData(payload, code) != nil
}

func optionData(payload []byte, code uint16) []byte {
	for i := 4; i+4 <= len(payload); {
		gotCode := binary.BigEndian.Uint16(payload[i : i+2])
		length := int(binary.BigEndian.Uint16(payload[i+2 : i+4]))
		i += 4
		if i+length > len(payload) {
			return nil
		}
		if gotCode == code {
			return payload[i : i+length]
		}
		i += length
	}
	return nil
}

func assertIAPD(t *testing.T, payload []byte, iaid, t1, t2, preferredLifetime, validLifetime uint32) {
	t.Helper()
	data := optionData(payload, optionIAPD)
	if len(data) < 12 {
		t.Fatalf("missing IA_PD option: % x", payload)
	}
	if got := binary.BigEndian.Uint32(data[0:4]); got != iaid {
		t.Fatalf("IAID = %d, want %d", got, iaid)
	}
	if got := binary.BigEndian.Uint32(data[4:8]); got != t1 {
		t.Fatalf("T1 = %d, want %d", got, t1)
	}
	if got := binary.BigEndian.Uint32(data[8:12]); got != t2 {
		t.Fatalf("T2 = %d, want %d", got, t2)
	}
	for i := 12; i+4 <= len(data); {
		code := binary.BigEndian.Uint16(data[i : i+2])
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		i += 4
		if i+length > len(data) {
			t.Fatalf("truncated IA_PD suboption: % x", data)
		}
		if code == optionIAPrefix {
			if length < 25 {
				t.Fatalf("IA Prefix suboption too short: % x", data[i:i+length])
			}
			if got := binary.BigEndian.Uint32(data[i : i+4]); got != preferredLifetime {
				t.Fatalf("preferred lifetime = %d, want %d", got, preferredLifetime)
			}
			if got := binary.BigEndian.Uint32(data[i+4 : i+8]); got != validLifetime {
				t.Fatalf("valid lifetime = %d, want %d", got, validLifetime)
			}
			return
		}
		i += length
	}
	t.Fatalf("missing IA Prefix suboption: % x", data)
}

func mustMAC(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatalf("parse MAC %q: %v", value, err)
	}
	return mac
}
