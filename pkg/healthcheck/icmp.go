// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/imksoo/routerd/pkg/api"
)

func ProbeICMP(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
	target := strings.TrimSpace(spec.Target)
	if target == "" {
		return ProbeResult{Message: "target is required"}
	}
	ip, network, err := resolveICMPTarget(ctx, target, spec.AddressFamily)
	if err != nil {
		return resultFromError(ctx, err)
	}
	// Use the same socket steering as the other probes. In particular, a raw
	// ICMP socket must not silently bypass its configured WAN or route mark.
	dialer, err := dialerForSpec(spec, network, ip.String())
	if err != nil {
		return resultFromError(ctx, err)
	}
	localAddress := "::"
	if ip.To4() != nil {
		localAddress = "0.0.0.0"
	}
	if dialer.LocalAddr != nil {
		localAddress = dialer.LocalAddr.String()
	}
	listenConfig := net.ListenConfig{Control: dialer.Control}
	conn, err := listenConfig.ListenPacket(ctx, network, localAddress)
	if err != nil {
		return resultFromError(ctx, err)
	}
	defer conn.Close()
	peer := &net.IPAddr{IP: ip}
	if ip.IsLinkLocalUnicast() && ip.To4() == nil {
		peer.Zone = spec.SourceInterface
	}
	if err := exchangeICMPEcho(ctx, conn, peer); err != nil {
		return resultFromError(ctx, err)
	}
	return ProbeResult{OK: true, Message: "icmp echo succeeded from " + peer.String()}
}

// exchangeICMPEcho owns one request/reply exchange on its caller-owned socket.
// Fresh identity and payload prevent a concurrent probe or a late reply from a
// previous attempt from proving this target healthy.
func exchangeICMPEcho(ctx context.Context, conn net.PacketConn, peer *net.IPAddr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var identity [20]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return fmt.Errorf("create ICMP echo identity: %w", err)
	}
	echo := &icmp.Echo{
		ID:   int(binary.BigEndian.Uint16(identity[:2])),
		Seq:  int(binary.BigEndian.Uint16(identity[2:4])),
		Data: identity[4:],
	}
	var requestType, replyType icmp.Type = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	protocol := 58
	if peer.IP.To4() != nil {
		requestType, replyType, protocol = ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply, 1
	}
	payload, err := (&icmp.Message{Type: requestType, Code: 0, Body: echo}).Marshal(nil)
	if err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	// Interrupt a blocking read even if cancellation precedes the deadline or
	// the caller supplied a cancel-only context.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := conn.WriteTo(payload, peer); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	buf := make([]byte, 1500)
	for {
		n, source, err := conn.ReadFrom(buf)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		sourceIP, ok := source.(*net.IPAddr)
		if !ok || !sourceIP.IP.Equal(peer.IP) || (peer.Zone != "" && sourceIP.Zone != peer.Zone) {
			continue
		}
		message, err := icmp.ParseMessage(protocol, buf[:n])
		if err != nil || message.Type != replyType || message.Code != 0 {
			continue
		}
		reply, ok := message.Body.(*icmp.Echo)
		if ok && reply.ID == echo.ID && reply.Seq == echo.Seq && bytes.Equal(reply.Data, echo.Data) {
			return nil
		}
	}
}

func resolveICMPTarget(ctx context.Context, target, family string) (net.IP, string, error) {
	if ip := net.ParseIP(target); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4, "ip4:icmp", nil
		}
		return ip.To16(), "ip6:ipv6-icmp", nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, target)
	if err != nil {
		return nil, "", err
	}
	for _, addr := range addrs {
		if family == "ipv6" {
			if addr.IP.To4() == nil {
				return addr.IP, "ip6:ipv6-icmp", nil
			}
			continue
		}
		if addr.IP.To4() != nil {
			return addr.IP.To4(), "ip4:icmp", nil
		}
		if family != "ipv4" {
			return addr.IP, "ip6:ipv6-icmp", nil
		}
	}
	return nil, "", fmt.Errorf("no %s address found for %s", defaultString(family, "IP"), target)
}
