// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"unsafe"

	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"golang.org/x/sys/unix"
)

func bpfTestReply(t *testing.T, messageType byte, xid uint32) ([]byte, []byte) {
	t.Helper()
	payload, err := dhcpv4client.Encode(dhcpv4client.Message{
		Op: 2, HType: 1, XID: xid,
		YIAddr: netip.MustParseAddr("192.0.2.10"),
		CHAddr: net.HardwareAddr{2, 0, 0, 0, 0, 10},
		Options: map[byte][]byte{
			dhcpv4client.OptionMessageType: {messageType},
			dhcpv4client.OptionServerID:    {192, 0, 2, 1},
			dhcpv4client.OptionSubnetMask:  {255, 255, 255, 0},
			dhcpv4client.OptionLeaseTime:   {0, 0, 14, 16},
			dhcpv4client.OptionHostname:    []byte("one"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := ethernetBroadcast(net.HardwareAddr{2, 0, 0, 0, 0, 1}, dhcpv4client.Packet{
		Payload: payload, Source: netip.MustParseAddr("192.0.2.1"), Destination: netip.MustParseAddr("255.255.255.255"),
	})
	// The existing frame encoder supplies the IP envelope. Only the UDP
	// server/client ports differ from an outbound DHCP client broadcast.
	binary.BigEndian.PutUint16(frame[34:36], 67)
	binary.BigEndian.PutUint16(frame[36:38], 68)
	return frame, payload
}

func bpfTestRecord(frame []byte) []byte {
	headerLen := int(unsafe.Sizeof(unix.BpfHdr{}))
	header := unix.BpfHdr{Hdrlen: uint16(headerLen), Caplen: uint32(len(frame)), Datalen: uint32(len(frame))}
	record := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&header)), headerLen)...)
	record = append(record, frame...)
	// BPF records are padded to the native long boundary; do not use the
	// parser's alignment helper to construct its own expected input.
	for len(record)%int(unsafe.Sizeof(uintptr(0))) != 0 {
		record = append(record, 0)
	}
	return record
}

func TestBPFCollectPacketsPreservesAlignedReplyRecords(t *testing.T) {
	firstFrame, firstPayload := bpfTestReply(t, dhcpv4client.MessageOffer, 11)
	secondFrame, secondPayload := bpfTestReply(t, dhcpv4client.MessageACK, 12)
	firstRecord := bpfTestRecord(firstFrame)
	if len(firstRecord) == int(unsafe.Sizeof(unix.BpfHdr{}))+len(firstFrame) {
		t.Fatal("fixture does not exercise inter-record alignment padding")
	}
	data := append(firstRecord, bpfTestRecord(secondFrame)...)
	conn := &bpfPacketConn{}
	conn.collectPackets(data)
	if len(conn.pending) != 2 {
		t.Fatalf("coalesced BPF read lost a DHCP reply: packets=%d", len(conn.pending))
	}
	for i, expected := range [][]byte{firstPayload, secondPayload} {
		output := make([]byte, len(expected))
		n, _, err := conn.ReadFromUDP(output)
		if err != nil || n != len(expected) || !bytes.Equal(output, expected) {
			t.Fatalf("BPF reply %d lost order or payload: received=%d expected=%d err=%v", i, n, len(expected), err)
		}
	}
}

func TestBPFCollectPacketsKeepsReplyAfterUnrelatedFrame(t *testing.T) {
	unrelated := make([]byte, 60)
	binary.BigEndian.PutUint16(unrelated[12:14], 0x0806)
	frame, payload := bpfTestReply(t, dhcpv4client.MessageOffer, 13)
	conn := &bpfPacketConn{}
	conn.collectPackets(append(bpfTestRecord(unrelated), bpfTestRecord(frame)...))
	if len(conn.pending) != 1 || !bytes.Equal(conn.pending[0], payload) {
		t.Fatalf("unrelated frame hid a later DHCP reply: packets=%d", len(conn.pending))
	}
}
