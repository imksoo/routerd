// SPDX-License-Identifier: BSD-3-Clause

package dpi

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
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

// PacketMetadata returns only the network tuple needed to identify an nDPI flow.
func PacketMetadata(req ClassifyRequest) ClassifyResult {
	result := ClassifyResult{
		L3Proto:           req.L3Proto,
		TransportProtocol: strings.ToLower(req.TransportProtocol),
		SrcAddress:        req.SrcAddress,
		SrcPort:           req.SrcPort,
		DstAddress:        req.DstAddress,
		DstPort:           req.DstPort,
	}
	if len(req.Packet) == 0 {
		return result
	}
	meta, ok := parseIPPacket(req.Packet)
	if !ok {
		return result
	}
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
		result.DetectedProtocol = stringutil.FirstNonEmpty(result.ApplicationProtocol, result.MasterProtocol, result.AppName, result.TransportProtocol)
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

type packetMeta struct {
	L3Proto           string
	TransportProtocol string
	SrcAddress        string
	SrcPort           int
	DstAddress        string
	DstPort           int
}

func parseIPPacket(packet []byte) (packetMeta, bool) {
	if len(packet) < 1 {
		return packetMeta{}, false
	}
	switch packet[0] >> 4 {
	case 4:
		return parseIPv4Packet(packet)
	case 6:
		return parseIPv6Packet(packet)
	default:
		return packetMeta{}, false
	}
}

func parseIPv4Packet(packet []byte) (packetMeta, bool) {
	if len(packet) < 20 {
		return packetMeta{}, false
	}
	ihl := int(packet[0]&0x0f) * 4
	if ihl < 20 || len(packet) < ihl {
		return packetMeta{}, false
	}
	meta := packetMeta{
		L3Proto:           "ipv4",
		TransportProtocol: protocolName(packet[9]),
		SrcAddress:        netip.AddrFrom4([4]byte(packet[12:16])).String(),
		DstAddress:        netip.AddrFrom4([4]byte(packet[16:20])).String(),
	}
	setTransportPorts(&meta, packet[ihl:])
	return meta, true
}

func parseIPv6Packet(packet []byte) (packetMeta, bool) {
	if len(packet) < 40 {
		return packetMeta{}, false
	}
	src, ok := netip.AddrFromSlice(packet[8:24])
	if !ok {
		return packetMeta{}, false
	}
	dst, ok := netip.AddrFromSlice(packet[24:40])
	if !ok {
		return packetMeta{}, false
	}
	meta := packetMeta{
		L3Proto:           "ipv6",
		TransportProtocol: protocolName(packet[6]),
		SrcAddress:        src.String(),
		DstAddress:        dst.String(),
	}
	setTransportPorts(&meta, packet[40:])
	return meta, true
}

func setTransportPorts(meta *packetMeta, segment []byte) {
	switch meta.TransportProtocol {
	case "tcp":
		if len(segment) >= 20 {
			meta.SrcPort = int(binary.BigEndian.Uint16(segment[0:2]))
			meta.DstPort = int(binary.BigEndian.Uint16(segment[2:4]))
		}
	case "udp":
		if len(segment) >= 8 {
			meta.SrcPort = int(binary.BigEndian.Uint16(segment[0:2]))
			meta.DstPort = int(binary.BigEndian.Uint16(segment[2:4]))
		}
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
