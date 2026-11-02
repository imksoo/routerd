// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const (
	etherTypeIPv4 = 0x0800
	etherTypeVLAN = 0x8100
)

func bindSocketToDevice(fd int, ifname string) error {
	return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname)
}

func listenDHCPv4(ifname string) (dhcpv4PacketConn, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	output := ifi
	vlanID := uint16(0)
	if parent, id, tagged, err := vlanParent(ifname); err != nil {
		return nil, err
	} else if tagged {
		output, err = net.InterfaceByName(parent)
		if err != nil {
			return nil, err
		}
		vlanID = id
	}
	rawFD, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(unix.ETH_P_ALL)))
	if err != nil {
		return nil, err
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var sockErr error
		if err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			if sockErr == nil {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
			}
			if sockErr == nil && ifname != "" {
				sockErr = bindSocketToDevice(int(fd), ifname)
			}
		}); err != nil && sockErr == nil {
			sockErr = err
		}
		return sockErr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		_ = unix.Close(rawFD)
		return nil, err
	}
	return &linuxDHCPv4PacketConn{rawFD: rawFD, write: pc.(*net.UDPConn), hwaddr: append(net.HardwareAddr(nil), ifi.HardwareAddr...), ifindex: ifi.Index, outputIfindex: output.Index, vlanID: vlanID}, nil
}

func vlanParent(ifname string) (string, uint16, bool, error) {
	data, err := os.ReadFile("/proc/net/vlan/" + ifname)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	var parent string
	var vlanID uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VID:" {
			vlanID, err = strconv.ParseUint(fields[1], 10, 12)
			if err != nil {
				return "", 0, false, err
			}
		}
		if len(fields) >= 2 && fields[0] == "Device:" {
			parent = fields[1]
		}
	}
	if parent == "" || vlanID == 0 {
		return "", 0, false, fmt.Errorf("read VLAN parent for %s", ifname)
	}
	return parent, uint16(vlanID), true, nil
}

type linuxDHCPv4PacketConn struct {
	rawFD         int
	write         *net.UDPConn
	hwaddr        net.HardwareAddr
	ifindex       int
	outputIfindex int
	vlanID        uint16
	deadline      time.Time
}

func (c *linuxDHCPv4PacketConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	return c.write.ReadFromUDP(buf)
}

func (c *linuxDHCPv4PacketConn) Send(ctx context.Context, packet dhcpv4client.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if packet.Destination == netip.MustParseAddr("255.255.255.255") {
		if err := unix.SetsockoptTimeval(c.rawFD, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &unix.Timeval{Sec: 2}); err != nil {
			return err
		}
		frame, protocol := ethernetBroadcast(c.hwaddr, packet), uint16(etherTypeIPv4)
		if c.vlanID != 0 {
			frame, protocol = vlanEthernetFrame(frame, c.vlanID), etherTypeVLAN
		}
		return unix.Sendto(c.rawFD, frame, 0, &unix.SockaddrLinklayer{Ifindex: c.outputIfindex, Protocol: htons(protocol)})
	}
	deadline := time.Now().Add(2 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := c.write.SetWriteDeadline(deadline); err != nil {
		return err
	}
	control := &ipv4.ControlMessage{IfIndex: c.ifindex}
	if !packet.Source.IsUnspecified() {
		control.Src = net.IP(packet.Source.AsSlice())
	}
	_, err := ipv4.NewPacketConn(c.write).WriteTo(packet.Payload, control, &net.UDPAddr{IP: net.IP(packet.Destination.AsSlice()), Port: 67})
	return err
}

func (c *linuxDHCPv4PacketConn) SetReadDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.write.SetReadDeadline(deadline)
}

func (c *linuxDHCPv4PacketConn) Close() error {
	err := unix.Close(c.rawFD)
	if closeErr := c.write.Close(); err == nil {
		return closeErr
	}
	return err
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }
