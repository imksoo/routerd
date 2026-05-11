// SPDX-License-Identifier: BSD-3-Clause

package dpi

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
)

type ClassifyRequest struct {
	Packet            []byte `json:"packet,omitempty"`
	Payload           []byte `json:"payload,omitempty"`
	L4Payload         []byte `json:"l4Payload,omitempty"`
	L3Proto           string `json:"l3Proto,omitempty"`
	TransportProtocol string `json:"transportProtocol,omitempty"`
	SrcAddress        string `json:"srcAddress,omitempty"`
	SrcPort           int    `json:"srcPort,omitempty"`
	DstAddress        string `json:"dstAddress,omitempty"`
	DstPort           int    `json:"dstPort,omitempty"`
}

type ClassifyResult struct {
	AppName           string `json:"appName,omitempty"`
	AppCategory       string `json:"appCategory,omitempty"`
	AppConfidence     int    `json:"appConfidence,omitempty"`
	TLSSNI            string `json:"tlsSNI,omitempty"`
	HTTPHost          string `json:"httpHost,omitempty"`
	DNSQuery          string `json:"dnsQuery,omitempty"`
	L3Proto           string `json:"l3Proto,omitempty"`
	TransportProtocol string `json:"transportProtocol,omitempty"`
	SrcAddress        string `json:"srcAddress,omitempty"`
	SrcPort           int    `json:"srcPort,omitempty"`
	DstAddress        string `json:"dstAddress,omitempty"`
	DstPort           int    `json:"dstPort,omitempty"`
	Engine            string `json:"engine"`
	Reason            string `json:"reason,omitempty"`
}

func Classify(req ClassifyRequest) ClassifyResult {
	result := ClassifyResult{
		L3Proto:           req.L3Proto,
		TransportProtocol: strings.ToLower(req.TransportProtocol),
		SrcAddress:        req.SrcAddress,
		SrcPort:           req.SrcPort,
		DstAddress:        req.DstAddress,
		DstPort:           req.DstPort,
		Engine:            "routerd-dpi-parser",
	}
	payload := req.L4Payload
	if len(payload) == 0 {
		payload = req.Payload
	}
	if len(req.Packet) > 0 {
		meta, l4, ok := parseIPPacket(req.Packet)
		if ok {
			payload = l4
			if result.L3Proto == "" {
				result.L3Proto = meta.L3Proto
			}
			if result.TransportProtocol == "" {
				result.TransportProtocol = meta.TransportProtocol
			}
			if result.SrcAddress == "" {
				result.SrcAddress = meta.SrcAddress
			}
			if result.DstAddress == "" {
				result.DstAddress = meta.DstAddress
			}
			if result.SrcPort == 0 {
				result.SrcPort = meta.SrcPort
			}
			if result.DstPort == 0 {
				result.DstPort = meta.DstPort
			}
		}
	}
	if host, ok := ExtractTLSSNI(payload); ok {
		result.AppName = "tls"
		result.AppCategory = "web"
		result.AppConfidence = 90
		result.TLSSNI = host
		result.Reason = "tls_client_hello_sni"
		return result
	}
	if host, ok := ExtractHTTPHost(payload); ok {
		result.AppName = "http"
		result.AppCategory = "web"
		result.AppConfidence = 80
		result.HTTPHost = host
		result.Reason = "http_host"
		return result
	}
	if qname, ok := ExtractDNSQuery(payload); ok {
		result.AppName = "dns"
		result.AppCategory = "network"
		result.AppConfidence = 75
		result.DNSQuery = qname
		result.Reason = "dns_query"
		return result
	}
	result.AppName = "unknown"
	result.AppConfidence = 0
	result.Reason = "no_application_signal"
	return result
}

type packetMeta struct {
	L3Proto           string
	TransportProtocol string
	SrcAddress        string
	SrcPort           int
	DstAddress        string
	DstPort           int
}

func parseIPPacket(packet []byte) (packetMeta, []byte, bool) {
	if len(packet) < 1 {
		return packetMeta{}, nil, false
	}
	switch packet[0] >> 4 {
	case 4:
		return parseIPv4Packet(packet)
	case 6:
		return parseIPv6Packet(packet)
	default:
		return packetMeta{}, nil, false
	}
}

func parseIPv4Packet(packet []byte) (packetMeta, []byte, bool) {
	if len(packet) < 20 {
		return packetMeta{}, nil, false
	}
	ihl := int(packet[0]&0x0f) * 4
	if ihl < 20 || len(packet) < ihl {
		return packetMeta{}, nil, false
	}
	meta := packetMeta{
		L3Proto:           "ipv4",
		TransportProtocol: protocolName(packet[9]),
		SrcAddress:        netip.AddrFrom4([4]byte(packet[12:16])).String(),
		DstAddress:        netip.AddrFrom4([4]byte(packet[16:20])).String(),
	}
	l4 := packet[ihl:]
	payload := transportPayload(&meta, l4)
	return meta, payload, true
}

func parseIPv6Packet(packet []byte) (packetMeta, []byte, bool) {
	if len(packet) < 40 {
		return packetMeta{}, nil, false
	}
	src, ok := netip.AddrFromSlice(packet[8:24])
	if !ok {
		return packetMeta{}, nil, false
	}
	dst, ok := netip.AddrFromSlice(packet[24:40])
	if !ok {
		return packetMeta{}, nil, false
	}
	meta := packetMeta{
		L3Proto:           "ipv6",
		TransportProtocol: protocolName(packet[6]),
		SrcAddress:        src.String(),
		DstAddress:        dst.String(),
	}
	payload := transportPayload(&meta, packet[40:])
	return meta, payload, true
}

func transportPayload(meta *packetMeta, segment []byte) []byte {
	switch meta.TransportProtocol {
	case "tcp":
		if len(segment) < 20 {
			return nil
		}
		meta.SrcPort = int(binary.BigEndian.Uint16(segment[0:2]))
		meta.DstPort = int(binary.BigEndian.Uint16(segment[2:4]))
		offset := int(segment[12]>>4) * 4
		if offset < 20 || len(segment) < offset {
			return nil
		}
		return segment[offset:]
	case "udp":
		if len(segment) < 8 {
			return nil
		}
		meta.SrcPort = int(binary.BigEndian.Uint16(segment[0:2]))
		meta.DstPort = int(binary.BigEndian.Uint16(segment[2:4]))
		return segment[8:]
	default:
		return nil
	}
}

func protocolName(proto byte) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("%d", proto)
	}
}

func ExtractTLSSNI(payload []byte) (string, bool) {
	if len(payload) < 5 || payload[0] != 0x16 {
		return "", false
	}
	recordLen := int(binary.BigEndian.Uint16(payload[3:5]))
	end := 5 + recordLen
	if end > len(payload) {
		end = len(payload)
	}
	pos := 5
	if pos+4 > end || payload[pos] != 0x01 {
		return "", false
	}
	handshakeLen := int(payload[pos+1])<<16 | int(payload[pos+2])<<8 | int(payload[pos+3])
	pos += 4
	if handshakeEnd := pos + handshakeLen; handshakeEnd < end {
		end = handshakeEnd
	}
	if pos+34 > end {
		return "", false
	}
	pos += 2 + 32
	if pos+1 > end {
		return "", false
	}
	sessionLen := int(payload[pos])
	pos++
	if pos+sessionLen+2 > end {
		return "", false
	}
	pos += sessionLen
	cipherLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2
	if pos+cipherLen+1 > end {
		return "", false
	}
	pos += cipherLen
	compressionLen := int(payload[pos])
	pos++
	if pos+compressionLen+2 > end {
		return "", false
	}
	pos += compressionLen
	extLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2
	extEnd := pos + extLen
	if extEnd > end {
		extEnd = end
	}
	for pos+4 <= extEnd {
		extType := binary.BigEndian.Uint16(payload[pos : pos+2])
		extDataLen := int(binary.BigEndian.Uint16(payload[pos+2 : pos+4]))
		pos += 4
		if pos+extDataLen > extEnd {
			return "", false
		}
		if extType == 0 {
			return parseSNIExtension(payload[pos : pos+extDataLen])
		}
		pos += extDataLen
	}
	return "", false
}

func parseSNIExtension(data []byte) (string, bool) {
	if len(data) < 2 {
		return "", false
	}
	listLen := int(binary.BigEndian.Uint16(data[0:2]))
	pos := 2
	end := pos + listLen
	if end > len(data) {
		return "", false
	}
	for pos+3 <= end {
		nameType := data[pos]
		nameLen := int(binary.BigEndian.Uint16(data[pos+1 : pos+3]))
		pos += 3
		if pos+nameLen > end {
			return "", false
		}
		if nameType == 0 && nameLen > 0 {
			name := string(data[pos : pos+nameLen])
			if strings.Contains(name, ".") {
				return name, true
			}
		}
		pos += nameLen
	}
	return "", false
}

func ExtractHTTPHost(payload []byte) (string, bool) {
	if len(payload) == 0 {
		return "", false
	}
	text := string(payload)
	if !(strings.HasPrefix(text, "GET ") || strings.HasPrefix(text, "POST ") || strings.HasPrefix(text, "HEAD ") || strings.HasPrefix(text, "PUT ") || strings.HasPrefix(text, "DELETE ") || strings.HasPrefix(text, "OPTIONS ")) {
		return "", false
	}
	for _, line := range strings.Split(text, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "host") {
			host := strings.TrimSpace(value)
			if host != "" {
				return host, true
			}
		}
	}
	return "", false
}

func ExtractDNSQuery(payload []byte) (string, bool) {
	if len(payload) < 13 {
		return "", false
	}
	qdCount := binary.BigEndian.Uint16(payload[4:6])
	if qdCount == 0 {
		return "", false
	}
	pos := 12
	var labels []string
	for pos < len(payload) {
		length := int(payload[pos])
		pos++
		if length == 0 {
			break
		}
		if length&0xc0 != 0 || length > 63 || pos+length > len(payload) {
			return "", false
		}
		labels = append(labels, string(payload[pos:pos+length]))
		pos += length
	}
	if len(labels) == 0 || pos+4 > len(payload) {
		return "", false
	}
	return strings.Join(labels, "."), true
}
