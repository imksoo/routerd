package dhcp6control

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

const (
	MessageSolicit            uint8 = 1
	MessageAdvertise          uint8 = 2
	MessageRequest            uint8 = 3
	MessageConfirm            uint8 = 4
	MessageRenew              uint8 = 5
	MessageRebind             uint8 = 6
	MessageReply              uint8 = 7
	MessageRelease            uint8 = 8
	MessageInformationRequest uint8 = 11

	optionClientID     uint16 = 1
	optionServerID     uint16 = 2
	optionORO          uint16 = 6
	optionElapsedTime  uint16 = 8
	optionStatusCode   uint16 = 13
	optionReconfAccept uint16 = 20
	optionIAPD         uint16 = 25
	optionIAPrefix     uint16 = 26
	etherTypeIPv6      uint16 = 0x86dd
	ipProtocolUDP      uint8  = 17
	dhcp6ClientPort    uint16 = 546
	dhcp6ServerPort    uint16 = 547
	ipv6HeaderLength          = 40
	udpHeaderLength           = 8
)

var dhcp6AllRelayAgentsAndServersMAC = net.HardwareAddr{0x33, 0x33, 0x00, 0x01, 0x00, 0x02}

type PacketSpec struct {
	MessageType   uint8
	TransactionID uint32

	SourceMAC      net.HardwareAddr
	DestinationMAC net.HardwareAddr
	SourceIP       netip.Addr
	DestinationIP  netip.Addr

	ClientDUID []byte
	ServerDUID []byte

	IAID              uint32
	T1                uint32
	T2                uint32
	Prefix            netip.Prefix
	PreferredLifetime uint32
	ValidLifetime     uint32

	ElapsedTime       uint16
	ORO               []uint16
	ReconfigureAccept bool
}

type MessageSummary struct {
	MessageType       uint8
	TransactionID     uint32
	ClientDUID        []byte
	ServerDUID        []byte
	ORO               []uint16
	ReconfigureAccept bool
	IAPD              []IAPDSummary
}

type IAPDSummary struct {
	IAID     uint32
	T1       uint32
	T2       uint32
	Prefixes []IAPrefixSummary
}

type IAPrefixSummary struct {
	Prefix            netip.Prefix
	PreferredLifetime uint32
	ValidLifetime     uint32
}

func DUIDLL(mac net.HardwareAddr) []byte {
	out := make([]byte, 4+len(mac))
	binary.BigEndian.PutUint16(out[0:2], 3)
	binary.BigEndian.PutUint16(out[2:4], 1)
	copy(out[4:], mac)
	return out
}

func ParseDUID(value string) ([]byte, error) {
	cleaned := strings.NewReplacer(":", "", "-", "", " ", "", "\t", "", "\n", "").Replace(strings.TrimSpace(value))
	if cleaned == "" {
		return nil, nil
	}
	if len(cleaned)%2 != 0 {
		return nil, fmt.Errorf("DUID hex length must be even")
	}
	out, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("parse DUID hex: %w", err)
	}
	return out, nil
}

func ParseDHCPv6(payload []byte) (MessageSummary, error) {
	if len(payload) < 4 {
		return MessageSummary{}, fmt.Errorf("DHCPv6 payload too short")
	}
	summary := MessageSummary{
		MessageType:   payload[0],
		TransactionID: uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3]),
	}
	if err := parseOptions(payload[4:], func(code uint16, data []byte) error {
		switch code {
		case optionClientID:
			summary.ClientDUID = append([]byte(nil), data...)
		case optionServerID:
			summary.ServerDUID = append([]byte(nil), data...)
		case optionORO:
			if len(data)%2 != 0 {
				return fmt.Errorf("ORO option length must be even")
			}
			for i := 0; i+2 <= len(data); i += 2 {
				summary.ORO = append(summary.ORO, binary.BigEndian.Uint16(data[i:i+2]))
			}
		case optionReconfAccept:
			summary.ReconfigureAccept = true
		case optionIAPD:
			iapd, err := parseIAPD(data)
			if err != nil {
				return err
			}
			summary.IAPD = append(summary.IAPD, iapd)
		}
		return nil
	}); err != nil {
		return MessageSummary{}, err
	}
	return summary, nil
}

func parseIAPD(data []byte) (IAPDSummary, error) {
	if len(data) < 12 {
		return IAPDSummary{}, fmt.Errorf("IA_PD option too short")
	}
	summary := IAPDSummary{
		IAID: binary.BigEndian.Uint32(data[0:4]),
		T1:   binary.BigEndian.Uint32(data[4:8]),
		T2:   binary.BigEndian.Uint32(data[8:12]),
	}
	if err := parseOptions(data[12:], func(code uint16, data []byte) error {
		if code != optionIAPrefix {
			return nil
		}
		if len(data) < 25 {
			return fmt.Errorf("IA Prefix option too short")
		}
		var addrBytes [16]byte
		copy(addrBytes[:], data[9:25])
		addr := netip.AddrFrom16(addrBytes)
		summary.Prefixes = append(summary.Prefixes, IAPrefixSummary{
			Prefix:            netip.PrefixFrom(addr, int(data[8])).Masked(),
			PreferredLifetime: binary.BigEndian.Uint32(data[0:4]),
			ValidLifetime:     binary.BigEndian.Uint32(data[4:8]),
		})
		return nil
	}); err != nil {
		return IAPDSummary{}, err
	}
	return summary, nil
}

func parseOptions(data []byte, visit func(code uint16, data []byte) error) error {
	for i := 0; i < len(data); {
		if i+4 > len(data) {
			return fmt.Errorf("truncated DHCPv6 option header")
		}
		code := binary.BigEndian.Uint16(data[i : i+2])
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		i += 4
		if i+length > len(data) {
			return fmt.Errorf("truncated DHCPv6 option %d", code)
		}
		if err := visit(code, data[i:i+length]); err != nil {
			return err
		}
		i += length
	}
	return nil
}

func BuildDHCPv6(spec PacketSpec) ([]byte, error) {
	if spec.MessageType == 0 {
		return nil, fmt.Errorf("message type is required")
	}
	if spec.TransactionID > 0xffffff {
		return nil, fmt.Errorf("transaction ID must fit in 24 bits")
	}
	if len(spec.ClientDUID) == 0 {
		return nil, fmt.Errorf("client DUID is required")
	}
	// Option order matches the working IX2215 (NEC IPoE PD client) reference
	// observed on 2026-05-01 against this PR-400NE HGW:
	//
	//   Client-ID, Server-ID (Request/Renew/Rebind/Release), IA_PD,
	//   Elapsed-Time, Reconfigure-Accept
	//
	// The previous routerd order placed Elapsed-Time and ORO before IA_PD,
	// which empirically did not elicit a Reply. ORO is omitted to match the
	// reference packet; we don't ask the HGW for DNS via DHCPv6 because LAN
	// DNS is served by dnsmasq from upstream.
	var out []byte
	out = append(out, spec.MessageType, byte(spec.TransactionID>>16), byte(spec.TransactionID>>8), byte(spec.TransactionID))
	out = appendOption(out, optionClientID, spec.ClientDUID)
	if len(spec.ServerDUID) > 0 {
		out = appendOption(out, optionServerID, spec.ServerDUID)
	}
	if spec.Prefix.IsValid() {
		out = appendOption(out, optionIAPD, buildIAPD(spec))
	} else if messageCarriesIAPD(spec.MessageType) {
		out = appendOption(out, optionIAPD, buildIAPDNoPrefix(spec))
	}
	out = appendOption(out, optionElapsedTime, uint16Bytes(spec.ElapsedTime))
	// ORO is supported here for callers that need to request specific options
	// from the server, but routerd's active controller intentionally omits it
	// to match the working IX2215 reference packet on this PR-400NE HGW.
	if len(spec.ORO) > 0 {
		var oro []byte
		for _, opt := range spec.ORO {
			oro = append(oro, uint16Bytes(opt)...)
		}
		out = appendOption(out, optionORO, oro)
	}
	if spec.ReconfigureAccept {
		out = appendOption(out, optionReconfAccept, nil)
	}
	return out, nil
}

func messageCarriesIAPD(messageType uint8) bool {
	switch messageType {
	case MessageSolicit, MessageRequest, MessageRenew, MessageRebind, MessageRelease:
		return true
	default:
		return false
	}
}

func BuildEthernetIPv6UDP(spec PacketSpec) ([]byte, error) {
	payload, err := BuildDHCPv6(spec)
	if err != nil {
		return nil, err
	}
	srcMAC := spec.SourceMAC
	dstMAC := spec.DestinationMAC
	if len(srcMAC) != 6 {
		return nil, fmt.Errorf("source MAC is required")
	}
	if len(dstMAC) == 0 {
		dstMAC = dhcp6AllRelayAgentsAndServersMAC
	}
	if len(dstMAC) != 6 {
		return nil, fmt.Errorf("destination MAC must be 6 bytes")
	}
	if !spec.SourceIP.Is6() || !spec.DestinationIP.Is6() {
		return nil, fmt.Errorf("source and destination IP must be IPv6")
	}

	udpLen := udpHeaderLength + len(payload)
	ipPayloadLen := udpLen
	frameLen := 14 + ipv6HeaderLength + udpLen
	frame := make([]byte, frameLen)
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], srcMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv6)

	ip := frame[14 : 14+ipv6HeaderLength]
	ip[0] = 0x60
	binary.BigEndian.PutUint16(ip[4:6], uint16(ipPayloadLen))
	ip[6] = ipProtocolUDP
	// Hop limit 64 matches the working IX2215 reference packet captured on
	// 2026-05-01 from this lab's PR-400NE HGW. Hop limit 1 was previously
	// used on the (incorrect) assumption that link-local multicast is fine
	// at TTL=1, but Proxmox virtual-network paths and the working reference
	// client both show that 64 is the value the HGW expects.
	ip[7] = 64
	copy(ip[8:24], spec.SourceIP.AsSlice())
	copy(ip[24:40], spec.DestinationIP.AsSlice())

	udp := frame[14+ipv6HeaderLength : 14+ipv6HeaderLength+udpHeaderLength]
	binary.BigEndian.PutUint16(udp[0:2], dhcp6ClientPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcp6ServerPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	copy(frame[14+ipv6HeaderLength+udpHeaderLength:], payload)
	csum := udpChecksum(spec.SourceIP, spec.DestinationIP, frame[14+ipv6HeaderLength:])
	binary.BigEndian.PutUint16(udp[6:8], csum)
	return frame, nil
}

func buildIAPD(spec PacketSpec) []byte {
	out := buildIAPDNoPrefix(spec)
	prefix := spec.Prefix.Masked()
	sub := make([]byte, 25)
	binary.BigEndian.PutUint32(sub[0:4], spec.PreferredLifetime)
	binary.BigEndian.PutUint32(sub[4:8], spec.ValidLifetime)
	sub[8] = byte(prefix.Bits())
	copy(sub[9:25], prefix.Addr().AsSlice())
	return appendOption(out, optionIAPrefix, sub)
}

func buildIAPDNoPrefix(spec PacketSpec) []byte {
	out := make([]byte, 12)
	binary.BigEndian.PutUint32(out[0:4], spec.IAID)
	binary.BigEndian.PutUint32(out[4:8], spec.T1)
	binary.BigEndian.PutUint32(out[8:12], spec.T2)
	return out
}

func appendOption(out []byte, code uint16, data []byte) []byte {
	out = append(out, uint16Bytes(code)...)
	out = append(out, uint16Bytes(uint16(len(data)))...)
	out = append(out, data...)
	return out
}

func uint16Bytes(value uint16) []byte {
	return []byte{byte(value >> 8), byte(value)}
}

func udpChecksum(src, dst netip.Addr, udp []byte) uint16 {
	var pseudo []byte
	pseudo = append(pseudo, src.AsSlice()...)
	pseudo = append(pseudo, dst.AsSlice()...)
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len(udp)))
	pseudo = append(pseudo, lenBytes...)
	pseudo = append(pseudo, 0, 0, 0, ipProtocolUDP)
	sum := checksumAdd(pseudo, 0)
	sum = checksumAdd(udp, sum)
	result := ^uint16(sum + (sum >> 16))
	if result == 0 {
		return 0xffff
	}
	return result
}

func checksumAdd(data []byte, sum uint32) uint32 {
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return sum
}
