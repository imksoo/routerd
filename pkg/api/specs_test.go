package api

import "testing"

func TestIPv6PDProfileDefaults(t *testing.T) {
	tests := []struct {
		name             string
		profile          string
		wantPrefixLength int
		wantDUIDType     string
		wantHint         bool
	}{
		{
			name:             "default",
			profile:          IPv6PDProfileDefault,
			wantPrefixLength: 0,
			wantHint:         true,
		},
		{
			name:             "NTT HGW LAN PD",
			profile:          IPv6PDProfileNTTHGWLANPD,
			wantPrefixLength: 60,
			wantDUIDType:     "link-layer",
			wantHint:         false,
		},
		{
			name:             "NTT NGN direct Hikari Denwa",
			profile:          IPv6PDProfileNTTNGNDirectHikariDenwa,
			wantPrefixLength: 60,
			wantDUIDType:     "link-layer",
			wantHint:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveIPv6PDPrefixLength(tt.profile, 0); got != tt.wantPrefixLength {
				t.Fatalf("prefix length = %d, want %d", got, tt.wantPrefixLength)
			}
			if got := EffectiveIPv6PDDUIDType(tt.profile, ""); got != tt.wantDUIDType {
				t.Fatalf("DUID type = %q, want %q", got, tt.wantDUIDType)
			}
			if got := ShouldRenderIPv6PDPrefixHint(tt.profile); got != tt.wantHint {
				t.Fatalf("render prefix hint = %t, want %t", got, tt.wantHint)
			}
		})
	}
}

func TestIPv6PDProfileConfiguredValuesOverrideDefaults(t *testing.T) {
	if got := EffectiveIPv6PDPrefixLength(IPv6PDProfileNTTHGWLANPD, 56); got != 56 {
		t.Fatalf("prefix length = %d, want 56", got)
	}
	if got := EffectiveIPv6PDDUIDType(IPv6PDProfileNTTHGWLANPD, "uuid"); got != "uuid" {
		t.Fatalf("DUID type = %q, want uuid", got)
	}
}
