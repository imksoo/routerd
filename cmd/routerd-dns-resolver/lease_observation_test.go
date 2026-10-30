// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/miekg/dns"
)

func observedLeaseForTest(lease dhcpLeaseEvent) dhcpLeaseEvent {
	if lease.Source == "" {
		lease.Source = "DHCPv4Server/dhcpv4"
	}
	if lease.MAC == "" {
		lease.MAC = "02:00:00:00:00:55"
	}
	lease.ObservedAt = time.Now().UTC()
	lease.Infinite = true
	return lease
}

func TestNamedLeaseExpiryAndOrderingAcrossReload(t *testing.T) {
	config := testResolverConfig(nil)
	other := config.Zones[0]
	other.Name = "foreign"
	other.Spec.Zone = "foreign.example"
	other.Spec.DHCPDerived.Sources = []string{"DHCPv4Server/other"}
	config.Zones = append(config.Zones, other)
	table := newZoneTable(config.Zones)
	now := time.Now().UTC()
	lease := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: "DHCPv4Server/dhcpv4", MAC: "client-a", IP: "192.0.2.55", Hostname: "client", ObservedAt: now, ExpiresAt: now.Add(250 * time.Millisecond)}
	if err := table.ApplyLease(lease); err != nil {
		t.Fatal(err)
	}
	assertZoneARecord(t, table, "client.lab.example.", lease.IP)
	query := new(dns.Msg)
	query.SetQuestion("client.foreign.example.", dns.TypeA)
	if answer, ok := table.Answer(query, []string{"DNSZone/foreign"}); !ok || len(answer.Answer) != 0 {
		t.Fatalf("foreign source received lease: %v", answer)
	}
	query.SetQuestion("client.lab.example.", dns.TypeA)
	answer, _ := table.Answer(query, []string{"DNSZone/lan-zone"})
	if answer.Answer[0].Header().Ttl > 1 {
		t.Fatal("TTL outlives lease")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		answer, _ = table.Answer(query, []string{"DNSZone/lan-zone"})
		if len(answer.Answer) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(answer.Answer) != 0 {
		t.Fatal("expired forward record retained")
	}
	assertZoneNoPTRRecord(t, table, "55.2.0.192.in-addr.arpa.")
	// Removal is an observed tombstone: an older queued add cannot revive it,
	// including after the table is replaced during reload.
	lease.ObservedAt = now.Add(time.Millisecond)
	lease.Action = daemonapi.DHCPLeaseActionRemoved
	if err := table.ApplyLease(lease); err != nil {
		t.Fatal(err)
	}
	reloaded := newZoneTable(config.Zones)
	reloaded.CopyDynamicFrom(table)
	lease.Action = daemonapi.DHCPLeaseActionAdded
	lease.ObservedAt = now
	lease.Infinite = true
	lease.ExpiresAt = time.Time{}
	if err := reloaded.ApplyLease(lease); err != nil {
		t.Fatal(err)
	}
	answer, _ = reloaded.Answer(query, []string{"DNSZone/lan-zone"})
	if len(answer.Answer) != 0 {
		t.Fatal("replayed add revived removed lease")
	}
}

func TestLeaseFileUsesUniqueCurrentNamedScopeAndIPv6ClientDUID(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "dnsmasq.leases")
	data := fmt.Sprintf("%d client-a 192.0.2.55 selected *\n%d client-b 198.51.100.55 other *\n%d client-c 192.0.2.56 expired *\n%d 123 2001:db8::55 six 00:04:aa:bb\n", now.Add(time.Minute).Unix(), now.Add(time.Minute).Unix(), now.Add(-time.Second).Unix(), now.Add(time.Minute).Unix())
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	config := testResolverConfig(nil)
	zone := &config.Zones[0]
	zone.Spec.DHCPDerived.LeaseFile = path
	zone.Spec.DHCPDerived.Sources = append(zone.Spec.DHCPDerived.Sources, "DHCPv6Server/six")
	zone.DHCPScopes = []resolvercfg.DHCPAddressScope{{Source: "DHCPv4Server/dhcpv4", Start: "192.0.2.0", End: "192.0.2.255"}, {Source: "DHCPv4Server/other", Start: "198.51.100.0", End: "198.51.100.255"}, {Source: "DHCPv6Server/six", Start: "2001:db8::1", End: "2001:db8::ffff"}}
	table := newZoneTable(config.Zones)
	if err := table.LoadError(); err != nil {
		t.Fatal(err)
	}
	assertZoneARecord(t, table, "selected.lab.example.", "192.0.2.55")
	for _, name := range []string{"other", "expired"} {
		q := new(dns.Msg)
		q.SetQuestion(name+".lab.example.", dns.TypeA)
		answer, _ := table.Answer(q, []string{"DNSZone/lan-zone"})
		if len(answer.Answer) != 0 {
			t.Fatalf("unselected/expired row served: %v", answer)
		}
	}
	q := new(dns.Msg)
	q.SetQuestion("six.lab.example.", dns.TypeAAAA)
	answer, _ := table.Answer(q, []string{"DNSZone/lan-zone"})
	if len(answer.Answer) != 1 {
		t.Fatalf("IPv6 lease not recovered: %v", answer)
	}
	record := table.zones["lan-zone"].Records["six.lab.example."]
	if record.MAC != "00:04:aa:bb" {
		t.Fatalf("IAID mistaken for DUID: %+v", record)
	}
	// A shared file row cannot select one of overlapping named allocations.
	zone.DHCPScopes = append(zone.DHCPScopes, resolvercfg.DHCPAddressScope{Source: "DHCPv4Server/overlap", Start: "192.0.2.1", End: "192.0.2.250"})
	table = newZoneTable(config.Zones)
	q.SetQuestion("selected.lab.example.", dns.TypeA)
	answer, _ = table.Answer(q, []string{"DNSZone/lan-zone"})
	if len(answer.Answer) != 0 {
		t.Fatal("ambiguous source inferred from selected zone")
	}
}

func TestConfiguredAssignmentIsNotAnObservedLease(t *testing.T) {
	config := testResolverConfig(nil)
	config.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{Hostname: "reservation", IP: "192.0.2.55", MAC: "reserved-client", Source: "DHCPv4Server/dhcpv4", Origin: resolvercfg.DHCPAssignmentReservation}}
	table := newZoneTable(config.Zones)
	// Another client's callback cannot steal a configured reservation or erase it.
	lease := observedLeaseForTest(dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Hostname: "other", IP: "192.0.2.55"})
	if err := table.ApplyLease(lease); err != nil {
		t.Fatal(err)
	}
	lease.Action = daemonapi.DHCPLeaseActionRemoved
	lease.ObservedAt = time.Now().UTC()
	if err := table.ApplyLease(lease); err != nil {
		t.Fatal(err)
	}
	assertZoneARecord(t, table, "reservation.lab.example.", "192.0.2.55")
	assertZonePTRRecord(t, table, "55.2.0.192.in-addr.arpa.", "reservation.lab.example.")
	// Untyped hostname snapshots have neither source authority nor a deadline.
	config.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{Hostname: "unverified", IP: "192.0.2.56"}}
	table = newZoneTable(config.Zones)
	q := new(dns.Msg)
	q.SetQuestion("unverified.lab.example.", dns.TypeA)
	answer, _ := table.Answer(q, []string{"DNSZone/lan-zone"})
	if len(answer.Answer) != 0 {
		t.Fatal("raw hostname snapshot became infinite lease")
	}
}

func TestRequiredLeaseFileFailureIsObservable(t *testing.T) {
	config := testResolverConfig(nil)
	config.Zones[0].Spec.DHCPDerived.LeaseFile = filepath.Join(t.TempDir(), "missing")
	if newZoneTable(config.Zones).LoadError() == nil {
		t.Fatal("required lease read failure became absence")
	}
}
