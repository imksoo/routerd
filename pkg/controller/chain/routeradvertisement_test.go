// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestBuildDeprecatedPrefixRouterAdvertisement(t *testing.T) {
	spec := api.IPv6RouterAdvertisementSpec{
		MFlag:         true,
		OFlag:         true,
		PRFPreference: "low",
		ValidLifetime: "7200",
	}
	prefixes := []deprecatedPrefixInformation{{
		Prefix:        netip.MustParsePrefix("2001:db8:1241::99/64"),
		ValidLifetime: 3601,
	}}

	got := buildDeprecatedPrefixRouterAdvertisement(spec, prefixes)
	want, err := hex.DecodeString("8600000040d81c200000000000000000" +
		"030440c000000e11000000000000000020010db8124100000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("RA packet = %x, want %x", got, want)
	}
	if got[3] != 0 || got[8] != 0 || got[12] != 0 {
		t.Fatalf("checksum, reachable time, and retrans timer must remain zero: %x", got)
	}
}

func TestRouterAdvertisementLifetimeSafelyFitsUint16(t *testing.T) {
	for _, test := range []struct {
		value string
		want  uint16
	}{
		{value: "", want: defaultRouterAdvertisementLifetime},
		{value: "invalid", want: defaultRouterAdvertisementLifetime},
		{value: "-1", want: defaultRouterAdvertisementLifetime},
		{value: "0", want: 0},
		{value: "7200", want: 7200},
		{value: "65535", want: 65535},
		{value: "65536", want: 65535},
		{value: "18446744073709551615", want: 65535},
	} {
		if got := routerAdvertisementLifetime(test.value); got != test.want {
			t.Errorf("routerAdvertisementLifetime(%q) = %d, want %d", test.value, got, test.want)
		}
	}
}

func TestIPv6RouterAdvertisementControllerSendsPreviousPrefixWithdrawalsEveryReconcile(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan-vmac"},
			Spec:     api.InterfaceSpec{IfName: "lan-vrrp"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"},
			Metadata: api.ObjectMeta{Name: "lan-ra"},
			Spec: api.IPv6RouterAdvertisementSpec{
				Interface:         "lan-vmac",
				PrefixFrom:        api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
				MFlag:             true,
				OFlag:             true,
				PRFPreference:     "high",
				PreferredLifetime: "3600",
				ValidLifetime:     "7200",
			},
		},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {
			"phase":   "Applied",
			"address": "2409:10:3d60:1221::1/64",
			"previousAddresses": []map[string]any{
				{"address": "2409:10:3d60:1241::1/64", "expiresAt": now.Add(time.Hour)},
				{"address": "2409:10:3d60:1241::99/64", "expiresAt": now.Add(time.Hour + 30*time.Second)},
				{"address": "2409:10:3d60:1251::1/64", "expiresAt": now.Add(2 * time.Minute)},
				{"address": "2409:10:3d60:1231::1/64", "expiresAt": now.Add(-time.Second)},
				{"address": "2409:10:3d60:1221::1/64", "expiresAt": now.Add(time.Hour)},
				{"address": "2409:10:3d60:1261::23/128", "expiresAt": now.Add(time.Hour)},
			},
		},
	}
	var sourceCalls, sendCalls int
	var gotIfname string
	var gotSource netip.Addr
	var gotPacket []byte
	controller := DHCPv6ServerController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
		RALinkLocal: func(_ context.Context, ifname string) (netip.Addr, error) {
			sourceCalls++
			gotIfname = ifname
			return netip.MustParseAddr("fe80::5eff:fe00:112"), nil
		},
		RASender: func(_ context.Context, ifname string, source netip.Addr, packet []byte) error {
			sendCalls++
			if gotIfname != ifname {
				t.Fatalf("sender interface = %q, source resolver interface = %q", ifname, gotIfname)
			}
			gotSource = source
			gotPacket = append([]byte(nil), packet...)
			return nil
		},
	}

	for i := 0; i < 2; i++ {
		if err := controller.reconcileRouterAdvertisements(t.Context(), "/run/routerd/dnsmasq.conf", "/run/routerd/dnsmasq.pid", false); err != nil {
			t.Fatal(err)
		}
	}
	if sourceCalls != 2 || sendCalls != 2 {
		t.Fatalf("periodic reconcile source calls=%d sender calls=%d, want 2 each", sourceCalls, sendCalls)
	}
	if gotIfname != "lan-vrrp" || gotSource.String() != "fe80::5eff:fe00:112" {
		t.Fatalf("RA source = %s%%%s, want fe80::5eff:fe00:112%%lan-vrrp", gotSource, gotIfname)
	}
	if len(gotPacket) != 16+2*32 {
		t.Fatalf("RA packet length = %d, want %d: %x", len(gotPacket), 16+2*32, gotPacket)
	}
	if gotPacket[5] != 0xc8 {
		t.Fatalf("RA flags = %#x, want M+O+high (0xc8)", gotPacket[5])
	}
	if got := binary.BigEndian.Uint16(gotPacket[6:8]); got != 7200 {
		t.Fatalf("router lifetime = %d, want 7200", got)
	}
	wantPrefixes := []string{"2409:10:3d60:1241::", "2409:10:3d60:1251::"}
	wantValid := []uint32{3630, 120}
	for i := range wantPrefixes {
		option := gotPacket[16+i*32 : 16+(i+1)*32]
		if option[0] != 3 || option[1] != 4 || option[2] != 64 || option[3] != 0xc0 {
			t.Fatalf("PIO %d header = %x, want type=3 len=4 /64 L+A", i, option[:4])
		}
		if got := binary.BigEndian.Uint32(option[4:8]); got != wantValid[i] {
			t.Fatalf("PIO %d valid lifetime = %d, want %d", i, got, wantValid[i])
		}
		if got := binary.BigEndian.Uint32(option[8:12]); got != 0 {
			t.Fatalf("PIO %d preferred lifetime = %d, want 0", i, got)
		}
		addr, ok := netip.AddrFromSlice(option[16:32])
		if !ok || addr.String() != wantPrefixes[i] {
			t.Fatalf("PIO %d prefix = %v, want %s", i, addr, wantPrefixes[i])
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra")
	if status["phase"] != "Applied" || status["prefix"] != "2409:10:3d60:1221::/64" {
		t.Fatalf("RA status = %#v", status)
	}
}

func TestIPv6RouterAdvertisementControllerReturnsRawSendError(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"},
			Metadata: api.ObjectMeta{Name: "lan-ra"},
			Spec: api.IPv6RouterAdvertisementSpec{
				Interface:     "lan-vrrp",
				PrefixFrom:    api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
				ValidLifetime: "7200",
			},
		},
	}}}
	store := mapStore{api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {
		"address": "2001:db8:1221::1/64",
		"previousAddresses": []map[string]any{
			{"address": "2001:db8:1241::1/64", "expiresAt": now.Add(time.Hour)},
		},
	}}
	sendErr := errors.New("injected raw ICMPv6 send failure")
	controller := DHCPv6ServerController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
		RALinkLocal: func(context.Context, string) (netip.Addr, error) {
			return netip.MustParseAddr("fe80::1"), nil
		},
		RASender: func(context.Context, string, netip.Addr, []byte) error {
			return sendErr
		},
	}

	err := controller.reconcileRouterAdvertisements(t.Context(), "/run/routerd/dnsmasq.conf", "/run/routerd/dnsmasq.pid", false)
	if !errors.Is(err, sendErr) {
		t.Fatalf("reconcile error = %v, want wrapped %v", err, sendErr)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra"); len(status) != 0 {
		t.Fatalf("failed RA send was published as applied: %#v", status)
	}
}

func TestDnsmasqDoesNotRenderDeprecatedPreviousPrefixRange(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan-vmac"}, Spec: api.InterfaceSpec{IfName: "lan-vrrp"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{Interface: "lan-vmac", Mode: "stateless", LeaseTime: "12h"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
			Interface: "lan-vmac", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
		}},
	}}}
	store := mapStore{api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {
		"address": "2409:10:3d60:1221::1/64",
		"previousAddresses": []map[string]any{
			{"address": "2409:10:3d60:1241::1/64", "expiresAt": time.Now().UTC().Add(time.Hour)},
		},
	}}
	lines, err := dnsmasqLANServiceLines(router, store)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")
	if strings.Contains(got, "deprecated") || strings.Contains(got, "2409:10:3d60:1241::") {
		t.Fatalf("dnsmasq still renders an explicit deprecated range:\n%s", got)
	}
	if !strings.Contains(got, "dhcp-range=set:lan-v6,::,constructor:lan-vrrp,ra-stateless,64,12h") {
		t.Fatalf("current constructor range disappeared:\n%s", got)
	}
}
