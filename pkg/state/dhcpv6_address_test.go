// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/pdclient"
)

func dhcpv6AddressStateFixture() DHCPv6AddressState {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	return DHCPv6AddressState{
		Phase: "Pending", Reason: "AddressApplying",
		Source:     DHCPv6AddressSource{Worker: "DHCPv6Address/wan", IfName: "wan0", ClientDUID: "00030001020000000001", IAID: 1, Mode: pdclient.RequestAddresses},
		ServerDUID: "00030001020000000002", SnapshotAt: now,
		Leases: []pdclient.AddressSnapshot{{Address: "2001:db8::123", Preferred: 30, Valid: 60, AcquiredAt: now}},
		Native: &DHCPv6AddressReceipt{IfName: "wan0", IfIndex: 7, Scope: "fixture-scope", Addresses: []DHCPv6OwnedAddress{{Address: "2001:db8::123/128", Pending: &DHCPv6AddressIntent{Operation: "apply", PreferredUntil: now.Add(30 * time.Second), ValidUntil: now.Add(time.Minute), AcquiredAt: now}}}},
	}
}

func TestDHCPv6AddressRecordPersistsLeaseAndPendingOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	want := dhcpv6AddressStateFixture()
	record, err := s.SaveDHCPv6AddressRecord("wan", DHCPv6AddressRecord{}, want)
	if err != nil {
		t.Fatal(err)
	}
	if record.Revision != 1 || record.Owner == "" {
		t.Fatal("record has no durable write identity")
	}
	unchanged, err := s.SaveDHCPv6AddressRecord("wan", record, want)
	if err != nil || unchanged.Revision != record.Revision {
		t.Fatalf("unchanged state generated another write: %+v %v", unchanged, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.ReadDHCPv6AddressRecord("wan")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, record) {
		t.Fatalf("reopen changed acquisition time or pending operation: got=%+v want=%+v", got, record)
	}
	if !reflect.DeepEqual(got.State, want) {
		t.Fatalf("stored state differs: %+v", got.State)
	}
	// Revoking protocol authority must not erase the still-pending native work.
	want.Leases = nil
	if _, err := s.SaveDHCPv6AddressRecord("wan", got, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.ReadDHCPv6AddressRecord("wan")
	if err != nil {
		t.Fatal(err)
	}
	if got.State.Native == nil || got.State.Native.Addresses[0].Pending == nil {
		t.Fatal("lease revocation forgot native cleanup")
	}
}

func TestDHCPv6AddressRecordRejectsStaleAndRecreatedOwner(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.SaveDHCPv6AddressRecord("wan", DHCPv6AddressRecord{}, dhcpv6AddressStateFixture())
	if err != nil {
		t.Fatal(err)
	}
	updated := first.State
	updated.Reason = "NewObservation"
	if _, err := s.SaveDHCPv6AddressRecord("wan", first, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDHCPv6AddressRecord("wan", first, first.State); err == nil {
		t.Fatal("late observer overwrote a newer record")
	}
	if err := s.DeleteObject(api.NetAPIVersion, "DHCPv6Address", "wan"); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.SaveDHCPv6AddressRecord("wan", DHCPv6AddressRecord{}, first.State)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Owner == first.Owner {
		t.Fatal("recreated owner reused the retired receipt identity")
	}
	if _, err := s.SaveDHCPv6AddressRecord("wan", first, first.State); err == nil {
		t.Fatal("same numeric revision admitted an old owner's write")
	}
}

func TestDHCPv6AddressRecordDeletionRequiresWithdrawnCurrentOwner(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	record, err := s.SaveDHCPv6AddressRecord("wan", DHCPv6AddressRecord{}, dhcpv6AddressStateFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDHCPv6AddressRecord("wan", record); err == nil {
		t.Fatal("pending native ownership was deleted")
	}
	next := record.State
	next.Native = nil
	withdrawn, err := s.SaveDHCPv6AddressRecord("wan", record, next)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDHCPv6AddressRecord("wan", record); err == nil {
		t.Fatal("stale cleanup acknowledgment deleted current state")
	}
	if err := s.DeleteDHCPv6AddressRecord("wan", withdrawn); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadDHCPv6AddressRecord("wan")
	if err != nil || got.Revision != 0 {
		t.Fatalf("withdrawn state remains: %+v %v", got, err)
	}
}
