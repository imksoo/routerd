package api

import "testing"

func TestEffectiveIPv6PDClient(t *testing.T) {
	tests := []struct {
		name       string
		osName     string
		nixOS      bool
		profile    string
		configured string
		want       string
	}{
		{name: "freebsd default", osName: "freebsd", want: IPv6PDClientDHCP6C},
		{name: "linux default profile", osName: "linux", profile: IPv6PDProfileDefault, want: IPv6PDClientNetworkd},
		{name: "linux ntt profile", osName: "linux", profile: IPv6PDProfileNTTHGWLANPD, want: IPv6PDClientDHCPCD},
		{name: "nixos ntt profile", osName: "linux", nixOS: true, profile: IPv6PDProfileNTTHGWLANPD, want: IPv6PDClientDHCPCD},
		{name: "configured wins", osName: "linux", nixOS: true, profile: IPv6PDProfileNTTHGWLANPD, configured: IPv6PDClientNetworkd, want: IPv6PDClientNetworkd},
		{name: "unknown os", osName: "plan9", want: IPv6PDClientNetworkd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveIPv6PDClient(tt.osName, tt.nixOS, tt.profile, tt.configured); got != tt.want {
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
			ctx:  IPv6PDClientContext{OS: "linux", Client: IPv6PDClientDHCP6C, Profile: IPv6PDProfileNTTHGWLANPD},
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
