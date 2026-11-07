// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/miekg/dns"
)

func TestDaemonDNSProbePrivateQuestionsClearRDButRootQuestionsKeepRD(t *testing.T) {
	for _, root := range []bool{false, true} {
		name := "private"
		if root {
			name = "public-root"
		}
		t.Run(name, func(t *testing.T) {
			flags := make(chan bool, 2)
			_, port := daemonProbeDNSPeer(t, func(w dns.ResponseWriter, q *dns.Msg) {
				flags <- q.RecursionDesired
				a := new(dns.Msg)
				a.SetReply(q)
				if root {
					a.Answer = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 1}, Ns: "ns.example."}}
				} else {
					a.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1}, A: net.ParseIP("192.0.2.55")}}
				}
				_ = w.WriteMsg(a)
			})
			router := daemonProbeDNSRouter(port)
			if root {
				router.Spec.Resources[1].Spec = api.DNSForwarderSpec{Resolver: "lan", Match: []string{"."}, Upstreams: []string{"DNSUpstream/public"}}
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSUpstreamSpec{Address: "192.0.2.53", Protocol: "udp"}})
			}
			checks, err := daemonDNSProbe(t.Context(), router, nil, "lan")
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, check := range checks {
				if check.Kind == "communication" {
					count++
					if check.Status != "pass" {
						t.Fatalf("wire reply failed: %+v", check)
					}
				}
			}
			if count != 2 || len(flags) != 2 {
				t.Fatalf("actual UDP/TCP questions missing: %+v", checks)
			}
			for i := 0; i < 2; i++ {
				if flag := <-flags; flag != root {
					t.Fatalf("RD=%t, want %t", flag, root)
				}
			}
		})
	}
}

func TestDaemonDNSProbeNonlocalListenerRemainsUnobservedWithoutWire(t *testing.T) {
	router := daemonProbeDNSRouter(5053)
	spec, _ := router.Spec.Resources[0].DNSResolverSpec()
	spec.Listen[0].Addresses = []string{"192.0.2.253"}
	router.Spec.Resources[0].Spec = spec
	checks, err := daemonDNSProbe(t.Context(), router, nil, "lan")
	if err != nil {
		t.Fatal(err)
	}
	unobserved := false
	for _, check := range checks {
		if check.Kind == "communication" {
			t.Fatalf("nonlocal or unobservable native address started a wire attempt: %+v", check)
		}
		unobserved = unobserved || strings.Contains(check.Detail, "native local address")
	}
	if !unobserved {
		t.Fatalf("missing native-local observation was not explicit: %+v", checks)
	}
}
