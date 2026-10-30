// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"encoding/json"
	"testing"
)

func TestCloneOwnsNestedConfiguration(t *testing.T) {
	enabled := true
	original := Router{Metadata: ObjectMeta{Annotations: map[string]string{"source": "operator"}}, Spec: RouterSpec{
		Apply: ApplyPolicySpec{ProtectedInterfaces: []string{"mgmt"}},
		Resources: []Resource{{TypeMeta: TypeMeta{Kind: "WireGuardInterface"}, Metadata: ObjectMeta{OwnerRefs: []OwnerRef{{Name: "parent"}}}, Spec: WireGuardInterfaceSpec{MTU: 1420}},
			{TypeMeta: TypeMeta{Kind: "EgressRoutePolicy"}, Spec: EgressRoutePolicySpec{Candidates: []EgressRoutePolicyCandidate{{Name: "wan", Enabled: &enabled, Targets: []EgressRoutePolicyTarget{{Name: "primary"}}}}}},
			{TypeMeta: TypeMeta{Kind: "SAMPeerGroup"}, Spec: SAMPeerGroupSpec{OwnedPrefixesByNode: map[string][]string{"edge": {"192.0.2.0/24"}}}},
		},
	}}
	cloned := original.Clone()
	cloned.Metadata.Annotations["source"] = "runtime"
	cloned.Spec.Apply.ProtectedInterfaces[0] = "other"
	cloned.Spec.Resources[0].Metadata.OwnerRefs[0].Name = "other"
	policy, err := cloned.Spec.Resources[1].EgressRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	policy.Candidates[0].Targets[0].Name = "backup"
	*policy.Candidates[0].Enabled = false
	nodes, err := cloned.Spec.Resources[2].SAMPeerGroupSpec()
	if err != nil {
		t.Fatal(err)
	}
	nodes.OwnedPrefixesByNode["edge"][0] = "198.51.100.0/24"
	oldPolicy, _ := original.Spec.Resources[1].EgressRoutePolicySpec()
	oldNodes, _ := original.Spec.Resources[2].SAMPeerGroupSpec()
	if original.Metadata.Annotations["source"] != "operator" || original.Spec.Apply.ProtectedInterfaces[0] != "mgmt" || original.Spec.Resources[0].Metadata.OwnerRefs[0].Name != "parent" || oldPolicy.Candidates[0].Targets[0].Name != "primary" || !*oldPolicy.Candidates[0].Enabled || oldNodes.OwnedPrefixesByNode["edge"][0] != "192.0.2.0/24" {
		t.Fatal("cloned configuration aliases operator-owned mutable data")
	}
}

func TestJSONResourceDecodesConcreteSpecAtBoundary(t *testing.T) {
	var resource Resource
	if err := json.Unmarshal([]byte(`{"apiVersion":"net.routerd.net/v1alpha1","kind":"DNSZone","metadata":{"name":"home"},"spec":{"zone":"example.test","records":[{"hostname":"router","ipv4":"192.0.2.1"}]}}`), &resource); err != nil {
		t.Fatal(err)
	}
	spec, err := resource.DNSZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Zone != "example.test" || len(spec.Records) != 1 {
		t.Fatalf("decoded spec=%#v", spec)
	}
}
