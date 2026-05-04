package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
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
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			Gateway:     "192.168.10.1",
			DNSServers:  []string{"192.168.10.1"},
			NTPServers:  []string{"192.168.10.1"},
			Domain:      "lan",
			Options:     []api.DHCPv4OptionSpec{{Name: "domain-search", Value: "lan"}},
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"lan"}, Upstream: "192.0.2.53"}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6Information/wan-info": {"dnsServers": []any{"2001:db8::53"}},
	}

	got := dnsmasqLANServiceLines(router, store)
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
		"ra-param=ens19,mtu:1500,high,0,7200",
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

	got := dnsmasqLANServiceLines(router, store)
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
	path := filepath.Join(t.TempDir(), "dnsmasq.conf")
	changed, err := writeDnsmasqConfig(&api.Router{}, mapStore{}, path, "/run/routerd/test.pid", 53, []string{"127.0.0.1", "192.168.160.5"})
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
