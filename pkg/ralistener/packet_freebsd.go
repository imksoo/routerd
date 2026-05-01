//go:build freebsd

package ralistener

import (
	"context"
	"net"

	"routerd/pkg/dhcp6recorder"
)

type bpfPacketConn struct {
	source dhcp6recorder.FrameSource
}

func NewPacketConn(ifname string) (PacketConn, error) {
	source, err := dhcp6recorder.NewAFPacketSource(ifname)
	if err != nil {
		return nil, err
	}
	return &bpfPacketConn{source: source}, nil
}

func (c *bpfPacketConn) ReadFrom(buf []byte) (int, net.Addr, error) {
	for {
		frame, err := c.source.ReadFrame(context.Background())
		if err != nil {
			return 0, nil, err
		}
		payload, addr, ok := ICMPv6RAPayloadFromEthernet(frame)
		if !ok {
			continue
		}
		return copy(buf, payload), addr, nil
	}
}

func (c *bpfPacketConn) Close() error {
	return c.source.Close()
}
