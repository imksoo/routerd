// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"errors"
	"github.com/imksoo/routerd/pkg/dpi"
	"net/netip"
	"strings"
)

func packetForNDPI(req dpi.ClassifyRequest) ([]byte, error) {
	if len(req.Packet) > 0 {
		info, ok := dpi.InspectPacket(req.Packet)
		if !ok {
			return nil, errors.New("invalid IP packet")
		}
		return info.Packet, nil
	}
	meta := dpi.Classify(req)
	if meta.SrcPort < 0 || meta.SrcPort > 65535 || meta.DstPort < 0 || meta.DstPort > 65535 {
		return nil, errors.New("transport port is outside uint16 range")
	}
	src, err := netip.ParseAddr(meta.SrcAddress)
	if err != nil {
		return nil, err
	}
	dst, err := netip.ParseAddr(meta.DstAddress)
	if err != nil {
		return nil, err
	}
	if !src.Is4() || !dst.Is4() {
		return nil, errors.New("cannot synthesize non-IPv4 packet")
	}
	payload := req.L4Payload
	if len(payload) == 0 {
		payload = req.Payload
	}
	switch strings.ToLower(meta.TransportProtocol) {
	case "tcp":
		if len(payload) > 65535-40 {
			return nil, errors.New("TCP payload exceeds IPv4 length limit")
		}
		return synthesizeIPv4TCP(src, dst, meta.SrcPort, meta.DstPort, payload), nil
	case "udp":
		if len(payload) > 65535-28 {
			return nil, errors.New("UDP payload exceeds IPv4 length limit")
		}
		return synthesizeIPv4UDP(src, dst, meta.SrcPort, meta.DstPort, payload), nil
	default:
		return nil, errors.New("unsupported transport protocol")
	}
}

func synthesizeIPv4TCP(src, dst netip.Addr, srcPort, dstPort int, payload []byte) []byte {
	l4Len := 20 + len(payload)
	packet := make([]byte, 20+l4Len)
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 6
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], src.AsSlice())
	copy(packet[16:20], dst.AsSlice())
	binary.BigEndian.PutUint16(packet[20:22], uint16(srcPort))
	binary.BigEndian.PutUint16(packet[22:24], uint16(dstPort))
	packet[32] = 0x50
	packet[33] = 0x18
	copy(packet[40:], payload)
	return packet
}

func synthesizeIPv4UDP(src, dst netip.Addr, srcPort, dstPort int, payload []byte) []byte {
	l4Len := 8 + len(payload)
	packet := make([]byte, 20+l4Len)
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 17
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], src.AsSlice())
	copy(packet[16:20], dst.AsSlice())
	binary.BigEndian.PutUint16(packet[20:22], uint16(srcPort))
	binary.BigEndian.PutUint16(packet[22:24], uint16(dstPort))
	binary.BigEndian.PutUint16(packet[24:26], uint16(l4Len))
	copy(packet[28:], payload)
	return packet
}
