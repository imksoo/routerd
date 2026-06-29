// SPDX-License-Identifier: BSD-3-Clause

package golden_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestCloudEdgeDynamicRRLeafExamplesUseDualRRShape(t *testing.T) {
	rrA := loadExampleRouter(t, "cloudedge-dynamic-rr-a-hub.yaml")
	rrB := loadExampleRouter(t, "cloudedge-dynamic-rr-b-hub.yaml")
	leaf := loadExampleRouter(t, "cloudedge-dynamic-leaf-pve.yaml")
	leafA := loadExampleRouter(t, "cloudedge-dynamic-leaf-a-wg.yaml")
	leafB := loadExampleRouter(t, "cloudedge-dynamic-leaf-b-fou.yaml")
	seed := loadFixtureRouter(t, "cloudedge-rr-claims-seed.yaml")

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
			assertHasResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentPolicy", "cloudedge-public-wg-leaves")
			assertHasResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentPolicy", "cloudedge-private-fou-leaves")
			assertMissingResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-pve")
			assertMissingResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-a")
			assertMissingResource(t, tt.router, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-b")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "BGPPeer", "leaf-pve")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "BGPPeer", "leaf-a")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "BGPPeer", "leaf-b")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "BGPPeer", "leaf-azure")
			assertMissingResource(t, tt.router, api.NetAPIVersion, "WireGuardInterface", "wg-hybrid")
			dynamicPeer := mustResource(t, tt.router, api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")
			dynamicSpec, err := dynamicPeer.BGPDynamicPeerSpec()
			if err != nil {
				t.Fatal(err)
			}
			assertStringSet(t, tt.name+" dynamic source prefixes", dynamicSpec.Listen.SourcePrefixes, []string{"10.255.0.0/20"})
			assertStringSet(t, tt.name+" dynamic import prefixes", dynamicSpec.ImportPolicy.AllowedPrefixes, []string{"10.77.60.0/24"})
			assertStringSet(t, tt.name+" dynamic export prefixes", dynamicSpec.ExportPolicy.AllowedPrefixes, []string{"10.77.60.0/24"})
			assertNoPrefixes(t, tt.name+" dynamic import prefixes", dynamicSpec.ImportPolicy.AllowedPrefixes, "0.0.0.0/0", "10.10.0.0/24")
			assertNoPrefixes(t, tt.name+" dynamic export prefixes", dynamicSpec.ExportPolicy.AllowedPrefixes, "0.0.0.0/0", "10.10.0.0/24")
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
			assertRRAdmissionTransport(t, tt.router, tt.self+"-wg", "ipip", "wireguard", "SAMEnrollmentPolicy/cloudedge-public-wg-leaves", false)
			assertRRAdmissionTransport(t, tt.router, tt.self+"-fou", "fou", "none", "SAMEnrollmentPolicy/cloudedge-private-fou-leaves", true)
			assertEnrollmentPolicy(t, tt.router, "cloudedge-public-wg-leaves", tt.self+"-wg", "cloudedge-public-underlay", true)
			assertEnrollmentPolicy(t, tt.router, "cloudedge-private-fou-leaves", tt.self+"-fou", "cloudedge-private-underlay", false)
		})
	}
	assertHasResource(t, seed, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-pve")
	assertHasResource(t, seed, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-a")
	assertHasResource(t, seed, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-b")

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
	assertLeafBGPRouterPolicy(t, leaf, "leaf-pve", "10.77.60.21/32")
	assertMissingResource(t, leaf, api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")

	t.Run("leaf-a wireguard path consumes both RRs", func(t *testing.T) {
		assertHasResource(t, leafA, api.MobilityAPIVersion, "SAMRRSet", "cloudedge-rrs")
		assertHasResource(t, leafA, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-a")
		assertHasResource(t, leafA, api.NetAPIVersion, "WireGuardInterface", "wg-cloudedge")
		assertMissingResource(t, leafA, api.NetAPIVersion, "BGPPeer", "rr-a")
		assertMissingResource(t, leafA, api.NetAPIVersion, "BGPPeer", "rr-b")
		assertMissingResource(t, leafA, api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")

		wg := mustResource(t, leafA, api.NetAPIVersion, "WireGuardInterface", "wg-cloudedge")
		wgSpec, err := wg.WireGuardInterfaceSpec()
		if err != nil {
			t.Fatal(err)
		}
		if len(wgSpec.PeersFrom) != 1 || wgSpec.PeersFrom[0].Resource != "SAMRRSet/cloudedge-rrs" {
			t.Fatalf("leaf-a wireguard peersFrom = %#v, want SAMRRSet/cloudedge-rrs", wgSpec.PeersFrom)
		}

		profile := mustResource(t, leafA, api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
		spec, err := profile.SAMTransportProfileSpec()
		if err != nil {
			t.Fatal(err)
		}
		if spec.Mode != "ipip" || spec.Encryption != "wireguard" {
			t.Fatalf("leaf-a transport = %s/%s, want ipip/wireguard", spec.Mode, spec.Encryption)
		}
		if len(spec.PeersFrom) != 1 || spec.PeersFrom[0].Resource != "SAMRRSet/cloudedge-rrs" {
			t.Fatalf("leaf-a transport peersFrom = %#v, want SAMRRSet/cloudedge-rrs", spec.PeersFrom)
		}
		assertLeafBGPRouterPolicy(t, leafA, "leaf-a", "10.77.60.31/32")
		assertRRSetMembers(t, leafA, "rr-a", "rr-b")
	})

	t.Run("leaf-b fou path consumes both RRs without wireguard", func(t *testing.T) {
		assertHasResource(t, leafB, api.MobilityAPIVersion, "SAMRRSet", "cloudedge-rrs")
		assertHasResource(t, leafB, api.MobilityAPIVersion, "SAMEnrollmentClaim", "leaf-b")
		assertMissingResource(t, leafB, api.NetAPIVersion, "WireGuardInterface", "wg-cloudedge")
		assertMissingResource(t, leafB, api.NetAPIVersion, "WireGuardPeer", "rr-a")
		assertMissingResource(t, leafB, api.NetAPIVersion, "WireGuardPeer", "rr-b")
		assertMissingResource(t, leafB, api.NetAPIVersion, "BGPPeer", "rr-a")
		assertMissingResource(t, leafB, api.NetAPIVersion, "BGPPeer", "rr-b")
		assertMissingResource(t, leafB, api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")

		profile := mustResource(t, leafB, api.MobilityAPIVersion, "SAMTransportProfile", "leaf-b")
		spec, err := profile.SAMTransportProfileSpec()
		if err != nil {
			t.Fatal(err)
		}
		if spec.Mode != "fou" || spec.Encryption != "none" {
			t.Fatalf("leaf-b transport = %s/%s, want fou/none", spec.Mode, spec.Encryption)
		}
		if spec.EncapSport != 5555 || spec.EncapDport != 5555 {
			t.Fatalf("leaf-b encap ports = %d/%d, want 5555/5555", spec.EncapSport, spec.EncapDport)
		}
		if len(spec.PeersFrom) != 1 || spec.PeersFrom[0].Resource != "SAMRRSet/cloudedge-rrs" {
			t.Fatalf("leaf-b transport peersFrom = %#v, want SAMRRSet/cloudedge-rrs", spec.PeersFrom)
		}
		assertLeafBGPRouterPolicy(t, leafB, "leaf-b", "10.77.60.32/32")
		assertRRSetMembers(t, leafB, "rr-a", "rr-b")
	})
}

func TestPVEMinimalDynamicRRLeafExamples(t *testing.T) {
	rr := loadExampleRouter(t, "pve-minimal-rr.yaml")
	leafA := loadExampleRouter(t, "pve-minimal-leaf-a-wg.yaml")
	leafB := loadExampleRouter(t, "pve-minimal-leaf-b-fou.yaml")
	fetchedRRSet := loadFixtureRouter(t, "pve-minimal-leaf-rrset-fetched.yaml")

	assertHasResource(t, rr, api.NetAPIVersion, "BGPDynamicPeer", "pve-leaves")
	assertHasResource(t, rr, api.MobilityAPIVersion, "SAMEnrollmentPolicy", "pve-wg-leaves")
	assertHasResource(t, rr, api.MobilityAPIVersion, "SAMEnrollmentPolicy", "pve-fou-leaves")
	assertMissingResource(t, rr, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-a")
	assertMissingResource(t, rr, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-b")
	assertMissingResource(t, rr, api.NetAPIVersion, "BGPPeer", "pve-leaf-a")
	assertMissingResource(t, rr, api.NetAPIVersion, "BGPPeer", "pve-leaf-b")
	seed := loadFixtureRouter(t, "pve-minimal-rr-claims-seed.yaml")
	assertHasResource(t, seed, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-a")
	assertHasResource(t, seed, api.MobilityAPIVersion, "SAMEnrollmentClaim", "pve-leaf-b")

	dynamicPeer := mustResource(t, rr, api.NetAPIVersion, "BGPDynamicPeer", "pve-leaves")
	dynamicSpec, err := dynamicPeer.BGPDynamicPeerSpec()
	if err != nil {
		t.Fatal(err)
	}
	assertStringSet(t, "pve dynamic source prefixes", dynamicSpec.Listen.SourcePrefixes, []string{"10.255.10.0/24"})
	assertStringSet(t, "pve dynamic import prefixes", dynamicSpec.ImportPolicy.AllowedPrefixes, []string{"10.77.70.0/24"})
	if dynamicSpec.ImportPolicy.AllowedPrefixLengthMin != 32 || dynamicSpec.ImportPolicy.AllowedPrefixLengthMax != 32 {
		t.Fatalf("pve dynamic import prefix lengths = %d/%d, want 32/32", dynamicSpec.ImportPolicy.AllowedPrefixLengthMin, dynamicSpec.ImportPolicy.AllowedPrefixLengthMax)
	}

	assertRRAdmissionTransport(t, rr, "pve-rr-wg", "ipip", "wireguard", "SAMEnrollmentPolicy/pve-wg-leaves", false)
	assertRRAdmissionTransport(t, rr, "pve-rr-fou", "fou", "none", "SAMEnrollmentPolicy/pve-fou-leaves", true)

	t.Run("leaf-a wireguard ipip consumes pve rr", func(t *testing.T) {
		assertHasResource(t, leafA, api.NetAPIVersion, "WireGuardInterface", "wg-pve")
		assertHasResource(t, leafA, api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
		assertMissingResource(t, leafA, api.MobilityAPIVersion, "SAMRRSet", "pve-rrs")
		assertHasResource(t, fetchedRRSet, api.MobilityAPIVersion, "SAMRRSet", "pve-rrs")
		assertMissingResource(t, leafA, api.NetAPIVersion, "BGPDynamicPeer", "pve-leaves")
		assertMissingResource(t, leafA, api.NetAPIVersion, "BGPPeer", "pve-rr")
		profile := mustResource(t, leafA, api.MobilityAPIVersion, "SAMTransportProfile", "pve-leaf-a")
		spec, err := profile.SAMTransportProfileSpec()
		if err != nil {
			t.Fatal(err)
		}
		if spec.Mode != "ipip" || spec.Encryption != "wireguard" {
			t.Fatalf("pve-leaf-a transport = %s/%s, want ipip/wireguard", spec.Mode, spec.Encryption)
		}
		if len(spec.PeersFrom) != 1 || spec.PeersFrom[0].Resource != "SAMRRSet/pve-rrs" {
			t.Fatalf("pve-leaf-a peersFrom = %#v, want SAMRRSet/pve-rrs", spec.PeersFrom)
		}
		assertNamedRRSetMembers(t, fetchedRRSet, "pve-rrs", "pve-rr")
		assertLeafBGPRouterPolicy(t, leafA, "pve-leaf-a", "10.77.70.21/32")
	})

	t.Run("leaf-b fou consumes pve rr without wireguard", func(t *testing.T) {
		assertHasResource(t, leafB, api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-b")
		assertMissingResource(t, leafB, api.MobilityAPIVersion, "SAMRRSet", "pve-rrs")
		assertHasResource(t, fetchedRRSet, api.MobilityAPIVersion, "SAMRRSet", "pve-rrs")
		assertMissingResource(t, leafB, api.NetAPIVersion, "WireGuardInterface", "wg-pve")
		assertMissingResource(t, leafB, api.NetAPIVersion, "WireGuardPeer", "pve-rr")
		assertMissingResource(t, leafB, api.NetAPIVersion, "BGPDynamicPeer", "pve-leaves")
		assertMissingResource(t, leafB, api.NetAPIVersion, "BGPPeer", "pve-rr")
		profile := mustResource(t, leafB, api.MobilityAPIVersion, "SAMTransportProfile", "pve-leaf-b")
		spec, err := profile.SAMTransportProfileSpec()
		if err != nil {
			t.Fatal(err)
		}
		if spec.Mode != "fou" || spec.Encryption != "none" {
			t.Fatalf("pve-leaf-b transport = %s/%s, want fou/none", spec.Mode, spec.Encryption)
		}
		if spec.EncapSport != 5555 || spec.EncapDport != 5555 {
			t.Fatalf("pve-leaf-b encap ports = %d/%d, want 5555/5555", spec.EncapSport, spec.EncapDport)
		}
		if len(spec.PeersFrom) != 1 || spec.PeersFrom[0].Resource != "SAMRRSet/pve-rrs" {
			t.Fatalf("pve-leaf-b peersFrom = %#v, want SAMRRSet/pve-rrs", spec.PeersFrom)
		}
		assertNamedRRSetMembers(t, fetchedRRSet, "pve-rrs", "pve-rr")
		assertLeafBGPRouterPolicy(t, leafB, "pve-leaf-b", "10.77.70.22/32")
	})
}

func TestDynamicRRLeafRunbookDocumentsLeafRRSetFetch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "operations", "dynamic-rr-leaf-enrollment-test.md"))
	if err != nil {
		t.Fatalf("read dynamic RR/leaf runbook: %v", err)
	}
	doc := string(data)
	for _, want := range []string{
		"Leaf-Side RRSet Fetch",
		"routerctl mobility enrollment-join",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("runbook missing %q", want)
		}
	}
}

func loadFixtureRouter(t *testing.T, name string) *api.Router {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var router api.Router
	if err := yaml.Unmarshal(data, &router); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return &router
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

func assertRRAdmissionTransport(t *testing.T, router *api.Router, name, mode, encryption, peersFrom string, wantFOU bool) {
	t.Helper()
	resource := mustResource(t, router, api.MobilityAPIVersion, "SAMTransportProfile", name)
	spec, err := resource.SAMTransportProfileSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Mode != mode || spec.Encryption != encryption {
		t.Fatalf("SAMTransportProfile/%s transport = %s/%s, want %s/%s", name, spec.Mode, spec.Encryption, mode, encryption)
	}
	if len(spec.PeersFrom) != 1 || spec.PeersFrom[0].Resource != peersFrom {
		t.Fatalf("SAMTransportProfile/%s peersFrom = %#v, want %s", name, spec.PeersFrom, peersFrom)
	}
	if spec.BGP.GeneratePeers == nil || *spec.BGP.GeneratePeers {
		t.Fatalf("SAMTransportProfile/%s generatePeers = %#v, want false for RR dynamic admission", name, spec.BGP.GeneratePeers)
	}
	if wantFOU && (spec.EncapSport != 5555 || spec.EncapDport != 5555) {
		t.Fatalf("SAMTransportProfile/%s encap ports = %d/%d, want 5555/5555", name, spec.EncapSport, spec.EncapDport)
	}
}

func assertEnrollmentPolicy(t *testing.T, router *api.Router, name, profile, audience string, wantWireGuard bool) {
	t.Helper()
	resource := mustResource(t, router, api.MobilityAPIVersion, "SAMEnrollmentPolicy", name)
	spec, err := resource.SAMEnrollmentPolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.TransportProfileRef != "SAMTransportProfile/"+profile {
		t.Fatalf("SAMEnrollmentPolicy/%s transportProfileRef = %q, want SAMTransportProfile/%s", name, spec.TransportProfileRef, profile)
	}
	if spec.JoinAudience != audience {
		t.Fatalf("SAMEnrollmentPolicy/%s joinAudience = %q, want %q", name, spec.JoinAudience, audience)
	}
	if (spec.WireGuard.Interface != "") != wantWireGuard {
		t.Fatalf("SAMEnrollmentPolicy/%s wireGuard = %#v, want present=%v", name, spec.WireGuard, wantWireGuard)
	}
	assertStringSet(t, "SAMEnrollmentPolicy/"+name+" endpoint prefixes", spec.EndpointPrefixes, []string{"10.20.0.0/24"})
	assertStringSet(t, "SAMEnrollmentPolicy/"+name+" tunnel prefixes", spec.TunnelAddressPrefixes, []string{"10.255.0.0/20"})
}

func assertRRSetMembers(t *testing.T, router *api.Router, want ...string) {
	t.Helper()
	assertNamedRRSetMembers(t, router, "cloudedge-rrs", want...)
}

func assertNamedRRSetMembers(t *testing.T, router *api.Router, name string, want ...string) {
	t.Helper()
	resource := mustResource(t, router, api.MobilityAPIVersion, "SAMRRSet", name)
	spec, err := resource.SAMRRSetSpec()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, member := range spec.Members {
		got[member.NodeRef] = true
	}
	for _, nodeRef := range want {
		if !got[nodeRef] {
			t.Fatalf("SAMRRSet/%s missing member %s: %#v", name, nodeRef, spec.Members)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("SAMRRSet/%s member count = %d, want %d: %#v", name, len(got), len(want), spec.Members)
	}
}

func assertLeafBGPRouterPolicy(t *testing.T, router *api.Router, leafName, ownedPrefix string) {
	t.Helper()
	resource := mustResource(t, router, api.NetAPIVersion, "BGPRouter", "mobility-bgp")
	spec, err := resource.BGPRouterSpec()
	if err != nil {
		t.Fatal(err)
	}
	assertStringSet(t, leafName+" BGP export prefixes", spec.ExportPolicy.AllowedPrefixes, []string{ownedPrefix})
	assertStringSet(t, leafName+" BGP connected redistribute prefixes", spec.Redistribute.Connected.AllowedPrefixes, []string{ownedPrefix})
	assertNoPrefixes(t, leafName+" BGP export prefixes", spec.ExportPolicy.AllowedPrefixes, "0.0.0.0/0", "10.10.0.0/24", "10.20.0.0/24")
	assertNoPrefixes(t, leafName+" BGP connected redistribute prefixes", spec.Redistribute.Connected.AllowedPrefixes, "0.0.0.0/0", "10.10.0.0/24", "10.20.0.0/24")
}

func assertStringSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		if seen[value] != 1 {
			t.Fatalf("%s = %#v, want %#v", label, got, want)
		}
	}
}

func assertNoPrefixes(t *testing.T, label string, got []string, forbidden ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range forbidden {
		if seen[value] {
			t.Fatalf("%s contains forbidden prefix %s: %#v", label, value, got)
		}
	}
}
