// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"strings"
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

func TestMobilityPoolRejectsRemovedMemberDeliveryFields(t *testing.T) {
	var resource Resource
	err := yaml.Unmarshal([]byte(`
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: {name: pool}
spec:
  prefix: 10.77.60.0/24
  groupRef: cloudedge
  members:
    - nodeRef: edge-a
      delivery:
        peerRef: onprem
`), &resource)
	if err == nil {
		t.Fatal("MobilityPool accepted removed member delivery configuration")
	}
	if !strings.Contains(err.Error(), "members[0].delivery is not supported") {
		t.Fatalf("unmarshal error = %v", err)
	}
}

func TestMobilityPoolRejectsTopologyAndUnknownMemberFields(t *testing.T) {
	for _, test := range []struct {
		field string
		value string
	}{
		{field: "site", value: "cloud-a"},
		{field: "role", value: "cloud"},
		{field: "placement", value: "{group: cloud-a, priority: 10}"},
		{field: "maintenance", value: "{drain: true}"},
		{field: "maxSecondaryIPs", value: "4"},
		{field: "eventEndpoint", value: "https://edge-a.example.test/events"},
		{field: "samEndpoint", value: "10.0.0.1"},
		{field: "macAddresses", value: "[02:00:00:00:00:01]"},
		{field: "routeReflector", value: "true"},
		{field: "wireGuard", value: "{publicKey: key}"},
		{field: "unknown", value: "value"},
	} {
		t.Run(test.field, func(t *testing.T) {
			var resource Resource
			err := yaml.Unmarshal([]byte(`
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: {name: pool}
spec:
  prefix: 10.77.60.0/24
  groupRef: cloudedge
  members:
    - nodeRef: edge-a
      `+test.field+`: `+test.value+`
`), &resource)
			if err == nil {
				t.Fatalf("MobilityPool accepted members[0].%s", test.field)
			}
			if !strings.Contains(err.Error(), "members[0]."+test.field+" is not supported") {
				t.Fatalf("unmarshal error = %v", err)
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

func TestMobilityPoolRejectsRemovedPolicyFields(t *testing.T) {
	for _, field := range []string{"mode", "capturePolicy", "authority", "ipOwnershipPolicy", "deliveryPolicy", "publishMemberSet"} {
		t.Run(field, func(t *testing.T) {
			var resource Resource
			err := yaml.Unmarshal([]byte(`
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: {name: pool}
spec:
  prefix: 10.77.60.0/24
  groupRef: cloudedge
  `+field+`: {}
  members:
    - nodeRef: edge-a
`), &resource)
			if err == nil {
				t.Fatalf("MobilityPool accepted removed %s configuration", field)
			}
			if !strings.Contains(err.Error(), field+" is not supported") {
				t.Fatalf("unmarshal error = %v", err)
			}
		})
	}
}

func TestSAMEnrollmentPolicyRejectsRemovedRevokeAfterInactive(t *testing.T) {
	var resource Resource
	err := yaml.Unmarshal([]byte(`
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMEnrollmentPolicy
metadata: {name: leaves}
spec:
  transportProfileRef: SAMTransportProfile/leaves
  tunnelAddressPrefixes: [10.255.0.0/20]
  revokeAfterInactive: 168h
`), &resource)
	if err == nil {
		t.Fatal("SAMEnrollmentPolicy accepted removed revokeAfterInactive")
	}
	if !strings.Contains(err.Error(), "spec.revokeAfterInactive is not supported") {
		t.Fatalf("unmarshal error = %v", err)
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
