// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
)

func TestCacheAgesAllRecordLifetimesWithoutChangingEDNSFlags(t *testing.T) {
	request := new(dns.Msg)
	request.SetQuestion("example.org.", dns.TypeA)
	response := new(dns.Msg)
	response.SetReply(request)
	response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "example.org.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 90}, A: net.IPv4(192, 0, 2, 1)}}
	response.Ns = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: "example.org.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 30}, Ns: "ns.example.org."}}
	response.Extra = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "ns.example.org.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 45}, A: net.IPv4(192, 0, 2, 2)}}
	response.SetEdns0(1232, true)
	wire, err := response.Pack()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d := &daemon{cache: map[string]cacheEntry{"answer": {Message: wire, Stored: now.Add(-10 * time.Second), Expires: now.Add(20 * time.Second), Generation: 1}}}
	wire, ok := d.cacheGet("answer", 1)
	if !ok {
		t.Fatal("unexpired cache entry was lost")
	}
	var cached dns.Msg
	if err := cached.Unpack(wire); err != nil {
		t.Fatal(err)
	}
	for _, rr := range []dns.RR{cached.Answer[0], cached.Ns[0], cached.Extra[0]} {
		if ttl := rr.Header().Ttl; ttl < 19 || ttl > 20 {
			t.Errorf("cached TTL = %d, want at most remaining 20 seconds", ttl)
		}
	}
	if opt := cached.IsEdns0(); opt == nil || !opt.Do() || opt.UDPSize() != 1232 {
		t.Fatalf("EDNS flags corrupted: %v", opt)
	}
	if response.Answer[0].Header().Ttl != 90 {
		t.Fatal("cache read mutated the original answer")
	}
}

func TestCacheIdentityIncludesQuestionClassFlagsAndEDNS(t *testing.T) {
	base := new(dns.Msg)
	base.SetQuestion("Example.org.", dns.TypeA)
	key := dnsCacheKey("127.0.0.1:53", base)
	variants := []*dns.Msg{base.Copy(), base.Copy(), base.Copy(), base.Copy()}
	variants[0].Question[0].Qclass = dns.ClassCHAOS
	variants[1].CheckingDisabled = true
	variants[2].SetEdns0(1232, true)
	variants[3].Question[0].Qtype = 65001
	for _, variant := range variants {
		if dnsCacheKey("127.0.0.1:53", variant) == key {
			t.Errorf("semantically different query reused cache: %v", variant)
		}
	}
	base.Id++
	base.Question[0].Name = "example.org."
	if dnsCacheKey("127.0.0.1:53", base) != key {
		t.Fatal("transaction ID or name case fragmented identical queries")
	}
}

func TestCacheHitEchoesCurrentQuestionAndTransaction(t *testing.T) {
	config := testResolverConfig(nil)
	config.Spec.Cache.Enabled = true
	d := newTestDaemon(t, "", config, true)
	first := new(dns.Msg)
	first.SetQuestion("ExAmPlE.org.", dns.TypeA)
	response := new(dns.Msg)
	response.SetReply(first)
	response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "example.org.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(192, 0, 2, 1)}}
	wire, err := response.Pack()
	if err != nil {
		t.Fatal(err)
	}
	d.cacheSet(dnsCacheKey("127.0.0.1:53", first), wire, time.Minute, 10, d.generation)
	second := first.Copy()
	second.Id++
	second.Question[0].Name = "eXaMpLe.ORG."
	result, err := d.resolve("127.0.0.1:53", second)
	if err != nil || !result.CacheHit {
		t.Fatalf("cache hit = %+v, %v", result, err)
	}
	if result.Response.Id != second.Id || result.Response.Question[0] != second.Question[0] {
		t.Fatalf("cached answer did not echo current query: %v", result.Response)
	}
}

func TestCacheLifetimeUsesShortestRecordAndNegativeSOA(t *testing.T) {
	msg := new(dns.Msg)
	msg.Answer = []dns.RR{
		&dns.CNAME{Hdr: dns.RR_Header{Name: "alias.example.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: "example.org."},
		&dns.A{Hdr: dns.RR_Header{Name: "example.org.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 5}, A: net.IPv4(192, 0, 2, 1)},
	}
	if ttl := dnsMessageTTL(msg, api.DNSResolverCacheSpec{}); ttl != 5*time.Second {
		t.Fatalf("positive cache lifetime = %v", ttl)
	}
	if ttl := dnsMessageTTL(msg, api.DNSResolverCacheSpec{MinTTL: "60s"}); ttl != 0 {
		t.Fatalf("minimum admission extended authoritative TTL: %v", ttl)
	}
	msg.Answer[1].Header().Ttl = 0
	if ttl := dnsMessageTTL(msg, api.DNSResolverCacheSpec{}); ttl != 0 {
		t.Fatalf("zero TTL cached for %v", ttl)
	}
	msg.Answer = nil
	msg.Rcode = dns.RcodeNameError
	if ttl := dnsMessageTTL(msg, api.DNSResolverCacheSpec{NegativeTTL: "30s"}); ttl != 0 {
		t.Fatalf("negative answer without SOA cached for %v", ttl)
	}
	msg.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: "example.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 20}, Minttl: 7}}
	if ttl := dnsMessageTTL(msg, api.DNSResolverCacheSpec{NegativeTTL: "30s"}); ttl != 7*time.Second {
		t.Fatalf("negative cache lifetime = %v", ttl)
	}
}

func TestConditionalReloadAcknowledgesAppliedConfigAndPreservesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver.json")
	config := testResolverConfig(nil)
	writeRuntimeConfig(t, path, config)
	d := newTestDaemon(t, path, config, true)
	loaded, err := loadConfig(d.opts)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := loaded.Digest()
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/reload", nil).WithContext(context.Background())
		request.Header.Set(resolvercfg.ConfigDigestHeader, digest)
		recorder := httptest.NewRecorder()
		d.routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("reload response = %d: %s", recorder.Code, recorder.Body.String())
		}
		var result resolvercfg.ReloadResult
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.ConfigDigest != digest || result.Reloaded != (attempt == 0) {
			t.Fatalf("attempt %d acknowledgement = %+v", attempt, result)
		}
		if attempt == 0 {
			d.cache["sentinel"] = cacheEntry{Generation: d.generation}
		} else if _, ok := d.cache["sentinel"]; !ok {
			t.Fatal("unchanged conditional reload reset the cache")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/reload", nil)
	request.Header.Set(resolvercfg.ConfigDigestHeader, "different-intent")
	recorder := httptest.NewRecorder()
	d.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("concurrent desired change acknowledged: %d", recorder.Code)
	}
}
