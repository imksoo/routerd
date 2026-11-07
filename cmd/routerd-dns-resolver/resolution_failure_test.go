// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
)

type failureResponseWriter struct{ response *dns.Msg }

func (*failureResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5053}
}
func (*failureResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("192.0.2.123"), Port: 40000}
}
func (w *failureResponseWriter) WriteMsg(msg *dns.Msg) error { w.response = msg.Copy(); return nil }
func (w *failureResponseWriter) Write(b []byte) (int, error) {
	w.response = new(dns.Msg)
	return len(b), w.response.Unpack(b)
}
func (*failureResponseWriter) Close() error        { return nil }
func (*failureResponseWriter) TsigStatus() error   { return nil }
func (*failureResponseWriter) TsigTimersOnly(bool) {}
func (*failureResponseWriter) Hijack()             {}

func startFailureTestUpstream(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	ready := make(chan struct{})
	server := &dns.Server{PacketConn: conn, Handler: handler, NotifyStartedFunc: func() { close(ready) }}
	go func() { _ = server.ActivateAndServe() }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		_ = conn.Close()
		t.Fatal("test DNS server did not start")
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	return "udp://" + conn.LocalAddr().String()
}

func failureTestRequest() *dns.Msg {
	req := new(dns.Msg)
	req.SetQuestion("connectivitycheck.example.", dns.TypeA)
	return req
}

func failureTestAnswer(w dns.ResponseWriter, req *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   net.ParseIP("192.0.2.42"),
	}}
	_ = w.WriteMsg(resp)
}

func TestResolutionFailureIsSERVFAILWithSourceAndEvent(t *testing.T) {
	upstream := startFailureTestUpstream(t, func(dns.ResponseWriter, *dns.Msg) {})
	config := testForwardConfig("primary", upstream)
	config.Spec.Sources[0].QueryTimeout = "30ms"
	config.Spec.Sources[0].Healthcheck.Timeout = "3s"
	d := newTestDaemon(t, "", config, true)
	queryLog, err := logstore.OpenDNSQueryLog(filepath.Join(t.TempDir(), "queries.db"))
	if err != nil {
		t.Fatal(err)
	}
	d.queryLog = queryLog
	t.Cleanup(d.closeQueryLog)
	w := &failureResponseWriter{}
	d.handleDNS(w, failureTestRequest())
	if w.response == nil || w.response.Rcode != dns.RcodeServerFailure {
		t.Fatalf("upstream timeout response = %v, want SERVFAIL", w.response)
	}
	_, rows := awaitQueryHistory(t, d, 1)
	if rows[0].ResponseCode != "SERVFAIL" || rows[0].Upstream != "primary" || rows[0].CacheHit {
		t.Fatalf("query failure lost its source or response code: %+v", rows[0])
	}
	events := d.eventsSince("").Events
	if len(events) != 1 || events[0].Type != "routerd.dns.query.failed" {
		t.Fatalf("query failure event missing: %+v", events)
	}
	if events[0].Attributes["clientAddress"] != "192.0.2.123" || events[0].Attributes["source"] != "primary" || !strings.Contains(events[0].Message, "failed") {
		t.Fatalf("query failure event lacks diagnostic context: %+v", events[0])
	}
}

func TestTwoTierResolverRecoversWithoutCachingUpstreamFailure(t *testing.T) {
	var recovered atomic.Bool
	var requests atomic.Int32
	upstream := startFailureTestUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		requests.Add(1)
		if recovered.Load() {
			failureTestAnswer(w, req)
		}
	})
	coreConfig := testForwardConfig("external", upstream)
	coreConfig.Spec.Listen[0].Port = freeTCPPort(t)
	coreConfig.Spec.Sources[0].QueryTimeout = "30ms"
	coreConfig.Spec.Cache = api.DNSResolverCacheSpec{Enabled: true, MinTTL: "60s", NegativeTTL: "30s"}
	core := newTestDaemon(t, "", coreConfig, false)
	if err := core.startDNS(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.shutdownDNSServers)
	var coreAddr string
	for addr := range core.listeners {
		coreAddr = addr
	}
	lanConfig := testForwardConfig("core", "udp://"+coreAddr)
	lanConfig.Spec.Sources[0].QueryTimeout = "500ms"
	lanConfig.Spec.Cache = coreConfig.Spec.Cache
	lan := newTestDaemon(t, "", lanConfig, true)
	req := failureTestRequest()
	first := &failureResponseWriter{}
	lan.handleDNS(first, req)
	if first.response.Rcode != dns.RcodeServerFailure {
		t.Fatalf("initial LAN response = %v, want SERVFAIL", first.response)
	}
	recovered.Store(true)
	second := &failureResponseWriter{}
	lan.handleDNS(second, req)
	if second.response.Rcode != dns.RcodeSuccess || len(second.response.Answer) != 1 || requests.Load() != 2 {
		t.Fatalf("recovery was hidden by a cached error: response=%v upstream requests=%d", second.response, requests.Load())
	}
}

func TestFailedConditionalForwarderDoesNotLeakToPublicSource(t *testing.T) {
	private := startFailureTestUpstream(t, func(dns.ResponseWriter, *dns.Msg) {})
	var publicRequests atomic.Int32
	public := startFailureTestUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		publicRequests.Add(1)
		failureTestAnswer(w, req)
	})
	config := testForwardConfig("private", private)
	config.Spec.Sources[0].Match = []string{"corp.example"}
	config.Spec.Sources[0].QueryTimeout = "30ms"
	config.Spec.Sources = append(config.Spec.Sources, api.DNSResolverSourceSpec{
		Name: "public", Kind: "forward", Match: []string{"."}, Upstreams: []string{public},
	})
	d := newTestDaemon(t, "", config, true)
	req := new(dns.Msg)
	req.SetQuestion("secret.corp.example.", dns.TypeA)
	w := &failureResponseWriter{}
	d.handleDNS(w, req)
	if w.response.Rcode != dns.RcodeServerFailure || publicRequests.Load() != 0 {
		t.Fatalf("private failure escaped source boundary: response=%v public requests=%d", w.response, publicRequests.Load())
	}
}

func TestForwardedNXDOMAINIsPreservedAndCached(t *testing.T) {
	var requests atomic.Int32
	upstream := startFailureTestUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		requests.Add(1)
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeNameError)
		resp.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: "example.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 30}, Ns: "ns.example.", Mbox: "hostmaster.example.", Minttl: 30}}
		_ = w.WriteMsg(resp)
	})
	config := testForwardConfig("filtered", upstream)
	config.Spec.Cache = api.DNSResolverCacheSpec{Enabled: true, NegativeTTL: "30s"}
	d := newTestDaemon(t, "", config, true)
	for i := 0; i < 2; i++ {
		result, err := d.resolve("127.0.0.1:5053", failureTestRequest())
		if err != nil || result.Response.Rcode != dns.RcodeNameError || result.CacheHit != (i == 1) {
			t.Fatalf("valid NXDOMAIN changed: result=%+v error=%v", result, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("valid NXDOMAIN was not cached: requests=%d", requests.Load())
	}
}

func TestDNSMessageTTLDoesNotCacheErrorResponses(t *testing.T) {
	cache := api.DNSResolverCacheSpec{MinTTL: "60s", MaxTTL: "24h", NegativeTTL: "30s"}
	for _, rcode := range []int{dns.RcodeServerFailure, dns.RcodeFormatError, dns.RcodeRefused, dns.RcodeNotImplemented} {
		msg := new(dns.Msg)
		msg.SetRcode(failureTestRequest(), rcode)
		if ttl := dnsMessageTTL(msg, cache); ttl != 0 {
			t.Errorf("rcode %d cached for %v", rcode, ttl)
		}
	}
}

func TestDNSSECForwardKeepsOneOPTAndPreservesClientOptions(t *testing.T) {
	var valid atomic.Bool
	upstream := startFailureTestUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		count := 0
		for _, rr := range req.Extra {
			if rr.Header().Rrtype == dns.TypeOPT {
				count++
			}
		}
		opt := req.IsEdns0()
		valid.Store(count == 1 && opt != nil && opt.Do() && opt.UDPSize() == 1400 && len(opt.Option) == 1 && opt.Option[0].Option() == 65001)
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.AuthenticatedData = true
		resp.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(192, 0, 2, 42)}}
		_ = w.WriteMsg(resp)
	})
	config := testForwardConfig("validated", upstream)
	config.Spec.Sources[0].DNSSECValidate = true
	d := newTestDaemon(t, "", config, true)
	request := failureTestRequest()
	request.SetEdns0(1400, false)
	request.IsEdns0().Option = []dns.EDNS0{&dns.EDNS0_LOCAL{Code: 65001, Data: []byte("client-option")}}
	if _, err := d.resolve("127.0.0.1:5053", request); err != nil {
		t.Fatal(err)
	}
	if !valid.Load() {
		t.Fatal("DNSSEC forwarding duplicated OPT or lost client EDNS options")
	}
	if request.IsEdns0().Do() {
		t.Fatal("forwarding mutated the caller's EDNS flags")
	}
}
