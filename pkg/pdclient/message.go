package pdclient

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

const (
	MessageSolicit   uint8 = 1
	MessageAdvertise uint8 = 2
	MessageRequest   uint8 = 3
	MessageRenew     uint8 = 5
	MessageRebind    uint8 = 6
	MessageReply     uint8 = 7
	MessageRelease   uint8 = 8

	optionClientID    uint16 = 1
	optionServerID    uint16 = 2
	optionElapsedTime uint16 = 8
	optionIAPD        uint16 = 25
	optionIAPrefix    uint16 = 26
)

// Message is the minimal DHCPv6 subset required for IA_PD. It represents a
// DHCPv6 payload, not an Ethernet/IP/UDP frame.
type Message struct {
	Type          uint8
	TransactionID uint32
	ClientDUID    []byte
	ServerDUID    []byte
	IAID          uint32
	T1            uint32
	T2            uint32
	Prefix        netip.Prefix
	Preferred     uint32
	Valid         uint32
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
	out := []byte{msg.Type, byte(msg.TransactionID >> 16), byte(msg.TransactionID >> 8), byte(msg.TransactionID)}
	out = appendOption(out, optionClientID, msg.ClientDUID)
	if len(msg.ServerDUID) > 0 {
		out = appendOption(out, optionServerID, msg.ServerDUID)
	}
	if messageCarriesIAPD(msg.Type) {
		out = appendOption(out, optionIAPD, encodeIAPD(msg))
	}
	out = appendOption(out, optionElapsedTime, []byte{0, 0})
	return out, nil
}

func DecodeMessage(payload []byte) (Message, error) {
	if len(payload) < 4 {
		return Message{}, fmt.Errorf("DHCPv6 payload too short")
	}
	msg := Message{
		Type:          payload[0],
		TransactionID: uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3]),
	}
	err := walkOptions(payload[4:], func(code uint16, data []byte) error {
		switch code {
		case optionClientID:
			msg.ClientDUID = append([]byte(nil), data...)
		case optionServerID:
			msg.ServerDUID = append([]byte(nil), data...)
		case optionIAPD:
			return decodeIAPD(data, &msg)
		}
		return nil
	})
	return msg, err
}

func messageCarriesIAPD(messageType uint8) bool {
	switch messageType {
	case MessageSolicit, MessageRequest, MessageRenew, MessageRebind, MessageRelease, MessageAdvertise, MessageReply:
		return true
	default:
		return false
	}
}

func appendOption(out []byte, code uint16, data []byte) []byte {
	var header [4]byte
	binary.BigEndian.PutUint16(header[0:2], code)
	binary.BigEndian.PutUint16(header[2:4], uint16(len(data)))
	out = append(out, header[:]...)
	out = append(out, data...)
	return out
}

func encodeIAPD(msg Message) []byte {
	var out [12]byte
	binary.BigEndian.PutUint32(out[0:4], msg.IAID)
	binary.BigEndian.PutUint32(out[4:8], msg.T1)
	binary.BigEndian.PutUint32(out[8:12], msg.T2)
	data := append([]byte(nil), out[:]...)
	if msg.Prefix.IsValid() {
		var prefix [25]byte
		binary.BigEndian.PutUint32(prefix[0:4], msg.Preferred)
		binary.BigEndian.PutUint32(prefix[4:8], msg.Valid)
		prefix[8] = byte(msg.Prefix.Bits())
		addr := msg.Prefix.Masked().Addr().As16()
		copy(prefix[9:25], addr[:])
		data = appendOption(data, optionIAPrefix, prefix[:])
	}
	return data
}

func decodeIAPD(data []byte, msg *Message) error {
	if len(data) < 12 {
		return fmt.Errorf("IA_PD option too short")
	}
	msg.IAID = binary.BigEndian.Uint32(data[0:4])
	msg.T1 = binary.BigEndian.Uint32(data[4:8])
	msg.T2 = binary.BigEndian.Uint32(data[8:12])
	return walkOptions(data[12:], func(code uint16, data []byte) error {
		if code != optionIAPrefix {
			return nil
		}
		if len(data) < 25 {
			return fmt.Errorf("IA Prefix option too short")
		}
		var raw [16]byte
		copy(raw[:], data[9:25])
		msg.Prefix = netip.PrefixFrom(netip.AddrFrom16(raw), int(data[8])).Masked()
		msg.Preferred = binary.BigEndian.Uint32(data[0:4])
		msg.Valid = binary.BigEndian.Uint32(data[4:8])
		return nil
	})
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
