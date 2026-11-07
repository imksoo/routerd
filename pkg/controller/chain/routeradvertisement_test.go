// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestBuildDeprecatedPrefixRouterAdvertisement(t *testing.T) {
	spec := api.IPv6RouterAdvertisementSpec{
		MFlag:         true,
		OFlag:         true,
		PRFPreference: "low",
		ValidLifetime: "7200",
	}
	prefixes := []deprecatedPrefixInformation{{
		Prefix:        netip.MustParsePrefix("2001:db8:1241::99/64"),
		ValidLifetime: 3601,
	}}

	got := buildDeprecatedPrefixRouterAdvertisement(spec, prefixes)
	want, err := hex.DecodeString("8600000040d81c200000000000000000" +
		"030440c000000e11000000000000000020010db8124100000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("RA packet = %x, want %x", got, want)
	}
	if got[3] != 0 || got[8] != 0 || got[12] != 0 {
		t.Fatalf("checksum, reachable time, and retrans timer must remain zero: %x", got)
	}
}

func TestRouterAdvertisementLifetimeSafelyFitsUint16(t *testing.T) {
	for _, test := range []struct {
		value string
		want  uint16
	}{
		{value: "", want: defaultRouterAdvertisementLifetime},
		{value: "invalid", want: defaultRouterAdvertisementLifetime},
		{value: "-1", want: defaultRouterAdvertisementLifetime},
		{value: "0", want: 0},
		{value: "7200", want: 7200},
		{value: "65535", want: 65535},
		{value: "65536", want: 65535},
		{value: "18446744073709551615", want: 65535},
	} {
		if got := routerAdvertisementLifetime(test.value); got != test.want {
			t.Errorf("routerAdvertisementLifetime(%q) = %d, want %d", test.value, got, test.want)
		}
	}
}
