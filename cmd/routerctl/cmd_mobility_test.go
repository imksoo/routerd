// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/config"
)

func TestMobilityEnrollmentHMACCommand(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretPath, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	configPath := filepath.Join("..", "..", "examples", "cloudedge-dynamic-leaf-pve.yaml")
	var stdout, stderr bytes.Buffer
	if err := mobilityCommand([]string{"enrollment-hmac", "--config", configPath, "--claim", "leaf-pve", "--secret-file", secretPath}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility enrollment-hmac: %v stderr=%s", err, stderr.String())
	}
	hmacValue := strings.TrimSpace(stdout.String())
	if hmacValue == "" || strings.Contains(hmacValue, "EXAMPLE") {
		t.Fatalf("unexpected hmac output %q", hmacValue)
	}

	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load example: %v", err)
	}
	for i := range router.Spec.Resources {
		resource := &router.Spec.Resources[i]
		switch resource.Kind {
		case "SAMEnrollmentPolicy":
			spec, err := resource.SAMEnrollmentPolicySpec()
			if err != nil {
				t.Fatalf("%s spec: %v", resource.ID(), err)
			}
			spec.JoinTokenFrom.File = secretPath
			resource.Spec = spec
		case "SAMEnrollmentClaim":
			spec, err := resource.SAMEnrollmentClaimSpec()
			if err != nil {
				t.Fatalf("%s spec: %v", resource.ID(), err)
			}
			stdout.Reset()
			stderr.Reset()
			if err := mobilityCommand([]string{"enrollment-hmac", "--config", configPath, "--claim", resource.Metadata.Name, "--secret-file", secretPath}, &stdout, &stderr); err != nil {
				t.Fatalf("mobility enrollment-hmac %s: %v stderr=%s", resource.Metadata.Name, err, stderr.String())
			}
			spec.JoinHMAC = strings.TrimSpace(stdout.String())
			resource.Spec = spec
		}
	}
	rendered, err := yaml.Marshal(router)
	if err != nil {
		t.Fatalf("Marshal candidate: %v", err)
	}
	candidate := filepath.Join(t.TempDir(), "rr-a.yaml")
	if err := os.WriteFile(candidate, rendered, 0o600); err != nil {
		t.Fatalf("WriteFile candidate: %v", err)
	}
	router, err = config.Load(candidate)
	if err != nil {
		t.Fatalf("Load candidate: %v", err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("Validate candidate with generated HMAC: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := mobilityCommand([]string{"enrollment-hmac", "--config", configPath, "--claim", "leaf-pve", "--secret-file", secretPath, "--show-payload"}, &stdout, &stderr); err != nil {
		t.Fatalf("mobility enrollment-hmac --show-payload: %v stderr=%s", err, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "leafID=leaf-pve") || !strings.Contains(out, hmacValue) {
		t.Fatalf("show-payload output missing payload or hmac:\n%s", out)
	}
}

func TestMobilityLeafConfigCommandGeneratesValidConfig(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "join-token")
	if err := os.WriteFile(secretPath, []byte("test-join-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{
		"leaf-config",
		"--leaf-id", "pve-leaf-b",
		"--underlay-ifname", "vmbr0",
		"--underlay-address", "10.30.0.22/24",
		"--local-endpoint", "10.30.0.22",
		"--endpoint-prefix", "10.30.0.0/24",
		"--inner-prefix", "10.255.10.0/24",
		"--tunnel-address", "10.255.10.22/32",
		"--mobility-prefix", "10.77.70.0/24",
		"--owned-address", "10.77.70.22/32",
		"--rr-set", "pve-rrs",
		"--direct-peer-group", "pve-fou-direct-leaves",
		"--rr-local-preference", "110",
		"--direct-local-preference", "240",
		"--policy", "pve-fou-leaves",
		"--join-audience", "pve-private-underlay",
		"--join-nonce", "pve-leaf-b-0001",
		"--join-timestamp", "2026-06-28T00:00:00Z",
		"--bootstrap-endpoint", "https://10.30.0.10:65432",
		"--bootstrap-endpoint", "https://10.30.0.11:65432",
		"--control-api-token-file", "/usr/local/etc/routerd/secrets/control-api-token",
		"--control-api-ca-file", "/usr/local/etc/routerd/secrets/rr-ca.pem",
		"--control-api-client-cert-file", "/usr/local/etc/routerd/secrets/leaf.crt",
		"--control-api-client-key-file", "/usr/local/etc/routerd/secrets/leaf.key",
		"--secret-file", secretPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("mobility leaf-config: %v stderr=%s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "EXAMPLE_HMAC") {
		t.Fatalf("leaf-config should compute joinHMAC when --secret-file is supplied:\n%s", stdout.String())
	}
	router, err := config.LoadBytes(stdout.Bytes(), "generated-leaf.yaml")
	if err != nil {
		t.Fatalf("LoadBytes generated config: %v\n%s", err, stdout.String())
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("Validate generated config: %v\n%s", err, stdout.String())
	}
	claim, err := mobilityEnrollmentClaim(router, "pve-leaf-b")
	if err != nil {
		t.Fatalf("generated claim: %v", err)
	}
	if claim.JoinHMAC == "" || claim.JoinHMAC == "EXAMPLE_HMAC_SHA256_HEX" {
		t.Fatalf("claim.JoinHMAC = %q", claim.JoinHMAC)
	}
	if claim.TunnelAddress != "10.255.10.22/32" || !claim.DirectMesh || len(claim.Mobility.OwnedAddresses) != 1 || claim.Mobility.OwnedAddresses[0] != "10.77.70.22/32" {
		t.Fatalf("claim = %#v", claim)
	}
	var transport api.SAMTransportProfileSpec
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" || resource.Metadata.Name != "pve-leaf-b" {
			continue
		}
		var err error
		transport, err = resource.SAMTransportProfileSpec()
		if err != nil {
			t.Fatalf("SAMTransportProfile spec: %v", err)
		}
	}
	if len(transport.PeersFrom) != 2 || transport.PeersFrom[0].Resource != "SAMRRSet/pve-rrs" || transport.PeersFrom[1].Resource != "SAMPeerGroup/pve-fou-direct-leaves" || !transport.PeersFrom[1].Direct {
		t.Fatalf("generated direct transport peersFrom = %#v", transport.PeersFrom)
	}
	if transport.BGP.ImportPolicy.LocalPreference != 110 || transport.BGP.DirectLocalPreference != 240 {
		t.Fatalf("generated direct transport preferences = %#v", transport.BGP)
	}
	if got, want := transport.BGP.ImportPolicy.NextHopRewrite, "peer-address"; got != want {
		t.Fatalf("generated RR import next-hop rewrite = %q, want %q", got, want)
	}
	var bgpRouter api.BGPRouterSpec
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" || resource.Metadata.Name != "mobility-bgp" {
			continue
		}
		var err error
		bgpRouter, err = resource.BGPRouterSpec()
		if err != nil {
			t.Fatalf("BGPRouter spec: %v", err)
		}
	}
	if expected := bgpstate.MobilityNodeIdentityCommunity("pve-leaf-b"); !bgpstate.HasCommunity(bgpRouter.Communities.Set.Out, expected) {
		t.Fatalf("generated BGPRouter outbound communities = %#v, want direct leaf identity %q", bgpRouter.Communities.Set.Out, expected)
	}
	var foundClient bool
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClient" || resource.Metadata.Name != "pve-leaf-b" {
			continue
		}
		foundClient = true
		spec, err := resource.SAMEnrollmentClientSpec()
		if err != nil {
			t.Fatalf("SAMEnrollmentClientSpec: %v", err)
		}
		if len(spec.BootstrapEndpoints) != 2 || spec.ControlAPITokenFrom.File == "" || spec.ControlAPITLS.CAFile == "" || spec.ControlAPITLS.CertFile == "" || spec.ControlAPITLS.KeyFile == "" {
			t.Fatalf("SAMEnrollmentClient spec = %#v", spec)
		}
	}
	if !foundClient {
		t.Fatal("generated config missing SAMEnrollmentClient/pve-leaf-b")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion {
			switch resource.Kind {
			case "SAMEnrollmentPolicy", "SAMNodeSet", "MobilityPool":
				t.Fatalf("generated leaf must not contain %s: %s", resource.Kind, resource.ID())
			}
		}
		if resource.APIVersion == api.FederationAPIVersion && resource.Kind == "EventGroup" {
			t.Fatalf("generated leaf must not contain EventGroup: %s", resource.ID())
		}
	}
}

// A direct-mesh leaf joins before it owns a mobility address in the normal PVE
// rollout. The generator must preserve that valid empty-ownership claim while
// emitting neither a local service address nor any BGP export candidate.
func TestMobilityLeafConfigCommandAllowsDirectLeafWithoutOwnedAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{
		"leaf-config",
		"--leaf-id", "pve-rt01",
		"--underlay-ifname", "vmbr0",
		"--underlay-address", "10.20.0.21/24",
		"--local-endpoint", "10.20.0.21",
		"--endpoint-prefix", "10.20.0.0/24",
		"--inner-prefix", "10.255.10.0/24",
		"--tunnel-address", "10.255.10.21/32",
		"--mobility-prefix", "10.77.60.0/24",
		"--rr-set", "svnet1-rrs",
		"--direct-peer-group", "svnet1-direct-leaves",
		"--policy", "svnet1-leaves",
		"--join-audience", "svnet1-underlay",
		"--join-nonce", "pve-rt01-0001",
		"--join-timestamp", "2026-08-22T16:00:00Z",
		"--bootstrap-endpoint", "https://10.20.0.2:65432",
		"--bootstrap-endpoint", "https://10.20.0.3:65432",
		"--mode", "ipip",
		"--secret", "test-join-token",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("mobility leaf-config without owned address: %v stderr=%s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "EXAMPLE_HMAC") {
		t.Fatalf("leaf-config should compute joinHMAC when --secret is supplied:\n%s", stdout.String())
	}
	router, err := config.LoadBytes(stdout.Bytes(), "generated-empty-owner-leaf.yaml")
	if err != nil {
		t.Fatalf("LoadBytes generated config: %v\n%s", err, stdout.String())
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("Validate generated config: %v\n%s", err, stdout.String())
	}
	claim, err := mobilityEnrollmentClaim(router, "pve-rt01")
	if err != nil {
		t.Fatalf("generated claim: %v", err)
	}
	if !claim.DirectMesh || len(claim.Mobility.OwnedAddresses) != 0 {
		t.Fatalf("empty-owner direct claim = %#v", claim)
	}

	var foundRouter, foundTransport bool
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv4StaticAddress" && resource.Metadata.Name == "owned-service-ip" {
			t.Fatalf("empty-owner leaf must not generate owned-service-ip: %#v", resource)
		}
		switch {
		case resource.APIVersion == api.NetAPIVersion && resource.Kind == "BGPRouter" && resource.Metadata.Name == "mobility-bgp":
			foundRouter = true
			spec, err := resource.BGPRouterSpec()
			if err != nil {
				t.Fatalf("BGPRouter spec: %v", err)
			}
			if len(spec.ExportPolicy.AllowedPrefixes) != 0 || len(spec.Redistribute.Connected.AllowedPrefixes) != 0 {
				t.Fatalf("empty-owner BGP export = %#v, want no route advertisement", spec)
			}
		case resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMTransportProfile" && resource.Metadata.Name == "pve-rt01":
			foundTransport = true
			spec, err := resource.SAMTransportProfileSpec()
			if err != nil {
				t.Fatalf("SAMTransportProfile spec: %v", err)
			}
			if len(spec.BGP.ExportPolicy.AllowedPrefixes) != 0 {
				t.Fatalf("empty-owner transport export = %#v, want no route advertisement", spec.BGP.ExportPolicy)
			}
			if len(spec.PeersFrom) != 2 || spec.PeersFrom[0].Resource != "SAMRRSet/svnet1-rrs" || spec.PeersFrom[1].Resource != "SAMPeerGroup/svnet1-direct-leaves" || !spec.PeersFrom[1].Direct {
				t.Fatalf("empty-owner direct transport peersFrom = %#v", spec.PeersFrom)
			}
		}
	}
	if !foundRouter || !foundTransport {
		t.Fatalf("generated empty-owner leaf missing BGP router or transport: %#v", router.Spec.Resources)
	}
}

func TestMobilityLeafConfigCommandRejectsMissingRequiredInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := mobilityCommand([]string{"leaf-config", "--leaf-id", "leaf-a"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "mobility leaf-config requires --") {
		t.Fatalf("leaf-config error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestMobilityGeneratedLeafConfigRejectsNonPreferredDirectPath(t *testing.T) {
	_, err := mobilityGeneratedLeafConfig(mobilityLeafConfigOptions{
		LeafID:                "leaf-a",
		UnderlayIfName:        "eth0",
		UnderlayAddress:       "10.30.0.2/24",
		LocalEndpoint:         "10.30.0.2",
		EndpointPrefix:        "10.30.0.0/24",
		InnerPrefix:           "10.255.10.0/24",
		TunnelAddress:         "10.255.10.2/32",
		MobilityPrefix:        "10.77.70.0/24",
		OwnedAddress:          "10.77.70.2/32",
		RRSet:                 "rrs",
		DirectPeerGroup:       "direct-leaves",
		Policy:                "leaves",
		JoinAudience:          "underlay",
		BootstrapEndpoints:    []string{"https://10.30.0.10:65432"},
		BGPASN:                64577,
		Mode:                  "ipip",
		RRLocalPreference:     200,
		DirectLocalPreference: 200,
	})
	if err == nil || !strings.Contains(err.Error(), "--direct-local-preference greater than --rr-local-preference") {
		t.Fatalf("mobilityGeneratedLeafConfig error = %v", err)
	}
}

func TestMobilityLeafConfigCommandRejectsRemovedPoolFlags(t *testing.T) {
	for _, flagName := range []string{"--mobility-pool", "--mobility-pool-prefix", "--site", "--role"} {
		t.Run(flagName, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := mobilityCommand([]string{"leaf-config", flagName, "removed"}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
				t.Fatalf("leaf-config %s error = %v stdout=%s stderr=%s", flagName, err, stdout.String(), stderr.String())
			}
		})
	}
}
