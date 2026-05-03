package dhcpv4client

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestEncodeDecodeDiscover(t *testing.T) {
	hw, _ := net.ParseMAC("02:00:00:00:00:01")
	packet := EncodeRequest(MessageDiscover, 0x12345678, hw, map[byte][]byte{OptionHostname: []byte("routerd")})
	msg, err := Decode(packet)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.XID != 0x12345678 || msg.MessageType() != MessageDiscover || msg.CHAddr.String() != hw.String() {
		t.Fatalf("message = %+v", msg)
	}
	if string(msg.Options[OptionHostname]) != "routerd" {
		t.Fatalf("hostname option = %q", msg.Options[OptionHostname])
	}
}

func TestLeaseFromACK(t *testing.T) {
	packet := make([]byte, 240)
	packet[0], packet[1], packet[2] = 2, 1, 6
	copy(packet[16:20], []byte{192, 0, 2, 10})
	copy(packet[236:240], []byte{99, 130, 83, 99})
	opts := []byte{OptionMessageType, 1, MessageACK, OptionRouter, 4, 192, 0, 2, 1, OptionDNSServer, 8, 192, 0, 2, 53, 192, 0, 2, 54, OptionLeaseTime, 4, 0, 0, 14, 16, OptionEnd}
	packet = append(packet, opts...)
	msg, err := Decode(packet)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	lease := LeaseFromACK(msg, now)
	if lease.Address.String() != "192.0.2.10" || lease.DefaultGateway.String() != "192.0.2.1" {
		t.Fatalf("lease = %+v", lease)
	}
	if got := binary.BigEndian.Uint32(msg.Options[OptionLeaseTime]); got != 3600 {
		t.Fatalf("lease option = %d", got)
	}
	if lease.RenewAt() != now.Add(1800*time.Second) {
		t.Fatalf("renewAt = %s", lease.RenewAt())
	}
}
