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
