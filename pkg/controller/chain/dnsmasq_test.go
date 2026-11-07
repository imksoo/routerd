// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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
