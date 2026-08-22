// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConfigSchemaDescribesTunnelInterfacePeerAddress(t *testing.T) {
	changeToRepositoryRoot(t)
	encoded, err := json.Marshal(configSchema())
	if err != nil {
		t.Fatal(err)
	}
	got := string(encoded)
	for _, want := range []string{
		`"peerAddress":`,
		`PeerAddress is the explicit IPv4 inner destination`,
		`FreeBSD and rejected on Linux; routerd never derives it from the CIDR.`,
		`"kind":{"const":"TunnelInterface"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated config schema is missing %q", want)
		}
	}
}

func TestConfigSchemaRequiresMobilityPoolTopology(t *testing.T) {
	changeToRepositoryRoot(t)
	encoded, err := json.Marshal(configSchema())
	if err != nil {
		t.Fatal(err)
	}
	got := string(encoded)
	for _, want := range []string{
		`"membersFrom":{"`,
		`"minItems":1`,
		`"required":["prefix","groupRef","membersFrom"]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated MobilityPool schema is missing %q", want)
		}
	}
}

func TestControlSchemasCoverSAMEnrollmentTopology(t *testing.T) {
	changeToRepositoryRoot(t)
	control, err := json.Marshal(controlSchema())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\"required\":[\"apiVersion\",\"kind\",\"claim\"]",
		"\"required\":[\"apiVersion\",\"kind\",\"accepted\",\"claimRef\",\"dynamicSource\",\"generation\",\"observedAt\",\"expiresAt\"]",
		"\"required\":[\"apiVersion\",\"kind\",\"name\"]",
		"\"required\":[\"apiVersion\",\"kind\",\"revoked\",\"claimRef\",\"dynamicSource\",\"generation\",\"observedAt\",\"expiresAt\"]",
		"\"required\":[\"name\",\"claimRef\"]",
		"\"required\":[\"apiVersion\",\"kind\",\"metadata\",\"claimDigest\",\"rrSet\"]",
		"\"peerGroup\"",
	} {
		if !strings.Contains(string(control), want) {
			t.Fatalf("generated control schema is missing %q", want)
		}
	}

	openAPI, err := json.Marshal(controlOpenAPISchema())
	if err != nil {
		t.Fatal(err)
	}
	got := string(openAPI)
	for _, want := range []string{
		"\"/api/control.routerd.net/v1alpha1/sam-enrollment-claims\"",
		"\"/api/control.routerd.net/v1alpha1/sam-enrollment-claims/{name}/revoke\"",
		"\"/api/control.routerd.net/v1alpha1/sam-enrollment-topologies/{name}\"",
		"\"operationId\":\"submitSAMEnrollmentClaim\"",
		"\"operationId\":\"revokeSAMEnrollmentClaim\"",
		"\"operationId\":\"getSAMEnrollmentTopology\"",
		"\"name\":\"claim\"",
		"\"name\":\"claimDigest\"",
		"\"name\":\"claimIdentityDigest\"",
		"\"SAMEnrollmentClaimSubmitRequest\"",
		"\"SAMEnrollmentClaimSubmitResult\"",
		"\"SAMEnrollmentClaimRevokeRequest\"",
		"\"SAMEnrollmentClaimRevokeResult\"",
		"\"SAMEnrollmentTopologyGetRequest\"",
		"\"SAMEnrollmentTopologyGetResult\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated control OpenAPI schema is missing %q", want)
		}
	}
	if strings.Contains(got, "\"/api/control.routerd.net/v1alpha1/sam-rrsets/") {
		t.Fatal("generated control OpenAPI schema must not expose the legacy SAM RRSet route")
	}
}

func changeToRepositoryRoot(t *testing.T) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
}
