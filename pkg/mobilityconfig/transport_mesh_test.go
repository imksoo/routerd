// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSAMTransportMeshFingerprintCanonicalizesTransportDefaults(t *testing.T) {
	base := api.SAMTransportProfileSpec{
		Mode:           "ipip",
		Encryption:     "none",
		InnerPrefix:    "10.255.1.0/24",
		AddressingMode: "pair-stable",
		BGP:            api.SAMTransportBGPProfileSpec{PeerASN: 64512},
	}
	alias := base
	alias.Encryption = ""
	alias.InnerPrefix = "10.255.1.5/24"
	if got, want := SAMTransportMeshFingerprint(alias), SAMTransportMeshFingerprint(base); got != want {
		t.Fatalf("semantic transport fingerprint = %q, want %q", got, want)
	}

	other := base
	other.InnerPrefix = "10.255.2.0/24"
	if SAMTransportMeshFingerprint(other) == SAMTransportMeshFingerprint(base) {
		t.Fatal("different inner transport prefix unexpectedly shares a mesh fingerprint")
	}
}
