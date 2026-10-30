// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	maxDNSQueries           = 128
	dnsResponseWriteTimeout = 2 * time.Second
)

// miekg/dns does not apply Server.WriteTimeout to response writes. The owned
// listener therefore sets a deadline on the actual accepted connection.
type dnsDeadlineListener struct{ net.Listener }

func (l dnsDeadlineListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return dnsWriteConn{Conn: c}, nil
}

type dnsWriteConn struct{ net.Conn }

func (c dnsWriteConn) Write(data []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(dnsResponseWriteTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(data)
}

// Keep the original UDP socket visible to miekg/dns, preserving its native
// destination/interface session metadata rather than replacing PacketConn.
type dnsPacketWriter struct {
	dns.Writer
	packet net.PacketConn
	mu     *sync.Mutex
}

func (w dnsPacketWriter) Write(data []byte) (int, error) {
	// PacketConn deadlines apply to every concurrent write. Admit only one
	// write, otherwise a later reply could extend an already blocked deadline.
	if !w.mu.TryLock() {
		return 0, fmt.Errorf("DNS UDP response writer is busy")
	}
	defer w.mu.Unlock()
	if err := w.packet.SetWriteDeadline(time.Now().Add(dnsResponseWriteTimeout)); err != nil {
		return 0, err
	}
	return w.Writer.Write(data)
}

func stopRuntimeSources(sources []runtimeSource) {
	for _, source := range sources {
		if source.Pool != nil {
			source.Pool.Stop()
		}
	}
}
