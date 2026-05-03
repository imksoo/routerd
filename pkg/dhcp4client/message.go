package dhcp4client

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
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
	ServerID         netip.Addr
	DefaultGateway   netip.Addr
	BroadcastAddress netip.Addr
	DNSServers       []netip.Addr
	NTPServers       []netip.Addr
	Domain           string
	TFTPServer       string
	Bootfile         string
	MTU              int
	LeaseTime        time.Duration
	T1               time.Duration
	T2               time.Duration
	AcquiredAt       time.Time
	RenewedAt        time.Time
}

func (l Lease) RenewAt() time.Time {
	t1 := l.T1
	if t1 == 0 {
		t1 = l.LeaseTime / 2
	}
	return l.AcquiredAt.Add(t1)
}

func (l Lease) RebindAt() time.Time {
	t2 := l.T2
	if t2 == 0 {
		t2 = time.Duration(float64(l.LeaseTime) * 0.875)
	}
	return l.AcquiredAt.Add(t2)
}

func (l Lease) ExpiresAt() time.Time {
	return l.AcquiredAt.Add(l.LeaseTime)
}

type Message struct {
	Op      byte
	XID     uint32
	Flags   uint16
	CIAddr  netip.Addr
	YIAddr  netip.Addr
	SIAddr  netip.Addr
	GIAddr  netip.Addr
	CHAddr  net.HardwareAddr
	Options map[byte][]byte
}

func NewXID() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint32(b[:])
}

func EncodeRequest(msgType byte, xid uint32, hw net.HardwareAddr, opts map[byte][]byte) []byte {
	packet := make([]byte, 240)
	packet[0] = 1
	packet[1] = 1
	packet[2] = byte(len(hw))
	binary.BigEndian.PutUint32(packet[4:8], xid)
	binary.BigEndian.PutUint16(packet[10:12], 0x8000)
	copy(packet[28:44], hw)
	copy(packet[236:240], []byte{99, 130, 83, 99})
	options := []byte{OptionMessageType, 1, msgType}
	options = append(options, OptionParameterRequest, 11,
		OptionLeaseTime, OptionRouter, OptionDNSServer, OptionHostname, OptionDomainName,
		OptionMTU, OptionBroadcastAddress, OptionNTPServer, OptionTFTPServer, OptionBootfile, OptionServerID)
	for code, value := range opts {
		if len(value) == 0 || code == OptionEnd || code == OptionMessageType {
			continue
		}
		options = append(options, code, byte(len(value)))
		options = append(options, value...)
	}
	options = append(options, OptionEnd)
	return append(packet, options...)
}

func Decode(packet []byte) (Message, error) {
	if len(packet) < 240 {
		return Message{}, fmt.Errorf("short DHCPv4 packet")
	}
	if string(packet[236:240]) != string([]byte{99, 130, 83, 99}) {
		return Message{}, fmt.Errorf("missing DHCP magic cookie")
	}
	msg := Message{
		Op:      packet[0],
		XID:     binary.BigEndian.Uint32(packet[4:8]),
		Flags:   binary.BigEndian.Uint16(packet[10:12]),
		CIAddr:  addr4(packet[12:16]),
		YIAddr:  addr4(packet[16:20]),
		SIAddr:  addr4(packet[20:24]),
		GIAddr:  addr4(packet[24:28]),
		CHAddr:  append(net.HardwareAddr(nil), packet[28:28+int(packet[2])]...),
		Options: map[byte][]byte{},
	}
	for i := 240; i < len(packet); {
		code := packet[i]
		i++
		if code == 0 {
			continue
		}
		if code == OptionEnd {
			break
		}
		if i >= len(packet) {
			break
		}
		n := int(packet[i])
		i++
		if i+n > len(packet) {
			break
		}
		msg.Options[code] = append([]byte(nil), packet[i:i+n]...)
		i += n
	}
	return msg, nil
}

func (m Message) MessageType() byte {
	if value := m.Options[OptionMessageType]; len(value) == 1 {
		return value[0]
	}
	return 0
}

func LeaseFromACK(msg Message, now time.Time) Lease {
	leaseSeconds := optionUint32(msg.Options[OptionLeaseTime])
	if leaseSeconds == 0 {
		leaseSeconds = 3600
	}
	lease := Lease{
		Address:          msg.YIAddr,
		ServerID:         optionAddr(msg.Options[OptionServerID]),
		DefaultGateway:   firstAddr(msg.Options[OptionRouter]),
		BroadcastAddress: optionAddr(msg.Options[OptionBroadcastAddress]),
		DNSServers:       optionAddrs(msg.Options[OptionDNSServer]),
		NTPServers:       optionAddrs(msg.Options[OptionNTPServer]),
		Domain:           optionString(msg.Options[OptionDomainName]),
		TFTPServer:       optionString(msg.Options[OptionTFTPServer]),
		Bootfile:         optionString(msg.Options[OptionBootfile]),
		LeaseTime:        time.Duration(leaseSeconds) * time.Second,
		T1:               time.Duration(optionUint32(msg.Options[OptionT1])) * time.Second,
		T2:               time.Duration(optionUint32(msg.Options[OptionT2])) * time.Second,
		AcquiredAt:       now,
		RenewedAt:        now,
	}
	if mtu := optionUint16(msg.Options[OptionMTU]); mtu != 0 {
		lease.MTU = int(mtu)
	}
	if lease.T1 == 0 {
		lease.T1 = lease.LeaseTime / 2
	}
	if lease.T2 == 0 {
		lease.T2 = time.Duration(float64(lease.LeaseTime) * 0.875)
	}
	return lease
}

func RequestOptions(cfg Config, offered Message) map[byte][]byte {
	opts := map[byte][]byte{}
	if offered.YIAddr.IsValid() {
		opts[OptionRequestedIP] = offered.YIAddr.AsSlice()
	}
	if server := offered.Options[OptionServerID]; len(server) == 4 {
		opts[OptionServerID] = server
	}
	if cfg.Hostname != "" {
		opts[OptionHostname] = []byte(cfg.Hostname)
	}
	if cfg.RequestedAddress != "" {
		if addr, err := netip.ParseAddr(cfg.RequestedAddress); err == nil && addr.Is4() {
			opts[OptionRequestedIP] = addr.AsSlice()
		}
	}
	if cfg.ClassID != "" {
		opts[OptionClassID] = []byte(cfg.ClassID)
	}
	if cfg.ClientID != "" {
		opts[OptionClientID] = []byte(cfg.ClientID)
	} else {
		opts[OptionClientID] = append([]byte{1}, cfg.HardwareAddr...)
	}
	return opts
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
	var out []netip.Addr
	for len(b) >= 4 {
		out = append(out, addr4(b[:4]))
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
