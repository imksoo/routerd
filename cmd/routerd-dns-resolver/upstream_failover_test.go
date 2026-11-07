// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// These fixtures exercise DNS exchanges on loopback only. Responses and
// canceled requests are the acceptance evidence; watchdogs only bound a hung
// test, and do not qualify production DNS latency or Internet availability.
func failoverDoHServer(t *testing.T, reply func(http.ResponseWriter, *http.Request, *dns.Msg)) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read DoH query: %v", err)
			return
		}
		var query dns.Msg
		if err := query.Unpack(wire); err != nil {
			t.Errorf("unpack DoH query: %v", err)
			return
		}
		reply(w, r, &query)
	}))
	t.Cleanup(server.Close)
	return server
}

func failoverWriteDNS(t *testing.T, w http.ResponseWriter, response *dns.Msg) {
	t.Helper()
	wire, err := response.Pack()
	if err != nil {
		t.Errorf("pack fixture response: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/dns-message")
	if _, err := w.Write(wire); err != nil {
		t.Errorf("write fixture response: %v", err)
	}
}

func failoverDoHPool(t *testing.T, cfg upstreamPoolConfig, servers ...*httptest.Server) *upstreamPool {
	t.Helper()
	var urls []string
	roots := x509.NewCertPool()
	for _, server := range servers {
		urls = append(urls, server.URL+"/dns-query")
		roots.AddCert(server.Certificate())
	}
	pool, err := newUpstreamPool(urls, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, upstream := range pool.upstreams {
		transport := upstream.client.Transport.(*http.Transport)
		transport.TLSClientConfig.RootCAs = roots
		t.Cleanup(transport.CloseIdleConnections)
	}
	return pool
}

func failoverSuccessfulReply(query *dns.Msg) *dns.Msg {
	response := new(dns.Msg)
	response.SetReply(query)
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   net.ParseIP("192.0.2.53"),
	}}
	return response
}

func failoverDualDNSServer(t *testing.T, handler dns.Handler) string {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tcp.Close() })
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	for _, server := range []*dns.Server{
		{Listener: tcp, Handler: handler},
		{PacketConn: udp, Handler: handler},
	} {
		started := make(chan struct{})
		done := make(chan error, 1)
		server.NotifyStartedFunc = func() { close(started) }
		go func() { done <- server.ActivateAndServe() }()
		select {
		case <-started:
		case err := <-done:
			t.Fatalf("start DNS fixture: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("watchdog: DNS fixture startup was not observed")
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.ShutdownContext(ctx); err != nil {
				t.Errorf("stop DNS fixture: %v", err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("DNS fixture exited with error: %v", err)
				}
			case <-ctx.Done():
				t.Error("watchdog: DNS fixture did not stop")
			}
		})
	}
	return tcp.Addr().String()
}

func TestUpstreamTruncatedUDPRetriesSameServerOverTCP(t *testing.T) {
	var udpCalls, tcpCalls atomic.Int32
	address := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
		response := failoverSuccessfulReply(query)
		if w.LocalAddr().Network() == "udp" {
			udpCalls.Add(1)
			response.Answer = nil
			response.Truncated = true
		} else {
			tcpCalls.Add(1)
		}
		if err := w.WriteMsg(response); err != nil {
			t.Errorf("write DNS fixture response: %v", err)
		}
	}))
	pool, err := newUpstreamPool([]string{"udp://" + address}, upstreamPoolConfig{QueryTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := pool.Exchange(context.Background(), dnsProbeQuery(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var response dns.Msg
	if err := response.Unpack(wire); err != nil || response.Truncated || len(response.Answer) != 1 {
		t.Fatalf("TCP response=%v unpack error=%v", response, err)
	}
	if udpCalls.Load() != 1 || tcpCalls.Load() != 1 {
		t.Fatalf("requests UDP=%d TCP=%d, want one each", udpCalls.Load(), tcpCalls.Load())
	}
}

func TestUpstreamQueryTimeoutFallsBackWithoutWaitingForProbeTimeout(t *testing.T) {
	primaryCanceled := make(chan struct{})
	primary := failoverDoHServer(t, func(_ http.ResponseWriter, r *http.Request, _ *dns.Msg) {
		<-r.Context().Done()
		close(primaryCanceled)
	})
	var backupCalls atomic.Int32
	backup := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
		backupCalls.Add(1)
		failoverWriteDNS(t, w, failoverSuccessfulReply(query))
	})
	pool := failoverDoHPool(t, upstreamPoolConfig{
		QueryTimeout: time.Second,
		ProbeTimeout: 5 * time.Second,
	}, primary, backup)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	wire, err := pool.Exchange(ctx, dnsProbeQuery(), nil)
	if err != nil {
		t.Fatalf("query-timeout fallback did not produce an answer: %v", err)
	}
	var response dns.Msg
	if err := response.Unpack(wire); err != nil || response.Rcode != dns.RcodeSuccess || len(response.Answer) != 1 {
		t.Fatalf("fallback response=%v unpack error=%v", response, err)
	}
	if backupCalls.Load() != 1 {
		t.Fatalf("backup requests=%d, want 1", backupCalls.Load())
	}
	select {
	case <-primaryCanceled:
	case <-ctx.Done():
		t.Fatal("watchdog: primary request cancellation was not observed")
	}
	if got := pool.Snapshot()[0]; got.LastFailure.IsZero() || got.LastError == "" {
		t.Fatalf("primary timeout was not retained: %+v", got)
	}
}

func TestUpstreamQueryTimeoutCanExceedProbeTimeout(t *testing.T) {
	// The response arrives after the health-probe deadline, but within the
	// separate query deadline. A Client.Timeout tied to probes would reject it.
	server := failoverDoHServer(t, func(w http.ResponseWriter, r *http.Request, query *dns.Msg) {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			failoverWriteDNS(t, w, failoverSuccessfulReply(query))
		case <-r.Context().Done():
		}
	})
	pool := failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Second, ProbeTimeout: 20 * time.Millisecond}, server)
	if _, err := pool.Exchange(context.Background(), dnsProbeQuery(), nil); err != nil {
		t.Fatalf("query was limited by health-probe timeout: %v", err)
	}
	pool.probe(context.Background(), pool.snapshotPointers()[0], nil)
	if got := pool.Snapshot()[0]; got.LastError == "" || got.LastFailure.IsZero() {
		t.Fatalf("health probe did not retain its shorter deadline: %+v", got)
	}
}

func TestUpstreamRejectsFailuresAndMismatchedResponsesBeforeFallback(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dns.Msg)
		raw    []byte
	}{
		{name: "SERVFAIL", mutate: func(m *dns.Msg) { m.Rcode = dns.RcodeServerFailure }},
		{name: "FORMERR", mutate: func(m *dns.Msg) { m.Rcode = dns.RcodeFormatError }},
		{name: "REFUSED", mutate: func(m *dns.Msg) { m.Rcode = dns.RcodeRefused }},
		{name: "wrong ID", mutate: func(m *dns.Msg) { m.Id++ }},
		{name: "query instead of response", mutate: func(m *dns.Msg) { m.Response = false }},
		{name: "wrong opcode", mutate: func(m *dns.Msg) { m.Opcode = dns.OpcodeStatus }},
		{name: "missing question", mutate: func(m *dns.Msg) { m.Question = nil }},
		{name: "wrong name", mutate: func(m *dns.Msg) { m.Question[0].Name = "different.example." }},
		{name: "wrong type", mutate: func(m *dns.Msg) { m.Question[0].Qtype = dns.TypeAAAA }},
		{name: "wrong class", mutate: func(m *dns.Msg) { m.Question[0].Qclass = dns.ClassCHAOS }},
		{name: "truncated", mutate: func(m *dns.Msg) { m.Truncated = true }},
		{name: "malformed", raw: []byte{1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
				if tt.raw != nil {
					_, _ = w.Write(tt.raw)
					return
				}
				response := failoverSuccessfulReply(query)
				tt.mutate(response)
				failoverWriteDNS(t, w, response)
			})
			var backupCalls atomic.Int32
			backup := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
				backupCalls.Add(1)
				failoverWriteDNS(t, w, failoverSuccessfulReply(query))
			})
			pool := failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Second}, primary, backup)
			wire, err := pool.Exchange(context.Background(), dnsProbeQuery(), nil)
			if err != nil {
				t.Fatal(err)
			}
			var response dns.Msg
			if err := response.Unpack(wire); err != nil || response.Rcode != dns.RcodeSuccess || backupCalls.Load() != 1 {
				t.Fatalf("fallback response=%v unpack error=%v backup requests=%d", response, err, backupCalls.Load())
			}
			if got := pool.Snapshot()[0]; got.LastError == "" {
				t.Fatalf("invalid response failure was not retained: %+v", got)
			}
		})
	}
}

func TestUpstreamPreservesAuthoritativeAndFilteredAnswers(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dns.Msg)
	}{
		{name: "NXDOMAIN", mutate: func(m *dns.Msg) { m.Rcode = dns.RcodeNameError; m.Answer = nil }},
		{name: "NODATA", mutate: func(m *dns.Msg) { m.Answer = nil }},
		{name: "filtered address", mutate: func(m *dns.Msg) { m.Answer[0].(*dns.A).A = net.IPv4zero }},
		{name: "case insensitive question", mutate: func(m *dns.Msg) { m.Question[0].Name = strings.ToUpper(m.Question[0].Name) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := make(chan []byte, 1)
			primary := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
				response := failoverSuccessfulReply(query)
				tt.mutate(response)
				wire, _ := response.Pack()
				expected <- wire
				failoverWriteDNS(t, w, response)
			})
			var backupCalls atomic.Int32
			backup := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
				backupCalls.Add(1)
				failoverWriteDNS(t, w, failoverSuccessfulReply(query))
			})
			pool := failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Second}, primary, backup)
			response, err := pool.Exchange(context.Background(), dnsProbeQuery(), nil)
			if err != nil {
				t.Fatal(err)
			}
			want := <-expected
			if string(response) != string(want) || backupCalls.Load() != 0 {
				t.Fatalf("answer changed or bypassed: response=%x want=%x backup requests=%d", response, want, backupCalls.Load())
			}
		})
	}
}

func TestUpstreamAllFailuresReturnError(t *testing.T) {
	var requests atomic.Int32
	server := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
		requests.Add(1)
		response := new(dns.Msg)
		response.SetRcode(query, dns.RcodeServerFailure)
		failoverWriteDNS(t, w, response)
	})
	pool := failoverDoHPool(t, upstreamPoolConfig{QueryTimeout: time.Second}, server, server)
	response, err := pool.Exchange(context.Background(), dnsProbeQuery(), nil)
	if err == nil || response != nil || requests.Load() != 2 || !strings.Contains(err.Error(), "SERVFAIL") {
		t.Fatalf("all failures: response=%x err=%v requests=%d", response, err, requests.Load())
	}
}

func TestUpstreamCancellationStopsUDPAndDoesNotTryFallback(t *testing.T) {
	primary, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = primary.Close() })
	var backupCalls atomic.Int32
	backup := failoverDoHServer(t, func(w http.ResponseWriter, _ *http.Request, query *dns.Msg) {
		backupCalls.Add(1)
		failoverWriteDNS(t, w, failoverSuccessfulReply(query))
	})
	pool, err := newUpstreamPool([]string{"udp://" + primary.LocalAddr().String(), backup.URL}, upstreamPoolConfig{QueryTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(backup.Certificate())
	transport := pool.upstreams[1].client.Transport.(*http.Transport)
	transport.TLSClientConfig.RootCAs = roots
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := pool.Exchange(ctx, dnsProbeQuery(), nil)
		done <- err
	}()
	_ = primary.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := primary.ReadFrom(make([]byte, 512)); err != nil {
		t.Fatalf("fixture did not observe the initial query: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog: cancellation did not complete the pending exchange")
	}
	if backupCalls.Load() != 0 {
		t.Fatalf("canceled exchange reached fallback %d times", backupCalls.Load())
	}
	for _, upstream := range pool.Snapshot() {
		if !upstream.LastFailure.IsZero() {
			t.Fatalf("caller cancellation marked an upstream failed: %+v", upstream)
		}
	}
}

func TestBootstrapResolverConcurrentSelection(t *testing.T) {
	first, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	resolver := resolverForBootstrap([]string{first.LocalAddr().String(), second.LocalAddr().String()})
	var firstCalls, secondCalls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := resolver.Dial(context.Background(), "udp", "unused")
			if err != nil {
				t.Errorf("bootstrap Dial: %v", err)
				return
			}
			defer conn.Close()
			switch conn.RemoteAddr().String() {
			case first.LocalAddr().String():
				firstCalls.Add(1)
			case second.LocalAddr().String():
				secondCalls.Add(1)
			default:
				t.Errorf("unexpected bootstrap server %s", conn.RemoteAddr())
			}
		}()
	}
	wg.Wait()
	if firstCalls.Load() != 16 || secondCalls.Load() != 16 {
		t.Fatalf("bootstrap selection counts=%d/%d, want 16/16", firstCalls.Load(), secondCalls.Load())
	}
}
