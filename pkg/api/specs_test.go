// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIPsecConnectionUsesCanonicalPhaseProposalFields(t *testing.T) {
	for name, field := range map[string]string{
		"canonical": "phase1Proposals",
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

func TestMobilityPoolDecodesOnlyLocalMemberOverlay(t *testing.T) {
	var resource Resource
	err := yaml.Unmarshal([]byte(`
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: {name: pool}
spec:
  prefix: 10.77.60.0/24
  groupRef: cloudedge
  membersFrom:
    - resource: SAMNodeSet/cloudedge
  members:
    - nodeRef: edge-a
      profileRef: cloud-self
      capture: {type: provider-secondary-ip, providerRef: aws, nicRef: eni-a}
      staticOwnedAddresses: [10.77.60.10/32]
      ownershipDiscovery: {mode: provider-private-ip}
`), &resource)
	if err != nil {
		t.Fatalf("unmarshal local overlay: %v", err)
	}
	spec, err := resource.MobilityPoolSpec()
	if err != nil {
		t.Fatalf("MobilityPoolSpec: %v", err)
	}
	if len(spec.MembersFrom) != 1 || spec.MembersFrom[0].Resource != "SAMNodeSet/cloudedge" {
		t.Fatalf("membersFrom = %#v", spec.MembersFrom)
	}
	if len(spec.Members) != 1 || spec.Members[0].NodeRef != "edge-a" || spec.Members[0].Capture.NICRef != "eni-a" {
		t.Fatalf("members = %#v", spec.Members)
	}
}

func TestIPv6PDProfileDefaults(t *testing.T) {
	tests := []struct {
		name             string
		profile          string
		wantPrefixLength int
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
		},
		{
			name:             "NTT NGN direct Hikari Denwa",
			profile:          IPv6PDProfileNTTNGNDirectHikariDenwa,
			wantPrefixLength: 60,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveIPv6PDPrefixLength(tt.profile, 0); got != tt.wantPrefixLength {
				t.Fatalf("prefix length = %d, want %d", got, tt.wantPrefixLength)
			}
		})
	}
}

func TestIPv6PDProfileConfiguredValuesOverrideDefaults(t *testing.T) {
	if got := EffectiveIPv6PDPrefixLength(IPv6PDProfileNTTHGWLANPD, 56); got != 56 {
		t.Fatalf("prefix length = %d, want 56", got)
	}
}
