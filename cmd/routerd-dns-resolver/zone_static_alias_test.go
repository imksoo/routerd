// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
)

func TestZoneStaticAliasesShareAddress(t *testing.T) {
	tests := []struct {
		name   string
		first  api.DNSZoneRecordSpec
		second api.DNSZoneRecordSpec
	}{
		{
			name:   "shared IPv4",
			first:  api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"},
			second: api.DNSZoneRecordSpec{Hostname: "node", IPv4: "192.0.2.1"},
		},
		{
			name:   "shared IPv6",
			first:  api.DNSZoneRecordSpec{Hostname: "router", IPv6: "2001:db8::1"},
			second: api.DNSZoneRecordSpec{Hostname: "node", IPv6: "2001:db8::1"},
		},
		{
			name:   "distinct IPv4 shared IPv6",
			first:  api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1", IPv6: "2001:db8::1"},
			second: api.DNSZoneRecordSpec{Hostname: "node", IPv4: "192.0.2.2", IPv6: "2001:db8::1"},
		},
		{
			name:   "shared IPv4 distinct IPv6",
			first:  api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1", IPv6: "2001:db8::1"},
			second: api.DNSZoneRecordSpec{Hostname: "node", IPv4: "192.0.2.1", IPv6: "2001:db8::2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				first, second := tt.first, tt.second
				if reverse {
					first, second = second, first
				}
				t.Run("first="+first.Hostname, func(t *testing.T) {
					config := []resolvercfg.RuntimeZone{{
						Name: "lan-zone",
						Spec: api.DNSZoneSpec{
							Zone:    "example.test",
							Records: []api.DNSZoneRecordSpec{first, second},
						},
					}}
					zones := newZoneTable(config)
					for _, phase := range []string{"initial", "reload"} {
						t.Run(phase, func(t *testing.T) {
							for _, record := range []api.DNSZoneRecordSpec{first, second} {
								name := record.Hostname + ".example.test."
								for _, address := range []struct {
									qtype uint16
									ip    string
								}{{dns.TypeA, record.IPv4}, {dns.TypeAAAA, record.IPv6}} {
									if address.ip == "" {
										continue
									}
									req := new(dns.Msg)
									req.SetQuestion(name, address.qtype)
									resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
									if !ok || resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
										t.Fatalf("%s %s: answered=%v response=%v", name, dns.TypeToString[address.qtype], ok, resp)
									}
									var got string
									switch answer := resp.Answer[0].(type) {
									case *dns.A:
										got = answer.A.String()
									case *dns.AAAA:
										got = answer.AAAA.String()
									}
									if got != address.ip {
										t.Fatalf("%s %s = %q, want %q", name, dns.TypeToString[address.qtype], got, address.ip)
									}
									ptr, err := dns.ReverseAddr(address.ip)
									if err != nil {
										t.Fatal(err)
									}
									wantPTR := name
									if address.ip == first.IPv4 || address.ip == first.IPv6 {
										wantPTR = first.Hostname + ".example.test."
									}
									assertZonePTRRecord(t, zones, ptr, wantPTR)
								}
							}
						})
						previous := zones
						zones = newZoneTable(config)
						zones.CopyDynamicFrom(previous)
					}
				})
			}
		})
	}
}

func TestZoneStaticAliasesSurviveDHCPLeaseEvents(t *testing.T) {
	config := []resolvercfg.RuntimeZone{{
		Name: "lan-zone",
		Spec: api.DNSZoneSpec{
			Zone:        "example.test",
			DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"DHCPv4Server/lan"}},
			Records: []api.DNSZoneRecordSpec{
				{Hostname: "router", IPv4: "192.0.2.1"},
				{Hostname: "node", IPv4: "192.0.2.1"},
			},
		},
		DHCPHostRecords: []resolvercfg.RuntimeDHCPHostRecord{{Hostname: "sticky", IP: "192.0.2.1"}},
	}}
	zones := newZoneTable(config)
	for _, action := range []string{daemonapi.DHCPLeaseActionAdded, daemonapi.DHCPLeaseActionRemoved} {
		zones.ApplyLease(observedLeaseForTest(dhcpLeaseEvent{Source: "DHCPv4Server/lan", Action: action, IP: "192.0.2.1", Hostname: "dynamic"}))
		assertZoneARecord(t, zones, "router.example.test.", "192.0.2.1")
		assertZoneARecord(t, zones, "node.example.test.", "192.0.2.1")
		assertZonePTRRecord(t, zones, "1.2.0.192.in-addr.arpa.", "router.example.test.")
		for _, name := range []string{"sticky.example.test.", "dynamic.example.test."} {
			req := new(dns.Msg)
			req.SetQuestion(name, dns.TypeA)
			resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
			if !ok || resp.Rcode != dns.RcodeNameError {
				t.Fatalf("%s after %s: answered=%v response=%v, want NXDOMAIN", name, action, ok, resp)
			}
		}
	}
}

func TestZoneStaticAliasPTRSurvivesOwnerAddressReplacement(t *testing.T) {
	zones := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "lan-zone",
		Spec: api.DNSZoneSpec{
			Zone: "example.test",
			Records: []api.DNSZoneRecordSpec{
				{Hostname: "router", IPv4: "192.0.2.1"},
				{Hostname: "node", IPv4: "192.0.2.1"},
				{Hostname: "router", IPv4: "192.0.2.2"},
			},
		},
	}})
	assertZoneARecord(t, zones, "router.example.test.", "192.0.2.2")
	assertZoneARecord(t, zones, "node.example.test.", "192.0.2.1")
	assertZonePTRRecord(t, zones, "1.2.0.192.in-addr.arpa.", "node.example.test.")
	assertZonePTRRecord(t, zones, "2.2.0.192.in-addr.arpa.", "router.example.test.")
}
