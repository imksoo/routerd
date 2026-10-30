// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/miekg/dns"
)

func TestEquivalentIPv6LeaseSpellingsShareOneObservationAndWithdrawal(t *testing.T) {
	for _, origin := range []string{"event", "lease-file"} {
		t.Run(origin, func(t *testing.T) {
			zone := resolvercfg.RuntimeZone{Name: "v6", Spec: api.DNSZoneSpec{Zone: "v6.example", DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"DHCPv6Server/v6"}}}, DHCPScopes: []resolvercfg.DHCPAddressScope{{Source: "DHCPv6Server/v6", Start: "2001:db8::1", End: "2001:db8::ffff"}}}
			if origin == "lease-file" {
				path := filepath.Join(t.TempDir(), "leases")
				if err := os.WriteFile(path, []byte("0 123 2001:0db8:0:0::55 client 00:04:aa:bb\n"), 0600); err != nil {
					t.Fatal(err)
				}
				zone.Spec.DHCPDerived.LeaseFile = path
			}
			table := newZoneTable([]resolvercfg.RuntimeZone{zone})
			lease := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv6Server/v6", MAC: "00:04:aa:bb", IP: "2001:0db8:0:0::55", Hostname: "client", ObservedAt: time.Now().Add(-time.Second), Infinite: true}
			if origin == "event" {
				if err := table.ApplyLease(lease); err != nil {
					t.Fatal(err)
				}
			}
			query := new(dns.Msg)
			query.SetQuestion("client.v6.example.", dns.TypeAAAA)
			answer, _ := table.Answer(query, []string{"DNSZone/v6"})
			if len(answer.Answer) != 1 {
				t.Fatalf("initial IPv6 lease missing: %v", answer)
			}
			lease.Action = daemonapi.DHCPLeaseActionRemoved
			lease.IP = "2001:db8::55"
			lease.ObservedAt = time.Now().UTC()
			if err := table.ApplyLease(lease); err != nil {
				t.Fatal(err)
			}
			answer, _ = table.Answer(query, []string{"DNSZone/v6"})
			if len(answer.Answer) != 0 {
				t.Fatalf("equivalent IPv6 removal retained active record: %v", answer)
			}
			ptr, _ := dns.ReverseAddr("2001:db8::55")
			assertZoneNoPTRRecord(t, table, ptr)
			if len(table.zones["v6"].LeaseEvents) != 1 {
				t.Fatal("equivalent address created separate observation keys")
			}
		})
	}
}
