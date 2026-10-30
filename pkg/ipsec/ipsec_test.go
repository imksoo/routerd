// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestRenderSwanctl(t *testing.T) {
	data, err := RenderSwanctl("aws-a", api.IPsecConnectionSpec{
		LocalAddress:    "198.51.100.10",
		RemoteAddress:   "203.0.113.20",
		PreSharedKey:    "secret",
		LeftSubnet:      "10.0.0.0/24",
		RightSubnet:     "10.10.0.0/16",
		Phase1Proposals: []string{"aes256-sha256-modp2048"},
		Phase2Proposals: []string{"aes256gcm16"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"connections {",
		"aws-a {",
		"local_addrs = 198.51.100.10",
		"remote_addrs = 203.0.113.20",
		"local_ts = 10.0.0.0/24",
		"remote_ts = 10.10.0.0/16",
		"esp_proposals = aes256gcm16",
		"secrets {",
		`secret = "secret"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered swanctl missing %q:\n%s", want, got)
		}
	}
}

func TestRenderSwanctlRejectsMissingAuth(t *testing.T) {
	_, err := RenderSwanctl("bad", api.IPsecConnectionSpec{
		LocalAddress:  "198.51.100.10",
		RemoteAddress: "203.0.113.20",
		LeftSubnet:    "10.0.0.0/24",
		RightSubnet:   "10.10.0.0/16",
	})
	if err == nil {
		t.Fatal("RenderSwanctl returned nil error")
	}
}

func TestRenderSwanctlEscapesPSKAndBindsBothEndpointIdentities(t *testing.T) {
	data, err := RenderSwanctl("site-a", api.IPsecConnectionSpec{
		LocalAddress:  "198.51.100.10",
		RemoteAddress: "203.0.113.20",
		PreSharedKey:  `secret # "quoted" {value}`,
		LeftSubnet:    "10.0.0.0/24",
		RightSubnet:   "10.10.0.0/16",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`id-1 = "198.51.100.10"`,
		`id-2 = "203.0.113.20"`,
		`secret = "secret # \"quoted\" {value}"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered swanctl missing %q:\n%s", want, got)
		}
	}
}
