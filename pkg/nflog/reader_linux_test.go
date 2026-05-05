package nflog

import (
	"encoding/binary"
	"testing"
)

func TestParseMessagesIPv4TCPWithPrefix(t *testing.T) {
	payload := []byte{
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 64, 6, 0x00, 0x00,
		192, 168, 1, 33, 192, 168, 1, 249,
		0xe1, 0x4e, 0x1f, 0x90, 0, 0, 0, 0, 0, 0, 0, 0, 0x50, 0x02, 0, 0, 0, 0, 0, 0,
	}
	msg := testNetlinkPacketMessage(
		attr(nfulaPrefix, append([]byte("routerd firewall wan-to-self deny "), 0)),
		attr(nfulaPayload, payload),
	)
	packets, err := ParseMessages(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 {
		t.Fatalf("packets = %d", len(packets))
	}
	p := packets[0]
	if p.Prefix != "routerd firewall wan-to-self deny " || p.Protocol != "tcp" || p.L3Proto != "ipv4" {
		t.Fatalf("packet = %+v", p)
	}
	if p.SrcAddress != "192.168.1.33" || p.SrcPort != 57678 || p.DstAddress != "192.168.1.249" || p.DstPort != 8080 {
		t.Fatalf("packet = %+v", p)
	}
}

func TestParseMessagesIPv6UDPWithPrefix(t *testing.T) {
	payload := []byte{
		0x60, 0x00, 0x00, 0x00, 0x00, 0x08, 17, 64,
		0x24, 0x09, 0x00, 0x10, 0x3d, 0x60, 0x12, 0x71, 0, 0, 0, 0, 0, 0, 0, 0x01,
		0xff, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xfb,
		0x14, 0xe9, 0x14, 0xe9, 0x00, 0x08, 0, 0,
	}
	msg := testNetlinkPacketMessage(
		attr(nfulaPrefix, append([]byte("routerd firewall input deny "), 0)),
		attr(nfulaPayload, payload),
	)
	packets, err := ParseMessages(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 {
		t.Fatalf("packets = %d", len(packets))
	}
	p := packets[0]
	if p.Protocol != "udp" || p.L3Proto != "ipv6" {
		t.Fatalf("packet = %+v", p)
	}
	if p.SrcAddress != "2409:10:3d60:1271::1" || p.SrcPort != 5353 || p.DstAddress != "ff02::fb" || p.DstPort != 5353 {
		t.Fatalf("packet = %+v", p)
	}
}

func testNetlinkPacketMessage(attrs ...[]byte) []byte {
	var body []byte
	body = append(body, 0, 0, 0, 0)
	for _, attr := range attrs {
		body = append(body, attr...)
	}
	length := 16 + len(body)
	out := make([]byte, length)
	binary.LittleEndian.PutUint32(out[0:4], uint32(length))
	binary.LittleEndian.PutUint16(out[4:6], uint16(nfnlSubsysULog<<8)|nfulnlMsgPacket)
	copy(out[16:], body)
	return out
}
