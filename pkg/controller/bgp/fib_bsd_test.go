// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
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
2001:db8:79::/64                  fe80::1%vtnet0               UG1   vtnet0
10.77.60.0/24                     192.0.2.1                    UG1   vtnet0
`, false)
	want := map[string]FIBRoute{
		"2001:db8:77::/64": {Prefix: "2001:db8:77::/64", NextHops: []string{"2001:db8:1::1", "2001:db8:1::2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owned IPv6 routes = %#v, want %#v", got, want)
	}
}
