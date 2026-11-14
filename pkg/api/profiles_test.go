// SPDX-License-Identifier: BSD-3-Clause

package api

import "testing"

func TestEffectiveIPv6PDClient(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		want       string
	}{
		{name: "default", want: IPv6PDClientRouterd},
		{name: "configured wins", configured: IPv6PDClientNetworkd, want: IPv6PDClientNetworkd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveIPv6PDClient(tt.configured); got != tt.want {
				t.Fatalf("EffectiveIPv6PDClient() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchKnownIPv6PDNGCombinations(t *testing.T) {
	tests := []struct {
		name string
		ctx  IPv6PDClientContext
		want int
	}{
		{
			name: "freebsd dhcpcd ntt",
			ctx:  IPv6PDClientContext{OS: "freebsd", Client: IPv6PDClientDHCPCD, Profile: IPv6PDProfileNTTHGWLANPD},
			want: 1,
		},
		{
			name: "linux networkd ntt",
			ctx:  IPv6PDClientContext{OS: "linux", Client: IPv6PDClientNetworkd, Profile: IPv6PDProfileNTTNGNDirectHikariDenwa},
			want: 1,
		},
		{
			name: "linux dhcp6c ntt",
			ctx:  IPv6PDClientContext{OS: "linux", Client: IPv6PDClientDHCPv6C, Profile: IPv6PDProfileNTTHGWLANPD},
		},
		{
			name: "freebsd dhcpcd default",
			ctx:  IPv6PDClientContext{OS: "freebsd", Client: IPv6PDClientDHCPCD, Profile: IPv6PDProfileDefault},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(MatchKnownIPv6PDNGCombinations(tt.ctx)); got != tt.want {
				t.Fatalf("matches = %d, want %d", got, tt.want)
			}
		})
	}
}
