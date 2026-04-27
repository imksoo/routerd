package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/reconcile"
	"routerd/pkg/render"
	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
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

func TestParseFreeBSDRCConf(t *testing.T) {
	got, err := parseFreeBSDRCConf([]byte(`# Generated
gateway_enable="YES"
ifconfig_vtnet2="DHCP"
ifconfig_vtnet0_ipv6="inet6 accept_rtadv"
`))
	if err != nil {
		t.Fatalf("parse rc.conf: %v", err)
	}
	for key, want := range map[string]string{
		"gateway_enable":       "YES",
		"ifconfig_vtnet2":      "DHCP",
		"ifconfig_vtnet0_ipv6": "inet6 accept_rtadv",
	} {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q", key, got[key], want)
		}
	}
	if ifname := freeBSDIfconfigKeyInterface("ifconfig_vtnet0_ipv6"); ifname != "vtnet0" {
		t.Fatalf("ifconfig key interface = %q, want vtnet0", ifname)
	}
	ifnames := freeBSDDHCPClientIfnames([]byte("interface \"vtnet2\" {\n  ignore routers;\n};\n"))
	if len(ifnames) != 1 || ifnames[0] != "vtnet2" {
		t.Fatalf("dhclient ifnames = %v, want [vtnet2]", ifnames)
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
			SourceCIDRs:      []string{"192.168.10.0/24"},
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
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark { 0x110, 0x111 } meta mark set ct mark",
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
			SourceCIDRs:      []string{"192.168.10.0/24"},
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
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark { 0x100, 0x101, 0x102 } meta mark set ct mark",
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

func TestAvailableIPv4DefaultRouteCandidatesSkipsMissingRouteSetDevices(t *testing.T) {
	candidates := []api.IPv4DefaultRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, RouteSet: "dslite-set"},
		{Name: "wan", Priority: 20, Interface: "wan", HealthCheck: "wan-check"},
	}
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{
		"dslite-set": {
			Targets: []api.IPv4PolicyRouteTarget{
				{OutboundInterface: "ds-lite-a"},
				{OutboundInterface: "ds-lite-b"},
			},
		},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"ds-lite-a": "ds-lite-a",
		"ds-lite-b": "ds-lite-b",
	}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     &api.Router{},
		Aliases:    aliases,
		RouteSets:  routeSets,
		Health:     map[string]bool{"wan-check": true},
		LinkExists: func(ifname string) bool { return ifname == "ens18" },
	}, candidates)
	if len(available) != 1 || available[0].Name != "wan" {
		t.Fatalf("available candidates = %+v, want wan only", available)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesKeepsRouteSetWithAnyDevice(t *testing.T) {
	candidates := []api.IPv4DefaultRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, RouteSet: "dslite-set"},
		{Name: "wan", Priority: 20, Interface: "wan"},
	}
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{
		"dslite-set": {
			Targets: []api.IPv4PolicyRouteTarget{
				{OutboundInterface: "ds-lite-a"},
				{OutboundInterface: "ds-lite-b"},
			},
		},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"ds-lite-a": "ds-lite-a",
		"ds-lite-b": "ds-lite-b",
	}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     &api.Router{},
		Aliases:    aliases,
		RouteSets:  routeSets,
		LinkExists: func(ifname string) bool { return ifname == "ens18" || ifname == "ds-lite-b" },
	}, candidates)
	if len(available) != 2 || available[0].Name != "dslite" {
		t.Fatalf("available candidates = %+v, want dslite first", available)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesSkipsDSLiteWithoutLocalAddress(t *testing.T) {
	candidates := []api.IPv4DefaultRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, RouteSet: "dslite-set"},
		{Name: "wan", Priority: 20, Interface: "wan"},
	}
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{
		"dslite-set": {
			Targets: []api.IPv4PolicyRouteTarget{{OutboundInterface: "ds-lite-a"}},
		},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"lan":       "ens19",
		"ds-lite-a": "ds-lite-a",
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite-a"},
			Spec: api.DSLiteTunnelSpec{
				Interface:             "wan",
				LocalAddressSource:    "delegatedAddress",
				LocalDelegatedAddress: "lan-ipv6",
				LocalAddressSuffix:    "::3",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ipv6"},
			Spec: api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				AddressSuffix:    "::3",
			},
		},
	}}}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     router,
		Aliases:    aliases,
		RouteSets:  routeSets,
		LinkExists: func(ifname string) bool { return ifname == "ens18" || ifname == "ds-lite-a" },
	}, candidates)
	if len(available) != 1 || available[0].Name != "wan" {
		t.Fatalf("available candidates = %+v, want wan only", available)
	}
}

func TestStaleIPv4ManagedFwmarkRules(t *testing.T) {
	desired := map[ipv4FwmarkRule]bool{
		{Priority: 10, Mark: 0x111, Table: 111}: true,
		{Priority: 20, Mark: 0x112, Table: 112}: true,
	}
	current := []ipv4FwmarkRule{
		{Priority: 10, Mark: 0x111, Table: 111},
		{Priority: 20, Mark: 0x112, Table: 112},
		{Priority: 30, Mark: 0x112, Table: 112},
		{Priority: 10000, Mark: 0x100, Table: 100},
		{Priority: 10001, Mark: 0x101, Table: 101},
		{Priority: 500, Mark: 0x900, Table: 900},
	}
	got := staleIPv4ManagedFwmarkRules(desired, current)
	want := []ipv4FwmarkRule{
		{Priority: 30, Mark: 0x112, Table: 112},
		{Priority: 10000, Mark: 0x100, Table: 100},
		{Priority: 10001, Mark: 0x101, Table: 101},
	}
	if len(got) != len(want) {
		t.Fatalf("stale rules = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stale rules = %+v, want %+v", got, want)
		}
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

func TestHealthCheckPingSourceUsesDSLiteLocalAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:          "wan",
					TunnelName:         "ds-transix",
					LocalAddressSource: "static",
					LocalAddress:       "2001:db8::3",
					RemoteAddress:      "2001:db8::100",
				},
			},
		}},
	}
	source := healthCheckPingSource(router, api.HealthCheckSpec{
		Interface:    "transix",
		TargetSource: "dsliteRemote",
	}, map[string]string{"wan": "ens18", "transix": "ds-transix"})
	if source != "2001:db8::3" {
		t.Fatalf("ping source = %q, want 2001:db8::3", source)
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

func TestManagedHostnames(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "system-hostname"},
				Spec:     api.HostnameSpec{Hostname: "router03.example.internal", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "observed-hostname"},
				Spec:     api.HostnameSpec{Hostname: "ignored.example", Managed: false},
			},
		}},
	}
	got, err := managedHostnames(router)
	if err != nil {
		t.Fatalf("managed hostnames: %v", err)
	}
	if len(got) != 1 || got[0] != "router03.example.internal" {
		t.Fatalf("managed hostnames = %v, want router03.example.internal", got)
	}
}

func TestDriftedAdoptionCandidates(t *testing.T) {
	candidates := []reconcile.AdoptionCandidate{
		{
			Kind:     "host.hostname",
			Name:     "system",
			Desired:  map[string]string{"hostname": "router03.example.internal"},
			Observed: map[string]string{"hostname": "router03"},
		},
		{
			Kind:     "linux.ipv4.routeTable",
			Name:     "table=111",
			Desired:  map[string]string{"table": "111", "ifname": "ppp0"},
			Observed: map[string]string{"table": "111", "ifname": "ppp0"},
		},
	}
	got := driftedAdoptionCandidates(candidates)
	if len(got) != 1 || got[0].Kind != "host.hostname" {
		t.Fatalf("drifted candidates = %+v, want hostname only", got)
	}
}

func TestAdoptedArtifactsForResultDeduplicates(t *testing.T) {
	artifacts := []resource.Artifact{
		{Kind: "nft.table", Name: "routerd_nat", Owner: "one"},
		{Kind: "nft.table", Name: "routerd_nat", Owner: "one"},
		{Kind: "host.hostname", Name: "system", Owner: "host"},
	}
	got := adoptedArtifactsForResult(artifacts)
	if len(got) != 2 {
		t.Fatalf("adopted artifacts = %+v, want two", got)
	}
}

func TestDeriveIPv6Address(t *testing.T) {
	got, err := deriveIPv6Address([]string{"2001:db8:3d60:1220::/64"}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address: %v", err)
	}
	if got != "2001:db8:3d60:1220::100" {
		t.Fatalf("address = %s, want 2001:db8:3d60:1220::100", got)
	}
}

func TestDeriveIPv6AddressFromGlobalAddress(t *testing.T) {
	got, err := deriveIPv6AddressFromGlobalAddress([]string{
		"fe80::3",
		"2001:db8:3d60:1220::3",
	}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address from global address: %v", err)
	}
	if got != "2001:db8:3d60:1220::100" {
		t.Fatalf("address = %s, want 2001:db8:3d60:1220::100", got)
	}
}

func TestDelegatedPrefixFromObservedUsesKernelPrefix(t *testing.T) {
	got, ok := delegatedPrefixFromObserved([]string{
		"fe80::/64",
		"2001:db8:3d60:1240::/64",
	}, nil, 60)
	if !ok {
		t.Fatal("delegated prefix not found")
	}
	if got != "2001:db8:3d60:1240::/60" {
		t.Fatalf("prefix = %s, want 2001:db8:3d60:1240::/60", got)
	}
}

func TestDelegatedPrefixFromObservedFallsBackToAddress(t *testing.T) {
	got, ok := delegatedPrefixFromObserved(nil, []string{
		"fe80::3",
		"2001:db8:3d60:1240::2",
	}, 60)
	if !ok {
		t.Fatal("delegated prefix not found")
	}
	if got != "2001:db8:3d60:1240::/60" {
		t.Fatalf("prefix = %s, want 2001:db8:3d60:1240::/60", got)
	}
}

func TestParseFreeBSDIfconfigIPv6(t *testing.T) {
	prefixes, addrs := parseFreeBSDIfconfigIPv6(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet 192.168.160.1 netmask 0xffffff00 broadcast 192.168.160.255
	inet6 fe80::be24:11ff:fea3:c1f4%vtnet1 prefixlen 64 scopeid 0x2
	inet6 2001:db8:3d60:1240:be24:11ff:fea3:c1f4 prefixlen 64
`)
	if len(prefixes) != 1 || prefixes[0] != "2001:db8:3d60:1240::/64" {
		t.Fatalf("prefixes = %v, want delegated /64", prefixes)
	}
	wantAddrs := []string{"fe80::be24:11ff:fea3:c1f4", "2001:db8:3d60:1240:be24:11ff:fea3:c1f4"}
	if fmt.Sprint(addrs) != fmt.Sprint(wantAddrs) {
		t.Fatalf("addrs = %v, want %v", addrs, wantAddrs)
	}
}

func TestRetainCurrentPrefixDuringConvergence(t *testing.T) {
	store := routerstate.New()
	current := routerstate.Value{
		Status:    routerstate.StatusSet,
		Value:     "2001:db8:3d60:1240::/60",
		UpdatedAt: time.Now().UTC().Add(-30 * time.Second),
	}
	got, ok := retainCurrentPrefixDuringConvergence(current, 5*time.Minute, store)
	if !ok || got != current.Value {
		t.Fatalf("retained prefix = %q %v, want current prefix", got, ok)
	}
	current.UpdatedAt = time.Now().UTC().Add(-10 * time.Minute)
	if got, ok := retainCurrentPrefixDuringConvergence(current, 5*time.Minute, store); ok {
		t.Fatalf("retained expired prefix = %q", got)
	}
}

func TestParseRFC4361ClientID(t *testing.T) {
	identity := parseRFC4361ClientID("ffca53095a0003000102005e102030")
	if identity.IAID != "ca53095a" {
		t.Fatalf("IAID = %q, want ca53095a", identity.IAID)
	}
	if identity.DUID != "0003000102005e102030" {
		t.Fatalf("DUID = %q, want link-layer DUID", identity.DUID)
	}
}

func TestLinkLayerDUIDFromMAC(t *testing.T) {
	got := linkLayerDUIDFromMAC("02:00:5e:10:20:30")
	if got != "0003000102005e102030" {
		t.Fatalf("DUID = %q, want 0003000102005e102030", got)
	}
}

func TestStateWhenRequiresSetAndEqual(t *testing.T) {
	store := routerstate.New()
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"wan.ipv6.mode": {Equals: "pd-ready"},
	}}
	if resourceWhenMatches(when, store) {
		t.Fatal("unknown state matched equals")
	}
	store.Unset("wan.ipv6.mode", "observed absent")
	if resourceWhenMatches(when, store) {
		t.Fatal("unset state matched equals")
	}
	store.Set("wan.ipv6.mode", "address-only", "observed fallback")
	if resourceWhenMatches(when, store) {
		t.Fatal("different set value matched equals")
	}
	store.Set("wan.ipv6.mode", "pd-ready", "observed pd")
	if !resourceWhenMatches(when, store) {
		t.Fatal("matching set value did not match equals")
	}
}

func TestFilterDefaultRouteCandidatesByWhen(t *testing.T) {
	store := routerstate.New()
	store.Set("wan.ipv6.mode", "address-only", "test")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{Kind: "IPv4DefaultRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default-v4"}, Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{
			{Name: "dslite", RouteSet: "dslite", Priority: 10, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {In: []string{"pd-ready", "address-only"}}}}},
			{Name: "pppoe", Interface: "wan-pppoe", Priority: 20, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {Equals: "ipv4-only"}}}},
		}}},
	}}}
	filtered := filterRouterByWhen(router, store)
	spec, err := filtered.Spec.Resources[0].IPv4DefaultRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Candidates) != 1 || spec.Candidates[0].Name != "dslite" {
		t.Fatalf("candidates = %+v, want only dslite", spec.Candidates)
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
