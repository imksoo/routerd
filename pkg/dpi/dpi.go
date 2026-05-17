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
	AppName             string            `json:"appName,omitempty"`
	AppCategory         string            `json:"appCategory,omitempty"`
	AppConfidence       int               `json:"appConfidence,omitempty"`
	DetectedProtocol    string            `json:"detectedProtocol,omitempty"`
	MasterProtocol      string            `json:"masterProtocol,omitempty"`
	ApplicationProtocol string            `json:"applicationProtocol,omitempty"`
	Category            string            `json:"category,omitempty"`
	Risk                []string          `json:"risk,omitempty"`
	Confidence          int               `json:"confidence,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Source              string            `json:"source,omitempty"`
	TLSSNI              string            `json:"tlsSNI,omitempty"`
	HTTPHost            string            `json:"httpHost,omitempty"`
	DNSQuery            string            `json:"dnsQuery,omitempty"`
	L3Proto             string            `json:"l3Proto,omitempty"`
	TransportProtocol   string            `json:"transportProtocol,omitempty"`
	SrcAddress          string            `json:"srcAddress,omitempty"`
	SrcPort             int               `json:"srcPort,omitempty"`
	DstAddress          string            `json:"dstAddress,omitempty"`
	DstPort             int               `json:"dstPort,omitempty"`
	Engine              string            `json:"engine"`
	Reason              string            `json:"reason,omitempty"`
}

func Classify(req ClassifyRequest) (result ClassifyResult) {
	result = ClassifyResult{
		L3Proto:           req.L3Proto,
		TransportProtocol: strings.ToLower(req.TransportProtocol),
		SrcAddress:        req.SrcAddress,
		SrcPort:           req.SrcPort,
		DstAddress:        req.DstAddress,
		DstPort:           req.DstPort,
		Engine:            "builtin",
		Source:            "builtin",
	}
	defer func() {
		result = FinalizeResult(result)
	}()
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
	if vpn, ok := classifyVPNDatagram(result.TransportProtocol, result.SrcPort, result.DstPort, payload); ok {
		result.AppName = vpn.app
		result.AppCategory = vpn.category
		result.AppConfidence = vpn.confidence
		result.Reason = vpn.reason
		return result
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
	if name, ok := ExtractNBNSQuery(payload); ok {
		result.AppName = "netbios"
		result.AppCategory = "network"
		result.AppConfidence = 75
		result.DNSQuery = name
		result.Reason = "nbns_query"
		return result
	}
	if qname, ok := ExtractDNSQuery(payload); ok {
		if tailscaleDNSName(qname) {
			result.AppName = "tailscale"
			result.AppCategory = "vpn"
			result.AppConfidence = 80
			result.DNSQuery = qname
			result.Reason = "tailscale_dns_query"
			return result
		}
		result.AppName = "dns"
		result.AppCategory = "network"
		result.AppConfidence = 75
		result.DNSQuery = qname
		result.Reason = "dns_query"
		return result
	}
	if vpn, ok := classifyVPNPortFallback(result.TransportProtocol, result.SrcPort, result.DstPort); ok {
		result.AppName = vpn.app
		result.AppCategory = vpn.category
		result.AppConfidence = vpn.confidence
		result.Reason = vpn.reason
		return result
	}
	result.AppName = "unknown"
	result.AppConfidence = 0
	result.Reason = "no_application_signal"
	return result
}

func FinalizeResult(result ClassifyResult) ClassifyResult {
	result.AppName = strings.ToLower(strings.TrimSpace(result.AppName))
	result.AppCategory = strings.ToLower(strings.TrimSpace(result.AppCategory))
	result.DetectedProtocol = strings.ToLower(strings.TrimSpace(result.DetectedProtocol))
	result.MasterProtocol = strings.ToLower(strings.TrimSpace(result.MasterProtocol))
	result.ApplicationProtocol = strings.ToLower(strings.TrimSpace(result.ApplicationProtocol))
	result.Category = strings.ToLower(strings.TrimSpace(result.Category))
	result.Source = strings.ToLower(strings.TrimSpace(result.Source))
	result.Engine = strings.ToLower(strings.TrimSpace(result.Engine))
	result.TransportProtocol = strings.ToLower(strings.TrimSpace(result.TransportProtocol))
	if result.ApplicationProtocol == "" && result.AppName != "" && result.AppName != "unknown" {
		result.ApplicationProtocol = result.AppName
	}
	if result.DetectedProtocol == "" {
		result.DetectedProtocol = firstNonEmpty(result.ApplicationProtocol, result.MasterProtocol, result.AppName, result.TransportProtocol)
	}
	if result.Category == "" {
		result.Category = result.AppCategory
	}
	if result.Confidence == 0 {
		result.Confidence = result.AppConfidence
	}
	if result.Metadata == nil {
		result.Metadata = map[string]string{}
	}
	if result.Reason != "" {
		result.Metadata["reason"] = result.Reason
	}
	if result.TLSSNI != "" {
		result.Metadata["tls.sni"] = result.TLSSNI
	}
	if result.HTTPHost != "" {
		result.Metadata["http.host"] = result.HTTPHost
	}
	if result.DNSQuery != "" {
		result.Metadata["dns.query"] = result.DNSQuery
	}
	if len(result.Metadata) == 0 {
		result.Metadata = nil
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type vpnClassification struct {
	app        string
	category   string
	confidence int
	reason     string
}

func classifyVPNDatagram(protocol string, srcPort, dstPort int, payload []byte) (vpnClassification, bool) {
	if strings.ToLower(strings.TrimSpace(protocol)) != "udp" {
		return vpnClassification{}, false
	}
	if IsSTUNPacket(payload) {
		if srcPort == 41641 || dstPort == 41641 {
			return vpnClassification{app: "tailscale", category: "vpn", confidence: 95, reason: "tailscale_stun_magic_cookie"}, true
		}
		return vpnClassification{app: "stun", category: "nat-traversal", confidence: 95, reason: "stun_magic_cookie"}, true
	}
	if wireGuardMessageType(payload) != 0 {
		if srcPort == 41641 || dstPort == 41641 {
			return vpnClassification{app: "tailscale", category: "vpn", confidence: 90, reason: "tailscale_wireguard_message"}, true
		}
		return vpnClassification{app: "wireguard", category: "vpn", confidence: 85, reason: "wireguard_message_type"}, true
	}
	return vpnClassification{}, false
}

func classifyVPNPortFallback(protocol string, srcPort, dstPort int) (vpnClassification, bool) {
	if strings.ToLower(strings.TrimSpace(protocol)) != "udp" {
		return vpnClassification{}, false
	}
	switch {
	case srcPort == 41641 || dstPort == 41641:
		return vpnClassification{app: "tailscale", category: "port-fallback", confidence: 55, reason: "tailscale_default_port"}, true
	case srcPort == 3478 || dstPort == 3478 || srcPort == 5349 || dstPort == 5349:
		return vpnClassification{app: "stun", category: "port-fallback", confidence: 50, reason: "stun_well_known_port"}, true
	case srcPort == 443 || dstPort == 443:
		return vpnClassification{app: "quic", category: "port-fallback", confidence: 35, reason: "udp_443_quic_http3"}, true
	}
	return vpnClassification{}, false
}

func IsSTUNPacket(payload []byte) bool {
	if len(payload) < 20 {
		return false
	}
	if payload[0]&0xc0 != 0 {
		return false
	}
	if binary.BigEndian.Uint32(payload[4:8]) != 0x2112a442 {
		return false
	}
	msgLen := int(binary.BigEndian.Uint16(payload[2:4]))
	return msgLen%4 == 0 && msgLen <= len(payload)-20
}

func wireGuardMessageType(payload []byte) uint32 {
	if len(payload) < 4 {
		return 0
	}
	msgType := binary.LittleEndian.Uint32(payload[:4])
	switch msgType {
	case 1:
		if len(payload) >= 148 {
			return msgType
		}
	case 2:
		if len(payload) >= 92 {
			return msgType
		}
	case 3:
		if len(payload) >= 64 {
			return msgType
		}
	case 4:
		if len(payload) >= 32 {
			return msgType
		}
	}
	return 0
}

func tailscaleDNSName(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if name == "" {
		return false
	}
	return name == "stun.l.google.com" ||
		name == "login.tailscale.com" ||
		name == "controlplane.tailscale.com" ||
		strings.HasSuffix(name, ".tailscale.com") ||
		strings.HasSuffix(name, ".ts.net")
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
		if !validDNSLabel(payload[pos : pos+length]) {
			return "", false
		}
		labels = append(labels, string(payload[pos:pos+length]))
		pos += length
	}
	if len(labels) == 0 || pos+4 > len(payload) {
		return "", false
	}
	qtype := binary.BigEndian.Uint16(payload[pos : pos+2])
	qclass := binary.BigEndian.Uint16(payload[pos+2 : pos+4])
	if !validDNSQuestionType(qtype) || !validDNSQuestionClass(qclass) {
		return "", false
	}
	return strings.Join(labels, "."), true
}

func validDNSLabel(label []byte) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	for _, ch := range label {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-' || ch == '_':
		default:
			return false
		}
	}
	return true
}

func validDNSQuestionType(qtype uint16) bool {
	switch qtype {
	case 1, 2, 5, 6, 12, 15, 16, 28, 33, 43, 46, 47, 48, 52, 64, 65, 255:
		return true
	default:
		return false
	}
}

func validDNSQuestionClass(qclass uint16) bool {
	switch qclass {
	case 1, 3, 4, 255, 0x8001:
		return true
	default:
		return false
	}
}

func ExtractNBNSQuery(payload []byte) (string, bool) {
	if len(payload) < 12+1+32+4 {
		return "", false
	}
	qdCount := binary.BigEndian.Uint16(payload[4:6])
	if qdCount == 0 {
		return "", false
	}
	pos := 12
	if int(payload[pos]) != 32 || pos+1+32+4 > len(payload) {
		return "", false
	}
	pos++
	encoded := payload[pos : pos+32]
	decoded := make([]byte, 16)
	for i := 0; i < 16; i++ {
		hi := encoded[i*2]
		lo := encoded[i*2+1]
		if hi < 'A' || hi > 'P' || lo < 'A' || lo > 'P' {
			return "", false
		}
		decoded[i] = ((hi - 'A') << 4) | (lo - 'A')
	}
	pos += 32
	if pos >= len(payload) || payload[pos] != 0x00 {
		return "", false
	}
	pos++
	if pos+4 > len(payload) {
		return "", false
	}
	qtype := binary.BigEndian.Uint16(payload[pos : pos+2])
	qclass := binary.BigEndian.Uint16(payload[pos+2 : pos+4])
	if qclass != 0x0001 || (qtype != 0x0020 && qtype != 0x0021) {
		return "", false
	}
	name := strings.TrimRight(string(decoded[:15]), " \x00")
	suffix := decoded[15]
	if name == "" {
		name = "*"
	}
	return fmt.Sprintf("%s<0x%02x>", name, suffix), true
}
