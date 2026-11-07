package chain

import (
	"errors"
	"net"
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
