// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/strongswan/govici/vici"
)

func OpenSession(ctx context.Context, path string) (*vici.Session, error) {
	return vici.NewSession(vici.WithSocketPath(path), vici.WithDialContext(func(_ context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &boundedVICIConnection{Conn: conn}, nil
	}))
}

// govici allocates a payload from its peer's length header. Enforce VICI's
// documented 512 KiB packet ceiling before exposing that header to its decoder.
type boundedVICIConnection struct {
	net.Conn
	header    [4]byte
	offset    int
	remaining uint32
}

func (c *boundedVICIConnection) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if c.remaining == 0 && c.offset == 0 {
		if _, err := io.ReadFull(c.Conn, c.header[:]); err != nil {
			return 0, err
		}
		size := binary.BigEndian.Uint32(c.header[:])
		if size == 0 || size > 512<<10 {
			return 0, fmt.Errorf("invalid VICI packet length %d", size)
		}
		c.remaining = size
	}
	if c.offset < len(c.header) {
		n := copy(data, c.header[c.offset:])
		c.offset += n
		return n, nil
	}
	if uint32(len(data)) > c.remaining {
		data = data[:int(c.remaining)]
	}
	n, err := c.Conn.Read(data)
	c.remaining -= uint32(n)
	if c.remaining == 0 {
		c.offset = 0
	}
	return n, err
}
