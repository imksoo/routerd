package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/render"
)

func TestApplyNetworkConfigSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	netplanPath := filepath.Join(dir, "netplan", "90-routerd.yaml")
	dropinPath := filepath.Join(dir, "systemd", "10-netplan-ens18.network.d", "90-routerd-dhcp6-pd.conf")
	netplanData := []byte("network:\n  version: 2\n")
	dropinData := []byte("[Network]\nDHCP=yes\n")

	if err := os.MkdirAll(filepath.Dir(netplanPath), 0755); err != nil {
		t.Fatalf("create netplan dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dropinPath), 0755); err != nil {
		t.Fatalf("create dropin dir: %v", err)
	}
	if err := os.WriteFile(netplanPath, netplanData, 0600); err != nil {
		t.Fatalf("write netplan fixture: %v", err)
	}
	if err := os.WriteFile(dropinPath, dropinData, 0644); err != nil {
		t.Fatalf("write dropin fixture: %v", err)
	}

	changed, err := applyNetworkConfig(netplanPath, netplanData, []render.File{
		{Path: dropinPath, Data: dropinData},
	})
	if err != nil {
		t.Fatalf("apply network config: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed files = %v, want none", changed)
	}
}

func TestWriteFileIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.conf")

	changed, err := writeFileIfChanged(path, []byte("one\n"), 0644)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !changed {
		t.Fatal("first write changed = false, want true")
	}

	changed, err = writeFileIfChanged(path, []byte("one\n"), 0644)
	if err != nil {
		t.Fatalf("same write: %v", err)
	}
	if changed {
		t.Fatal("same write changed = true, want false")
	}

	changed, err = writeFileIfChanged(path, []byte("two\n"), 0644)
	if err != nil {
		t.Fatalf("different write: %v", err)
	}
	if !changed {
		t.Fatal("different write changed = false, want true")
	}
}

func TestReplaceManagedPPPoEBlocks(t *testing.T) {
	current := "# existing\nold * value *\n# BEGIN routerd pppoe old\n\"u\" * \"old\" *\n# END routerd pppoe old\n"
	got := replaceManagedPPPoEBlocks(current, []render.PPPoESecretEntry{
		{Name: "wan", Username: "user@example.jp", Password: "secret"},
	})
	for _, want := range []string{
		"# existing\nold * value *\n",
		"# BEGIN routerd pppoe wan\n",
		"\"user@example.jp\" * \"secret\" *\n",
		"# END routerd pppoe wan\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("managed secrets missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\"u\" * \"old\"") {
		t.Fatalf("old managed block was not removed:\n%s", got)
	}
}

func TestRenderIPv4DefaultRoutePolicyMarks(t *testing.T) {
	data, err := renderIPv4DefaultRoutePolicyMarks(
		"test/default",
		api.IPv4DefaultRoutePolicySpec{
			SourceCIDRs:      []string{"192.168.160.0/24"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
		},
		api.IPv4DefaultRoutePolicyCandidate{Name: "pppoe", Mark: 273},
		[]api.IPv4DefaultRoutePolicyCandidate{
			{Name: "dslite", Mark: 272},
			{Name: "pppoe", Mark: 273},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("render default route policy marks: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table ip routerd_default_route",
		"ip saddr 192.168.160.0/24 ip daddr 0.0.0.0/0 ct mark { 0x110, 0x111 } meta mark set ct mark",
		"ct mark != 0x0 ct mark != { 0x110, 0x111 } meta mark set 0x111 ct mark set meta mark",
		"ct mark 0x0 meta mark set 0x111 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderIPv4DefaultRoutePolicyMarksRouteSetActive(t *testing.T) {
	data, err := renderIPv4DefaultRoutePolicyMarks(
		"test/default",
		api.IPv4DefaultRoutePolicySpec{
			SourceCIDRs:      []string{"192.168.160.0/24"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
		},
		api.IPv4DefaultRoutePolicyCandidate{Name: "dslite", RouteSet: "lan-dslite-balance"},
		[]api.IPv4DefaultRoutePolicyCandidate{
			{Name: "dslite", RouteSet: "lan-dslite-balance"},
		},
		map[string]api.IPv4PolicyRouteSetSpec{
			"lan-dslite-balance": {
				Targets: []api.IPv4PolicyRouteTarget{
					{Name: "transix-a", Mark: 256},
					{Name: "transix-b", Mark: 257},
					{Name: "transix-c", Mark: 258},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("render default route policy marks: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip saddr 192.168.160.0/24 ip daddr 0.0.0.0/0 ct mark { 0x100, 0x101, 0x102 } meta mark set ct mark",
		"ct mark != 0x0 ct mark != { 0x100, 0x101, 0x102 } meta mark set 0x0 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ct mark 0x0 meta mark set 0x") {
		t.Fatalf("routeSet active candidate should leave new flows unmarked for IPv4PolicyRouteSet hashing:\n%s", got)
	}
}

func TestSelectIPv4DefaultRouteCandidateTreatsMissingHealthCheckAsUp(t *testing.T) {
	candidate, ok := selectIPv4DefaultRouteCandidate([]api.IPv4DefaultRoutePolicyCandidate{
		{Name: "preferred", Priority: 10, HealthCheck: "preferred-check"},
		{Name: "fallback", Priority: 20},
	}, map[string]bool{"preferred-check": false})
	if !ok {
		t.Fatal("candidate not selected")
	}
	if candidate.Name != "fallback" {
		t.Fatalf("candidate = %s, want fallback", candidate.Name)
	}
}

func TestResolveHealthCheckTargetDSLiteRemoteAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:     "wan",
					TunnelName:    "ds-transix",
					RemoteAddress: "2404:8e00::feed:100",
				},
			},
		}},
	}
	target, family, err := resolveHealthCheckTarget(router, api.HealthCheckSpec{
		Interface:    "transix",
		TargetSource: "dsliteRemote",
	}, map[string]string{"transix": "ds-transix"})
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if target != "2404:8e00::feed:100" || family != "ipv6" {
		t.Fatalf("target/family = %s/%s, want 2404:8e00::feed:100/ipv6", target, family)
	}
}

func TestChangedNetworkdInterfaces(t *testing.T) {
	got := changedNetworkdInterfaces([]string{
		"/etc/systemd/network/10-netplan-ens19.network.d/90-routerd-dhcp6-pd.conf",
		"/etc/systemd/network/10-netplan-ens19.network.d/90-routerd-extra.conf",
		"/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf",
	})
	want := []string{"ens19", "ens18"}
	if len(got) != len(want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interfaces = %v, want %v", got, want)
		}
	}
}

func TestDeriveIPv6Address(t *testing.T) {
	got, err := deriveIPv6Address([]string{"2409:10:3d60:1220::/64"}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address: %v", err)
	}
	if got != "2409:10:3d60:1220::100" {
		t.Fatalf("address = %s, want 2409:10:3d60:1220::100", got)
	}
}

func TestSelectAAAAByOrdinal(t *testing.T) {
	values := []string{
		"2404:8e00::feed:100",
		"2404:8e00::feed:101",
		"2404:8e00::feed:102",
	}
	got, err := selectAAAA(values, 2, "ordinal")
	if err != nil {
		t.Fatalf("select AAAA: %v", err)
	}
	if got != "2404:8e00::feed:101" {
		t.Fatalf("AAAA = %s, want 2404:8e00::feed:101", got)
	}
}

func TestSelectAAAAModulo(t *testing.T) {
	values := []string{
		"2404:8e00::feed:100",
		"2404:8e00::feed:101",
	}
	got, err := selectAAAA(values, 3, "ordinalModulo")
	if err != nil {
		t.Fatalf("select AAAA: %v", err)
	}
	if got != "2404:8e00::feed:100" {
		t.Fatalf("AAAA = %s, want 2404:8e00::feed:100", got)
	}
}
