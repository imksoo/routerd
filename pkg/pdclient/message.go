// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
)

const (
	MessageSolicit     uint8 = 1
	MessageAdvertise   uint8 = 2
	MessageRequest     uint8 = 3
	MessageRenew       uint8 = 5
	MessageRebind      uint8 = 6
	MessageReply       uint8 = 7
	MessageRelease     uint8 = 8
	MessageInfoRequest uint8 = 11

	optionClientID        uint16 = 1
	optionServerID        uint16 = 2
	optionIANA            uint16 = 3
	optionIAAddress       uint16 = 5
	optionORO             uint16 = 6
	optionElapsedTime     uint16 = 8
	optionStatusCode      uint16 = 13
	optionDNSServers      uint16 = 23
	optionDomainSearch    uint16 = 24
	optionIAPD            uint16 = 25
	optionIAPrefix        uint16 = 26
	optionSNTPServers     uint16 = 31
	optionInfoRefreshTime uint16 = 32
	optionSOLMaxRT        uint16 = 82
	optionINFMaxRT        uint16 = 83
	optionAFTRName        uint16 = 64
)

const (
	StatusSuccess       uint16 = 0
	StatusUnspecFail    uint16 = 1
	StatusNoAddrsAvail  uint16 = 2
	StatusNoBinding     uint16 = 3
	StatusNotOnLink     uint16 = 4
	StatusNoPrefixAvail uint16 = 6
)

// IAPrefix is one independent prefix option within an IA_PD.
type IAPrefix struct {
	Prefix     netip.Prefix
	Preferred  uint32
	Valid      uint32
	StatusCode uint16
}

// IAPD carries the association timers, operation status, and all its prefixes.
type IAPD struct {
	IAID       uint32
	T1         uint32
	T2         uint32
	Prefixes   []IAPrefix
	StatusCode uint16
}

// IAAddress retains an individual address's preferred and valid lifetimes.
// An address is not a delegated prefix and does not imply an on-link subnet.
type IAAddress struct {
	Address    netip.Addr
	Preferred  uint32
	Valid      uint32
	StatusCode uint16
}

// IANA carries the non-temporary address association, independently of IA_PD
// even when both associations use the same IAID on the same interface.
type IANA struct {
	IAID       uint32
	T1         uint32
	T2         uint32
	Addresses  []IAAddress
	StatusCode uint16
}

// Message represents a DHCPv6 payload, not an Ethernet/IP/UDP frame.
type Message struct {
	Type            uint8
	TransactionID   uint32
	ClientDUID      []byte
	ServerDUID      []byte
	IAPDs           []IAPD
	IANAs           []IANA
	ORO             []uint16
	DNSServers      []netip.Addr
	SNTPServers     []netip.Addr
	DomainSearch    []string
	AFTRName        string
	ElapsedTime     uint16
	StatusCode      uint16
	InfoRefreshTime *uint32
	SOLMaxRT        uint32
	INFMaxRT        uint32
}

func EncodeMessage(msg Message) ([]byte, error) {
	if msg.Type == 0 {
		return nil, fmt.Errorf("message type is required")
	}
	if msg.TransactionID > 0xffffff {
		return nil, fmt.Errorf("transaction ID must fit in 24 bits")
	}
	if len(msg.ClientDUID) == 0 {
		return nil, fmt.Errorf("client DUID is required")
	}
	if len(msg.ClientDUID) > 65535 || len(msg.ServerDUID) > 65535 || len(msg.ORO) > 32767 {
		return nil, fmt.Errorf("DHCPv6 option exceeds wire length")
	}
	out := []byte{msg.Type, byte(msg.TransactionID >> 16), byte(msg.TransactionID >> 8), byte(msg.TransactionID)}
	out = appendOption(out, optionClientID, msg.ClientDUID)
	if len(msg.ServerDUID) > 0 {
		out = appendOption(out, optionServerID, msg.ServerDUID)
	}
	if len(msg.ORO) > 0 {
		out = appendOption(out, optionORO, encodeORO(msg.ORO))
	}
	for _, ia := range msg.IAPDs {
		data, err := encodeIAPD(ia)
		if err != nil {
			return nil, err
		}
		out = appendOption(out, optionIAPD, data)
	}
	for _, ia := range msg.IANAs {
		data, err := encodeIANA(ia)
		if err != nil {
			return nil, err
		}
		out = appendOption(out, optionIANA, data)
	}
	out = appendStatus(out, msg.StatusCode)
	for _, field := range []struct {
		code  uint16
		value uint32
	}{
		{optionSOLMaxRT, msg.SOLMaxRT}, {optionINFMaxRT, msg.INFMaxRT},
	} {
		if field.value != 0 {
			out = appendOption(out, field.code, encodeUint32(field.value))
		}
	}
	if msg.InfoRefreshTime != nil {
		out = appendOption(out, optionInfoRefreshTime, encodeUint32(*msg.InfoRefreshTime))
	}
	for _, field := range []struct {
		code  uint16
		addrs []netip.Addr
	}{
		{optionDNSServers, msg.DNSServers}, {optionSNTPServers, msg.SNTPServers},
	} {
		var data []byte
		for _, addr := range field.addrs {
			if !addr.Is6() || addr.Is4In6() {
				return nil, fmt.Errorf("DHCPv6 requires IPv6 addresses")
			}
			raw := addr.As16()
			data = append(data, raw[:]...)
		}
		if len(data) > 65535 {
			return nil, fmt.Errorf("DHCPv6 address option exceeds wire length")
		}
		if len(data) > 0 {
			out = appendOption(out, field.code, data)
		}
	}
	if len(msg.DomainSearch) > 0 {
		var data []byte
		for _, domain := range msg.DomainSearch {
			encoded, err := encodeDomainName(domain)
			if err != nil {
				return nil, err
			}
			data = append(data, encoded...)
		}
		if len(data) > 65535 {
			return nil, fmt.Errorf("DHCPv6 domain option exceeds wire length")
		}
		out = appendOption(out, optionDomainSearch, data)
	}
	if msg.AFTRName != "" {
		data, err := encodeDomainName(msg.AFTRName)
		if err != nil {
			return nil, err
		}
		out = appendOption(out, optionAFTRName, data)
	}
	out = appendOption(out, optionElapsedTime, []byte{byte(msg.ElapsedTime >> 8), byte(msg.ElapsedTime)})
	if len(out) > 65527 {
		return nil, fmt.Errorf("DHCPv6 payload exceeds UDP length")
	}
	return out, nil
}

func DecodeMessage(payload []byte) (Message, error) { return decodeMessage(payload, nil) }

// DecodeMessageForIAID retains the complete configured association, including
// zero-prefix status replies, while ignoring unrelated associations.
func DecodeMessageForIAID(payload []byte, iaid uint32) (Message, error) {
	return decodeMessage(payload, &iaid)
}

func decodeMessage(payload []byte, wantedIAID *uint32) (Message, error) {
	if len(payload) < 4 || len(payload) > 65527 {
		return Message{}, fmt.Errorf("invalid DHCPv6 payload length")
	}
	msg := Message{Type: payload[0], TransactionID: uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])}
	err := walkOptions(payload[4:], func(code uint16, data []byte) error {
		switch code {
		case optionClientID:
			if msg.ClientDUID != nil {
				return fmt.Errorf("duplicate client identifier")
			}
			msg.ClientDUID = append([]byte{}, data...)
		case optionServerID:
			if msg.ServerDUID != nil {
				return fmt.Errorf("duplicate server identifier")
			}
			msg.ServerDUID = append([]byte{}, data...)
		case optionDNSServers:
			msg.DNSServers = decodeIPv6Addrs(data)
		case optionSNTPServers:
			msg.SNTPServers = decodeIPv6Addrs(data)
		case optionDomainSearch:
			msg.DomainSearch = decodeDomainList(data)
		case optionAFTRName:
			msg.AFTRName = decodeDomainName(data)
		case optionORO:
			if len(data)%2 == 0 {
				for i := 0; i < len(data); i += 2 {
					msg.ORO = append(msg.ORO, binary.BigEndian.Uint16(data[i:i+2]))
				}
			}
		case optionStatusCode:
			if len(data) >= 2 {
				msg.StatusCode = binary.BigEndian.Uint16(data[:2])
			}
		case optionElapsedTime:
			if len(data) == 2 {
				msg.ElapsedTime = binary.BigEndian.Uint16(data)
			}
		case optionInfoRefreshTime:
			if len(data) == 4 {
				value := binary.BigEndian.Uint32(data)
				msg.InfoRefreshTime = &value
			}
		case optionSOLMaxRT:
			if len(data) == 4 {
				msg.SOLMaxRT = binary.BigEndian.Uint32(data)
			}
		case optionINFMaxRT:
			if len(data) == 4 {
				msg.INFMaxRT = binary.BigEndian.Uint32(data)
			}
		case optionIAPD:
			ia, err := decodeIAPD(data)
			if err != nil || (wantedIAID != nil && ia.IAID != *wantedIAID) {
				return nil
			}
			// An IAID names one association. Duplicate options cannot establish
			// a second timer/status owner for that association.
			for _, existing := range msg.IAPDs {
				if existing.IAID == ia.IAID {
					return nil
				}
			}
			msg.IAPDs = append(msg.IAPDs, ia)
		case optionIANA:
			ia, err := decodeIANA(data)
			if err != nil || (wantedIAID != nil && ia.IAID != *wantedIAID) {
				return nil
			}
			for _, existing := range msg.IANAs {
				if existing.IAID == ia.IAID {
					return nil
				}
			}
			msg.IANAs = append(msg.IANAs, ia)
		}
		return nil
	})
	return msg, err
}

func encodeUint32(value uint32) []byte {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	return raw[:]
}

func appendStatus(out []byte, status uint16) []byte {
	if status == 0 {
		return out
	}
	return appendOption(out, optionStatusCode, []byte{byte(status >> 8), byte(status)})
}

func encodeDomainName(name string) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	var out []byte
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("invalid DHCPv6 domain name")
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	if len(out) > 254 {
		return nil, fmt.Errorf("DHCPv6 domain name too long")
	}
	return append(out, 0), nil
}

func encodeORO(codes []uint16) []byte {
	out := make([]byte, 0, len(codes)*2)
	var raw [2]byte
	for _, code := range codes {
		binary.BigEndian.PutUint16(raw[0:2], code)
		out = append(out, raw[:]...)
	}
	return out
}

func appendOption(out []byte, code uint16, data []byte) []byte {
	var header [4]byte
	binary.BigEndian.PutUint16(header[0:2], code)
	binary.BigEndian.PutUint16(header[2:4], uint16(len(data)))
	out = append(out, header[:]...)
	out = append(out, data...)
	return out
}

func decodeIPv6Addrs(data []byte) []netip.Addr {
	if len(data)%16 != 0 {
		return nil
	}
	out := make([]netip.Addr, 0, len(data)/16)
	for i := 0; i < len(data); i += 16 {
		var raw [16]byte
		copy(raw[:], data[i:i+16])
		out = append(out, netip.AddrFrom16(raw))
	}
	return out
}

func decodeDomainList(data []byte) []string {
	var out []string
	for len(data) > 0 {
		name, n := decodeDomainNameAt(data)
		if n <= 0 {
			return out
		}
		if name != "" {
			out = append(out, name)
		}
		data = data[n:]
	}
	return out
}

func decodeDomainName(data []byte) string {
	name, _ := decodeDomainNameAt(data)
	return name
}

func decodeDomainNameAt(data []byte) (string, int) {
	var labels []string
	i := 0
	for i < len(data) {
		l := int(data[i])
		i++
		if l == 0 {
			return strings.Join(labels, "."), i
		}
		if l&0xc0 != 0 || i+l > len(data) {
			return "", -1
		}
		labels = append(labels, string(data[i:i+l]))
		i += l
	}
	return "", -1
}

func encodeIAPD(ia IAPD) ([]byte, error) {
	var header [12]byte
	binary.BigEndian.PutUint32(header[0:4], ia.IAID)
	binary.BigEndian.PutUint32(header[4:8], ia.T1)
	binary.BigEndian.PutUint32(header[8:12], ia.T2)
	data := append([]byte(nil), header[:]...)
	data = appendStatus(data, ia.StatusCode)
	for _, entry := range ia.Prefixes {
		if !entry.Prefix.IsValid() || !entry.Prefix.Addr().Is6() || entry.Prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("invalid delegated IPv6 prefix")
		}
		var prefix [25]byte
		binary.BigEndian.PutUint32(prefix[0:4], entry.Preferred)
		binary.BigEndian.PutUint32(prefix[4:8], entry.Valid)
		prefix[8] = byte(entry.Prefix.Bits())
		addr := entry.Prefix.Masked().Addr().As16()
		copy(prefix[9:], addr[:])
		data = appendOption(data, optionIAPrefix, appendStatus(prefix[:], entry.StatusCode))
	}
	if len(data) > 65535 {
		return nil, fmt.Errorf("IA_PD exceeds wire length")
	}
	return data, nil
}

func decodeIAPD(data []byte) (IAPD, error) {
	if len(data) < 12 {
		return IAPD{}, fmt.Errorf("IA_PD option too short")
	}
	ia := IAPD{IAID: binary.BigEndian.Uint32(data[:4]), T1: binary.BigEndian.Uint32(data[4:8]), T2: binary.BigEndian.Uint32(data[8:12])}
	// RFC 9915 21.21: discard this IA, preserving independent options.
	if ia.T1 > 0 && ia.T2 > 0 && ia.T1 > ia.T2 {
		return IAPD{}, fmt.Errorf("IA_PD T1 exceeds T2")
	}
	err := walkOptions(data[12:], func(code uint16, data []byte) error {
		if code == optionStatusCode && len(data) >= 2 {
			ia.StatusCode = binary.BigEndian.Uint16(data[:2])
			return nil
		}
		if code != optionIAPrefix || len(data) < 25 || data[8] > 128 {
			return nil
		}
		entry := IAPrefix{Preferred: binary.BigEndian.Uint32(data[:4]), Valid: binary.BigEndian.Uint32(data[4:8])}
		if entry.Preferred > entry.Valid {
			return nil
		}
		var raw [16]byte
		copy(raw[:], data[9:25])
		entry.Prefix = netip.PrefixFrom(netip.AddrFrom16(raw), int(data[8])).Masked()
		if err := walkOptions(data[25:], func(code uint16, value []byte) error {
			if code == optionStatusCode && len(value) >= 2 {
				entry.StatusCode = binary.BigEndian.Uint16(value[:2])
			}
			return nil
		}); err != nil {
			return nil
		}
		for _, existing := range ia.Prefixes {
			if existing.Prefix == entry.Prefix {
				return nil
			}
		}
		ia.Prefixes = append(ia.Prefixes, entry)
		return nil
	})
	return ia, err
}

func walkOptions(data []byte, visit func(code uint16, data []byte) error) error {
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
