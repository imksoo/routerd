// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"
)

type State string

const (
	StateIdle        State = "Idle"
	StateDiscovering State = "Discovering"
	StateRequesting  State = "Requesting"
	StateBound       State = "Bound"
	StateRenewing    State = "Renewing"
	StateRebinding   State = "Rebinding"
	StateExpired     State = "Expired"
	StateReleased    State = "Released"
)

const (
	MessageDiscover = 1
	MessageOffer    = 2
	MessageRequest  = 3
	MessageDecline  = 4
	MessageACK      = 5
	MessageNAK      = 6
	MessageRelease  = 7

	OptionSubnetMask       = 1
	OptionRouter           = 3
	OptionDNSServer        = 6
	OptionHostname         = 12
	OptionDomainName       = 15
	OptionMTU              = 26
	OptionBroadcastAddress = 28
	OptionNTPServer        = 42
	OptionRequestedIP      = 50
	OptionLeaseTime        = 51
	OptionOverload         = 52
	OptionMessageType      = 53
	OptionServerID         = 54
	OptionParameterRequest = 55
	OptionT1               = 58
	OptionT2               = 59
	OptionClassID          = 60
	OptionClientID         = 61
	OptionTFTPServer       = 66
	OptionBootfile         = 67
	OptionEnd              = 255
)

type Config struct {
	Resource         string
	Interface        string
	HardwareAddr     net.HardwareAddr
	Hostname         string
	RequestedAddress string
	ClassID          string
	ClientID         string
	Now              func() time.Time
}

type Lease struct {
	Address          netip.Addr
	PrefixLength     int
	ServerID         netip.Addr
	DefaultGateway   netip.Addr
	BroadcastAddress netip.Addr
	DNSServers       []netip.Addr
	NTPServers       []netip.Addr
	Domain           string
	TFTPServer       string
	Bootfile         string
	MTU              int
	Infinite         bool
	LeaseTime        time.Duration
	T1               time.Duration
	T2               time.Duration
	AcquiredAt       time.Time
	RenewedAt        time.Time
}

func (l Lease) RenewAt() time.Time {
	if l.Infinite && l.T1 == 0 {
		return time.Time{}
	}
	t1 := l.T1
	if t1 == 0 {
		t1 = l.LeaseTime / 2
	}
	return l.AcquiredAt.Add(t1)
}

func (l Lease) RebindAt() time.Time {
	if l.Infinite && l.T2 == 0 {
		return time.Time{}
	}
	t2 := l.T2
	if t2 == 0 {
		t2 = time.Duration(float64(l.LeaseTime) * 0.875)
	}
	return l.AcquiredAt.Add(t2)
}

func (l Lease) ExpiresAt() time.Time {
	if l.Infinite {
		return time.Time{}
	}
	return l.AcquiredAt.Add(l.LeaseTime)
}

func (l Lease) ValidAt(now time.Time) bool {
	return usableIPv4(l.Address) && !l.AcquiredAt.IsZero() && !now.Before(l.AcquiredAt) && (l.Infinite || l.LeaseTime > 0 && now.Before(l.ExpiresAt()))
}

type Message struct {
	Op      byte
	HType   byte
	Seconds uint16
	XID     uint32
	Flags   uint16
	CIAddr  netip.Addr
	YIAddr  netip.Addr
	SIAddr  netip.Addr
	GIAddr  netip.Addr
	CHAddr  net.HardwareAddr
	Options map[byte][]byte
}

// Encode serializes the BOOTP header and DHCP options. Repeated option fragments
// are concatenated by Decode as required by RFC 3396.
func Encode(msg Message) ([]byte, error) {
	if len(msg.CHAddr) > 16 {
		return nil, fmt.Errorf("DHCP hardware address exceeds chaddr field")
	}
	packet := make([]byte, 240)
	packet[0], packet[1], packet[2] = msg.Op, msg.HType, byte(len(msg.CHAddr))
	binary.BigEndian.PutUint32(packet[4:8], msg.XID)
	binary.BigEndian.PutUint16(packet[8:10], msg.Seconds)
	binary.BigEndian.PutUint16(packet[10:12], msg.Flags)
	for _, field := range []struct {
		offset  int
		address netip.Addr
	}{
		{12, msg.CIAddr}, {16, msg.YIAddr}, {20, msg.SIAddr}, {24, msg.GIAddr},
	} {
		if field.address.IsValid() {
			if !field.address.Is4() {
				return nil, fmt.Errorf("non-IPv4 DHCP address")
			}
			copy(packet[field.offset:field.offset+4], field.address.AsSlice())
		}
	}
	copy(packet[28:44], msg.CHAddr)
	copy(packet[236:240], []byte{99, 130, 83, 99})
	codes := make([]int, 0, len(msg.Options))
	for code := range msg.Options {
		if code != 0 && code != OptionEnd {
			codes = append(codes, int(code))
		}
	}
	sort.Ints(codes)
	for _, code := range codes {
		value := msg.Options[byte(code)]
		for len(value) > 0 {
			n := min(255, len(value))
			packet = append(packet, byte(code), byte(n))
			packet = append(packet, value[:n]...)
			value = value[n:]
		}
	}
	packet = append(packet, OptionEnd)
	// BOOTP's minimum message length also accommodates older DHCP relays.
	if len(packet) < 300 {
		packet = append(packet, make([]byte, 300-len(packet))...)
	}
	return packet, nil
}

func Decode(packet []byte) (Message, error) {
	if len(packet) < 240 {
		return Message{}, fmt.Errorf("short DHCPv4 packet")
	}
	if !bytes.Equal(packet[236:240], []byte{99, 130, 83, 99}) {
		return Message{}, fmt.Errorf("missing DHCP magic cookie")
	}
	if packet[2] > 16 {
		return Message{}, fmt.Errorf("DHCP hardware address exceeds chaddr field")
	}
	msg := Message{
		Op: packet[0], HType: packet[1], XID: binary.BigEndian.Uint32(packet[4:8]),
		Seconds: binary.BigEndian.Uint16(packet[8:10]), Flags: binary.BigEndian.Uint16(packet[10:12]),
		CIAddr: addr4(packet[12:16]), YIAddr: addr4(packet[16:20]),
		SIAddr: addr4(packet[20:24]), GIAddr: addr4(packet[24:28]),
		CHAddr:  append(net.HardwareAddr(nil), packet[28:28+int(packet[2])]...),
		Options: make(map[byte][]byte),
	}
	if err := decodeOptions(packet[240:], msg.Options); err != nil {
		return Message{}, err
	}
	// RFC 2131 section 4.1 specifies options, then file, then sname order.
	if overload := msg.Options[OptionOverload]; len(overload) == 1 {
		if overload[0]&1 != 0 {
			if err := decodeOptions(packet[108:236], msg.Options); err != nil {
				return Message{}, err
			}
		}
		if overload[0]&2 != 0 {
			if err := decodeOptions(packet[44:108], msg.Options); err != nil {
				return Message{}, err
			}
		}
	}
	return msg, nil
}

func decodeOptions(data []byte, options map[byte][]byte) error {
	for len(data) > 0 {
		code := data[0]
		data = data[1:]
		if code == 0 {
			continue
		}
		if code == OptionEnd {
			return nil
		}
		if len(data) < 1 || int(data[0])+1 > len(data) {
			return fmt.Errorf("truncated DHCP option %d", code)
		}
		n := int(data[0])
		data = data[1:]
		options[code] = append(options[code], data[:n]...)
		data = data[n:]
	}
	return nil
}

func (m Message) MessageType() byte {
	if value := m.Options[OptionMessageType]; len(value) == 1 {
		return value[0]
	}
	return 0
}

// LeaseFromACK requires the address, server and lifetime needed to own a lease.
// Optional malformed values are ignored independently. An absent mask during
// renewal can reuse the confirmed mask for the same address.
func LeaseFromACK(msg Message, previous Lease, sentAt time.Time) (Lease, error) {
	seconds := optionUint32(msg.Options[OptionLeaseTime])
	if !usableIPv4(msg.YIAddr) || !usableIPv4(optionAddr(msg.Options[OptionServerID])) || seconds == 0 {
		return Lease{}, fmt.Errorf("DHCP ACK lacks a usable address, server identifier or lease time")
	}
	prefix, maskOK := maskPrefix(msg.Options[OptionSubnetMask])
	if !maskOK && previous.Address == msg.YIAddr && previous.PrefixLength >= 0 && previous.PrefixLength <= 32 && previous.Address.IsValid() {
		prefix, maskOK = previous.PrefixLength, true
	}
	if !maskOK {
		return Lease{}, fmt.Errorf("DHCP ACK has no usable subnet mask")
	}
	lease := Lease{
		Address: msg.YIAddr, PrefixLength: prefix, ServerID: optionAddr(msg.Options[OptionServerID]),
		DefaultGateway: firstAddr(msg.Options[OptionRouter]), BroadcastAddress: optionAddr(msg.Options[OptionBroadcastAddress]),
		DNSServers: optionAddrs(msg.Options[OptionDNSServer]), NTPServers: optionAddrs(msg.Options[OptionNTPServer]),
		Domain: optionString(msg.Options[OptionDomainName]), TFTPServer: optionString(msg.Options[OptionTFTPServer]),
		Bootfile: optionString(msg.Options[OptionBootfile]), LeaseTime: time.Duration(seconds) * time.Second,
		Infinite: seconds == 0xffffffff, T1: time.Duration(optionUint32(msg.Options[OptionT1])) * time.Second,
		T2: time.Duration(optionUint32(msg.Options[OptionT2])) * time.Second, AcquiredAt: sentAt, RenewedAt: sentAt,
	}
	if mtu := optionUint16(msg.Options[OptionMTU]); mtu >= 68 {
		lease.MTU = int(mtu)
	}
	if lease.Infinite {
		if lease.T1 >= lease.T2 || lease.T2 >= lease.LeaseTime {
			lease.T1, lease.T2 = 0, 0
		}
	} else {
		// Invalid optional timers do not invalidate the usable address/lifetime.
		if lease.T2 <= 0 || lease.T2 >= lease.LeaseTime {
			lease.T2 = lease.LeaseTime * 7 / 8
		}
		if lease.T1 <= 0 || lease.T1 >= lease.T2 {
			lease.T1 = lease.LeaseTime / 2
			if lease.T1 >= lease.T2 {
				lease.T1 = lease.T2 / 2
			}
		}
	}
	return lease, nil
}

func maskPrefix(b []byte) (int, bool) {
	if len(b) != 4 {
		return 0, false
	}
	ones, bits := net.IPMask(b).Size()
	return ones, bits == 32
}

func usableIPv4(addr netip.Addr) bool {
	return addr.Is4() && !addr.IsUnspecified() && !addr.IsMulticast() && addr != netip.MustParseAddr("255.255.255.255")
}

func addr4(b []byte) netip.Addr {
	if len(b) < 4 {
		return netip.Addr{}
	}
	return netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]})
}

func firstAddr(b []byte) netip.Addr {
	addrs := optionAddrs(b)
	if len(addrs) == 0 {
		return netip.Addr{}
	}
	return addrs[0]
}

func optionAddr(b []byte) netip.Addr {
	if len(b) != 4 {
		return netip.Addr{}
	}
	return addr4(b)
}

func optionAddrs(b []byte) []netip.Addr {
	if len(b)%4 != 0 {
		return nil
	}
	var out []netip.Addr
	for len(b) >= 4 {
		if addr := addr4(b[:4]); usableIPv4(addr) {
			out = append(out, addr)
		}
		b = b[4:]
	}
	return out
}

func optionUint32(b []byte) uint32 {
	if len(b) != 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func optionUint16(b []byte) uint16 {
	if len(b) != 2 {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func optionString(b []byte) string {
	return strings.TrimRight(string(b), "\x00")
}
