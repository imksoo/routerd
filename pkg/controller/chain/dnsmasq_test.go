package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestDnsmasqLANServiceLines(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:     "lan",
			AddressPool:   api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			GatewayFrom:   api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"},
			DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan-base", Field: "address"}},
			NTPServerFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan-base", Field: "address"}},
			Domain:        "lan",
			Options:       []api.DHCPv4OptionSpec{{Name: "domain-search", Value: "lan"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{
			Server:     "lan-v4",
			MACAddress: "02:00:00:00:01:50",
			Hostname:   "printer",
			IPAddress:  "192.168.10.150",
			Options:    []api.DHCPv4OptionSpec{{Code: 42, Value: "192.168.10.1"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{
			Interface:     "lan",
			Mode:          "both",
			AddressPool:   api.DHCPAddressPoolSpec{Start: "::100", End: "::1ff", LeaseTime: "6h"},
			DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}},
			SNTPServers:   []string{"2001:db8::123"},
			DomainSearch:  []string{"lan"},
			RapidCommit:   true,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
			Interface:     "lan",
			PrefixFrom:    api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "address"},
			RDNSSFrom:     []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}},
			DNSSL:         []string{"lan"},
			MTU:           1500,
			PRFPreference: "high",
			ValidLifetime: "7200",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{TunnelName: "dslite0", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"}, Metadata: api.ObjectMeta{Name: "lan-dslite-mtu"}, Spec: api.PathMTUPolicySpec{
			FromInterface: "lan",
			ToInterfaces:  []string{"ds-lite"},
			MTU:           api.PathMTUPolicyMTUSpec{Source: "minInterface"},
			IPv6RA:        api.PathMTUPolicyIPv6RASpec{Enabled: true, Scope: "lan-ra"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"lan"}, Upstream: "192.0.2.53"}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6Information/wan-info": {"dnsServers": []any{"2001:db8::53"}},
		api.NetAPIVersion + "/IPv4StaticAddress/lan-base": {"address": "192.168.10.1/24"},
	}

	got, err := dnsmasqLANServiceLines(router, store)
	if err != nil {
		t.Fatalf("render lines: %v", err)
	}
	for _, want := range []string{
		"interface=ens19",
		"dhcp-range=set:lan-v4,192.168.10.100,192.168.10.199,8h",
		"dhcp-option=tag:lan-v4,option:router,192.168.10.1",
		"dhcp-option=tag:lan-v4,option:dns-server,192.168.10.1",
		"dhcp-option=tag:lan-v4,option:ntp-server,192.168.10.1",
		"dhcp-option=tag:lan-v4,option:domain-name,lan",
		"dhcp-option=tag:lan-v4,option:domain-search,lan",
		"dhcp-host=02:00:00:00:01:50,set:printer,printer,192.168.10.150",
		"dhcp-option=tag:printer,42,192.168.10.1",
		"dhcp-range=set:lan-v6,::100,::1ff,constructor:ens19,slaac,64,6h",
		"dhcp-option=tag:lan-v6,option6:dns-server,[2001:db8::53]",
		"dhcp-option=tag:lan-v6,option6:sntp-server,[2001:db8::123]",
		"dhcp-option=tag:lan-v6,option6:domain-search,lan",
		"dhcp-option=tag:lan-v6,option6:rapid-commit",
		"ra-param=ens19,mtu:1454,high,0,7200",
		"dhcp-option=option6:domain-search,lan",
		"dhcp-relay=0.0.0.0,192.0.2.53,ens19",
	} {
		if !containsLine(got, want) {
			t.Fatalf("dnsmasq LAN service lines missing %q:\n%#v", want, got)
		}
	}
}

func TestDnsmasqLANServiceLinesStripIPv6PrefixLengthFromOptions(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{
			Interface:     "lan",
			Mode:          "stateless",
			DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan", Field: "address"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
			Interface: "lan",
			PrefixFrom: api.StatusValueSourceSpec{
				Resource: "IPv6DelegatedAddress/lan",
				Field:    "address",
			},
			RDNSSFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan", Field: "address"}},
		}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan": {
			"address": "2409:10:3d60:1271::1/64",
		},
	}

	got, err := dnsmasqLANServiceLines(router, store)
	if err != nil {
		t.Fatalf("render lines: %v", err)
	}
	if !containsLine(got, "dhcp-option=tag:lan-v6,option6:dns-server,[2409:10:3d60:1271::1]") {
		t.Fatalf("DHCPv6 DNS option did not strip prefix length:\n%#v", got)
	}
	if !containsLine(got, "dhcp-option=option6:dns-server,[2409:10:3d60:1271::1]") {
		t.Fatalf("RA RDNSS option did not strip prefix length:\n%#v", got)
	}
	for _, line := range got {
		if strings.Contains(line, "option6:dns-server,[2409:10:3d60:1271::1/64]") {
			t.Fatalf("line still contains prefix length: %q", line)
		}
	}
}

func TestWriteDnsmasqConfigDisablesDNSPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsmasq.conf")
	changed, err := writeDnsmasqConfig(&api.Router{}, mapStore{}, path, filepath.Join(dir, "run", "test.pid"), 53, []string{"127.0.0.1", "192.168.160.5"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first write should report changed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "port=0\n") {
		t.Fatalf("dnsmasq config did not disable DNS serving:\n%s", data)
	}
	if strings.Contains(string(data), "listen-address=") {
		t.Fatalf("dnsmasq config should not own DNS listen addresses:\n%s", data)
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestIPv4StaticAddressControllerAppliesAddressOnAliasedInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "172.18.0.1/16"}},
	}}}
	store := mapStore{}
	var got []string
	controller := IPv4StaticAddressController{
		Router: router,
		Store:  store,
		AddressPresent: func(context.Context, string, string) bool {
			return false
		},
		Command: func(ctx context.Context, name string, args ...string) error {
			got = append([]string{name}, args...)
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{"ip", "-4", "addr", "replace", "172.18.0.1/16", "dev", "ens19"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("command = %v, want %v", got, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "lan-base")
	if status["phase"] != "Applied" || status["ifname"] != "ens19" {
		t.Fatalf("status = %#v", status)
	}
}

func TestIPv4StaticAddressApplyCommandFreeBSD(t *testing.T) {
	name, args := ipv4StaticAddressApplyCommand("freebsd", "vtnet1", "192.168.160.4/24")
	got := strings.Join(append([]string{name}, args...), " ")
	want := "ifconfig vtnet1 inet 192.168.160.4/24 alias"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestIfconfigHasIPv4AddressIgnoresPrefix(t *testing.T) {
	out := []byte("vtnet1: flags=...\n\tinet 192.168.160.4 netmask 0xffffff00 broadcast 192.168.160.255\n")
	if !ifconfigHasIPv4Address(out, "192.168.160.4/24") {
		t.Fatal("ifconfigHasIPv4Address = false, want true")
	}
}

func TestInterfaceIfNameResolvesBridgeIfName(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br-lab"}, Spec: api.BridgeSpec{IfName: "bridge100"}},
	}}}
	if got := interfaceIfName(router, "br-lab"); got != "bridge100" {
		t.Fatalf("interfaceIfName = %q, want bridge100", got)
	}
	if got := interfaceName(router, "br-lab"); got != "bridge100" {
		t.Fatalf("interfaceName = %q, want bridge100", got)
	}
}

func TestIPv6StaticAddressApplyCommandFreeBSD(t *testing.T) {
	name, args := ipv6StaticAddressApplyCommand("freebsd", "vtnet1", "2409:10:3d60:1250::11/64")
	got := strings.Join(append([]string{name}, args...), " ")
	want := "ifconfig vtnet1 inet6 2409:10:3d60:1250::11 prefixlen 64 alias"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestFreeBSDIPv4RouteApplyCommandUsesDSLiteInnerGateway(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "0.0.0.0/0", "gif40", "")
	got := strings.Join(append([]string{name}, args...), " ")
	want := "route -n change default 192.0.0.1"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestFreeBSDIPv4RouteAddArgsFallback(t *testing.T) {
	got := strings.Join(freeBSDIPv4RouteAddArgs([]string{"-n", "change", "default", "192.168.1.1"}), " ")
	want := "-n add default 192.168.1.1"
	if got != want {
		t.Fatalf("add args = %q, want %q", got, want)
	}
	if !freeBSDRouteNeedsAdd([]byte("route: route has not been found\nnot in table")) {
		t.Fatal("expected FreeBSD route output to trigger add fallback")
	}
}

func TestIPv4StaticAddressControllerRestoresMissingAddressWithUnchangedStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "172.18.0.1/16"}},
	}}}
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "lan-base", map[string]any{
		"phase": "Applied", "interface": "lan", "ifname": "ens19", "address": "172.18.0.1/16", "dryRun": false,
	})
	var applied bool
	controller := IPv4StaticAddressController{
		Router: router,
		Store:  store,
		AddressPresent: func(context.Context, string, string) bool {
			return false
		},
		Command: func(ctx context.Context, name string, args ...string) error {
			applied = true
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("expected missing address to be restored")
	}
}

func TestLANAddressControllerPopulatesLinkBeforeDependencyCheck(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lo", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Link"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.LinkSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv6DelegatedAddressSpec{
			PrefixDelegation: "wan-pd",
			Interface:        "lan",
			SubnetID:         "1",
			AddressSuffix:    "::1",
			DependsOn: []api.ResourceDependencySpec{
				{Resource: "DHCPv6PrefixDelegation/wan-pd", Phase: daemonapi.ResourcePhaseBound},
				{Resource: "Link/lan", Phase: "Up"},
			},
		}},
	}}}
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{
		"phase":         daemonapi.ResourcePhaseBound,
		"currentPrefix": "2409:10:3d60:1270::/60",
	})
	var got []string
	controller := LANAddressController{
		Router: router,
		Store:  store,
		AddressPresent: func(context.Context, string, string) bool {
			return false
		},
		Command: func(ctx context.Context, name string, args ...string) error {
			got = append([]string{name}, args...)
			return nil
		},
	}
	if err := controller.reconcile(t.Context(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	want := []string{"ip", "-6", "addr", "replace", "2409:10:3d60:1271::1/64", "dev", "lo"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("command = %v, want %v", got, want)
	}
	link := store.ObjectStatus(api.NetAPIVersion, "Link", "lan")
	if link["phase"] != "Up" || link["ifname"] != "lo" {
		t.Fatalf("link status = %#v", link)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-base")
	if status["phase"] != "Applied" || status["address"] != "2409:10:3d60:1271::1/64" {
		t.Fatalf("delegated address status = %#v", status)
	}
}

func TestLANAddressControllerRestoresMissingAddressWithUnchangedStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv6DelegatedAddressSpec{
			PrefixDelegation: "wan-pd",
			Interface:        "lan",
			SubnetID:         "1",
			AddressSuffix:    "::1",
		}},
	}}}
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{"phase": daemonapi.ResourcePhaseBound, "currentPrefix": "2409:10:3d60:1270::/60"})
	store.SaveObjectStatus(api.NetAPIVersion, "Link", "lan", map[string]any{"phase": "Up", "ifname": "lo"})
	store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-base", map[string]any{
		"phase": "Applied", "address": "2409:10:3d60:1271::1/64", "interface": "lan", "prefixSource": "wan-pd", "dryRun": false,
	})
	var applied bool
	controller := LANAddressController{
		Router: router,
		Store:  store,
		AddressPresent: func(context.Context, string, string) bool {
			return false
		},
		Command: func(ctx context.Context, name string, args ...string) error {
			applied = true
			return nil
		},
	}
	if err := controller.reconcile(t.Context(), "wan-pd"); err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("expected missing delegated address to be restored")
	}
}

func TestLocalIPv6AddressKeepsHostBitsFromPrefix(t *testing.T) {
	got := localIPv6Address("2409:10:3d60:1250::11/64")
	if got != "2409:10:3d60:1250::11" {
		t.Fatalf("localIPv6Address = %q", got)
	}
}

func TestLinkControllerPublishesInterfaceIfName(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lo"}, Spec: api.InterfaceSpec{IfName: "lo", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Link"}, Metadata: api.ObjectMeta{Name: "lo"}, Spec: api.LinkSpec{IfName: "lo"}},
	}}}
	store := mapStore{}
	controller := LinkController{Router: router, Store: store}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	iface := store.ObjectStatus(api.NetAPIVersion, "Interface", "lo")
	if iface["ifname"] != "lo" {
		t.Fatalf("interface status = %#v", iface)
	}
	link := store.ObjectStatus(api.NetAPIVersion, "Link", "lo")
	if link["ifname"] != "lo" {
		t.Fatalf("link status = %#v", link)
	}
}

func TestDaemonStatusControllerDiscoversDaemonSockets(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.HealthCheckSpec{Daemon: "routerd-healthcheck"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "embedded"}, Spec: api.HealthCheckSpec{SocketSource: "embedded"}},
	}}}
	controller := DaemonStatusController{Router: router}
	got := strings.Join(controller.daemonSockets(), "\n")
	for _, want := range []string{
		"/run/routerd/dhcpv6-client/wan-pd.sock",
		"/run/routerd/healthcheck/internet.sock",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sockets = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "embedded") {
		t.Fatalf("embedded healthcheck should not have a daemon socket: %q", got)
	}
}

func TestDSLiteTunnelResolveRemoteDirectIPv6SkipsDNS(t *testing.T) {
	controller := DSLiteTunnelController{ResolverPort: 9}
	name, remote, err := controller.resolveRemote(t.Context(), api.DSLiteTunnelSpec{AFTRIPv6: "2404:8e00::feed:100"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "2404:8e00::feed:100" || remote != "2404:8e00::feed:100" {
		t.Fatalf("resolved name=%q remote=%q", name, remote)
	}
}

func TestDSLiteTunnelLocalDelegatedAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lo"}, Spec: api.InterfaceSpec{IfName: "lo"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.IPv6DelegatedAddressSpec{
			Interface:     "lo",
			SubnetID:      "",
			AddressSuffix: "::1",
		}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan": {"address": "2409:10:3d60:1221::1/64"},
	}
	controller := DSLiteTunnelController{Router: router, Store: store}
	local, ifname, err := controller.localAddress(api.DSLiteTunnelSpec{
		LocalAddressSource:    "delegatedAddress",
		LocalDelegatedAddress: "lan",
		LocalAddressSuffix:    "::3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if local != "2409:10:3d60:1221::3" || ifname != "lo" {
		t.Fatalf("local=%q ifname=%q", local, ifname)
	}
}

func TestDSLiteTunnelInnerLocalAddressFromStaticAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "ds-lite-source"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds-lite-a", Address: "192.168.160.250/32"}},
	}}}
	got, err := dsliteInnerLocalIPv4(router, nil, api.DSLiteTunnelSpec{
		LocalAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-source", Field: "address"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.160.250" {
		t.Fatalf("inner local = %q", got)
	}
}

func TestFirstUsableGlobalIPv6PrefersDynamicStableAddress(t *testing.T) {
	data := []byte(`[{"ifname":"ens18","addr_info":[
		{"family":"inet6","local":"2409:10:3d60:1200::temp","scope":"global","dynamic":true,"temporary":true,"preferred_life_time":1000},
		{"family":"inet6","local":"fe80::1","scope":"link","preferred_life_time":1000},
		{"family":"inet6","local":"2409:10:3d60:1200::stable","scope":"global","dynamic":true,"preferred_life_time":1000}
	]}]`)
	if got := firstUsableGlobalIPv6(data); got != "2409:10:3d60:1200::stable" {
		t.Fatalf("firstUsableGlobalIPv6 = %q", got)
	}
}

func TestFirstUsableIfconfigGlobalIPv6PrefersAutoconf(t *testing.T) {
	data := []byte(`vtnet0: flags=...
	inet6 fe80::be24:11ff:fefb:928d%vtnet0 prefixlen 64 scopeid 0x1
	inet6 2409:10:3d60:1200::dead prefixlen 64 temporary
	inet6 2409:10:3d60:1200:be24:11ff:fefb:928d prefixlen 64 autoconf
`)
	if got := firstUsableIfconfigGlobalIPv6(data); got != "2409:10:3d60:1200:be24:11ff:fefb:928d" {
		t.Fatalf("firstUsableIfconfigGlobalIPv6 = %q", got)
	}
}

func TestFreeBSDDSLiteRuntimeIfNameKeepsGIFNames(t *testing.T) {
	if got := freeBSDDSLiteRuntimeIfName("gif40"); got != "gif40" {
		t.Fatalf("runtime ifname = %q, want gif40", got)
	}
	if got := freeBSDDSLiteRuntimeIfName("ds-lite-a"); !strings.HasPrefix(got, "gif") || got == "gif40" {
		t.Fatalf("runtime ifname for ds-lite-a = %q, want generated gif name", got)
	}
}
