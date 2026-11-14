// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"
)

func TestCanonicalYAMLPreservesCommentsAndOrder(t *testing.T) {
	input := []byte(`# router owner note
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  # stable router identity
  name: comment-router
spec:
  resources:
    # resources stay where the operator put them
    []
`)
	got, err := CanonicalYAML(input)
	if err != nil {
		t.Fatalf("canonical yaml: %v", err)
	}
	out := string(got)
	for _, want := range []string{
		"# router owner note",
		"# stable router identity",
		"# resources stay where the operator put them",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("canonical output lost comment %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "apiVersion:") > strings.Index(out, "kind:") {
		t.Fatalf("canonical output reordered top-level fields:\n%s", out)
	}
}

func TestUpsertCandidateYAMLPreservesExistingComments(t *testing.T) {
	current := []byte(`# canonical header
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: mutation-router
spec:
  resources:
    # keep existing resource note
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata:
        name: old-hostname
      spec:
        hostname: old.example
`)
	candidate := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata:
        name: new-hostname
      spec:
        hostname: new.example
`)
	got, router, err := UpsertCandidateYAML(current, candidate, false)
	if err != nil {
		t.Fatalf("upsert candidate: %v", err)
	}
	out := string(got)
	for _, want := range []string{"# canonical header", "# keep existing resource note", "name: old-hostname", "name: new-hostname"} {
		if !strings.Contains(out, want) {
			t.Fatalf("upsert output missing %q:\n%s", want, out)
		}
	}
	if router.Metadata.Name != "mutation-router" || len(router.Spec.Resources) != 2 {
		t.Fatalf("router = name %q resources %d, want mutation-router/2", router.Metadata.Name, len(router.Spec.Resources))
	}
}

func TestDeleteResourceYAMLPreservesRemainingComments(t *testing.T) {
	current := []byte(`# canonical header
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: mutation-router
spec:
  resources:
    # delete this resource
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata:
        name: old-hostname
      spec:
        hostname: old.example
    # keep this resource
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata:
        name: new-hostname
      spec:
        hostname: new.example
`)
	got, router, removed, err := DeleteResourceYAML(current, MutationTarget{APIVersion: "net.routerd.net/v1alpha1", Kind: "Hostname", Name: "old-hostname"})
	if err != nil {
		t.Fatalf("delete resource: %v", err)
	}
	if !removed {
		t.Fatal("removed = false, want true")
	}
	out := string(got)
	if strings.Contains(out, "old-hostname") || strings.Contains(out, "# delete this resource") {
		t.Fatalf("delete output kept removed resource:\n%s", out)
	}
	for _, want := range []string{"# canonical header", "# keep this resource", "new-hostname"} {
		if !strings.Contains(out, want) {
			t.Fatalf("delete output missing %q:\n%s", want, out)
		}
	}
	if len(router.Spec.Resources) != 1 || router.Spec.Resources[0].Metadata.Name != "new-hostname" {
		t.Fatalf("router resources = %+v", router.Spec.Resources)
	}
}
