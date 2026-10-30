// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestUpstreamAcceptsLargeAdvertisedUDPResponse(t *testing.T) {
	var udpCalls, tcpCalls atomic.Int32
	address := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
		if w.LocalAddr().Network() == "udp" {
			udpCalls.Add(1)
		} else {
			tcpCalls.Add(1)
		}
		response := new(dns.Msg)
		response.SetReply(query)
		response.SetEdns0(16384, false)
		for i := 0; i < 48; i++ {
			response.Answer = append(response.Answer, &dns.TXT{Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60}, Txt: []string{fmt.Sprintf("%03d", i) + strings.Repeat("x", 100)}})
		}
		if err := w.WriteMsg(response); err != nil {
			t.Error(err)
		}
	}))
	pool, err := newUpstreamPool([]string{"udp://" + address}, upstreamPoolConfig{QueryTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	query := new(dns.Msg)
	query.SetQuestion("large.example.", dns.TypeTXT)
	query.SetEdns0(16384, false)
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	answer, err := pool.Exchange(ctx, wire, nil)
	if err != nil {
		t.Fatal(err)
	}
	var response dns.Msg
	if err := response.Unpack(answer); err != nil {
		t.Fatal(err)
	}
	if len(answer) <= 4096 || len(response.Answer) != 48 || response.Truncated || udpCalls.Load() != 1 || tcpCalls.Load() != 0 {
		t.Fatalf("large UDP response lost: bytes=%d records=%d TC=%v udp=%d tcp=%d", len(answer), len(response.Answer), response.Truncated, udpCalls.Load(), tcpCalls.Load())
	}
}

func TestResolverTruncatesUDPToClientLimitAndPreservesTCP(t *testing.T) {
	upstream := failoverDoHServer(t, func(w http.ResponseWriter, r *http.Request, query *dns.Msg) {
		response := new(dns.Msg)
		response.SetReply(query)
		for i := 1; i <= 96; i++ {
			response.Answer = append(response.Answer, &dns.A{Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(192, 0, 2, byte(i))})
		}
		failoverWriteDNS(t, w, response)
	})
	daemon := newTestDaemon(t, "", testForwardConfig("large", upstream.URL+"/dns-query"), true)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport := daemon.sources[0].Pool.upstreams[0].client.Transport.(*http.Transport)
	transport.TLSClientConfig.RootCAs = roots
	t.Cleanup(transport.CloseIdleConnections)
	address := failoverDualDNSServer(t, dns.HandlerFunc(daemon.handleDNS))
	for _, size := range []uint16{0, 128, 1232} {
		query := new(dns.Msg)
		query.SetQuestion("router.lab.example.", dns.TypeA)
		if size != 0 {
			query.SetEdns0(size, false)
		}
		request, err := query.Pack()
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.DialTimeout("udp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		if _, err := conn.Write(request); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		wire := make([]byte, maxDNSMessageSize)
		n, err := conn.Read(wire)
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		wire = wire[:n]
		var response dns.Msg
		if err := response.Unpack(wire); err != nil {
			t.Fatal(err)
		}
		limit := int(size)
		if limit < 512 {
			limit = 512
		}
		if len(wire) > limit || !response.Truncated || len(response.Answer) >= 96 {
			t.Fatalf("clientlimit%d: bytes=%d TC=%v records=%d", size, len(wire), response.Truncated, len(response.Answer))
		}
		if size != 0 && response.IsEdns0() == nil {
			t.Fatal("EDNS response omitted OPT")
		}
	}
	query := new(dns.Msg)
	query.SetQuestion("router.lab.example.", dns.TypeA)
	response, _, err := (&dns.Client{Net: "tcp", Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
	if err != nil {
		t.Fatal(err)
	}
	if response.Truncated || len(response.Answer) != 96 {
		t.Fatalf("TCP response lost records: TC=%v records=%d", response.Truncated, len(response.Answer))
	}
}
