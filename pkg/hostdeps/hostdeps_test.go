// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sysctlprofile"
)

func TestDerivedSysctlResourcesForRouterHost(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", PrefixDelegation: "wan-pd", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "flets"}, Spec: api.PPPoESessionSpec{Interface: "wan", IfName: "ppp-flets"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-mesh"}, Spec: api.WireGuardInterfaceSpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-nat"}, Spec: api.NAT44RuleSpec{Type: "masquerade", EgressInterface: "wan"}},
	}}}

	keys := derivedSysctlKeys(t, router)
	for _, want := range []string{
		"net.ipv4.ip_forward",
		"net.ipv4.conf.all.send_redirects",
		"net.ipv4.conf.default.send_redirects",
		"net.ipv4.conf.ds-lite-a.rp_filter",
		"net.ipv4.conf.ppp-flets.rp_filter",
		"net.ipv4.conf.wg-mesh.rp_filter",
		"net.ipv4.conf.ens19.send_redirects",
		"net.ipv6.conf.ens18.accept_ra",
		"net.ipv6.conf.ens19.accept_ra",
		"net.ipv6.conf.ens19.accept_ra_defrtr",
	} {
		if !keys[want] {
			t.Fatalf("missing derived sysctl %s in %#v", want, sortedKeys(keys))
		}
	}
}

func TestExplicitSysctlSuppressesDerivedDuplicate(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "custom-ip-forward"}, Spec: api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: "1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-nat"}, Spec: api.NAT44RuleSpec{Type: "masquerade", EgressInterface: "wan"}},
	}}}

	count := 0
	for _, res := range DerivedSysctlResourcesForOS(router, platform.OSLinux) {
		switch res.Kind {
		case "Sysctl":
			spec, err := res.SysctlSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.Key == "net.ipv4.ip_forward" {
				count++
			}
		case "SysctlProfile":
			spec, err := res.SysctlProfileSpec()
			if err != nil {
				t.Fatal(err)
			}
			entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Key == "net.ipv4.ip_forward" {
					count++
				}
			}
		}
	}
	if count != 0 {
		t.Fatalf("derived duplicate ip_forward count = %d, want 0", count)
	}
}

func TestKernelModulesForTunnelInterfaceModes(t *testing.T) {
	tests := []struct {
		mode string
		want []string
	}{
		{mode: "ipip", want: []string{"ipip"}},
		{mode: "gre", want: []string{"ip_gre"}},
		{mode: "fou", want: []string{"fou", "ipip"}},
		{mode: "gue", want: []string{"fou", "ipip"}},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
				Metadata: api.ObjectMeta{Name: "tun0"},
				Spec:     api.TunnelInterfaceSpec{Mode: tt.mode},
			}}}}
			if got := KernelModulesForOS(router, platform.OSLinux); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("KernelModules(%s) = %#v, want %#v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestKernelModulesForFreeBSDUsePFRuntimeModules(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"}, Metadata: api.ObjectMeta{Name: "lan-policy"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "FirewallEventLog"}, Metadata: api.ObjectMeta{Name: "firewall-log"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "wan-pppoe"}, Spec: api.PPPoESessionSpec{Interface: "wan"}},
	}}}
	if got, want := KernelModulesForOS(router, platform.OSFreeBSD), []string{"ng_iface", "ng_pppoe", "ng_tcpmss", "pf", "pflog"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("KernelModulesForOS(freebsd) = %#v, want %#v", got, want)
	}
	if got, want := KernelModulesForOS(router, platform.OSLinux), []string{"nf_conntrack", "nfnetlink_log"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("KernelModulesForOS(linux) = %#v, want %#v", got, want)
	}
	resources := KernelModuleResourcesForOS(router, platform.OSFreeBSD)
	if len(resources) != 1 {
		t.Fatalf("resources = %#v", resources)
	}
	spec, err := resources[0].KernelModuleSpec()
	if err != nil {
		t.Fatal(err)
	}
	if !spec.Persistent || !reflect.DeepEqual(spec.Modules, []string{"ng_iface", "ng_pppoe", "ng_tcpmss", "pf", "pflog"}) {
		t.Fatalf("FreeBSD kernel module spec = %#v", spec)
	}
}

func TestDerivedSysctlResourcesForSAMAreStrictlyGated(t *testing.T) {
	empty := &api.Router{}
	if keys := derivedSysctlKeys(t, empty); len(keys) != 0 {
		t.Fatalf("empty router derived sysctls = %#v, want none", keys)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"}, Metadata: api.ObjectMeta{Name: "app"}, Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   "10.0.1.123/32",
			OwnerSide: "onprem",
			Capture:   api.AddressCapture{Type: "proxy-arp", Interface: "lan0"},
			Delivery:  api.AddressDelivery{PeerRef: "cloud-main", Mode: "route", TunnelInterface: "wg-sam"},
		}},
	}}}
	keys := derivedSysctlKeys(t, router)
	for _, want := range []string{"net.ipv4.ip_forward", "net.ipv4.conf.lan0.proxy_arp"} {
		if !keys[want] {
			t.Fatalf("missing SAM sysctl %s in %#v", want, sortedKeys(keys))
		}
	}
	for _, unwanted := range []string{"net.ipv4.conf.all.rp_filter", "net.ipv4.conf.default.rp_filter"} {
		if keys[unwanted] {
			t.Fatalf("SAM must not derive rp_filter sysctl %s: %#v", unwanted, sortedKeys(keys))
		}
	}

	spec := router.Spec.Resources[0].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	router.Spec.Resources[0].Spec = spec
	keys = derivedSysctlKeys(t, router)
	if keys["net.ipv4.conf.lan0.proxy_arp"] {
		t.Fatalf("VRRP-gated SAM must not derive unconditional proxy_arp sysctl: %#v", sortedKeys(keys))
	}
	if !keys["net.ipv4.ip_forward"] {
		t.Fatalf("VRRP-gated SAM still needs ip_forward sysctl: %#v", sortedKeys(keys))
	}
}

func TestPackageFeaturesIncludeArpingForVRRPGatedSAMCapture(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{Name: "app"},
		Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   "10.0.1.123/32",
			OwnerSide: "onprem",
			Capture: api.AddressCapture{
				Type:       "proxy-arp",
				Interface:  "lan0",
				ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"},
			},
			Delivery: api.AddressDelivery{PeerRef: "cloud-main", Mode: "route", TunnelInterface: "wg-sam"},
		},
	}}}}
	if features := packageFeatures(router); !features["arping"] {
		t.Fatalf("features = %#v, want arping for VRRP-gated SAM capture", features)
	}
}

func TestPackageFeaturesCoverStandaloneDataplaneResources(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want []string
	}{
		{kind: "PortForward", want: []string{"nft"}},
		{kind: "IngressService", want: []string{"nft"}},
		{kind: "LocalServiceRedirect", want: []string{"nft"}},
		{kind: "FirewallFlowPinhole", want: []string{"nft"}},
		{kind: "DSLiteTunnel", want: []string{"base", "nft"}},
		{kind: "PPPoESession", want: []string{"pppoe", "nft"}},
		{kind: "WireGuardInterface", want: []string{"wireguard", "kmod", "nft"}},
		{kind: "EgressRoutePolicy", want: []string{"base", "nft"}},
		{kind: "HealthCheck", want: []string{"network-utils"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			router := routerWithSingleKind(tc.kind)
			features := packageFeatures(router)
			for _, want := range tc.want {
				if !features[want] {
					t.Fatalf("packageFeatures(%s) missing %s in %#v", tc.kind, want, features)
				}
			}
		})
	}
}

func TestPackageFeaturesDeriveVRRPRuntimeOnlyForVRRPVirtualAddress(t *testing.T) {
	static := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "static-vip"},
		Spec:     api.VirtualAddressSpec{Family: "ipv4", Mode: "static"},
	}}}}
	if features := packageFeatures(static); features["vrrp"] {
		t.Fatalf("static VirtualAddress features = %#v, want no vrrp", features)
	}
	vrrp := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "vrrp-vip"},
		Spec:     api.VirtualAddressSpec{Family: "ipv4", Mode: "vrrp"},
	}}}}
	features := packageFeatures(vrrp)
	for _, want := range []string{"base", "vrrp"} {
		if !features[want] {
			t.Fatalf("vrrp VirtualAddress features missing %s in %#v", want, features)
		}
	}
}

func TestPackageFeaturesInternalEventResourcesNeedNoHostPackages(t *testing.T) {
	for _, kind := range []string{"EventRule", "DerivedEvent"} {
		t.Run(kind, func(t *testing.T) {
			if got := packageFeatures(routerWithSingleKind(kind)); !reflect.DeepEqual(got, map[string]bool{}) {
				t.Fatalf("packageFeatures(%s) = %#v, want no host package features", kind, got)
			}
		})
	}
}

func routerWithSingleKind(kind string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{Kind: kind},
		Metadata: api.ObjectMeta{Name: "test"},
	}}}}
}

func derivedSysctlKeys(t *testing.T, router *api.Router) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, res := range DerivedSysctlResourcesForOS(router, platform.OSLinux) {
		switch res.Kind {
		case "Sysctl":
			spec, err := res.SysctlSpec()
			if err != nil {
				t.Fatal(err)
			}
			keys[spec.Key] = true
		case "SysctlProfile":
			spec, err := res.SysctlProfileSpec()
			if err != nil {
				t.Fatal(err)
			}
			entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				keys[entry.Key] = true
			}
		}
	}
	return keys
}
