// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestRegistryDeclaresEveryConfigResourceKind(t *testing.T) {
	for _, resource := range api.ConfigResourceKinds() {
		if _, ok := Lookup(resource.APIVersion, resource.Kind); !ok {
			t.Errorf("missing lifecycle declaration for %s/%s", resource.APIVersion, resource.Kind)
		}
	}
}

func TestRegistryDoesNotDeclareUnknownConfigResourceKinds(t *testing.T) {
	known := map[string]bool{}
	for _, resource := range api.ConfigResourceKinds() {
		known[resource.APIVersion+"/"+resource.Kind] = true
	}
	for _, declaration := range AllDeclarations() {
		if !known[declaration.APIVersion+"/"+declaration.Kind] {
			t.Errorf("lifecycle declaration for unknown config resource kind %s/%s", declaration.APIVersion, declaration.Kind)
		}
	}
}

func TestRegistryDeclarationsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, declaration := range AllDeclarations() {
		key := declaration.APIVersion + "/" + declaration.Kind
		if seen[key] {
			t.Errorf("duplicate lifecycle declaration for %s", key)
		}
		seen[key] = true
		if declaration.Class == "" {
			t.Errorf("lifecycle declaration for %s has empty class", key)
		}
	}
}

func TestRegistryRequiresTeardownContractForManagedKinds(t *testing.T) {
	for _, declaration := range AllDeclarations() {
		switch declaration.Class {
		case ClassManagedHost, ClassController, ClassDynamicSource:
		default:
			continue
		}
		if len(declaration.ArtifactKinds) == 0 && declaration.TeardownLifecycle == "" && declaration.NoHostTeardownReason == "" {
			t.Errorf("lifecycle declaration for %s/%s has no teardown contract", declaration.APIVersion, declaration.Kind)
		}
	}
}

func TestRegistryNoHostTeardownContractsHaveReasons(t *testing.T) {
	for _, declaration := range AllDeclarations() {
		if declaration.NoHostTeardownReason == "" {
			continue
		}
		if declaration.TeardownLifecycle != "" || len(declaration.ArtifactKinds) > 0 {
			t.Errorf("lifecycle declaration for %s/%s mixes no-host teardown with host teardown contracts", declaration.APIVersion, declaration.Kind)
		}
	}
}

func TestRegistryArtifactKindsHaveTeardowns(t *testing.T) {
	known := map[string]bool{}
	for _, teardown := range ArtifactTeardownRegistry() {
		known[teardown.Kind] = true
	}
	for _, declaration := range AllDeclarations() {
		seen := map[string]bool{}
		for _, kind := range declaration.ArtifactKinds {
			if kind == "" {
				t.Errorf("lifecycle declaration for %s/%s has empty artifact kind", declaration.APIVersion, declaration.Kind)
				continue
			}
			if seen[kind] {
				t.Errorf("lifecycle declaration for %s/%s has duplicate artifact kind %s", declaration.APIVersion, declaration.Kind, kind)
			}
			seen[kind] = true
			if !known[kind] {
				t.Errorf("lifecycle declaration for %s/%s references artifact kind %s without teardown registry entry", declaration.APIVersion, declaration.Kind, kind)
			}
		}
	}
}

func TestOwnerKeyUsesStableObjectIdentity(t *testing.T) {
	got := OwnerKey(" net.routerd.net/v1alpha1 ", " BGPPeer ", " fabric ")
	if got != "net.routerd.net/v1alpha1/BGPPeer/fabric" {
		t.Fatalf("owner key = %q, want stable apiVersion/kind/name", got)
	}
}
