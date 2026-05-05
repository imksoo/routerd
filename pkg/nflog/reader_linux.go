package nflog

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	nfNetlinkV0 = 0

	nfnlSubsysULog = 4

	nfulnlMsgPacket = 0
	nfulnlMsgConfig = 1

	nfulaPacketHdr  = 1
	nfulaTimestamp  = 3
	nfulaIfInDev    = 4
	nfulaIfOutDev   = 5
	nfulaPayload    = 9
	nfulaPrefix     = 10
	nfulaCfgCmd     = 1
	nfulaCfgMode    = 2
	nfulnlCfgBind   = 1
	nfulnlCfgPFBind = 3

	nfulnlCopyPacket = 2
)

type Packet struct {
	Prefix      string
	Timestamp   time.Time
	SrcAddress  string
	SrcPort     int
	DstAddress  string
	DstPort     int
	Protocol    string
	L3Proto     string
	InIface     string
	OutIface    string
	PacketBytes int
	Payload     []byte
}

type Reader struct {
	fd    int
	group uint16
	seq   uint32
}

func Open(group int) (*Reader, error) {
	if group <= 0 || group > 65535 {
		return nil, fmt.Errorf("nflog group must be between 1 and 65535")
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_NETFILTER)
	if err != nil {
		return nil, err
	}
	r := &Reader{fd: fd, group: uint16(group)}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1<<20); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		if err := r.configCommand(family, 0, nfulnlCfgPFBind); err != nil && !errors.Is(err, unix.EBUSY) {
			_ = r.Close()
			return nil, err
		}
	}
	if err := r.configCommand(unix.AF_UNSPEC, r.group, nfulnlCfgBind); err != nil {
		_ = r.Close()
		return nil, err
	}
	if err := r.configMode(unix.AF_UNSPEC, r.group, nfulnlCopyPacket, 0xffff); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func (r *Reader) Close() error {
	if r == nil || r.fd < 0 {
		return nil
	}
	err := unix.Close(r.fd)
	r.fd = -1
	return err
}

func (r *Reader) Read(ctx context.Context) (Packet, error) {
	buf := make([]byte, 1<<20)
	for {
		select {
		case <-ctx.Done():
			return Packet{}, ctx.Err()
		default:
		}
		pfd := []unix.PollFd{{Fd: int32(r.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pfd, 1000)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return Packet{}, err
		}
		if n == 0 {
			continue
		}
		size, _, err := unix.Recvfrom(r.fd, buf, 0)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.ENOBUFS) {
				continue
			}
			return Packet{}, err
		}
		packets, err := ParseMessages(buf[:size])
		if err != nil {
			return Packet{}, err
		}
		if len(packets) > 0 {
			return packets[0], nil
		}
	}
}

func (r *Reader) configCommand(family uint8, group uint16, command uint8) error {
	return r.query(family, group, nfulnlMsgConfig, attr(nfulaCfgCmd, []byte{command, 0, 0, 0}))
}

func (r *Reader) configMode(family uint8, group uint16, mode uint8, copyRange uint32) error {
	payload := make([]byte, 6)
	binary.BigEndian.PutUint32(payload[0:4], copyRange)
	payload[4] = mode
	return r.query(family, group, nfulnlMsgConfig, attr(nfulaCfgMode, payload))
}

func (r *Reader) query(family uint8, group uint16, msgType uint8, attrs []byte) error {
	r.seq++
	msg := netlinkMessage(family, group, msgType, r.seq, attrs)
	if err := unix.Sendto(r.fd, msg, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	buf := make([]byte, 8192)
	for time.Now().Before(deadline) {
		pfd := []unix.PollFd{{Fd: int32(r.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pfd, 200)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if n == 0 {
			continue
		}
		size, _, err := unix.Recvfrom(r.fd, buf, 0)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if err := parseAck(buf[:size], r.seq); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("timeout waiting for nfnetlink ack")
}

func netlinkMessage(family uint8, group uint16, msgType uint8, seq uint32, attrs []byte) []byte {
	length := uint32(16 + 4 + len(attrs))
	out := make([]byte, length)
	binary.LittleEndian.PutUint32(out[0:4], length)
	binary.LittleEndian.PutUint16(out[4:6], uint16(nfnlSubsysULog<<8)|uint16(msgType))
	binary.LittleEndian.PutUint16(out[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	binary.LittleEndian.PutUint32(out[8:12], seq)
	out[16] = family
	out[17] = nfNetlinkV0
	binary.BigEndian.PutUint16(out[18:20], group)
	copy(out[20:], attrs)
	return out
}

func attr(kind uint16, payload []byte) []byte {
	length := 4 + len(payload)
	out := make([]byte, align4(length))
	binary.LittleEndian.PutUint16(out[0:2], uint16(length))
	binary.LittleEndian.PutUint16(out[2:4], kind)
	copy(out[4:], payload)
	return out
}

func parseAck(data []byte, seq uint32) error {
	for len(data) >= 16 {
		length := int(binary.LittleEndian.Uint32(data[0:4]))
		if length < 16 || length > len(data) {
			return fmt.Errorf("invalid netlink ack length %d", length)
		}
		msgType := binary.LittleEndian.Uint16(data[4:6])
		msgSeq := binary.LittleEndian.Uint32(data[8:12])
		if msgType == unix.NLMSG_ERROR && msgSeq == seq {
			if length < 20 {
				return fmt.Errorf("short netlink error ack")
			}
			code := int32(binary.LittleEndian.Uint32(data[16:20]))
			if code == 0 {
				return nil
			}
			return unix.Errno(-code)
		}
		data = data[align4(length):]
	}
	return fmt.Errorf("netlink ack not found")
}

func ParseMessages(data []byte) ([]Packet, error) {
	var packets []Packet
	for len(data) >= 16 {
		length := int(binary.LittleEndian.Uint32(data[0:4]))
		if length < 16 || length > len(data) {
			return packets, fmt.Errorf("invalid netlink message length %d", length)
		}
		msgType := binary.LittleEndian.Uint16(data[4:6])
		if msgType == uint16(nfnlSubsysULog<<8)|nfulnlMsgPacket {
			packet, ok, err := parsePacketMessage(data[16:length])
			if err != nil {
				return packets, err
			}
			if ok {
				packets = append(packets, packet)
			}
		}
		data = data[align4(length):]
	}
	return packets, nil
}

func parsePacketMessage(data []byte) (Packet, bool, error) {
	if len(data) < 4 {
		return Packet{}, false, nil
	}
	var packet Packet
	attrs := data[4:]
	for len(attrs) >= 4 {
		length := int(binary.LittleEndian.Uint16(attrs[0:2]))
		if length < 4 || length > len(attrs) {
			return Packet{}, false, fmt.Errorf("invalid nflog attribute length %d", length)
		}
		kind := binary.LittleEndian.Uint16(attrs[2:4]) & 0x3fff
		payload := attrs[4:length]
		switch kind {
		case nfulaPrefix:
			packet.Prefix = strings.TrimRight(string(payload), "\x00")
		case nfulaPayload:
			packet.Payload = append([]byte(nil), payload...)
			fillPacketPayload(&packet, payload)
		case nfulaTimestamp:
			if len(payload) >= 16 {
				sec := int64(binary.BigEndian.Uint64(payload[0:8]))
				usec := int64(binary.BigEndian.Uint64(payload[8:16]))
				packet.Timestamp = time.Unix(sec, usec*1000).UTC()
			}
		case nfulaIfInDev:
			packet.InIface = ifName(payload)
		case nfulaIfOutDev:
			packet.OutIface = ifName(payload)
		case nfulaPacketHdr:
			if len(payload) >= 2 {
				proto := binary.BigEndian.Uint16(payload[0:2])
				if proto == 0x86dd {
					packet.L3Proto = "ipv6"
				} else if proto == 0x0800 {
					packet.L3Proto = "ipv4"
				}
			}
		}
		attrs = attrs[align4(length):]
	}
	if packet.SrcAddress == "" || packet.DstAddress == "" || packet.Protocol == "" {
		return packet, false, nil
	}
	if packet.Timestamp.IsZero() {
		packet.Timestamp = time.Now().UTC()
	}
	return packet, true, nil
}

func fillPacketPayload(packet *Packet, payload []byte) {
	if len(payload) == 0 {
		return
	}
	version := payload[0] >> 4
	switch version {
	case 4:
		fillIPv4(packet, payload)
	case 6:
		fillIPv6(packet, payload)
	}
}

func fillIPv4(packet *Packet, payload []byte) {
	if len(payload) < 20 {
		return
	}
	ihl := int(payload[0]&0x0f) * 4
	if ihl < 20 || len(payload) < ihl {
		return
	}
	total := int(binary.BigEndian.Uint16(payload[2:4]))
	packet.PacketBytes = total
	packet.L3Proto = "ipv4"
	packet.Protocol = ipProtocol(payload[9])
	packet.SrcAddress = net.IP(payload[12:16]).String()
	packet.DstAddress = net.IP(payload[16:20]).String()
	fillPorts(packet, payload[9], payload[ihl:])
}

func fillIPv6(packet *Packet, payload []byte) {
	if len(payload) < 40 {
		return
	}
	packet.PacketBytes = int(binary.BigEndian.Uint16(payload[4:6])) + 40
	packet.L3Proto = "ipv6"
	next := payload[6]
	offset := 40
	for isIPv6ExtensionHeader(next) && len(payload) >= offset+2 {
		headerLen := (int(payload[offset+1]) + 1) * 8
		next = payload[offset]
		offset += headerLen
		if offset > len(payload) {
			return
		}
	}
	packet.Protocol = ipProtocol(next)
	packet.SrcAddress = net.IP(payload[8:24]).String()
	packet.DstAddress = net.IP(payload[24:40]).String()
	fillPorts(packet, next, payload[offset:])
}

func fillPorts(packet *Packet, proto uint8, payload []byte) {
	if len(payload) < 4 {
		return
	}
	switch proto {
	case 6, 17:
		packet.SrcPort = int(binary.BigEndian.Uint16(payload[0:2]))
		packet.DstPort = int(binary.BigEndian.Uint16(payload[2:4]))
	}
}

func ipProtocol(proto uint8) string {
	switch proto {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 58:
		return "icmpv6"
	default:
		return fmt.Sprintf("%d", proto)
	}
}

func isIPv6ExtensionHeader(proto uint8) bool {
	switch proto {
	case 0, 43, 44, 50, 51, 60, 135, 139, 140, 253, 254:
		return true
	default:
		return false
	}
}

func ifName(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	iface, err := net.InterfaceByIndex(int(binary.BigEndian.Uint32(payload[0:4])))
	if err != nil {
		return ""
	}
	return iface.Name
}

func align4(value int) int {
	return (value + 3) &^ 3
}
