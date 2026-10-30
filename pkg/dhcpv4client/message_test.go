// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestEncodeDecodeDiscover(t *testing.T) {
	hw, _ := net.ParseMAC("02:00:00:00:00:01")
	packet, err := Encode(Message{Op: 1, HType: 1, XID: 0x12345678, CHAddr: hw, Options: map[byte][]byte{OptionMessageType: {MessageDiscover}, OptionHostname: []byte("routerd")}})
	if err != nil {
		t.Fatal(err)
	}
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
	opts := []byte{OptionMessageType, 1, MessageACK, OptionSubnetMask, 4, 255, 255, 255, 0, OptionRouter, 4, 192, 0, 2, 1, OptionDNSServer, 8, 192, 0, 2, 53, 192, 0, 2, 54, OptionLeaseTime, 4, 0, 0, 14, 16, OptionServerID, 4, 192, 0, 2, 1, OptionEnd}
	packet = append(packet, opts...)
	msg, err := Decode(packet)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	lease, err := LeaseFromACK(msg, Lease{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Address.String() != "192.0.2.10" || lease.DefaultGateway.String() != "192.0.2.1" {
		t.Fatalf("lease = %+v", lease)
	}
	if lease.PrefixLength != 24 {
		t.Fatalf("prefix length = %d", lease.PrefixLength)
	}
	if got := binary.BigEndian.Uint32(msg.Options[OptionLeaseTime]); got != 3600 {
		t.Fatalf("lease option = %d", got)
	}
	if lease.RenewAt() != now.Add(1800*time.Second) {
		t.Fatalf("renewAt = %s", lease.RenewAt())
	}
}

func TestDecodeRejectsHardwareAddressBeyondCHAddr(t *testing.T) {
	for _, capacity := range []int{240, 1500} {
		packet := make([]byte, 240, capacity)
		packet[2] = 255
		copy(packet[236:], []byte{99, 130, 83, 99})
		if _, err := Decode(packet); err == nil {
			t.Fatalf("accepted oversized hardware address with capacity %d", capacity)
		}
	}
}

func TestDecodeConcatenatesOverloadedOptionsAndRejectsTruncation(t *testing.T) {
	packet, err := Encode(Message{Op: 2, HType: 1, CHAddr: net.HardwareAddr{2, 0, 0, 0, 0, 1}, Options: map[byte][]byte{OptionMessageType: {MessageACK}, OptionOverload: {3}, OptionDomainName: []byte("main")}})
	if err != nil {
		t.Fatal(err)
	}
	copy(packet[108:236], []byte{OptionDomainName, 4, 'f', 'i', 'l', 'e', OptionEnd})
	copy(packet[44:108], []byte{OptionDomainName, 5, 's', 'n', 'a', 'm', 'e', OptionEnd})
	msg, err := Decode(packet)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg.Options[OptionDomainName]) != "mainfilesname" {
		t.Fatalf("wrong overload concatenation: %q", msg.Options[OptionDomainName])
	}
	truncated := append(packet[:240:240], OptionMessageType, 1, MessageACK, OptionDNSServer, 8, 192, 0)
	if _, err := Decode(truncated); err == nil {
		t.Fatal("accepted truncated option as a complete ACK")
	}
}
