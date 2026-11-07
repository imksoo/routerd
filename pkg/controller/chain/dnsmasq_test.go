// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
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

func (s mapStore) Get(name string) routerstate.Value {
	now := s.Now()
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
}

func (s mapStore) Age(name string) time.Duration {
	return 0
}

func (s mapStore) Now() time.Time {
	return time.Now().UTC()
}

type statefulDHCPMapStore struct {
	mapStore
	now time.Time
}

func (s statefulDHCPMapStore) Get(name string) routerstate.Value {
	now := s.Now()
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
}

func (s statefulDHCPMapStore) Age(name string) time.Duration {
	return s.Now().Sub(s.Get(name).Since)
}

func (s statefulDHCPMapStore) Now() time.Time {
	if s.now.IsZero() {
		return time.Now().UTC()
	}
	return s.now
}

func TestDnsmasqLANServiceLines(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.DNSZoneSpec{Zone: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:     "lan",
			AddressPool:   api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			GatewayFrom:   api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"},
			DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan-base", Field: "address"}},
			NTPServerFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan-base", Field: "address"}},
			DomainFrom:    api.StatusValueSourceSpec{Resource: "DNSZone/lan-zone", Field: "zone"},
			Profiles: []api.DHCPv4ServerProfileSpec{{
				Name:        "legacy-iot",
				AddressPool: api.DHCPAddressPoolSpec{Start: "172.17.1.100", End: "172.17.1.199", LeaseTime: "12h"},
				Netmask:     "255.255.0.0", Gateway: "172.17.0.1", DNSServers: []string{"172.17.0.1"},
			}},
			Scopes: []api.DHCPv4ServerScopeSpec{{
				Name:       "atom-cam",
				Match:      api.DHCPv4ServerScopeMatchSpec{OUIPrefixes: []string{"7c:dd:e9"}},
				ProfileRef: "legacy-iot",
			}},
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
			DomainSearchFrom: []api.StatusValueSourceSpec{
				{Resource: "DNSZone/lan-zone", Field: "zone"},
			},
			RapidCommit: true,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
			Interface:     "lan",
			PrefixFrom:    api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "address"},
			RDNSSFrom:     []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}},
			DNSSLFrom:     []api.StatusValueSourceSpec{{Resource: "DNSZone/lan-zone", Field: "zone"}},
			MTU:           1500,
			PRFPreference: "high",
			ValidLifetime: "7200",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{TunnelName: "dslite0", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite"}}},
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
		"dhcp-option=tag:printer,42,192.168.10.1",
		"dhcp-mac=set:lan-v4-atom-cam-match,7c:dd:e9:*:*:*",
		"dhcp-range=tag:lan-v4-atom-cam-match,set:lan-v4-atom-cam,172.17.1.100,172.17.1.199,255.255.0.0,12h",
		"dhcp-option=tag:lan-v4-atom-cam,option:router,172.17.0.1",
		"dhcp-option=tag:lan-v4-atom-cam,option:dns-server,172.17.0.1",
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
	profileRange := indexLine(got, "dhcp-range=tag:lan-v4-atom-cam-match,set:lan-v4-atom-cam,172.17.1.100,172.17.1.199,255.255.0.0,12h")
	baseRange := indexLine(got, "dhcp-range=set:lan-v4,192.168.10.100,192.168.10.199,8h")
	if profileRange < 0 || baseRange < 0 || profileRange >= baseRange {
		t.Fatalf("profile range must precede catch-all range: %#v", got)
	}
}

func TestDnsmasqLANServiceLinesAddsDefaultIntervalForPartialRAParams(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "transix"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-transix"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-mtu"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-priority"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", PRFPreference: "high"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"transix"}}},
	}}}

	lines, err := dnsmasqLANServiceLines(router, mapStore{})
	if err != nil {
		t.Fatalf("render lines: %v", err)
	}
	for _, want := range []string{
		"ra-param=ens19,mtu:1454,0",
		"ra-param=ens19,high,0",
	} {
		if !containsLine(lines, want) {
			t.Fatalf("dnsmasq LAN service lines missing %q:\n%#v", want, lines)
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

func TestDnsmasqHostsReloadFailureRestoresSidecarForNextReconcileRetry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface: "lan", LeaseFile: filepath.Join(dir, "state", "dnsmasq.leases"), AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{
			Server: "lan-v4", MACAddress: "02:00:00:00:01:50", Hostname: "printer", IPAddress: "192.168.10.150",
		}},
	}}}
	if changed, _, err := writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil); err != nil || !changed {
		t.Fatalf("initial write changed=%t err=%v", changed, err)
	}

	snapshot, err := snapshotDnsmasqHostsFile(dnsmasqHostsFile(configPath))
	if err != nil {
		t.Fatal(err)
	}
	router.Spec.Resources[2].Spec = api.DHCPv4ReservationSpec{
		Server: "lan-v4", MACAddress: "02:00:00:00:01:50", Hostname: "printer", IPAddress: "192.168.10.151",
	}
	changed, reloadOnly, err := writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil)
	if err != nil || changed || !reloadOnly {
		t.Fatalf("sidecar update changed=%t reloadOnly=%t err=%v", changed, reloadOnly, err)
	}
	attempts := 0
	reload := func(context.Context, string) error {
		attempts++
		if attempts == 1 {
			return syscall.EPERM
		}
		return nil
	}
	if err := reloadDnsmasqHostsSidecar(t.Context(), pidFile, configPath, snapshot, reload); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("first reload error = %v, want EPERM", err)
	}
	hostsData, err := os.ReadFile(dnsmasqHostsFile(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostsData), "192.168.10.150") || strings.Contains(string(hostsData), "192.168.10.151") {
		t.Fatalf("failed reload did not restore prior sidecar:\n%s", hostsData)
	}

	retrySnapshot, err := snapshotDnsmasqHostsFile(dnsmasqHostsFile(configPath))
	if err != nil {
		t.Fatal(err)
	}
	changed, reloadOnly, err = writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil)
	if err != nil || changed || !reloadOnly {
		t.Fatalf("retry write changed=%t reloadOnly=%t err=%v", changed, reloadOnly, err)
	}
	if err := reloadDnsmasqHostsSidecar(t.Context(), pidFile, configPath, retrySnapshot, reload); err != nil {
		t.Fatalf("retry reload: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("reload attempts = %d, want 2", attempts)
	}
	if changed, reloadOnly, err = writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil); err != nil || changed || reloadOnly {
		t.Fatalf("converged write changed=%t reloadOnly=%t err=%v", changed, reloadOnly, err)
	}
}

func TestDHCPv4ServerPendingSourceOmitsScopeUntilResolved(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-pending"}, Spec: api.DHCPv4ServerSpec{
			Interface:     "lan",
			AddressPool:   api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan-base", Field: "address"}},
			LeaseFile:     leaseFile,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "guest-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.20.100", End: "192.168.20.199", LeaseTime: "8h"},
			DNSServers:  []string{"192.168.20.1"},
			LeaseFile:   leaseFile,
		}},
	}}}
	store := mapStore{}
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	if _, _, err := writeDnsmasqConfig(router, store, configPath, pidFile, 1053, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "set:lan-pending") {
		t.Fatalf("pending scope was rendered:\n%s", data)
	}
	if !strings.Contains(string(data), "dhcp-range=set:guest-v4,192.168.20.100,192.168.20.199,8h") {
		t.Fatalf("other scope was not rendered:\n%s", data)
	}
	controller := DHCPv6ServerController{Router: router, Store: store}
	if err := controller.saveDHCPv4ServerStatuses(router, configPath, pidFile); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Server", "lan-pending")
	if status["phase"] != "Pending" || status["reason"] != "DNSServerFromUnresolved: IPv4StaticAddress/lan-base" {
		t.Fatalf("pending status = %#v", status)
	}

	store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "lan-base", map[string]any{"address": "192.168.10.1/24"})
	if _, _, err := writeDnsmasqConfig(router, store, configPath, pidFile, 1053, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "dhcp-range=set:lan-pending,192.168.10.100,192.168.10.199,8h") ||
		!strings.Contains(string(data), "dhcp-option=tag:lan-pending,option:dns-server,192.168.10.1") {
		t.Fatalf("resolved scope or DNS option was not rendered:\n%s", data)
	}
	if err := controller.saveDHCPv4ServerStatuses(router, configPath, pidFile); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "DHCPv4Server", "lan-pending")
	if status["phase"] != "Applied" || !reflect.DeepEqual(status["dnsServers"], []string{"192.168.10.1"}) {
		t.Fatalf("resolved status = %#v", status)
	}
}

func TestWriteDnsmasqConfigDisablesDNSPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsmasq.conf")
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
		}},
	}}}
	changed, _, err := writeDnsmasqConfig(router, mapStore{}, path, filepath.Join(dir, "run", "test.pid"), 53, []string{"127.0.0.1", "192.168.160.5"})
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
	if !strings.Contains(string(data), "bind-dynamic\n") || strings.Contains(string(data), "bind-interfaces\n") {
		t.Fatalf("dnsmasq config should use bind-dynamic to avoid interface alias warnings:\n%s", data)
	}
}

func TestWriteDnsmasqConfigUsesDeclaredLeaseFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
		}},
	}}}
	if _, _, err := writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "dhcp-leasefile="+leaseFile+"\n") {
		t.Fatalf("declared lease file was not rendered:\n%s", data)
	}
	if _, err := os.Stat(filepath.Dir(leaseFile)); err != nil {
		t.Fatalf("lease directory was not created: %v", err)
	}
}

func TestPruneDHCPv4ProfileMismatchedLeases(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			LeaseFile: leaseFile,
			Profiles:  []api.DHCPv4ServerProfileSpec{{Name: "legacy-iot", AddressPool: api.DHCPAddressPoolSpec{Start: "172.17.1.100", End: "172.17.1.199"}}},
			Scopes:    []api.DHCPv4ServerScopeSpec{{Name: "iot", ProfileRef: "legacy-iot", Match: api.DHCPv4ServerScopeMatchSpec{OUIPrefixes: []string{"7c:dd:e9"}}}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "reserved-iot"}, Spec: api.DHCPv4ReservationSpec{Server: "lan-v4", MACAddress: "7c:dd:e9:00:00:04", IPAddress: "172.18.1.104"}},
	}}}
	leases := "0 7c:dd:e9:00:00:01 172.18.1.101 old-iot *\n" +
		"0 7c:dd:e9:00:00:02 172.17.1.102 profile-iot *\n" +
		"0 02:00:00:00:00:03 172.18.1.103 normal *\n" +
		"0 7c:dd:e9:00:00:04 172.18.1.104 reserved-iot *\n"
	if err := os.WriteFile(leaseFile, []byte(leases), 0o640); err != nil {
		t.Fatal(err)
	}
	changed, err := pruneDHCPv4ProfileMismatchedLeases(router, filepath.Join(dir, "dnsmasq.conf"), filepath.Join(dir, "dnsmasq.pid"))
	if err != nil {
		t.Fatalf("prune leases: %v", err)
	}
	if !changed {
		t.Fatal("expected old profile lease to be removed")
	}
	got, err := os.ReadFile(leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "old-iot") {
		t.Fatalf("old profile lease remains: %s", got)
	}
	for _, hostname := range []string{"profile-iot", "normal", "reserved-iot"} {
		if !strings.Contains(string(got), hostname) {
			t.Fatalf("expected %s to remain: %s", hostname, got)
		}
	}
	if changed, err := pruneDHCPv4ProfileMismatchedLeases(router, filepath.Join(dir, "dnsmasq.conf"), filepath.Join(dir, "dnsmasq.pid")); err != nil || changed {
		t.Fatalf("second prune = changed:%t err:%v, want false nil", changed, err)
	}
}

func TestWriteDnsmasqConfigKeepsReservationsInHostsFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{
			Server:     "lan-v4",
			MACAddress: "02:00:00:00:01:50",
			Hostname:   "printer",
			IPAddress:  "192.168.10.150",
		}},
	}}}
	changed, reloadOnly, err := writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !reloadOnly {
		t.Fatalf("first write changed=%t reloadOnly=%t, want both true", changed, reloadOnly)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	hostsPath := dnsmasqHostsFile(configPath)
	if !strings.Contains(string(data), "dhcp-hostsfile="+hostsPath+"\n") {
		t.Fatalf("config missing hostsfile include:\n%s", data)
	}
	if strings.Contains(string(data), "dhcp-host=02:00:00:00:01:50") {
		t.Fatalf("config contains inline reservation:\n%s", data)
	}
	hostsData, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostsData), "02:00:00:00:01:50,set:printer,printer,192.168.10.150") {
		t.Fatalf("hosts file missing reservation:\n%s", hostsData)
	}

	router.Spec.Resources[2].Spec = api.DHCPv4ReservationSpec{
		Server:     "lan-v4",
		MACAddress: "02:00:00:00:01:50",
		Hostname:   "printer",
		IPAddress:  "192.168.10.151",
	}
	changed, reloadOnly, err = writeDnsmasqConfig(router, mapStore{}, configPath, pidFile, 53, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !reloadOnly {
		t.Fatalf("reservation-only write changed=%t reloadOnly=%t, want config unchanged and reloadOnly", changed, reloadOnly)
	}
	hostsData, err = os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostsData), "02:00:00:00:01:50,set:printer,printer,192.168.10.151") {
		t.Fatalf("hosts file missing updated reservation:\n%s", hostsData)
	}
}

func TestDnsmasqHostFileLinesStripOptionPrefix(t *testing.T) {
	got := dnsmasqHostFileLines([]string{
		"dhcp-host=02:00:00:00:01:50,192.168.10.150,12h",
		"02:00:00:00:01:51,192.168.10.151,12h",
	})
	want := []string{
		"02:00:00:00:01:50,192.168.10.150,12h",
		"02:00:00:00:01:51,192.168.10.151,12h",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dnsmasqHostFileLines = %#v, want %#v", got, want)
	}
}

func TestDnsmasqStickyHostLinesExcludeReservationConflicts(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{StickyHoldDays: 3}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "nintendo-switch"}, Spec: api.DHCPv4ReservationSpec{
			Server: "lan-v4", MACAddress: "cc:c0:79:0e:73:0a", Hostname: "nintendo-switch", IPAddress: "172.18.1.96",
		}},
	}}}
	rows := []logstore.DHCPStickyLease{
		{MAC: "CC:C0:79:0E:73:0A", IP: "172.18.1.96", Hostname: "nintendo-switch", Family: "ipv4"},
		{MAC: "cc:c0:79:0e:73:0a", IP: "172.18.1.97", Hostname: "old-address", Family: "ipv4"},
		{MAC: "02:00:00:00:01:51", IP: "172.18.1.96", Hostname: "old-client", Family: "ipv4"},
		{MAC: "02:00:00:00:01:52", IP: "172.18.1.98", Hostname: "unrelated", Family: "ipv4"},
	}
	got := dnsmasqStickyHostLinesFromRows(router, rows, "ipv4", "12h")
	want := []string{"dhcp-host=02:00:00:00:01:52,172.18.1.98,unrelated,12h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sticky host lines = %#v, want %#v", got, want)
	}
}

func TestDHCPv4ServerWhenFalseRemovesDnsmasqScope(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"VirtualAddress/lan-vip.role": {Equals: "master"},
			}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "guest-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.20.100", End: "192.168.20.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
		}},
	}}}
	store := statefulDHCPMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}
	controller := DHCPv6ServerController{Router: router, Store: store, DryRun: true}

	effective := controller.effectiveRouter()
	if changed, _, err := writeDnsmasqConfig(effective, store, configPath, pidFile, 53, nil); err != nil || !changed {
		t.Fatalf("write backup config changed=%t err=%v", changed, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dhcp-range=set:lan-v4") {
		t.Fatalf("backup config still contains DHCP scope:\n%s", data)
	}
	if err := controller.saveDHCPv4ServerStatuses(effective, configPath, pidFile); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Server", "lan-v4")
	if status["phase"] != "Pending" || status["reason"] != "WhenFalse" {
		t.Fatalf("backup status = %#v", status)
	}

	store.mapStore[api.NetAPIVersion+"/VirtualAddress/lan-vip"]["role"] = "master"
	effective = controller.effectiveRouter()
	if _, _, err := writeDnsmasqConfig(effective, store, configPath, pidFile, 53, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "dhcp-range=set:lan-v4,192.168.10.100,192.168.10.199,8h") {
		t.Fatalf("master config missing DHCP scope:\n%s", data)
	}
}

func TestDHCPv4ReservationWhenTrueClearsWhenFalseStatus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	whenMaster := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-vip.role": {Equals: "master"},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
			When:        whenMaster,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "guest-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.20.100", End: "192.168.20.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{
			Server:     "lan-v4",
			MACAddress: "02:00:00:00:01:50",
			Hostname:   "printer",
			IPAddress:  "192.168.10.150",
			When:       whenMaster,
		}},
	}}}
	store := statefulDHCPMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}
	controller := DHCPv6ServerController{Router: router, Store: store, DryRun: true}

	effective := controller.effectiveRouter()
	if _, _, err := writeDnsmasqConfig(effective, store, configPath, pidFile, 53, nil); err != nil {
		t.Fatal(err)
	}
	if err := controller.saveDHCPv4ReservationStatuses(effective, configPath, pidFile); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", "printer")
	if status["phase"] != "Pending" || status["reason"] != "WhenFalse" {
		t.Fatalf("backup reservation status = %#v", status)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dhcp-host=02:00:00:00:01:50") {
		t.Fatalf("backup config still contains reservation:\n%s", data)
	}

	disabledHosts, err := os.ReadFile(dnsmasqHostsFile(configPath))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("disabled hosts sidecar: %v", err)
	}
	if strings.Contains(string(disabledHosts), "02:00:00:00:01:50") || strings.Contains(string(disabledHosts), "192.168.10.150") {
		t.Fatalf("disabled reservation leaked to hosts sidecar: %s", disabledHosts)
	}
	store.mapStore[api.NetAPIVersion+"/VirtualAddress/lan-vip"]["role"] = "master"
	effective = controller.effectiveRouter()
	if _, _, err := writeDnsmasqConfig(effective, store, configPath, pidFile, 53, nil); err != nil {
		t.Fatal(err)
	}
	if err := controller.saveDHCPv4ServerStatuses(effective, configPath, pidFile); err != nil {
		t.Fatal(err)
	}
	if err := controller.saveDHCPv4ReservationStatuses(effective, configPath, pidFile); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dhcp-host=02:00:00:00:01:50") {
		t.Fatalf("master config contains inline reservation:\n%s", data)
	}
	hostsData, err := os.ReadFile(dnsmasqHostsFile(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostsData), "02:00:00:00:01:50,set:printer,printer,192.168.10.150") {
		t.Fatalf("master hosts file missing reservation:\n%s", hostsData)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", "printer")
	if status["phase"] != "Rendered" {
		t.Fatalf("master reservation phase = %#v", status)
	}
	if _, ok := status["reason"]; ok {
		t.Fatalf("master reservation kept stale reason: %#v", status)
	}
	if status["server"] != "lan-v4" || status["macAddress"] != "02:00:00:00:01:50" || status["ipAddress"] != "192.168.10.150" {
		t.Fatalf("master reservation status missing identity fields: %#v", status)
	}
	servers, ok := status["servers"].([]string)
	if !ok || len(servers) != 1 || servers[0] != "lan-v4" {
		t.Fatalf("master reservation servers = %#v", status["servers"])
	}
}

func TestIPv6RouterAdvertisementWhenFalseRemovesDnsmasqRA(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dnsmasq.conf")
	pidFile := filepath.Join(dir, "dnsmasq.pid")
	leaseFile := filepath.Join(dir, "state", "dnsmasq", "dnsmasq.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
			Interface:   "lan",
			AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
			LeaseFile:   leaseFile,
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
			Interface: "lan",
			Prefix:    "2001:db8:1::/64",
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"VirtualAddress/lan-vip.role": {Equals: "master"},
			}},
		}},
	}}}
	store := statefulDHCPMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}
	controller := DHCPv6ServerController{Router: router, Store: store, DryRun: true}

	effective := controller.effectiveRouter()
	if changed, _, err := writeDnsmasqConfig(effective, store, configPath, pidFile, 53, nil); err != nil || !changed {
		t.Fatalf("write backup config changed=%t err=%v", changed, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "enable-ra") || strings.Contains(string(data), "constructor:ens19") {
		t.Fatalf("backup config still contains RA lines:\n%s", data)
	}
	store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra", map[string]any{
		"phase":     "Pending",
		"reason":    "WhenFalse",
		"managedBy": "systemd",
		"unitName":  "routerd-ra-observer@lan-ra.service",
	})
	if err := controller.reconcileRouterAdvertisements(context.Background(), configPath, pidFile, true); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra")
	if status["phase"] != "Pending" || status["reason"] != "WhenFalse" {
		t.Fatalf("backup status = %#v", status)
	}
	if status["managedBy"] != "systemd" || status["unitName"] != "routerd-ra-observer@lan-ra.service" {
		t.Fatalf("backup status = %#v, want systemd fields preserved", status)
	}

	store.mapStore[api.NetAPIVersion+"/VirtualAddress/lan-vip"]["role"] = "master"
	effective = controller.effectiveRouter()
	if _, _, err := writeDnsmasqConfig(effective, store, configPath, pidFile, 53, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "enable-ra") {
		t.Fatalf("master config missing RA lines:\n%s", data)
	}
}

func TestDnsmasqCmdlineUsesConfig(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cmdline  []string
		expected bool
	}{
		{name: "equals form", cmdline: []string{"dnsmasq", "--conf-file=/run/routerd/dnsmasq.conf"}, expected: true},
		{name: "separate arg form", cmdline: []string{"dnsmasq", "--conf-file", "/run/routerd/dnsmasq.conf"}, expected: true},
		{name: "different config", cmdline: []string{"dnsmasq", "--conf-file=/usr/local/etc/routerd/dnsmasq.conf"}, expected: false},
		{name: "missing config", cmdline: []string{"dnsmasq", "--keep-in-foreground"}, expected: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := dnsmasqCmdlineUsesConfig(tt.cmdline, "/run/routerd/dnsmasq.conf"); got != tt.expected {
				t.Fatalf("dnsmasqCmdlineUsesConfig() = %t, want %t", got, tt.expected)
			}
		})
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

func indexLine(lines []string, want string) int {
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	return -1
}

func TestRunnerClearsWhenFalseStatusFromObservedPhase(t *testing.T) {
	whenMaster := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-gw-v4.role": {Equals: "master"},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", When: whenMaster}},
	}}}
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "lan-gw-v4", map[string]any{"role": "master"})
	store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{
		"phase":  "Pending",
		"reason": "WhenFalse",
		"observed": map[string]any{
			"phase":         daemonapi.ResourcePhaseBound,
			"currentPrefix": "2001:db8:10::/60",
		},
	})
	runner := &Runner{Router: router, Store: store}
	if err := runner.saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd")
	if status["phase"] != daemonapi.ResourcePhaseBound || status["reason"] != nil || status["currentPrefix"] != "2001:db8:10::/60" {
		t.Fatalf("status = %#v, want observed phase promoted and WhenFalse cleared", status)
	}
}

func TestRunnerKeepsDependsOnFalseStatusWhenWhenTrue(t *testing.T) {
	whenMaster := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-gw-v4.role": {Equals: "master"},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", When: whenMaster}},
	}}}
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "lan-gw-v4", map[string]any{"role": "master"})
	store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{
		"phase":  "Pending",
		"reason": "DependsOnFalse",
		"observed": map[string]any{
			"phase":         daemonapi.ResourcePhaseBound,
			"currentPrefix": "2001:db8:10::/60",
		},
	})
	runner := &Runner{Router: router, Store: store}
	if err := runner.saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd")
	if status["phase"] != "Pending" || status["reason"] != "DependsOnFalse" || status["currentPrefix"] != nil {
		t.Fatalf("status = %#v, want DependsOnFalse preserved without observed promotion", status)
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

func TestIPAddrShowHasIPv6AddressIgnoresKernelPrefixLength(t *testing.T) {
	out := []byte(`7: ens19    inet6 2409:10:3d60:1271::13/128 scope global
7: ens19    inet6 2409:10:3d60:1271::1/64 scope global
7: ens19    inet6 2409:10:3d60:1271::11/128 scope global
`)
	if !ipAddrShowHasIPv6Address(out, "2409:10:3d60:1271::11/64") {
		t.Fatal("ipAddrShowHasIPv6Address = false, want true")
	}
}

func TestIPAddrShowHasIPv6AddressRejectsDADFailedTentative(t *testing.T) {
	out := []byte(`7: ens19    inet6 2409:10:3d60:1271::11/128 scope global dadfailed tentative
`)
	if ipAddrShowHasIPv6Address(out, "2409:10:3d60:1271::11/64") {
		t.Fatal("ipAddrShowHasIPv6Address = true, want false for dadfailed tentative address")
	}
}

func TestIPAddrShowHasIPv6AddressRejectsTentative(t *testing.T) {
	out := []byte(`7: ens19    inet6 2409:10:3d60:1271::11/64 scope global tentative
`)
	if ipAddrShowHasIPv6Address(out, "2409:10:3d60:1271::11/64") {
		t.Fatal("ipAddrShowHasIPv6Address = true, want false for active tentative address")
	}
}

func TestInterfaceIfNameResolvesBridgeIfName(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br-lab"}, Spec: api.BridgeSpec{IfName: "bridge100"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-svnet1"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-transport0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "flets"}, Spec: api.PPPoESessionSpec{IfName: "ppp-wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{TunnelName: "dslite0"}},
	}}}
	if got := interfaceIfName(router, "br-lab"); got != "bridge100" {
		t.Fatalf("interfaceIfName = %q, want bridge100", got)
	}
	if got := interfaceName(router, "br-lab"); got != "bridge100" {
		t.Fatalf("interfaceName = %q, want bridge100", got)
	}
	if got := interfaceIfName(router, "wg-svnet1"); got != "wg-transport0" {
		t.Fatalf("WireGuard interfaceIfName = %q, want wg-transport0", got)
	}
	if got := interfaceIfName(router, "flets"); got != "ppp-wan0" {
		t.Fatalf("PPPoE interfaceIfName = %q, want ppp-wan0", got)
	}
	if got := interfaceIfName(router, "ds-lite"); got != "dslite0" {
		t.Fatalf("DS-Lite interfaceIfName = %q, want dslite0", got)
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

func TestFreeBSDIPv4RouteApplyCommandUsesDSLiteInterface(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "0.0.0.0/0", "gif40", "", "")
	got := strings.Join(append([]string{name}, args...), " ")
	want := "route -n change default -interface gif40"
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

func TestParseIPv6InterfaceAddressPrefixes(t *testing.T) {
	out := `7: lan-vrrp inet6 2001:db8:1200:1241::1/64 scope global dynamic deprecated valid_lft 7199sec preferred_lft 0sec
7: lan-vrrp inet6 2001:db8:1200:1242::1/64 scope global deprecated valid_lft forever preferred_lft 0sec
7: lan-vrrp inet6 2001:db8:1200:1241::23/128 scope global valid_lft forever preferred_lft forever
7: lan-vrrp inet6 2001:db8:1200:1251::1/64 scope global dynamic valid_lft 7199sec preferred_lft 3599sec
7: lan-vrrp inet6 2001:db8:1200:1261::1/64 scope global temporary dynamic valid_lft 7199sec preferred_lft 3599sec
7: lan-vrrp inet6 2001:db8:1200:1271::1/64 scope global mngtmpaddr dynamic deprecated valid_lft 7199sec preferred_lft 0sec
7: lan-vrrp inet6 2001:db8:1200:1281::1/64 scope global autoconf dynamic deprecated valid_lft 7199sec preferred_lft 0sec
7: lan-vrrp inet6 2001:db8:1200:1291::1/64 scope global tentative valid_lft forever preferred_lft forever
7: lan-vrrp inet6 2001:db8:1200:1301::1/64 scope global dadfailed valid_lft forever preferred_lft forever
7: lan-vrrp inet6 fe80::1/64 scope link valid_lft forever preferred_lft forever
`
	got := parseIPv6InterfaceAddressPrefixes(out)
	want := []string{"2001:db8:1200:1241::1/64", "2001:db8:1200:1242::1/64", "2001:db8:1200:1241::23/128"}
	if !slices.Equal(got, want) {
		t.Fatalf("parsed addresses = %#v, want %#v", got, want)
	}
}

func TestIPv6DeprecatedAddressApplyCommand(t *testing.T) {
	name, args := ipv6DeprecatedAddressApplyCommand(platform.OSLinux, "ens19", "2001:db8::1/64", 42)
	if got, want := strings.Join(append([]string{name}, args...), " "), "ip -6 addr replace 2001:db8::1/64 dev ens19 preferred_lft 0 valid_lft 42"; got != want {
		t.Fatalf("Linux command = %q, want %q", got, want)
	}
	name, args = ipv6DeprecatedAddressApplyCommand(platform.OSFreeBSD, "vtnet1", "2001:db8::1/64", 42)
	if got, want := strings.Join(append([]string{name}, args...), " "), "ifconfig vtnet1 inet6 2001:db8::1 prefixlen 64 pltime 0 vltime 42 alias"; got != want {
		t.Fatalf("FreeBSD command = %q, want %q", got, want)
	}
}

func TestLinuxIPv6LocalEndpointArgsDeprecateDelegatedSource(t *testing.T) {
	got := linuxIPv6LocalEndpointArgs("wan0", "2001:db8::100", true)
	want := []string{"-6", "addr", "replace", "2001:db8::100/128", "dev", "wan0", "preferred_lft", "0", "valid_lft", "forever"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delegated endpoint args = %#v, want %#v", got, want)
	}
	ordinary := linuxIPv6LocalEndpointArgs("wan0", "2001:db8::1", false)
	ordinaryWant := []string{"-6", "addr", "replace", "2001:db8::1/128", "dev", "wan0"}
	if !reflect.DeepEqual(ordinary, ordinaryWant) {
		t.Fatalf("ordinary endpoint args = %#v, want %#v", ordinary, ordinaryWant)
	}
}

func TestDeprecatedIPv6ReadbackRequiresExactPrefixAndFlag(t *testing.T) {
	out := []byte("7: wan-vmac inet6 2001:db8:1221::23/128 scope global deprecated tentative valid_lft forever preferred_lft 0sec\n")
	if !ipAddrShowHasDeprecatedIPv6AddressWithPrefix(out, "2001:db8:1221::23/128") {
		t.Fatal("deprecated tentative standby source was not recognized")
	}
	if ipAddrShowHasDeprecatedIPv6AddressWithPrefix(out, "2001:db8:1221::23/64") {
		t.Fatal("wrong prefix length was recognized")
	}
	preferred := []byte("7: wan-vmac inet6 2001:db8:1221::23/128 scope global valid_lft forever preferred_lft forever\n")
	if ipAddrShowHasDeprecatedIPv6AddressWithPrefix(preferred, "2001:db8:1221::23/128") {
		t.Fatal("preferred address was recognized as deprecated")
	}
}

func TestLocalIPv6AddressKeepsHostBitsFromPrefix(t *testing.T) {
	got := localIPv6Address("2409:10:3d60:1250::11/64")
	if got != "2409:10:3d60:1250::11" {
		t.Fatalf("localIPv6Address = %q", got)
	}
}

func TestLinkControllerPublishesInterfaceStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lo"}, Spec: api.InterfaceSpec{IfName: "lo", Managed: false}},
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

func TestDSLiteWhenFalseDoesNotRemainInEffectiveDesiredSet(t *testing.T) {
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite"}},
	}}}
	effective := &api.Router{}
	controller := DSLiteTunnelController{Router: effective, DeclaredRouter: declared, Store: mapStore{}, DryRun: true}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds-lite")
	if status["phase"] != PhaseDisabled || status["reason"] != "WhenFalse" {
		t.Fatalf("when-false tunnel status = %#v", status)
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "ds-lite-a-source"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds-lite-a", Address: "192.0.0.2/29"}},
	}}}
	got, pending, err := dsliteInnerLocalIPv4(router, nil, api.DSLiteTunnelSpec{
		LocalAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-a-source", Field: "address"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending != "" {
		t.Fatalf("unexpected pending = %q", pending)
	}
	if got != "192.0.0.2" {
		t.Fatalf("inner local = %q", got)
	}
}

func TestDSLiteInnerLocalIPv4PendingWhenSourceUnresolved(t *testing.T) {
	got, pending, err := dsliteInnerLocalIPv4(&api.Router{}, nil, api.DSLiteTunnelSpec{
		LocalAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/missing", Field: "address"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("inner local = %q, want empty", got)
	}
	if !strings.Contains(pending, "InnerLocalIPv4Unresolved") {
		t.Fatalf("pending = %q", pending)
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

func TestFirstUsableGlobalIPv6PrefersOnLinkSLAACOverDelegatedEndpoints(t *testing.T) {
	data := []byte(`[{"ifname":"wan-vmac","addr_info":[
		{"family":"inet6","local":"2409:10:3d60:1221::23","prefixlen":128,"scope":"global","dynamic":true,"preferred_life_time":1000},
		{"family":"inet6","local":"2409:10:3d60:1200::10","prefixlen":64,"scope":"global","dynamic":true,"temporary":true,"preferred_life_time":1000},
		{"family":"inet6","local":"2409:10:3d60:1200::11","prefixlen":64,"scope":"global","dynamic":true,"deprecated":true,"preferred_life_time":1000},
		{"family":"inet6","local":"2409:10:3d60:1200::12","prefixlen":64,"scope":"global","dynamic":true,"tentative":true,"preferred_life_time":1000},
		{"family":"inet6","local":"2409:10:3d60:1200:0:5eff:fe00:113","prefixlen":64,"scope":"global","dynamic":true,"preferred_life_time":1000}
	]}]`)
	want := "2409:10:3d60:1200:0:5eff:fe00:113"
	if got := firstUsableGlobalIPv6(data); got != want {
		t.Fatalf("firstUsableGlobalIPv6 = %q, want %q", got, want)
	}
}

func TestFirstUsableIfconfigGlobalIPv6PrefersAutoconf(t *testing.T) {
	data := []byte(`vtnet0: flags=...
	inet6 fe80::be24:11ff:fefb:928d%vtnet0 prefixlen 64 scopeid 0x1
	inet6 2409:10:3d60:1200::dead prefixlen 64 temporary
	inet6 2409:10:3d60:1200::23 prefixlen 128 autoconf
	inet6 2409:10:3d60:1200::24 prefixlen 64 tentative autoconf
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
