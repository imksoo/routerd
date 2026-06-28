// SPDX-License-Identifier: BSD-3-Clause

package samenrollment

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestJoinCanonicalPayloadSortsClaimsAndKeepsWireGuardOptional(t *testing.T) {
	claim := api.SAMEnrollmentClaimSpec{
		PolicyRef:     " SAMEnrollmentPolicy/cloudedge-leaves ",
		RRSetRef:      "SAMRRSet/cloudedge-rrs",
		LeafID:        " leaf-a ",
		JoinAudience:  "cloudedge-public-underlay",
		JoinNonce:     "nonce-1",
		JoinTimestamp: "2026-06-28T00:00:00Z",
		TunnelAddress: "10.255.0.31/32",
		Endpoint:      "198.51.100.31",
		Mobility: api.SAMEnrollmentClaimMobilitySpec{
			OwnedAddresses: []string{"10.77.60.31/32", "10.77.60.30/32"},
		},
		BGP: api.SAMEnrollmentClaimBGPSpec{
			ASN:      64577,
			RouterID: "10.255.0.31",
		},
		WireGuard: api.SAMEnrollmentClaimWireGuardSpec{
			PublicKey:           "LEAF_A_WIREGUARD_PUBLIC_KEY",
			Endpoint:            "198.51.100.31:51820",
			AllowedIPs:          []string{"10.255.0.31/32", "10.20.0.31/32"},
			PersistentKeepalive: 25,
		},
	}

	want := strings.Join([]string{
		"policyRef=SAMEnrollmentPolicy/cloudedge-leaves",
		"rrSetRef=SAMRRSet/cloudedge-rrs",
		"leafID=leaf-a",
		"joinAudience=cloudedge-public-underlay",
		"joinNonce=nonce-1",
		"joinTimestamp=2026-06-28T00:00:00Z",
		"tunnelAddress=10.255.0.31/32",
		"endpoint=198.51.100.31",
		"mobility.ownedAddresses=10.77.60.30/32,10.77.60.31/32",
		"bgp.asn=64577",
		"bgp.routerID=10.255.0.31",
		"wireGuard.publicKey=LEAF_A_WIREGUARD_PUBLIC_KEY",
		"wireGuard.endpoint=198.51.100.31:51820",
		"wireGuard.allowedIPs=10.20.0.31/32,10.255.0.31/32",
		"wireGuard.persistentKeepalive=25",
	}, "\n")

	if got := JoinCanonicalPayload(claim); got != want {
		t.Fatalf("canonical payload:\n%s\nwant:\n%s", got, want)
	}

	claim.WireGuard = api.SAMEnrollmentClaimWireGuardSpec{}
	payload := JoinCanonicalPayload(claim)
	for _, wantEmpty := range []string{
		"wireGuard.publicKey=",
		"wireGuard.endpoint=",
		"wireGuard.allowedIPs=",
		"wireGuard.persistentKeepalive=0",
	} {
		if !strings.Contains(payload, wantEmpty) {
			t.Fatalf("non-WG payload missing %q:\n%s", wantEmpty, payload)
		}
	}
}

func TestJoinHMACChangesWithReplayFields(t *testing.T) {
	claim := api.SAMEnrollmentClaimSpec{
		PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
		RRSetRef:      "SAMRRSet/cloudedge-rrs",
		LeafID:        "leaf-b",
		JoinAudience:  "cloudedge-private-underlay",
		JoinNonce:     "nonce-1",
		JoinTimestamp: "2026-06-28T00:00:00Z",
		TunnelAddress: "10.255.0.32/32",
		Endpoint:      "10.20.0.32",
		Mobility:      api.SAMEnrollmentClaimMobilitySpec{OwnedAddresses: []string{"10.77.60.32/32"}},
		BGP:           api.SAMEnrollmentClaimBGPSpec{ASN: 64577, RouterID: "10.255.0.32"},
	}

	first := JoinHMAC([]byte("test-join-token"), claim)
	if len(first) != 64 {
		t.Fatalf("HMAC length = %d, want 64 hex chars: %q", len(first), first)
	}
	if got := JoinHMAC([]byte("test-join-token"), claim); got != first {
		t.Fatalf("HMAC is not stable: %q != %q", got, first)
	}
	claim.JoinNonce = "nonce-2"
	if got := JoinHMAC([]byte("test-join-token"), claim); got == first {
		t.Fatalf("HMAC did not change after nonce changed: %q", got)
	}
	claim.JoinNonce = "nonce-1"
	claim.JoinTimestamp = "2026-06-28T00:01:00Z"
	if got := JoinHMAC([]byte("test-join-token"), claim); got == first {
		t.Fatalf("HMAC did not change after timestamp changed: %q", got)
	}
}

func TestJoinHMACExcludesAdminOwnedExpiryAndRevocation(t *testing.T) {
	claim := api.SAMEnrollmentClaimSpec{
		PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
		RRSetRef:      "SAMRRSet/cloudedge-rrs",
		LeafID:        "leaf-b",
		JoinAudience:  "cloudedge-private-underlay",
		JoinNonce:     "nonce-1",
		JoinTimestamp: "2026-06-28T00:00:00Z",
		TunnelAddress: "10.255.0.32/32",
		Endpoint:      "10.20.0.32",
		Mobility:      api.SAMEnrollmentClaimMobilitySpec{OwnedAddresses: []string{"10.77.60.32/32"}},
		BGP:           api.SAMEnrollmentClaimBGPSpec{ASN: 64577, RouterID: "10.255.0.32"},
	}
	first := JoinHMAC([]byte("test-join-token"), claim)

	claim.ExpiresAt = "2026-06-28T00:10:00Z"
	claim.Revoked = true
	if got := JoinHMAC([]byte("test-join-token"), claim); got != first {
		t.Fatalf("HMAC changed after admin-owned expiresAt/revoked changed: %q != %q", got, first)
	}
	payload := JoinCanonicalPayload(claim)
	if strings.Contains(payload, "expiresAt=") || strings.Contains(payload, "revoked=") {
		t.Fatalf("canonical payload includes admin-owned expiry/revocation fields:\n%s", payload)
	}
}
