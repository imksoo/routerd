// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/miekg/dns"
)

func TestNonrecursiveQueriesNeverReachUpstreamOnUDPOrTCP(t *testing.T) {
	var calls atomic.Int32
	upstream := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		calls.Add(1)
		_ = w.WriteMsg(failoverSuccessfulReply(q))
	}))
	config := testResolverConfig(nil)
	config.Spec.Sources = append(config.Spec.Sources, api.DNSResolverSourceSpec{Name: "public", Kind: "forward", Match: []string{"."}, Upstreams: []string{"udp://" + upstream}})
	d := newTestDaemon(t, "", config, true)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	for _, protocol := range []string{"udp", "tcp"} {
		query := new(dns.Msg)
		query.SetQuestion("orphan.private.example.", dns.TypeA)
		query.RecursionDesired = false
		answer, _, err := (&dns.Client{Net: protocol, Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
		if err != nil {
			t.Fatal(err)
		}
		if answer.Rcode != dns.RcodeRefused || answer.RecursionDesired {
			t.Errorf("%s nonrecursive private miss was not refused: %v", protocol, answer)
		}
		query.SetQuestion("router.lab.example.", dns.TypeA)
		query.RecursionDesired = false
		answer, _, err = (&dns.Client{Net: protocol, Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
		if err != nil {
			t.Fatal(err)
		}
		if answer.Rcode != dns.RcodeSuccess || len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.1" || answer.RecursionDesired {
			t.Fatalf("%s local authoritative answer lost: %v", protocol, answer)
		}
	}
	if count := calls.Load(); count != 0 {
		t.Fatalf("nonrecursive private question reached upstream %d times", count)
	}
}

func TestNonrecursiveRequestDoesNotSkipEarlierForwardSource(t *testing.T) {
	var calls atomic.Int32
	upstream := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) { calls.Add(1); _ = w.WriteMsg(failoverSuccessfulReply(q)) }))
	config := testResolverConfig(nil)
	config.Spec.Sources = append([]api.DNSResolverSourceSpec{{Name: "earlier-public", Kind: "forward", Match: []string{"."}, Upstreams: []string{"udp://" + upstream}}}, config.Spec.Sources...)
	d := newTestDaemon(t, "", config, true)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	for _, protocol := range []string{"udp", "tcp"} {
		query := new(dns.Msg)
		query.SetQuestion("router.lab.example.", dns.TypeA)
		query.RecursionDesired = false
		answer, _, err := (&dns.Client{Net: protocol, Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
		if err != nil {
			t.Fatal(err)
		}
		if answer.Rcode != dns.RcodeRefused || len(answer.Answer) != 0 {
			t.Fatalf("%s nonrecursive query bypassed first matching source: %v", protocol, answer)
		}
	}
	if count := calls.Load(); count != 0 {
		t.Fatalf("earlier forward source received %d private questions", count)
	}
}

func TestNonrecursiveQueriesUseCachedLocalInformationAndEchoRD(t *testing.T) {
	var calls atomic.Int32
	upstream := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) { calls.Add(1); _ = w.WriteMsg(failoverSuccessfulReply(q)) }))
	config := testForwardConfig("public", "udp://"+upstream)
	config.Spec.Cache.Enabled = true
	d := newTestDaemon(t, "", config, true)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	query := new(dns.Msg)
	query.SetQuestion("cached.example.org.", dns.TypeA)
	answer, _, err := (&dns.Client{Net: "udp", Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
	if err != nil || answer.Rcode != dns.RcodeSuccess || len(answer.Answer) != 1 {
		t.Fatalf("recursive cache fill failed: %v, %v", answer, err)
	}
	for _, protocol := range []string{"udp", "tcp"} {
		query.RecursionDesired = false
		answer, _, err = (&dns.Client{Net: protocol, Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
		if err != nil {
			t.Fatal(err)
		}
		if answer.Rcode != dns.RcodeSuccess || answer.RecursionDesired || len(answer.Answer) != 1 {
			t.Fatalf("%s nonrecursive cached answer changed: %v", protocol, answer)
		}
	}
	if count := calls.Load(); count != 1 {
		t.Fatalf("cached nonrecursive request reached upstream: %d calls", count)
	}
}
