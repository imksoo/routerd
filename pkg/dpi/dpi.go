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
		result.Source = "builtin-payload"
		result.Reason = vpn.reason
		return result
	}
	if host, ok := ExtractTLSSNI(payload); ok {
		result.AppName = "tls"
		result.AppCategory = "web"
		result.AppConfidence = 90
		result.Source = "builtin-payload"
		result.TLSSNI = host
		result.Reason = "tls_client_hello_sni"
		return result
	}
	if host, ok := ExtractHTTPHost(payload); ok {
		result.AppName = "http"
		result.AppCategory = "web"
		result.AppConfidence = 80
		result.Source = "builtin-payload"
		result.HTTPHost = host
		result.Reason = "http_host"
		return result
	}
	if name, ok := ExtractNBNSQuery(payload); ok {
		result.AppName = "netbios"
		result.AppCategory = "network"
		result.AppConfidence = 75
		result.Source = "builtin-payload"
		result.DNSQuery = name
		result.Reason = "nbns_query"
		return result
	}
	if app, ok := classifyKnownPayload(result.TransportProtocol, result.SrcPort, result.DstPort, payload); ok {
		result.AppName = app.app
		result.AppCategory = app.category
		result.AppConfidence = app.confidence
		result.Source = "builtin-payload"
		result.Reason = app.reason
		if len(app.metadata) > 0 {
			result.Metadata = app.metadata
		}
		return result
	}
	if qname, ok := ExtractDNSQuery(payload); ok {
		if tailscaleDNSName(qname) {
			result.AppName = "tailscale"
			result.AppCategory = "vpn"
			result.AppConfidence = 80
			result.Source = "builtin-payload"
			result.DNSQuery = qname
			result.Reason = "tailscale_dns_query"
			return result
		}
		result.AppName = "dns"
		result.AppCategory = "network"
		result.AppConfidence = 75
		result.Source = "builtin-payload"
		result.DNSQuery = qname
		result.Reason = "dns_query"
		return result
	}
	if vpn, ok := classifyVPNPortFallback(result.TransportProtocol, result.SrcPort, result.DstPort); ok {
		result.AppName = vpn.app
		result.AppCategory = vpn.category
		result.AppConfidence = vpn.confidence
		result.Source = "port-fallback"
		result.Reason = vpn.reason
		return result
	}
	result.AppName = "unknown"
	result.AppConfidence = 0
	result.Source = "builtin"
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

type payloadClassification struct {
	app        string
	category   string
	confidence int
	reason     string
	metadata   map[string]string
}

func classifyKnownPayload(protocol string, srcPort, dstPort int, payload []byte) (payloadClassification, bool) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "tcp":
		if ssh, ok := ExtractSSHBanner(payload); ok {
			meta := map[string]string{}
			if ssh != "" {
				meta["ssh.software"] = ssh
			}
			return payloadClassification{app: "ssh", category: "remote-access", confidence: 85, reason: "ssh_banner", metadata: meta}, true
		}
		if ftp, ok := ExtractFTPControl(payload); ok {
			meta := map[string]string{}
			if ftp != "" {
				meta["ftp.message"] = ftp
			}
			return payloadClassification{app: "ftp", category: "file-transfer", confidence: 70, reason: "ftp_control", metadata: meta}, true
		}
		if mqtt, ok := ExtractMQTT(payload, srcPort, dstPort); ok {
			meta := map[string]string{"mqtt.packet_type": mqtt}
			return payloadClassification{app: "mqtt", category: "messaging", confidence: 75, reason: "mqtt_packet", metadata: meta}, true
		}
	case "udp":
		if dhcp, ok := ExtractDHCPv4(payload, srcPort, dstPort); ok {
			meta := map[string]string{}
			if dhcp.messageType != "" {
				meta["dhcp.message_type"] = dhcp.messageType
			}
			if dhcp.hostname != "" {
				meta["dhcp.hostname"] = dhcp.hostname
			}
			return payloadClassification{app: "dhcp", category: "network", confidence: 85, reason: "dhcpv4_message", metadata: meta}, true
		}
		if qname, ok := ExtractPortScopedDNSQuery(payload, srcPort, dstPort, 5353); ok {
			meta := map[string]string{"dns.query": qname}
			return payloadClassification{app: "mdns", category: "network", confidence: 80, reason: "mdns_query", metadata: meta}, true
		}
		if qname, ok := ExtractPortScopedDNSQuery(payload, srcPort, dstPort, 5355); ok {
			meta := map[string]string{"dns.query": qname}
			return payloadClassification{app: "llmnr", category: "network", confidence: 80, reason: "llmnr_query", metadata: meta}, true
		}
		if ntp, ok := ExtractNTP(payload, srcPort, dstPort); ok {
			meta := map[string]string{
				"ntp.version": ntp.version,
				"ntp.mode":    ntp.mode,
				"ntp.stratum": ntp.stratum,
			}
			return payloadClassification{app: "ntp", category: "network", confidence: 80, reason: "ntp_message", metadata: meta}, true
		}
		if ssdp, ok := ExtractSSDP(payload, srcPort, dstPort); ok {
			meta := map[string]string{}
			if ssdp != "" {
				meta["ssdp.method"] = ssdp
			}
			return payloadClassification{app: "ssdp", category: "network", confidence: 75, reason: "ssdp_message", metadata: meta}, true
		}
	}
	return payloadClassification{}, false
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

func ExtractPortScopedDNSQuery(payload []byte, srcPort, dstPort, port int) (string, bool) {
	if srcPort != port && dstPort != port {
		return "", false
	}
	return ExtractDNSQuery(payload)
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

func ExtractSSHBanner(payload []byte) (string, bool) {
	if len(payload) < len("SSH-1.0") {
		return "", false
	}
	prefix := string(payload[:6])
	if prefix != "SSH-1." && prefix != "SSH-2." {
		return "", false
	}
	lineEnd := len(payload)
	for i, ch := range payload {
		if ch == '\r' || ch == '\n' {
			lineEnd = i
			break
		}
	}
	if lineEnd > 255 {
		lineEnd = 255
	}
	line := string(payload[:lineEnd])
	parts := strings.SplitN(line, "-", 3)
	if len(parts) < 3 {
		return "", true
	}
	software := strings.TrimSpace(parts[2])
	if software == "" {
		return "", true
	}
	return software, true
}

func ExtractFTPControl(payload []byte) (string, bool) {
	if len(payload) < 3 {
		return "", false
	}
	lineEnd := len(payload)
	for i, ch := range payload {
		if ch == '\r' || ch == '\n' {
			lineEnd = i
			break
		}
	}
	if lineEnd == 0 || lineEnd > 256 {
		return "", false
	}
	line := string(payload[:lineEnd])
	if len(line) >= 4 && isDigit(line[0]) && isDigit(line[1]) && isDigit(line[2]) && (line[3] == ' ' || line[3] == '-') {
		return strings.TrimSpace(line[:3]), true
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	cmd := strings.ToUpper(fields[0])
	switch cmd {
	case "USER", "PASS", "AUTH", "PBSZ", "PROT", "SYST", "FEAT", "TYPE", "PASV", "EPSV", "PORT", "EPRT", "LIST", "NLST", "RETR", "STOR", "CWD", "PWD", "QUIT", "NOOP":
		return cmd, true
	default:
		return "", false
	}
}

func ExtractMQTT(payload []byte, srcPort, dstPort int) (string, bool) {
	if srcPort != 1883 && dstPort != 1883 {
		return "", false
	}
	if len(payload) < 2 {
		return "", false
	}
	packetType := payload[0] >> 4
	remainingLen, pos, ok := parseMQTTRemainingLength(payload[1:])
	if !ok || remainingLen < 0 || pos+1+remainingLen > len(payload) {
		return "", false
	}
	switch packetType {
	case 1:
		start := 1 + pos
		if start+7 <= len(payload) && payload[start] == 0 && payload[start+1] == 4 && string(payload[start+2:start+6]) == "MQTT" {
			return "connect", true
		}
		if start+9 <= len(payload) && payload[start] == 0 && payload[start+1] == 6 && string(payload[start+2:start+8]) == "MQIsdp" {
			return "connect", true
		}
	case 2:
		return "connack", true
	case 3:
		return "publish", true
	case 8:
		return "subscribe", true
	case 12:
		return "pingreq", true
	case 14:
		return "disconnect", true
	}
	return "", false
}

func parseMQTTRemainingLength(data []byte) (int, int, bool) {
	multiplier := 1
	value := 0
	for i := 0; i < len(data) && i < 4; i++ {
		encoded := int(data[i])
		value += (encoded & 127) * multiplier
		if encoded&128 == 0 {
			return value, i + 1, true
		}
		multiplier *= 128
	}
	return 0, 0, false
}

type ntpInfo struct {
	version string
	mode    string
	stratum string
}

func ExtractNTP(payload []byte, srcPort, dstPort int) (ntpInfo, bool) {
	if srcPort != 123 && dstPort != 123 {
		return ntpInfo{}, false
	}
	if len(payload) < 48 {
		return ntpInfo{}, false
	}
	version := int((payload[0] >> 3) & 0x07)
	mode := int(payload[0] & 0x07)
	if version < 1 || version > 4 || mode == 0 || mode > 5 {
		return ntpInfo{}, false
	}
	return ntpInfo{
		version: fmt.Sprintf("%d", version),
		mode:    ntpModeName(mode),
		stratum: fmt.Sprintf("%d", payload[1]),
	}, true
}

func ntpModeName(mode int) string {
	switch mode {
	case 1:
		return "symmetric-active"
	case 2:
		return "symmetric-passive"
	case 3:
		return "client"
	case 4:
		return "server"
	case 5:
		return "broadcast"
	default:
		return "unknown"
	}
}

type dhcpv4Info struct {
	messageType string
	hostname    string
}

func ExtractDHCPv4(payload []byte, srcPort, dstPort int) (dhcpv4Info, bool) {
	if !((srcPort == 67 || srcPort == 68) && (dstPort == 67 || dstPort == 68)) {
		return dhcpv4Info{}, false
	}
	if len(payload) < 240 {
		return dhcpv4Info{}, false
	}
	if payload[0] != 1 && payload[0] != 2 {
		return dhcpv4Info{}, false
	}
	if binary.BigEndian.Uint32(payload[236:240]) != 0x63825363 {
		return dhcpv4Info{}, false
	}
	info := dhcpv4Info{}
	for pos := 240; pos < len(payload); {
		code := payload[pos]
		pos++
		switch code {
		case 0:
			continue
		case 255:
			if info.messageType == "" {
				return dhcpv4Info{}, false
			}
			return info, true
		}
		if pos >= len(payload) {
			break
		}
		length := int(payload[pos])
		pos++
		if pos+length > len(payload) {
			break
		}
		value := payload[pos : pos+length]
		switch code {
		case 12:
			if validASCIIText(value) {
				info.hostname = string(value)
			}
		case 53:
			if len(value) == 1 {
				info.messageType = dhcpMessageTypeName(value[0])
			}
		}
		pos += length
	}
	if info.messageType == "" {
		return dhcpv4Info{}, false
	}
	return info, true
}

func dhcpMessageTypeName(value byte) string {
	switch value {
	case 1:
		return "discover"
	case 2:
		return "offer"
	case 3:
		return "request"
	case 4:
		return "decline"
	case 5:
		return "ack"
	case 6:
		return "nak"
	case 7:
		return "release"
	case 8:
		return "inform"
	default:
		return fmt.Sprintf("%d", value)
	}
}

func ExtractSSDP(payload []byte, srcPort, dstPort int) (string, bool) {
	if srcPort != 1900 && dstPort != 1900 {
		return "", false
	}
	if len(payload) == 0 || len(payload) > 2048 {
		return "", false
	}
	text := string(payload)
	switch {
	case strings.HasPrefix(text, "M-SEARCH * HTTP/1.1\r\n"):
		return "m-search", true
	case strings.HasPrefix(text, "NOTIFY * HTTP/1.1\r\n"):
		return "notify", true
	case strings.HasPrefix(text, "HTTP/1.1 200 OK\r\n"):
		return "response", true
	default:
		return "", false
	}
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func validASCIIText(value []byte) bool {
	if len(value) == 0 || len(value) > 255 {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch > 0x7e {
			return false
		}
	}
	return true
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
