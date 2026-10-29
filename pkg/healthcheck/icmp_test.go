// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Carry ICMP wire messages over test-owned loopback UDP sockets so the actual
// exchange can be exercised without raw-socket privileges or network changes.
type icmpLoopbackConn struct {
	*net.UDPConn
	remote *net.UDPAddr
	source net.IP
}

func (c *icmpLoopbackConn) WriteTo(data []byte, _ net.Addr) (int, error) {
	return c.UDPConn.WriteToUDP(data, c.remote)
}

func (c *icmpLoopbackConn) ReadFrom(data []byte) (int, net.Addr, error) {
	n, peer, err := c.UDPConn.ReadFromUDP(data)
	if err != nil {
		return n, nil, err
	}
	ip := peer.IP
	if c.source != nil {
		ip = c.source
	}
	return n, &net.IPAddr{IP: ip}, nil
}

func icmpLoopbackPair(t *testing.T) (*icmpLoopbackConn, *net.UDPConn) {
	t.Helper()
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if err := server.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &icmpLoopbackConn{UDPConn: client, remote: server.LocalAddr().(*net.UDPAddr)}, server
}

func TestICMPEchoRequiresMatchingReply(t *testing.T) {
	for _, mismatch := range []string{"id", "sequence", "payload", "type", "code", "peer"} {
		t.Run(mismatch, func(t *testing.T) {
			conn, server := icmpLoopbackPair(t)
			if mismatch == "peer" {
				conn.source = net.IPv4(192, 0, 2, 99)
			}
			done := make(chan error, 1)
			go func() {
				buf := make([]byte, 1500)
				n, peer, err := server.ReadFromUDP(buf)
				if err != nil {
					done <- err
					return
				}
				msg, err := icmp.ParseMessage(1, buf[:n])
				if err != nil {
					done <- err
					return
				}
				msg.Type = ipv4.ICMPTypeEchoReply
				echo := msg.Body.(*icmp.Echo)
				switch mismatch {
				case "id":
					echo.ID ^= 1
				case "sequence":
					echo.Seq ^= 1
				case "payload":
					echo.Data[0] ^= 1
				case "type":
					msg.Type = ipv4.ICMPTypeEcho
				case "code":
					msg.Code = 1
				}
				wire, err := msg.Marshal(nil)
				if err == nil {
					_, err = server.WriteToUDP(wire, peer)
				}
				done <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			err := exchangeICMPEcho(ctx, conn, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
			var netErr net.Error
			if !errors.Is(err, context.DeadlineExceeded) && (!errors.As(err, &netErr) || !netErr.Timeout()) {
				t.Fatalf("exchange error = %v, want timeout after ignoring unrelated reply", err)
			}
			if serveErr := <-done; serveErr != nil {
				t.Fatal(serveErr)
			}
		})
	}
}

func TestICMPEchoFreshIdentityAndMalformedPacketRecovery(t *testing.T) {
	var previous []byte
	for range 2 {
		conn, server := icmpLoopbackPair(t)
		request := make(chan []byte, 1)
		done := make(chan error, 1)
		go func() {
			buf := make([]byte, 1500)
			n, peer, err := server.ReadFromUDP(buf)
			if err != nil {
				done <- err
				return
			}
			request <- append([]byte(nil), buf[:n]...)
			msg, err := icmp.ParseMessage(1, buf[:n])
			if err != nil {
				done <- err
				return
			}
			if _, err := server.WriteToUDP([]byte{0}, peer); err != nil {
				done <- err
				return
			}
			msg.Type = ipv4.ICMPTypeEchoReply
			wire, err := msg.Marshal(nil)
			if err == nil {
				_, err = server.WriteToUDP(wire, peer)
			}
			done <- err
		}()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := exchangeICMPEcho(ctx, conn, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		wire := <-request
		if previous != nil && bytes.Equal(previous, wire) {
			t.Fatal("two probes reused the same echo identity and payload")
		}
		previous = wire
	}
}

func TestICMPEchoCancellationInterruptsRead(t *testing.T) {
	conn, server := icmpLoopbackPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- exchangeICMPEcho(ctx, conn, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}) }()
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.ReadFromUDP(make([]byte, 1500)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("exchange error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the ICMP read")
	}
}
