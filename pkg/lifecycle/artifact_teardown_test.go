// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resource"
)

func TestArtifactTeardownRegistryEligibility(t *testing.T) {
	tests := []struct {
		name     string
		artifact resource.Artifact
		want     bool
	}{
		{
			name: "routerd nft table",
			artifact: resource.Artifact{
				Kind:       "nft.table",
				Attributes: map[string]string{"family": "inet", "name": "routerd_nat"},
			},
			want: true,
		},
		{
			name: "foreign nft table",
			artifact: resource.Artifact{
				Kind:       "nft.table",
				Attributes: map[string]string{"family": "inet", "name": "other_nat"},
			},
		},
		{
			name: "pppoe peer file",
			artifact: resource.Artifact{
				Kind:  "file",
				Name:  "/etc/ppp/peers/../peers/routerd-old",
				Owner: api.NetAPIVersion + "/PPPoESession/old",
			},
			want: true,
		},
		{
			name: "pppoe secrets file not cleaned",
			artifact: resource.Artifact{
				Kind:  "file",
				Name:  "/etc/ppp/chap-secrets",
				Owner: api.NetAPIVersion + "/PPPoESession/old",
			},
		},
		{
			name: "static IPv6 address",
			artifact: resource.Artifact{
				Kind:  "net.ipv6.address",
				Name:  "ens19:2001:db8::1/64",
				Owner: api.NetAPIVersion + "/VirtualAddress/old-v6",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArtifactCleanupEligible(tt.artifact); got != tt.want {
				t.Fatalf("ArtifactCleanupEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}
