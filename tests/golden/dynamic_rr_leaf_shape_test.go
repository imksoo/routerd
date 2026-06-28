// SPDX-License-Identifier: BSD-3-Clause

package golden_test

import (
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
)

func TestCloudEdgeDynamicRRLeafExamplesUseDualRRShape(t *testing.T) {
	rrA := loadExampleRouter(t, "cloudedge-dynamic-rr-a-hub.yaml")
	rrB := loadExampleRouter(t, "cloudedge-dynamic-rr-b-hub.yaml")
	leaf := loadExampleRouter(t, "cloudedge-dynamic-leaf-pve.yaml")

	for _, tt := range []struct {
		name   string
		router *api.Router
		self   string
	}{
		{name: "rr-a", router: rrA, self: "rr-a"},
		{name: "rr-b", router: rrB, self: "rr-b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertHasResource(t, tt.router, api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")
			assertHasResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentPolicy", "cloudedge-leaves")
			assertHasResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-pve")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "BGPPeer", "leaf-pve")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "BGPPeer", "leaf-azure")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "WireGuardInterface", "wg-hybrid")
			profile := mustResource(t, tt.router, api.MobilityAPIVersion, "SAMTransportProfile", tt.self)
			spec, err := profile.SAMTransportProfileSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.SelfNodeRef != tt.self {
				t.Fatalf("%s selfNodeRef = %q, want %q", tt.name, spec.SelfNodeRef, tt.self)
			}
			if spec.Mode != "ipip" || spec.Encryption != "none" {
				t.Fatalf("%s transport = %s/%s, want ipip/none", tt.name, spec.Mode, spec.Encryption)
			}
			if len(spec.PeersFrom) != 1 || spec.PeersFrom[0].Resource != "SAMEnrollmentPolicy/cloudedge-leaves" {
				t.Fatalf("%s transport peersFrom = %#v, want SAMEnrollmentPolicy/cloudedge-leaves", tt.name, spec.PeersFrom)
			}
		})
	}

	assertHasResource(t, leaf, api.MobilityAPIVersion, "SAMRRSet", "cloudedge-rrs")
	assertHasResource(t, leaf, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-pve")
	assertMissingResource(t, leaf, api.NetAPIVersion, "WireGuardInterface", "wg-hybrid")
	assertMissingResource(t, leaf, api.NetAPIVersion, "WireGuardPeer", "aws-rr-a")
	assertMissingResource(t, leaf, api.NetAPIVersion, "WireGuardPeer", "aws-rr-b")
	assertMissingResource(t, leaf, api.NetAPIVersion, "BGPPeer", "aws-rr-a")
	assertMissingResource(t, leaf, api.NetAPIVersion, "BGPPeer", "aws-rr-b")
	profile := mustResource(t, leaf, api.MobilityAPIVersion, "SAMTransportProfile", "leaf-pve")
	profileSpec, err := profile.SAMTransportProfileSpec()
	if err != nil {
		t.Fatal(err)
	}
	if profileSpec.Mode != "ipip" || profileSpec.Encryption != "none" {
		t.Fatalf("leaf transport = %s/%s, want ipip/none", profileSpec.Mode, profileSpec.Encryption)
	}
	if len(profileSpec.PeersFrom) != 1 || profileSpec.PeersFrom[0].Resource != "SAMRRSet/cloudedge-rrs" {
		t.Fatalf("leaf transport peersFrom = %#v, want SAMRRSet/cloudedge-rrs", profileSpec.PeersFrom)
	}
	assertMissingResource(t, leaf, api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")
}

func loadExampleRouter(t *testing.T, name string) *api.Router {
	t.Helper()
	router, err := config.Load(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("validate %s: %v", name, err)
	}
	return router
}

func assertHasResource(t *testing.T, router *api.Router, apiVersion, kind, name string) {
	t.Helper()
	_ = mustResource(t, router, apiVersion, kind, name)
}

func assertMissingResource(t *testing.T, router *api.Router, apiVersion, kind, name string) {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			t.Fatalf("unexpected %s/%s in %s", kind, name, router.Metadata.Name)
		}
	}
}

func mustResource(t *testing.T, router *api.Router, apiVersion, kind, name string) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("missing %s/%s in %s", kind, name, router.Metadata.Name)
	return api.Resource{}
}
