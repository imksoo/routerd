package chain

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestVRRPVMACReadbackMatches(t *testing.T) {
	entry := api.VirtualAddressVRRPFailoverVMACSpec{MACAddress: "02:00:5e:00:01:12", LinkLocalAddress: "fe80::5eff:fe00:112"}
	good := net.HardwareAddr{0x02, 0x00, 0x5e, 0x00, 0x01, 0x12}
	ll := &net.IPNet{IP: net.ParseIP("fe80::5eff:fe00:112"), Mask: net.CIDRMask(64, 128)}
	for _, tc := range []struct {
		name  string
		mac   net.HardwareAddr
		flags net.Flags
		addrs []net.Addr
		want  bool
	}{
		{"exact", good, net.FlagUp, []net.Addr{ll}, true},
		{"wrong mac", net.HardwareAddr{2, 0, 0, 0, 0, 1}, net.FlagUp, []net.Addr{ll}, false},
		{"missing ll", good, net.FlagUp, nil, false},
		{"down", good, 0, []net.Addr{ll}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := vrrpVMACActiveReadbackMatches(entry, tc.mac, tc.flags, tc.addrs); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

func TestVMACHelperCommandErrorIncludesStderr(t *testing.T) {
	err := vmacHelperCommandError([]string{"deactivate", "--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true"}, errors.New("exit status 1"), []byte("Cannot find device \"lan\"\n"))
	if err == nil || !strings.Contains(err.Error(), "Cannot find device \"lan\"") {
		t.Fatalf("helper error = %v", err)
	}
}

func TestIPAddrShowHasStagedIPv6AddressWithPrefix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address string
		out     string
		want    bool
	}{
		{
			name:    "tentative sixty four is staged",
			address: "2409:10:3d60:1221::1/64",
			out:     "17: lan-vrrp    inet6 2409:10:3d60:1221::1/64 scope global tentative valid_lft forever preferred_lft forever\n",
			want:    true,
		},
		{
			name:    "tentative host route is staged",
			address: "2409:10:3d60:1221::21/128",
			out:     "7: wan-vmac    inet6 2409:10:3d60:1221::21/128 scope global tentative valid_lft forever preferred_lft forever\n",
			want:    true,
		},
		{
			name:    "dad failed is rejected",
			address: "2409:10:3d60:1221::21/128",
			out:     "7: wan-vmac    inet6 2409:10:3d60:1221::21/128 scope global tentative dadfailed valid_lft forever preferred_lft forever\n",
			want:    false,
		},
		{
			name:    "wrong address is rejected",
			address: "2409:10:3d60:1221::21/128",
			out:     "7: wan-vmac    inet6 2409:10:3d60:1221::22/128 scope global tentative valid_lft forever preferred_lft forever\n",
			want:    false,
		},
		{
			name:    "wrong prefix is rejected",
			address: "2409:10:3d60:1221::21/128",
			out:     "7: wan-vmac    inet6 2409:10:3d60:1221::21/64 scope global tentative valid_lft forever preferred_lft forever\n",
			want:    false,
		},
		{
			name:    "sixty four address with host prefix is rejected",
			address: "2409:10:3d60:1221::1/64",
			out:     "17: lan-vrrp    inet6 2409:10:3d60:1221::1/128 scope global tentative valid_lft forever preferred_lft forever\n",
			want:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ipAddrShowHasStagedIPv6AddressWithPrefix([]byte(tc.out), tc.address); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

func TestVRRPVMACStagingReadbackAllowsDownBackupWithoutSharedLinkLocal(t *testing.T) {
	entry := api.VirtualAddressVRRPFailoverVMACSpec{MACAddress: "02:00:5e:00:01:12", LinkLocalAddress: "fe80::5eff:fe00:112"}
	mac := net.HardwareAddr{0x02, 0x00, 0x5e, 0x00, 0x01, 0x12}
	ll := &net.IPNet{IP: net.ParseIP("fe80::5eff:fe00:112"), Mask: net.CIDRMask(64, 128)}
	for _, tc := range []struct {
		name  string
		mac   net.HardwareAddr
		flags net.Flags
		addrs []net.Addr
		want  bool
	}{
		{"cold down", mac, 0, nil, true},
		{"up", mac, net.FlagUp, nil, false},
		{"shared ll remains", mac, 0, []net.Addr{ll}, false},
		{"wrong mac", net.HardwareAddr{2, 0, 0, 0, 0, 1}, 0, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := vrrpVMACStagingReadbackMatches(entry, tc.mac, tc.flags, tc.addrs); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

func TestLANAddressControllerEnsureStagingVMACResolvesParentInterfaceAlias(t *testing.T) {
	router := stagingVMACAliasRouter()
	var got []string
	controller := LANAddressController{
		Router:         router,
		DeclaredRouter: router,
		Command: func(_ context.Context, name string, args ...string) error {
			got = append([]string{name}, args...)
			return nil
		},
	}
	if err := controller.ensureStagingVMAC(t.Context(), "lan-vmac"); err != nil {
		t.Fatalf("ensure staging VMAC: %v", err)
	}
	want := []string{"/usr/local/sbin/routerd-vrrp-vmac", "deactivate", "--vmac", "ens19,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true", "--guard-resource", "lan-gw-v4", "--reconcile"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helper invocation = %#v, want %#v", got, want)
	}
}

func stagingVMACAliasRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan-vmac"}, Spec: api.InterfaceSpec{IfName: "lan-vrrp", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "lan-gw-v4"}, Spec: api.VirtualAddressSpec{Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{AdditionalFailoverVMACs: []api.VirtualAddressVRRPFailoverVMACSpec{{
			ParentInterface: "lan", Interface: "lan-vrrp", MACAddress: "02:00:5e:00:01:12", LinkLocalAddress: "fe80::5eff:fe00:112", WithdrawRouterAdvertisement: true,
		}}}}},
	}}}
}

func TestLANAddressControllerEnsureStagingVMACRepairsNonColdReadback(t *testing.T) {
	entry := api.VirtualAddressVRRPFailoverVMACSpec{MACAddress: "02:00:5e:00:01:12", LinkLocalAddress: "fe80::5eff:fe00:112"}
	mac := net.HardwareAddr{0x02, 0x00, 0x5e, 0x00, 0x01, 0x12}
	ll := &net.IPNet{IP: net.ParseIP("fe80::5eff:fe00:112"), Mask: net.CIDRMask(64, 128)}
	for _, tc := range []struct {
		name      string
		mac       net.HardwareAddr
		flags     net.Flags
		addrs     []net.Addr
		wantCalls int
	}{
		{name: "cold down skips helper", mac: mac},
		{name: "up invokes helper", mac: mac, flags: net.FlagUp, wantCalls: 1},
		{name: "link local remains invokes helper", mac: mac, addrs: []net.Addr{ll}, wantCalls: 1},
		{name: "wrong mac invokes helper", mac: net.HardwareAddr{2, 0, 0, 0, 0, 1}, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := stagingVMACAliasRouter()
			cold := vrrpVMACStagingReadbackMatches(entry, tc.mac, tc.flags, tc.addrs)
			controller := LANAddressController{Router: router, DeclaredRouter: router, VMACPresent: func(string) bool { return cold }}
			calls := 0
			controller.EnsureVMAC = func(context.Context, string) error { calls++; return nil }
			if err := controller.ensureStagingVMAC(t.Context(), "lan-vmac"); err != nil {
				t.Fatalf("ensure staging VMAC: %v", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("helper calls=%d want=%d", calls, tc.wantCalls)
			}
		})
	}
}
