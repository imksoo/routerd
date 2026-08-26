// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestEnsureDSLiteTunnelChangesOwnedTunnelWhenEndpointOrDeviceDiffers(t *testing.T) {
	wantRemote := "2404:8e00::feed:100"
	wantLocal := "2409:10:3d60:1200:0:5eff:fe00:113"
	wantDevice := "wan-vmac"
	tests := []struct {
		name string
		show string
	}{
		{
			name: "local endpoint",
			show: "ds-lite-ra: ip/ipv6 remote 2404:8e00::feed:100 local 2409:10:3d60:1221::23 dev wan-vmac encaplimit none",
		},
		{
			name: "remote endpoint",
			show: "ds-lite-ra: ip/ipv6 remote 2001:db8::feed local 2409:10:3d60:1200:0:5eff:fe00:113 dev wan-vmac encaplimit none",
		},
		{
			name: "underlay device",
			show: "ds-lite-ra: ip/ipv6 remote 2404:8e00::feed:100 local 2409:10:3d60:1200:0:5eff:fe00:113 dev ens18 encaplimit none",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := installFakeDSLiteIP(t, tt.show, false)
			router, spec := testDSLiteRouterAndSpec(wantDevice)
			got, err := ensureDSLiteTunnel(context.Background(), router, spec, "ds-lite-ra", wantRemote, wantLocal, "192.0.0.5")
			if err != nil {
				t.Fatalf("ensure DS-Lite tunnel: %v", err)
			}
			if got != "ds-lite-ra" {
				t.Fatalf("resolved interface = %q, want ds-lite-ra", got)
			}
			calls := readDSLiteIPCalls(t, logPath)
			wantChange := "-6 tunnel change ds-lite-ra mode ipip6 remote " + wantRemote + " local " + wantLocal + " dev " + wantDevice + " encaplimit none"
			if !containsExactLine(calls, wantChange) {
				t.Fatalf("ip calls missing in-place change %q:\n%s", wantChange, calls)
			}
			if strings.Contains(calls, "tunnel del") || strings.Contains(calls, "tunnel add") {
				t.Fatalf("endpoint update used destructive recreate:\n%s", calls)
			}
		})
	}
}

func TestEnsureDSLiteTunnelKeepsExistingTunnelWhenChangeFails(t *testing.T) {
	logPath := installFakeDSLiteIP(t, "ds-lite-ra: ip/ipv6 remote 2404:8e00::feed:100 local 2409:10:3d60:1221::23 dev wan-vmac encaplimit none", true)
	router, spec := testDSLiteRouterAndSpec("wan-vmac")
	_, err := ensureDSLiteTunnel(context.Background(), router, spec, "ds-lite-ra", "2404:8e00::feed:100", "2409:10:3d60:1200:0:5eff:fe00:113", "192.0.0.5")
	if err == nil || !strings.Contains(err.Error(), "tunnel change") {
		t.Fatalf("change error = %v, want tunnel change failure", err)
	}
	calls := readDSLiteIPCalls(t, logPath)
	if strings.Contains(calls, "tunnel del") || strings.Contains(calls, "tunnel add") {
		t.Fatalf("failed change deleted the existing tunnel:\n%s", calls)
	}
}

func TestSetDSLiteTunnelLinkUpCorrectsMTU(t *testing.T) {
	logPath := installFakeDSLiteIP(t, "", false)
	if err := setDSLiteTunnelLinkUp(context.Background(), "ds-lite-ra", 1454); err != nil {
		t.Fatalf("set DS-Lite link: %v", err)
	}
	calls := readDSLiteIPCalls(t, logPath)
	if !containsExactLine(calls, "link set ds-lite-ra mtu 1454 up") {
		t.Fatalf("ip calls missing MTU correction:\n%s", calls)
	}
}

func TestLinuxDSLiteTunnelMatchesCanonicalIPv6(t *testing.T) {
	show := []byte("ds-lite-ra: ip/ipv6 remote 2404:8e00:0:0:0:0:feed:100 local 2409:10:3d60:1200:0:5eff:fe00:113 dev wan-vmac encaplimit none")
	if !linuxDSLiteTunnelMatches(show, "2404:8e00::feed:100", "2409:10:3d60:1200:0:5eff:fe00:113", "wan-vmac", "none") {
		t.Fatal("equivalent canonical IPv6 endpoints did not match")
	}
}

func TestWaitForIPv6AddressReadyWaitsThroughTentativeState(t *testing.T) {
	calls := 0
	ready := waitForIPv6AddressReady(t.Context(), "wan-vmac", "2001:db8::21/128", time.Second, func(_ context.Context, ifname, address string) bool {
		if ifname != "wan-vmac" || address != "2001:db8::21/128" {
			t.Fatalf("readback = %s %s", ifname, address)
		}
		calls++
		return calls >= 3
	})
	if !ready || calls != 3 {
		t.Fatalf("ready=%t calls=%d", ready, calls)
	}
}

func TestWaitForIPv6AddressReadyHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if waitForIPv6AddressReady(ctx, "wan-vmac", "2001:db8::21/128", time.Second, func(context.Context, string, string) bool { return false }) {
		t.Fatal("cancelled wait reported ready")
	}
}

func testDSLiteRouterAndSpec(device string) (*api.Router, api.DSLiteTunnelSpec) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-vmac"}, Spec: api.InterfaceSpec{IfName: device}},
	}}}
	return router, api.DSLiteTunnelSpec{Interface: "wan-vmac", EncapsulationLimit: "none"}
}

func installFakeDSLiteIP(t *testing.T, tunnelShow string, failChange bool) string {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "ip.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ROUTERD_TEST_IP_LOG"
if [ "$*" = "-6 tunnel show ds-lite-ra" ]; then
  printf '%s\n' "$ROUTERD_TEST_TUNNEL_SHOW"
  exit 0
fi
if [ "$1 $2 $3" = "-6 tunnel change" ] && [ "$ROUTERD_TEST_FAIL_CHANGE" = "1" ]; then
  printf '%s\n' "RTNETLINK answers: Invalid argument" >&2
  exit 2
fi
if [ "$*" = "-4 addr show dev ds-lite-ra" ]; then
  printf '%s\n' "inet 192.0.0.5 peer 192.0.0.1 scope global ds-lite-ra"
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "ip"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ROUTERD_TEST_IP_LOG", logPath)
	t.Setenv("ROUTERD_TEST_TUNNEL_SHOW", tunnelShow)
	if failChange {
		t.Setenv("ROUTERD_TEST_FAIL_CHANGE", "1")
	} else {
		t.Setenv("ROUTERD_TEST_FAIL_CHANGE", "0")
	}
	return logPath
}

func readDSLiteIPCalls(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func containsExactLine(text, want string) bool {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == want {
			return true
		}
	}
	return false
}
