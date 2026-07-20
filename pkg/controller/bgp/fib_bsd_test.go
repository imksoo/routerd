// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
)

func TestParseFreeBSDOwnedBGPRoutesPreservesMultipathAndSkipsForeign(t *testing.T) {
	got := parseFreeBSDOwnedBGPRoutes(`Internet:
Destination        Gateway            Flags     Netif Expire
10.77.60.0/24      192.0.2.1          UG1       vtnet0
10.77.60.0/24      192.0.2.2          UG1       vtnet1
10.77.61.0/24      192.0.2.3          UG        vtnet0
`)
	want := map[string]FIBRoute{
		"10.77.60.0/24": {Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.1", "192.0.2.2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owned routes = %#v, want %#v", got, want)
	}
}

func TestParseFreeBSDLocalIPv4Addresses(t *testing.T) {
	got := parseFreeBSDLocalIPv4Addresses(`vtnet0: flags=8843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST>
        inet 192.0.2.10 netmask 0xffffff00 broadcast 192.0.2.255
vtnet1: flags=8843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST>
        inet 198.51.100.10 netmask 255.255.255.0 broadcast 198.51.100.255
`)
	want := []freeBSDLocalIPv4Address{
		{Address: mustFreeBSDAddr("192.0.2.10"), Prefix: mustFreeBSDPrefix("192.0.2.0/24")},
		{Address: mustFreeBSDAddr("198.51.100.10"), Prefix: mustFreeBSDPrefix("198.51.100.0/24")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local addresses = %#v, want %#v", got, want)
	}
}

func TestFreeBSDFIBSyncerReplacesWithdrawsRetainsAndPreservesForeign(t *testing.T) {
	var commands [][]string
	netstat := `Destination Gateway Flags Netif
10.77.60.0/24 192.0.2.1 UG1 vtnet0
10.77.61.0/24 192.0.2.9 UG vtnet0
`
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		if path == freeBSDNetstatPath {
			return []byte(netstat), nil
		}
		if path == freeBSDIfconfigPath {
			return []byte("vtnet0:\n inet 192.0.2.10 netmask 0xffffff00\n"), nil
		}
		return nil, nil
	}
	s := newFreeBSDFIBSyncer(run)
	result, err := s.SyncBGP(context.Background(), []FIBRoute{{
		Prefix:          "10.77.60.0/24",
		NextHops:        []string{"192.0.2.2", "192.0.2.3"},
		RetainOnMissing: true,
	}})
	if err != nil || !result.Installed["10.77.60.0/24"] {
		t.Fatalf("first sync = %#v, %v", result, err)
	}
	wantFirst := [][]string{
		{freeBSDNetstatPath, "-rn", "-f", "inet"},
		{freeBSDIfconfigPath, "-a"},
		{freeBSDRoutePath, "-n", "delete", "-proto1", "-net", "10.77.60.0/24", "192.0.2.1"},
		{freeBSDRoutePath, "-n", "add", "-proto1", "-net", "10.77.60.0/24", "192.0.2.2"},
		{freeBSDRoutePath, "-n", "add", "-proto1", "-net", "10.77.60.0/24", "192.0.2.3"},
	}
	if !reflect.DeepEqual(commands, wantFirst) {
		t.Fatalf("replace commands = %#v, want %#v", commands, wantFirst)
	}
	commands = nil
	netstat = `Destination Gateway Flags Netif
10.77.60.0/24 192.0.2.2 UG1 vtnet0
10.77.60.0/24 192.0.2.3 UG1 vtnet0
10.77.61.0/24 192.0.2.9 UG vtnet0
`
	result, err = s.SyncBGP(context.Background(), nil)
	if err != nil || !result.Retained["10.77.60.0/24"] || result.Installed["10.77.61.0/24"] {
		t.Fatalf("retain sync = %#v, %v", result, err)
	}
	if len(commands) != 2 || commands[0][0] != freeBSDNetstatPath || commands[1][0] != freeBSDIfconfigPath {
		t.Fatalf("retain touched routes: %#v", commands)
	}
}

func TestFreeBSDFIBSyncerClearsRetainMetadataBeforeWithdraw(t *testing.T) {
	var commands [][]string
	netstat := "Destination Gateway Flags Netif\n10.77.60.0/24 192.0.2.2 UG1 vtnet0\n10.77.60.0/24 192.0.2.3 UG1 vtnet0\n"
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		switch path {
		case freeBSDNetstatPath:
			return []byte(netstat), nil
		case freeBSDIfconfigPath:
			return []byte("vtnet0:\n inet 192.0.2.10 netmask 0xffffff00\n"), nil
		default:
			return nil, nil
		}
	}
	s := newFreeBSDFIBSyncer(run)
	s.installed["10.77.60.0/24"] = FIBRoute{Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.2", "192.0.2.3"}, RetainOnMissing: true}
	s.sourceKnown["10.77.60.0/24"] = true
	s.retainOnMissing["10.77.60.0/24"] = true
	desired := []FIBRoute{{Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.2", "192.0.2.3"}}}
	if _, err := s.SyncBGP(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	if s.installed["10.77.60.0/24"].RetainOnMissing || s.retainOnMissing["10.77.60.0/24"] {
		t.Fatalf("retain metadata was not cleared: %#v %#v", s.installed, s.retainOnMissing)
	}
	commands = nil
	if _, err := s.SyncBGP(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{freeBSDNetstatPath, "-rn", "-f", "inet"},
		{freeBSDIfconfigPath, "-a"},
		{freeBSDRoutePath, "-n", "delete", "-proto1", "-net", "10.77.60.0/24", "192.0.2.2"},
		{freeBSDRoutePath, "-n", "delete", "-proto1", "-net", "10.77.60.0/24", "192.0.2.3"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("withdraw commands = %#v, want %#v", commands, want)
	}
}

func TestFreeBSDFIBSyncerFailsWithoutDeletingForeignOnRouteError(t *testing.T) {
	var commands [][]string
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		if path == freeBSDNetstatPath {
			return []byte("Destination Gateway Flags Netif\n10.77.61.0/24 192.0.2.9 UG vtnet0\n"), nil
		}
		if path == freeBSDIfconfigPath {
			return []byte("vtnet0:\n inet 192.0.2.10 netmask 0xffffff00\n"), nil
		}
		return []byte("permission denied"), errors.New("exit status 1")
	}
	s := newFreeBSDFIBSyncer(run)
	if _, err := s.SyncBGP(context.Background(), []FIBRoute{{Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.2"}}}); err == nil {
		t.Fatal("SyncBGP succeeded, want route add error")
	}
	if len(commands) != 3 || commands[2][2] != "add" {
		t.Fatalf("commands = %#v, want only owned add after netstat", commands)
	}
}

func TestFreeBSDFIBSyncerFiltersLocalHostsAndValidatesPreferredSource(t *testing.T) {
	var commands [][]string
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		switch path {
		case freeBSDNetstatPath:
			return []byte("Destination Gateway Flags Netif\n"), nil
		case freeBSDIfconfigPath:
			return []byte("vtnet0:\n inet 192.0.2.10 netmask 0xffffff00\n"), nil
		default:
			return nil, nil
		}
	}
	s := newFreeBSDFIBSyncer(run)
	result, err := s.SyncBGP(context.Background(), []FIBRoute{
		{Prefix: "192.0.2.10/32", NextHops: []string{"192.0.2.1"}},
		{Prefix: "192.0.2.20/32", NextHops: []string{"192.0.2.1"}, PreferredSource: "198.51.100.10"},
		{Prefix: "192.0.2.30/32", NextHops: []string{"192.0.2.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Installed["192.0.2.10/32"] {
		t.Fatalf("local host route was installed: %#v", result)
	}
	if !result.PreferredSourceSkipped["192.0.2.20/32"] || result.PreferredSourceSkippedReason["192.0.2.20/32"] != "LocalAddressMissing" {
		t.Fatalf("preferred source result = %#v", result)
	}
	if result.PreferredSource["192.0.2.30/32"] != "192.0.2.10" {
		t.Fatalf("inferred source = %#v", result.PreferredSource)
	}
	if len(commands) != 4 {
		t.Fatalf("commands = %#v, want one netstat, one ifconfig, and two adds", commands)
	}
}

func TestFreeBSDFIBSyncerReconcilesFirstSeenRouteSourceThenPreservesKnownMetadata(t *testing.T) {
	var commands [][]string
	netstat := "Destination Gateway Flags Netif\n10.77.60.0/24 192.0.2.1 UG1 vtnet0\n"
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		switch path {
		case freeBSDNetstatPath:
			return []byte(netstat), nil
		case freeBSDIfconfigPath:
			return []byte("vtnet0:\n inet 192.0.2.10 netmask 0xffffff00\n"), nil
		default:
			return nil, nil
		}
	}
	s := newFreeBSDFIBSyncer(run)
	desired := []FIBRoute{{Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.1"}, PreferredSource: "192.0.2.10"}}
	if _, err := s.SyncBGP(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 4 || commands[2][2] != "delete" || commands[3][2] != "add" || commands[3][6] != "-ifa" {
		t.Fatalf("first-seen source reconciliation commands = %#v", commands)
	}
	commands = nil
	if _, err := s.SyncBGP(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0][0] != freeBSDNetstatPath || commands[1][0] != freeBSDIfconfigPath {
		t.Fatalf("known metadata caused route churn: %#v", commands)
	}
}

func TestFreeBSDFIBSyncerRecreatesWhenPreferredSourceChanges(t *testing.T) {
	var commands [][]string
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		switch path {
		case freeBSDNetstatPath:
			return []byte("Destination Gateway Flags Netif\n10.77.60.0/24 192.0.2.1 UG1 vtnet0\n"), nil
		case freeBSDIfconfigPath:
			return []byte("vtnet0:\n inet 192.0.2.10 netmask 0xffffff00\n inet 192.0.2.11 netmask 0xffffff00\n"), nil
		default:
			return nil, nil
		}
	}
	s := newFreeBSDFIBSyncer(run)
	s.installed["10.77.60.0/24"] = FIBRoute{Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.1"}, PreferredSource: "192.0.2.10"}
	s.sourceKnown["10.77.60.0/24"] = true
	if _, err := s.SyncBGP(context.Background(), []FIBRoute{{Prefix: "10.77.60.0/24", NextHops: []string{"192.0.2.1"}, PreferredSource: "192.0.2.11"}}); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 4 || commands[2][2] != "delete" || commands[3][2] != "add" {
		t.Fatalf("source change used route change: %#v", commands)
	}
}

func mustFreeBSDAddr(value string) netip.Addr     { return netip.MustParseAddr(value) }
func mustFreeBSDPrefix(value string) netip.Prefix { return netip.MustParsePrefix(value) }
