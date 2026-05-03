package chain

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestDNSResolverUpstreamLinesExpandStatusReferences(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolverUpstream"},
			Metadata: api.ObjectMeta{Name: "ngn"},
			Spec: api.DNSResolverUpstreamSpec{
				Zones: []api.DNSResolverZoneSpec{
					{Zone: "transix.jp.", Servers: []api.DNSResolverServerSpec{{Address: "${DHCPv6Information/wan-info.status.dnsServers}"}}},
				},
				Default: api.DNSResolverDefaultSpec{Servers: []api.DNSResolverServerSpec{{Address: "2001:4860:4860::8888"}}},
			},
		},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6Information/wan-info": {
			"dnsServers": []any{"2409:10:3d60:1200:1eb1:7fff:fe73:76d8"},
		},
	}

	got := dnsmasqResolverLines(router, store)
	want := []string{
		"server=2001:4860:4860::8888",
		"server=/transix.jp/2409:10:3d60:1200:1eb1:7fff:fe73:76d8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolver lines = %#v, want %#v", got, want)
	}
}

func TestDNSResolverUpstreamLinesRenderDoHStub(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolverUpstream"},
			Metadata: api.ObjectMeta{Name: "doh"},
			Spec: api.DNSResolverUpstreamSpec{
				Zones: []api.DNSResolverZoneSpec{{
					Zone: ".",
					Servers: []api.DNSResolverServerSpec{{
						Type:          "doh",
						URL:           "https://1.1.1.1/dns-query",
						ListenAddress: "127.0.0.1",
						ListenPort:    5053,
					}},
				}},
			},
		},
	}}}
	got := dnsmasqResolverLines(router, mapStore{})
	if !reflect.DeepEqual(got, []string{"server=127.0.0.1#5053"}) {
		t.Fatalf("resolver lines = %#v", got)
	}
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
			Interface:    "lan",
			Mode:         "both",
			AddressPool:  api.DHCPAddressPoolSpec{Start: "::100", End: "::1ff", LeaseTime: "6h"},
			DNSServers:   []string{"${DHCPv6Information/wan-info.status.dnsServers}"},
			SNTPServers:  []string{"2001:db8::123"},
			DomainSearch: []string{"lan"},
			RapidCommit:  true,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
			Interface:     "lan",
			PrefixSource:  "${IPv6DelegatedAddress/lan.status.address}",
			RDNSS:         []string{"${DHCPv6Information/wan-info.status.dnsServers}"},
			DNSSL:         []string{"lan"},
			MTU:           1500,
			PRFPreference: "high",
			ValidLifetime: "7200",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSAnswerScope"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSAnswerScopeSpec{
			Interface:   "lan",
			LocalDomain: "lan",
			DDNS:        true,
			DNSSEC:      true,
			HostRecords: []api.DNSHostRecord{{Hostname: "router.lan", IPv4: "192.168.10.1", IPv6: "2001:db8::1"}},
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
	records := dnsmasqHostRecordLines(router, store)
	for _, want := range []string{"host-record=router.lan,192.168.10.1,2001:db8::1", "domain=lan", "local=/lan/", "dhcp-fqdn", "dnssec"} {
		if !containsLine(records, want) {
			t.Fatalf("dnsmasq host lines missing %q:\n%#v", want, records)
		}
	}
}

func TestWriteDnsmasqConfigUsesListenAddresses(t *testing.T) {
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
	if !strings.Contains(string(data), "listen-address=127.0.0.1,192.168.160.5\n") {
		t.Fatalf("dnsmasq config did not contain custom listen-address:\n%s", data)
	}
}

func TestWriteDnsmasqConfigDefaultsToLocalhost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsmasq.conf")
	if _, err := writeDnsmasqConfig(&api.Router{}, mapStore{}, path, "/run/routerd/test.pid", 1053, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "listen-address=127.0.0.1\n") {
		t.Fatalf("dnsmasq config did not default to localhost:\n%s", data)
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
			PrefixSource:  "${DHCPv6PrefixDelegation/wan-pd.status.currentPrefix}",
			SubnetID:      "1",
			AddressSuffix: "::1",
		}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6PrefixDelegation/wan-pd": {"currentPrefix": "2409:10:3d60:1220::/60"},
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
