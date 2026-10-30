// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/miekg/dns"
)

func TestDaemonDNSProbeNeverSendsShadowedPrivateQuestion(t *testing.T) {
	for _, inline := range []bool{false, true} {
		label := "ordered-forwarders"
		if inline {
			label = "forwarders-replace-inline"
		}
		t.Run(label, func(t *testing.T) {
			observed := make(chan string, 4)
			_, port := daemonProbeDNSPeer(t, func(w dns.ResponseWriter, q *dns.Msg) {
				observed <- q.Question[0].Name
				a := new(dns.Msg)
				a.SetReply(q)
				a.Answer = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 1}, Ns: "ns.public.example."}}
				_ = w.WriteMsg(a)
			})
			router := daemonProbeDNSRouter(port)
			public := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSForwarderSpec{Resolver: "lan", Match: []string{"."}, Upstreams: []string{"DNSUpstream/public"}}}
			if inline {
				resolver, _ := router.Spec.Resources[0].DNSResolverSpec()
				resolver.Sources = []api.DNSResolverSourceSpec{{Name: "old-private", Kind: "zone", Match: []string{"test.example"}, ZoneRef: []string{"DNSZone/local"}}}
				router.Spec.Resources[0].Spec = resolver
				router.Spec.Resources[1] = public
			} else {
				router.Spec.Resources = append(router.Spec.Resources[:1], append([]api.Resource{public}, router.Spec.Resources[1:]...)...)
			}
			router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSUpstreamSpec{Address: "192.0.2.53", Protocol: "udp"}})
			checks, err := daemonDNSProbe(t.Context(), router, nil, "lan")
			if err != nil {
				t.Fatal(err)
			}
			attempts := 0
			for _, c := range checks {
				if c.Kind == "communication" {
					attempts++
					if c.Status != "pass" {
						t.Errorf("safe public root reply failed: %+v", c)
					}
				}
			}
			if attempts != 2 {
				t.Fatalf("expected two safe root queries: %+v", checks)
			}
			for i := 0; i < 2; i++ {
				if got := <-observed; got != "." {
					t.Errorf("private question escaped through earlier public source: %q", got)
				}
			}
		})
	}
}

func TestDaemonDNSProbeRejectsWrongAnswerClassOnActualTransports(t *testing.T) {
	for _, root := range []bool{false, true} {
		label := "static-A"
		if root {
			label = "root-NS"
		}
		t.Run(label, func(t *testing.T) {
			_, port := daemonProbeDNSPeer(t, func(w dns.ResponseWriter, q *dns.Msg) {
				a := new(dns.Msg)
				a.SetReply(q)
				hdr := dns.RR_Header{Name: q.Question[0].Name, Class: dns.ClassCHAOS, Ttl: 1}
				if root {
					hdr.Rrtype = dns.TypeNS
					a.Answer = []dns.RR{&dns.NS{Hdr: hdr, Ns: "ns.public.example."}}
				} else {
					hdr.Rrtype = dns.TypeA
					a.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.ParseIP("192.0.2.55")}}
				}
				_ = w.WriteMsg(a)
			})
			router := daemonProbeDNSRouter(port)
			if root {
				router.Spec.Resources[1].Spec = api.DNSForwarderSpec{Resolver: "lan", Match: []string{"."}, Upstreams: []string{"DNSUpstream/public"}}
			}
			checks, err := daemonDNSProbe(t.Context(), router, nil, "lan")
			if err != nil {
				t.Fatal(err)
			}
			attempts := 0
			for _, c := range checks {
				if c.Kind == "communication" {
					attempts++
					if c.Status != "fail" || c.FailureKind != "dns_answer" {
						t.Errorf("non-INET RR became successful DNS resolution: %+v", c)
					}
				}
			}
			if attempts != 2 {
				t.Fatalf("both actual transports were not observed: %+v", checks)
			}
		})
	}
}

func TestDaemonDNSProbeBoundsQuestionPreparationBeforeSending(t *testing.T) {
	router := daemonProbeDNSRouter(65534)
	zone, _ := router.Spec.Resources[2].DNSZoneSpec()
	original := zone.Records[0]
	zone.Records = nil
	for i := 0; i < maxDaemonDNSQuestionItems; i++ {
		zone.Records = append(zone.Records, api.DNSZoneRecordSpec{Hostname: "*.private", IPv4: "192.0.2.55"})
	}
	zone.Records = append(zone.Records, original)
	router.Spec.Resources[2].Spec = zone
	checks, err := daemonDNSProbe(t.Context(), router, nil, "lan")
	if err != nil {
		t.Fatal(err)
	}
	bounded := false
	for _, c := range checks {
		if c.Kind == "communication" {
			t.Fatalf("preparation budget exhaustion still sent a speculative query: %+v", c)
		}
		if strings.Contains(c.Detail, "question preparation limit reached") {
			bounded = true
		}
	}
	if !bounded {
		t.Fatalf("question preparation bound was not explicit: %+v", checks)
	}
}
