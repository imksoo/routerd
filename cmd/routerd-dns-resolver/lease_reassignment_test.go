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

func TestNewClientWithoutHostnameWithdrawsPreviousAddressOwner(t *testing.T) {
	for _, initial := range []string{"observed", "held"} {
		t.Run(initial, func(t *testing.T) {
			config := testResolverConfig(nil)
			if initial == "held" {
				config.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{Hostname: "previous", IP: "192.0.2.55", MAC: "client-a", Source: "DHCPv4Server/dhcpv4", Origin: resolvercfg.DHCPAssignmentHeld, ExpiresAt: time.Now().Add(time.Hour)}}
			}
			table := newZoneTable(config.Zones)
			if initial == "observed" {
				lease := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv4Server/dhcpv4", MAC: "client-a", Hostname: "previous", IP: "192.0.2.55", ObservedAt: time.Now().Add(-time.Minute), Infinite: true}
				if err := table.ApplyLease(lease); err != nil {
					t.Fatal(err)
				}
			}
			assertZoneARecord(t, table, "previous.lab.example.", "192.0.2.55")
			replacement := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionRenewed, Source: "DHCPv4Server/dhcpv4", MAC: "client-b", IP: "192.0.2.55", ObservedAt: time.Now().Add(-time.Second), Infinite: true}
			if err := table.ApplyLease(replacement); err != nil {
				t.Fatal(err)
			}
			query := new(dns.Msg)
			query.SetQuestion("previous.lab.example.", dns.TypeA)
			answer, _ := table.Answer(query, []string{"DNSZone/lan-zone"})
			if len(answer.Answer) != 0 {
				t.Fatalf("new nameless allocation retained previous client's name: %v", answer)
			}
			assertZoneNoPTRRecord(t, table, "55.2.0.192.in-addr.arpa.")
		})
	}
}

func TestNamelessReassignmentFencesOldClientLeaseFileAcrossReload(t *testing.T) {
	config := testResolverConfig(nil)
	path := filepath.Join(t.TempDir(), "dnsmasq.leases")
	if err := os.WriteFile(path, []byte("0 client-a 192.0.2.55 previous *\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config.Zones[0].Spec.DHCPDerived.LeaseFile = path
	table := newZoneTable(config.Zones)
	replacement := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv4Server/dhcpv4", MAC: "client-b", IP: "192.0.2.55", ObservedAt: time.Now().UTC(), Infinite: true}
	if err := table.ApplyLease(replacement); err != nil {
		t.Fatal(err)
	}
	reloaded := newZoneTable(config.Zones)
	reloaded.CopyDynamicFrom(table)
	query := new(dns.Msg)
	query.SetQuestion("previous.lab.example.", dns.TypeA)
	answer, _ := reloaded.Answer(query, []string{"DNSZone/lan-zone"})
	if len(answer.Answer) != 0 {
		t.Fatal("stale file revived previous client after nameless reassignment")
	}
	assertZoneNoPTRRecord(t, reloaded, "55.2.0.192.in-addr.arpa.")
}

func TestNamelessAllocationAndOtherClientRemovalPreserveDeclaredNames(t *testing.T) {
	config := testResolverConfig(nil)
	config.Zones[0].Spec.Records = append(config.Zones[0].Spec.Records, api.DNSZoneRecordSpec{Hostname: "declared", IPv4: "192.0.2.55"})
	config.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{Hostname: "reserved", IP: "192.0.2.56", MAC: "reserved-client", Source: "DHCPv4Server/dhcpv4", Origin: resolvercfg.DHCPAssignmentReservation}}
	table := newZoneTable(config.Zones)
	for _, address := range []string{"192.0.2.55", "192.0.2.56"} {
		lease := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv4Server/dhcpv4", MAC: "other-client", IP: address, ObservedAt: time.Now().UTC(), Infinite: true}
		if err := table.ApplyLease(lease); err != nil {
			t.Fatal(err)
		}
	}
	assertZoneARecord(t, table, "declared.lab.example.", "192.0.2.55")
	assertZoneARecord(t, table, "reserved.lab.example.", "192.0.2.56")
	active := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv4Server/dhcpv4", MAC: "current-client", IP: "192.0.2.57", Hostname: "current", ObservedAt: time.Now().Add(-time.Second), Infinite: true}
	if err := table.ApplyLease(active); err != nil {
		t.Fatal(err)
	}
	removal := active
	removal.MAC = "unrelated-client"
	removal.Action = daemonapi.DHCPLeaseActionRemoved
	removal.ObservedAt = time.Now().UTC()
	if err := table.ApplyLease(removal); err != nil {
		t.Fatal(err)
	}
	assertZoneARecord(t, table, "current.lab.example.", "192.0.2.57")
	assertZonePTRRecord(t, table, "57.2.0.192.in-addr.arpa.", "current.lab.example.")
	if projected := table.zones["lan-zone"].LeaseEvents[leaseAddress{Source: active.Source, IP: active.IP}]; projected.Action != daemonapi.DHCPLeaseActionAdded || projected.MAC != active.MAC {
		t.Fatalf("unrelated removal replaced live source/IP recovery observation: %+v", projected)
	}
	reloaded := newZoneTable(config.Zones)
	reloaded.CopyDynamicFrom(table)
	assertZoneARecord(t, reloaded, "current.lab.example.", "192.0.2.57")

}

func TestNamelessReassignmentFencesOldHeldSnapshotAcrossReload(t *testing.T) {
	config := testResolverConfig(nil)
	config.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{Hostname: "previous", IP: "192.0.2.55", MAC: "client-a", Source: "DHCPv4Server/dhcpv4", Origin: resolvercfg.DHCPAssignmentHeld, ExpiresAt: time.Now().Add(time.Hour)}}
	table := newZoneTable(config.Zones)
	replacement := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv4Server/dhcpv4", MAC: "client-b", IP: "192.0.2.55", ObservedAt: time.Now().UTC(), Infinite: true}
	if err := table.ApplyLease(replacement); err != nil {
		t.Fatal(err)
	}
	reloaded := newZoneTable(config.Zones)
	reloaded.CopyDynamicFrom(table)
	query := new(dns.Msg)
	query.SetQuestion("previous.lab.example.", dns.TypeA)
	answer, _ := reloaded.Answer(query, []string{"DNSZone/lan-zone"})
	if len(answer.Answer) != 0 {
		t.Fatal("stale held assignment revived old client after reassignment")
	}
	assertZoneNoPTRRecord(t, reloaded, "55.2.0.192.in-addr.arpa.")
}
