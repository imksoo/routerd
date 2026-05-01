package ralistener

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"routerd/pkg/dhcp6control"
	routerstate "routerd/pkg/state"
)

const (
	etherTypeIPv6             = 0x86dd
	ipProtocolICMPv6          = 58
	icmpv6RouterAdvertisement = 134
	ndpOptionSourceLLA        = 1
	ndpOptionPrefixInfo       = 3
)

type Observation struct {
	SourceLinkLocal string
	HGWMAC          string
	ServerID        string
	MFlag           bool
	OFlag           bool
	Prefix          string
	RouterLifetime  uint16
	ObservedAt      time.Time
}

type PacketConn interface {
	ReadFrom([]byte) (int, net.Addr, error)
	Close() error
}

type Listener struct {
	Conn PacketConn
	Now  func() time.Time
}

func (l Listener) Run(ctx context.Context, handle func(Observation)) error {
	if l.Conn == nil {
		return errors.New("RA listener connection is required")
	}
	defer l.Conn.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	buf := make([]byte, 1500)
	for {
		n, addr, err := l.Conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		obs, err := Parse(buf[:n], addr, l.now())
		if err == nil {
			handle(obs)
		}
	}
}

func (l Listener) now() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

func Parse(packet []byte, addr net.Addr, observedAt time.Time) (Observation, error) {
	if len(packet) < 16 {
		return Observation{}, fmt.Errorf("router advertisement too short")
	}
	if packet[0] != icmpv6RouterAdvertisement {
		return Observation{}, fmt.Errorf("not a router advertisement")
	}
	source, _ := sourceAddr(addr)
	flags := packet[5]
	obs := Observation{
		SourceLinkLocal: source,
		MFlag:           flags&0x80 != 0,
		OFlag:           flags&0x40 != 0,
		RouterLifetime:  uint16(packet[6])<<8 | uint16(packet[7]),
		ObservedAt:      observedAt.UTC(),
	}
	offset := 16
	for offset+2 <= len(packet) {
		typ := packet[offset]
		optionLen := int(packet[offset+1]) * 8
		if optionLen == 0 || offset+optionLen > len(packet) {
			break
		}
		option := packet[offset : offset+optionLen]
		switch typ {
		case ndpOptionSourceLLA:
			if optionLen >= 8 {
				obs.HGWMAC = net.HardwareAddr(option[2:8]).String()
			}
		case ndpOptionPrefixInfo:
			if optionLen >= 32 {
				prefixLen := int(option[2])
				prefixBytes := option[16:32]
				addr, ok := netip.AddrFromSlice(prefixBytes)
				if ok {
					obs.Prefix = netip.PrefixFrom(addr, prefixLen).String()
				}
			}
		}
		offset += optionLen
	}
	if obs.HGWMAC == "" && source != "" {
		if mac, ok := MACFromModifiedEUI64(source); ok {
			obs.HGWMAC = mac.String()
		}
	}
	if obs.HGWMAC != "" {
		mac, err := net.ParseMAC(obs.HGWMAC)
		if err == nil {
			obs.ServerID = strings.ToLower(hex.EncodeToString(dhcp6control.DUIDLL(mac)))
		}
	}
	if obs.SourceLinkLocal == "" && obs.HGWMAC == "" {
		return Observation{}, fmt.Errorf("router advertisement has no usable source identity")
	}
	return obs, nil
}

func ICMPv6RAPayloadFromEthernet(frame []byte) ([]byte, net.Addr, bool) {
	if len(frame) < 14+40+16 {
		return nil, nil, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != etherTypeIPv6 {
		return nil, nil, false
	}
	ip := frame[14 : 14+40]
	if ip[0]>>4 != 6 || ip[6] != ipProtocolICMPv6 {
		return nil, nil, false
	}
	payloadLen := int(binary.BigEndian.Uint16(ip[4:6]))
	payloadStart := 14 + 40
	if payloadLen < 16 || len(frame) < payloadStart+payloadLen {
		return nil, nil, false
	}
	payload := frame[payloadStart : payloadStart+payloadLen]
	if payload[0] != icmpv6RouterAdvertisement {
		return nil, nil, false
	}
	source, ok := netip.AddrFromSlice(ip[8:24])
	if !ok {
		return nil, nil, false
	}
	return append([]byte(nil), payload...), &net.UDPAddr{IP: append(net.IP(nil), source.AsSlice()...)}, true
}

func sourceAddr(addr net.Addr) (string, bool) {
	if addr == nil {
		return "", false
	}
	host := addr.String()
	if udp, ok := addr.(*net.UDPAddr); ok {
		return udp.IP.String(), true
	}
	if ip, err := netip.ParseAddrPort(host); err == nil {
		return ip.Addr().String(), true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String(), true
	}
	if i := strings.LastIndex(host, "%"); i >= 0 {
		host = host[:i]
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String(), true
	}
	return "", false
}

func MACFromModifiedEUI64(linkLocal string) (net.HardwareAddr, bool) {
	addr, err := netip.ParseAddr(linkLocal)
	if err != nil || !addr.Is6() {
		return nil, false
	}
	bytes := addr.As16()
	if bytes[0] != 0xfe || bytes[1] != 0x80 {
		return nil, false
	}
	iid := bytes[8:16]
	if iid[3] != 0xff || iid[4] != 0xfe {
		return nil, false
	}
	mac := net.HardwareAddr{iid[0] ^ 0x02, iid[1], iid[2], iid[5], iid[6], iid[7]}
	return mac, true
}

func ApplyObservation(store routerstate.Store, resourceName string, obs Observation, reason string) error {
	if store == nil || resourceName == "" {
		return nil
	}
	base := "ipv6PrefixDelegation." + resourceName
	lease, _ := routerstate.PDLeaseFromStore(store, base)
	if lease.WANObserved == nil {
		lease.WANObserved = &routerstate.PDWANObserved{}
	}
	lease.WANObserved.HGWLinkLocal = obs.SourceLinkLocal
	lease.WANObserved.HGWMACDerived = obs.HGWMAC
	lease.WANObserved.RAMFlag = boolString(obs.MFlag)
	lease.WANObserved.RAOFlag = boolString(obs.OFlag)
	lease.WANObserved.RAPrefix = obs.Prefix
	lease.WANObserved.RAObservedAt = obs.ObservedAt.Format(time.RFC3339)
	if lease.ServerID == "" && obs.ServerID != "" {
		lease.ServerID = obs.ServerID
	}
	store.Set(base+".lease", routerstate.EncodePDLease(lease), reason)
	if recorder, ok := store.(routerstate.EventRecorder); ok {
		_ = recorder.RecordEvent("net.routerd.net/v1alpha1", "IPv6PrefixDelegation", resourceName, "Normal", reason, fmt.Sprintf("observed RA from %s prefix=%s", obs.SourceLinkLocal, obs.Prefix))
	}
	return nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
