package ralistener

import (
	"net"
	"testing"
	"time"

	routerstate "routerd/pkg/state"
)

func TestMACFromModifiedEUI64(t *testing.T) {
	mac, ok := MACFromModifiedEUI64("fe80::1eb1:7fff:fe73:76d8")
	if !ok {
		t.Fatal("expected EUI-64 link-local to decode")
	}
	if got, want := mac.String(), "1c:b1:7f:73:76:d8"; got != want {
		t.Fatalf("mac = %s, want %s", got, want)
	}
	if _, ok := MACFromModifiedEUI64("2001:db8::1"); ok {
		t.Fatal("global address should not decode as link-local EUI-64")
	}
}

func TestParseRouterAdvertisement(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	packet := routerAdvertisementFixture()
	obs, err := Parse(packet, &net.UDPAddr{IP: net.ParseIP("fe80::1eb1:7fff:fe73:76d8")}, now)
	if err != nil {
		t.Fatalf("parse RA: %v", err)
	}
	if !obs.OFlag || obs.MFlag {
		t.Fatalf("flags M=%t O=%t, want M=false O=true", obs.MFlag, obs.OFlag)
	}
	if got, want := obs.HGWMAC, "1c:b1:7f:73:76:d8"; got != want {
		t.Fatalf("HGW MAC = %s, want %s", got, want)
	}
	if got, want := obs.ServerID, "000300011cb17f7376d8"; got != want {
		t.Fatalf("server ID = %s, want %s", got, want)
	}
	if got, want := obs.Prefix, "2001:db8:1200:1::/64"; got != want {
		t.Fatalf("prefix = %s, want %s", got, want)
	}
	if !obs.ObservedAt.Equal(now) {
		t.Fatalf("observedAt = %s, want %s", obs.ObservedAt, now)
	}
}

func TestApplyObservation(t *testing.T) {
	store := routerstate.New()
	obs := Observation{
		SourceLinkLocal: "fe80::1eb1:7fff:fe73:76d8",
		HGWMAC:          "1c:b1:7f:73:76:d8",
		ServerID:        "000300011cb17f7376d8",
		OFlag:           true,
		Prefix:          "2001:db8:1200:1::/64",
		ObservedAt:      time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}
	if err := ApplyObservation(store, "wan-pd", obs, "RAObserved"); err != nil {
		t.Fatalf("apply observation: %v", err)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease not stored")
	}
	if lease.ServerID != obs.ServerID {
		t.Fatalf("server ID = %q, want %q", lease.ServerID, obs.ServerID)
	}
	if lease.WANObserved == nil {
		t.Fatal("WANObserved not stored")
	}
	if lease.WANObserved.HGWLinkLocal != obs.SourceLinkLocal || lease.WANObserved.RAOFlag != "true" || lease.WANObserved.RAPrefix != obs.Prefix {
		t.Fatalf("WANObserved = %+v", lease.WANObserved)
	}
}

func routerAdvertisementFixture() []byte {
	p := []byte{
		134, 0, 0, 0, // type, code, checksum
		64, 0x40, 0x07, 0x08, // hop limit, flags O, router lifetime 1800
		0, 0, 0, 0, // reachable time
		0, 0, 0, 0, // retrans timer
		1, 1, // source link-layer option, len=1
		0x1c, 0xb1, 0x7f, 0x73, 0x76, 0xd8,
		3, 4, // prefix info option, len=4
		64, 0xc0, // prefix len, L+A flags
		0, 0, 0x0e, 0x10, // valid lifetime 3600
		0, 0, 0x07, 0x08, // preferred lifetime 1800
		0, 0, 0, 0, // reserved
		0x20, 0x01, 0x0d, 0xb8, 0x12, 0x00, 0x00, 0x01,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
	return p
}
