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

func TestParseFreeBSDOwnedIPv6BGPRoutesPreservesMultipathAndSkipsForeign(t *testing.T) {
	got := parseFreeBSDOwnedBGPRoutesForFamily(`Internet6:
Destination                       Gateway                       Flags Netif Expire
2001:db8:77::/64                  2001:db8:1::1                UG1   vtnet0
2001:db8:77::/64                  2001:db8:1::2                UG1   vtnet1
2001:db8:78::/64                  2001:db8:1::3                UG    vtnet0
10.77.60.0/24                     192.0.2.1                    UG1   vtnet0
`, false)
	want := map[string]FIBRoute{
		"2001:db8:77::/64": {Prefix: "2001:db8:77::/64", NextHops: []string{"2001:db8:1::1", "2001:db8:1::2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owned IPv6 routes = %#v, want %#v", got, want)
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

func TestParseFreeBSDLocalIPv6AddressesSkipsScopedLinkLocal(t *testing.T) {
	got := parseFreeBSDLocalIPv6Addresses(`vtnet0: flags=8843<UP,RUNNING>
        inet6 2001:db8:1::10 prefixlen 64
        inet6 fe80::1%vtnet0 prefixlen 64 scopeid 0x1
`)
	want := []freeBSDLocalAddress{{Address: mustFreeBSDAddr("2001:db8:1::10"), Prefix: mustFreeBSDPrefix("2001:db8:1::/64")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local IPv6 addresses = %#v, want %#v", got, want)
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
		{freeBSDNetstatPath, "-rn", "-f", "inet6"},
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
	if len(commands) != 3 || commands[0][0] != freeBSDNetstatPath || commands[1][0] != freeBSDNetstatPath || commands[2][0] != freeBSDIfconfigPath {
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
		{freeBSDNetstatPath, "-rn", "-f", "inet6"},
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
	if len(commands) != 4 || commands[3][2] != "add" {
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
	if len(commands) != 5 {
		t.Fatalf("commands = %#v, want inet/inet6 netstat, one ifconfig, and two adds", commands)
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
	if len(commands) != 5 || commands[3][2] != "delete" || commands[4][2] != "add" || commands[4][6] != "-ifa" {
		t.Fatalf("first-seen source reconciliation commands = %#v", commands)
	}
	commands = nil
	if _, err := s.SyncBGP(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 || commands[0][0] != freeBSDNetstatPath || commands[1][0] != freeBSDNetstatPath || commands[2][0] != freeBSDIfconfigPath {
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
	if len(commands) != 5 || commands[3][2] != "delete" || commands[4][2] != "add" {
		t.Fatalf("source change used route change: %#v", commands)
	}
}

func TestFreeBSDFIBSyncerIPv6RestartReplaceWithdrawAndPreservesForeign(t *testing.T) {
	var commands [][]string
	inet6 := `Destination Gateway Flags Netif
2001:db8:77::/64 2001:db8:1::1 UG1 vtnet0
2001:db8:88::/64 2001:db8:1::9 UG vtnet0
`
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		switch {
		case path == freeBSDNetstatPath && len(args) == 3 && args[2] == "inet":
			return []byte("Destination Gateway Flags Netif\n"), nil
		case path == freeBSDNetstatPath && len(args) == 3 && args[2] == "inet6":
			return []byte(inet6), nil
		case path == freeBSDIfconfigPath:
			return []byte("vtnet0:\n inet6 2001:db8:1::10 prefixlen 64\n"), nil
		default:
			return nil, nil
		}
	}

	// A new syncer must rebuild the first observed owned route because netstat
	// cannot report its route IFA; it must not touch the unmarked foreign route.
	s := newFreeBSDFIBSyncer(run)
	desired := FIBRoute{Prefix: "2001:db8:77::/64", NextHops: []string{"2001:db8:1::2"}, PreferredSource: "2001:db8:1::10"}
	result, err := s.SyncBGP(context.Background(), []FIBRoute{desired})
	if err != nil || !result.Installed[desired.Prefix] {
		t.Fatalf("IPv6 restart replace = %#v, %v", result, err)
	}
	wantReplace := [][]string{
		{freeBSDNetstatPath, "-rn", "-f", "inet"},
		{freeBSDNetstatPath, "-rn", "-f", "inet6"},
		{freeBSDIfconfigPath, "-a"},
		{freeBSDRoutePath, "-n", "delete", "-inet6", "-proto1", "-net", "2001:db8:77::/64", "2001:db8:1::1"},
		{freeBSDRoutePath, "-n", "add", "-inet6", "-proto1", "-net", "2001:db8:77::/64", "-ifa", "2001:db8:1::10", "2001:db8:1::2"},
	}
	if !reflect.DeepEqual(commands, wantReplace) {
		t.Fatalf("IPv6 replace commands = %#v, want %#v", commands, wantReplace)
	}

	commands = nil
	inet6 = `Destination Gateway Flags Netif
2001:db8:77::/64 2001:db8:1::2 UG1 vtnet0
2001:db8:88::/64 2001:db8:1::9 UG vtnet0
`
	if _, err := s.SyncBGP(context.Background(), nil); err != nil {
		t.Fatalf("IPv6 withdraw: %v", err)
	}
	wantWithdraw := [][]string{
		{freeBSDNetstatPath, "-rn", "-f", "inet"},
		{freeBSDNetstatPath, "-rn", "-f", "inet6"},
		{freeBSDIfconfigPath, "-a"},
		{freeBSDRoutePath, "-n", "delete", "-inet6", "-proto1", "-net", "2001:db8:77::/64", "-ifa", "2001:db8:1::10", "2001:db8:1::2"},
	}
	if !reflect.DeepEqual(commands, wantWithdraw) {
		t.Fatalf("IPv6 withdraw commands = %#v, want %#v", commands, wantWithdraw)
	}
}

func TestFreeBSDFIBSyncerIPv6MultipathRetainAndRejectsScopedNextHop(t *testing.T) {
	var commands [][]string
	inet6 := "Destination Gateway Flags Netif\n"
	run := func(_ context.Context, path string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{path}, args...))
		switch {
		case path == freeBSDNetstatPath && args[2] == "inet":
			return []byte("Destination Gateway Flags Netif\n"), nil
		case path == freeBSDNetstatPath && args[2] == "inet6":
			return []byte(inet6), nil
		case path == freeBSDIfconfigPath:
			return []byte("vtnet0:\n inet6 2001:db8:1::10 prefixlen 64\n"), nil
		default:
			return nil, nil
		}
	}
	s := newFreeBSDFIBSyncer(run)
	bad, err := s.SyncBGP(context.Background(), []FIBRoute{{Prefix: "2001:db8:79::/64", NextHops: []string{"fe80::1%vtnet0"}}})
	if err != nil || bad.Unsupported["2001:db8:79::/64"] != "GoBGPFIBRouteUnsupported" {
		t.Fatalf("scoped next hop = %#v, %v", bad, err)
	}
	commands = nil
	route := FIBRoute{Prefix: "2001:db8:79::/64", NextHops: []string{"2001:db8:1::2", "2001:db8:1::3"}, RetainOnMissing: true}
	if result, err := s.SyncBGP(context.Background(), []FIBRoute{route}); err != nil || !result.Installed[route.Prefix] {
		t.Fatalf("IPv6 multipath install = %#v, %v", result, err)
	}
	if got := len(commands); got != 5 || commands[3][2] != "add" || commands[4][2] != "add" {
		t.Fatalf("IPv6 multipath commands = %#v", commands)
	}
	inet6 = "Destination Gateway Flags Netif\n2001:db8:79::/64 2001:db8:1::2 UG1 vtnet0\n2001:db8:79::/64 2001:db8:1::3 UG1 vtnet1\n"
	commands = nil
	retained, err := s.SyncBGP(context.Background(), nil)
	if err != nil || !retained.Retained[route.Prefix] || len(commands) != 3 {
		t.Fatalf("IPv6 retain = %#v, commands=%#v, err=%v", retained, commands, err)
	}
	if _, err := s.SyncBGP(context.Background(), []FIBRoute{{Prefix: route.Prefix, NextHops: route.NextHops}}); err != nil {
		t.Fatalf("clear IPv6 retain: %v", err)
	}
	commands = nil
	if _, err := s.SyncBGP(context.Background(), nil); err != nil {
		t.Fatalf("withdraw cleared IPv6 retain: %v", err)
	}
	if len(commands) != 5 || commands[3][2] != "delete" || commands[4][2] != "delete" {
		t.Fatalf("withdraw IPv6 multipath commands = %#v", commands)
	}
}

func mustFreeBSDAddr(value string) netip.Addr     { return netip.MustParseAddr(value) }
func mustFreeBSDPrefix(value string) netip.Prefix { return netip.MustParsePrefix(value) }
