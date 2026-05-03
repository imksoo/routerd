package chain

import (
	"reflect"
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
					{Zone: "transix.jp.", Servers: []string{"${DHCPv6Information/wan-info.status.dnsServers}"}},
				},
				Default: api.DNSResolverDefaultSpec{Servers: []string{"2001:4860:4860::8888"}},
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

func TestDnsmasqLANServiceLines(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPServer"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.IPv4DHCPServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			Gateway:     "192.168.10.1",
			DNSServers:  []string{"192.168.10.1"},
			NTPServers:  []string{"192.168.10.1"},
			Domain:      "lan",
			Options:     []api.DHCPOptionSpec{{Name: "domain-search", Value: "lan"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPReservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.IPv4DHCPReservationSpec{
			Server:     "lan-v4",
			MACAddress: "02:00:00:00:01:50",
			Hostname:   "printer",
			IPAddress:  "192.168.10.150",
			Options:    []api.DHCPOptionSpec{{Code: 42, Value: "192.168.10.1"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6DHCPv6ServerSpec{
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPRelay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPRelaySpec{Interfaces: []string{"lan"}, Upstream: "192.0.2.53"}},
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
