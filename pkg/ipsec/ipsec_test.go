// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"context"
	"errors"
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

func TestControllerLoadAllUsesCompleteConfigurationAndReportsOutput(t *testing.T) {
	var gotName string
	var gotArgs []string
	controller := Controller{
		Binary:     "/usr/local/sbin/swanctl",
		ConfigFile: "/usr/local/etc/swanctl/conf.d/routerd.conf",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return []byte("vici unavailable"), errors.New("exit status 1")
		},
	}
	err := controller.LoadAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "--load-all") || !strings.Contains(err.Error(), "vici unavailable") {
		t.Fatalf("LoadAll error = %v", err)
	}
	if gotName != "/usr/local/sbin/swanctl" || strings.Join(gotArgs, " ") != "--load-all --file /usr/local/etc/swanctl/conf.d/routerd.conf" {
		t.Fatalf("swanctl invocation = %q %v", gotName, gotArgs)
	}
}

func TestControllerLoadRetainsSafeWholeConfigurationSemantics(t *testing.T) {
	var gotArgs []string
	controller := Controller{Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil
	}}
	if err := controller.Load(context.Background(), "/etc/swanctl/conf.d/routerd-one.conf"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Join(gotArgs, " ") != "--load-all" {
		t.Fatalf("Load invocation = %v, want --load-all", gotArgs)
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
