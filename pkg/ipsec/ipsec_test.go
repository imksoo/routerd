package ipsec

import (
	"strings"
	"testing"

	"routerd/pkg/api"
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
		"secret = secret",
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
