// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	runtime "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func TestColdDNSStartCannotReviveDurablyRemovedLeaseFileRow(t *testing.T) {
	root := t.TempDir()
	leaseFile := filepath.Join(root, "dnsmasq.leases")
	if err := os.WriteFile(leaseFile, []byte("0 client-a 192.0.2.55 stale *\n"), 0600); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "state.db")
	store, err := state.OpenSQLite(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Second)
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-dhcp-event-relay"}, "routerd.dhcp.lease.removed", daemonapi.SeverityInfo)
	topic, ok := daemonapi.DHCPLeaseEventType(daemonapi.DHCPLeaseActionRemoved)
	if !ok {
		t.Fatal("missing removal topic")
	}
	event.Type = topic
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server", Name: "dhcpv4"}
	event.Attributes = map[string]string{"mac": "client-a", "ip": "192.0.2.55", "hostname": "stale", "observedAt": at.Format(time.RFC3339Nano), "infinite": "true"}
	if _, err := store.RecordBusEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.OpenSQLite(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observations, err := store.ReadDHCPLeaseObservations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Action != daemonapi.DHCPLeaseActionRemoved {
		t.Fatalf("durable named removal missing: %+v", observations)
	}
	config := testResolverConfig(nil)
	config.Zones[0].Spec.DHCPDerived.LeaseFile = leaseFile
	config.Zones[0].LeaseObservations = observations
	// Exercise the actual configuration IPC encoding before constructing a fresh
	// worker. It has no old table, goroutines, or in-memory tombstones to consult.
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var recovered runtime.RuntimeConfig
	if err := json.Unmarshal(body, &recovered); err != nil {
		t.Fatal(err)
	}
	worker := newTestDaemon(t, filepath.Join(root, "resolver.json"), recovered, true)
	query := new(dns.Msg)
	query.SetQuestion("stale.lab.example.", dns.TypeA)
	answer, _ := worker.zones.Answer(query, nil)
	if len(answer.Answer) != 0 {
		t.Fatal("cold worker revived stale acknowledged removal")
	}
	assertZoneNoPTRRecord(t, worker.zones, "55.2.0.192.in-addr.arpa.")
	// A later real lease update restores this exact source/address after a second
	// full reopen; a durable revoke does not permanently blacklist the address.
	event.Type, _ = daemonapi.DHCPLeaseEventType(daemonapi.DHCPLeaseActionAdded)
	event.Attributes["observedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.RecordBusEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.OpenSQLite(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observations, err = store.ReadDHCPLeaseObservations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	recovered.Zones[0].LeaseObservations = observations
	worker = newTestDaemon(t, filepath.Join(root, "resolver2.json"), recovered, true)
	assertZoneARecord(t, worker.zones, "stale.lab.example.", "192.0.2.55")
}

func TestReloadCurrentDurableRemovalOutranksUndeliveredOldWorkerLease(t *testing.T) {
	config := testResolverConfig(nil)
	old := newZoneTable(config.Zones)
	lease := observedLeaseForTest(dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, MAC: "client-a", IP: "192.0.2.55", Hostname: "old"})
	lease.ObservedAt = time.Now().UTC().Add(-time.Second)
	if err := old.ApplyLease(lease); err != nil {
		t.Fatal(err)
	}
	lease.Action = daemonapi.DHCPLeaseActionRemoved
	lease.ObservedAt = time.Now().UTC()
	config.Zones[0].LeaseObservations = []daemonapi.DHCPLeaseUpdate{lease}
	selected := newZoneTable(config.Zones)
	selected.CopyDynamicFrom(old)
	query := new(dns.Msg)
	query.SetQuestion("old.lab.example.", dns.TypeA)
	answer, _ := selected.Answer(query, nil)
	if len(answer.Answer) != 0 {
		t.Fatal("old in-memory record outranked durable current removal during reload")
	}
	assertZoneNoPTRRecord(t, selected, "55.2.0.192.in-addr.arpa.")
}
