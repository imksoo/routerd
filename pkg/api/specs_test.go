// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIPsecConnectionUsesCanonicalPhaseProposalFieldsAndAcceptsLegacyAlias(t *testing.T) {
	for name, field := range map[string]string{
		"canonical": "phase1Proposals",
		"legacy":    "psPhase1Proposals",
	} {
		t.Run(name, func(t *testing.T) {
			var resource Resource
			err := yaml.Unmarshal([]byte("apiVersion: net.routerd.net/v1alpha1\nkind: IPsecConnection\nmetadata: {name: ipsec}\nspec:\n  localAddress: 198.18.10.1\n  remoteAddress: 198.18.10.2\n  preSharedKey: disposable\n  "+field+": [invalid-proposal]\n  leftSubnet: 10.0.0.0/24\n  rightSubnet: 10.1.0.0/24\n"), &resource)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			spec, err := resource.IPsecConnectionSpec()
			if err != nil || len(spec.Phase1Proposals) != 1 || spec.Phase1Proposals[0] != "invalid-proposal" {
				t.Fatalf("proposal decode = %#v, err=%v", spec.Phase1Proposals, err)
			}
		})
	}
}

func TestMobilityDeliveryPolicyDoesNotExposeGratuitousARPOnSeize(t *testing.T) {
	if _, ok := reflect.TypeOf(MobilityDeliveryPolicy{}).FieldByName("GratuitousARPOnSeize"); ok {
		t.Fatalf("MobilityDeliveryPolicy exposes unreachable GratuitousARPOnSeize field")
	}
}

func TestMobilityDeliveryPolicyDoesNotExposeConntrackCleanupOnSeize(t *testing.T) {
	if _, ok := reflect.TypeOf(MobilityDeliveryPolicy{}).FieldByName("ConntrackCleanupOnSeize"); ok {
		t.Fatalf("MobilityDeliveryPolicy exposes no-op ConntrackCleanupOnSeize field")
	}
}

func TestIPv6PDProfileDefaults(t *testing.T) {
	tests := []struct {
		name             string
		profile          string
		wantPrefixLength int
		wantDUIDType     string
	}{
		{
			name:             "default",
			profile:          IPv6PDProfileDefault,
			wantPrefixLength: 0,
		},
		{
			name:             "NTT HGW LAN PD",
			profile:          IPv6PDProfileNTTHGWLANPD,
			wantPrefixLength: 60,
			wantDUIDType:     "link-layer",
		},
		{
			name:             "NTT NGN direct Hikari Denwa",
			profile:          IPv6PDProfileNTTNGNDirectHikariDenwa,
			wantPrefixLength: 60,
			wantDUIDType:     "link-layer",
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
