// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"reflect"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/sysctlprofile"
)

func TestDerivedSysctlResourcesForRouterHost(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", PrefixDelegation: "wan-pd", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-nat"}, Spec: api.NAT44RuleSpec{Type: "masquerade", EgressInterface: "wan"}},
	}}}

	keys := derivedSysctlKeys(t, router)
	for _, want := range []string{
		"net.ipv4.ip_forward",
		"net.ipv4.conf.all.send_redirects",
		"net.ipv4.conf.default.send_redirects",
		"net.ipv4.conf.ds-lite-a.rp_filter",
		"net.ipv4.conf.ens19.send_redirects",
		"net.ipv6.conf.ens18.accept_ra",
		"net.ipv6.conf.ens19.accept_ra",
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
	for _, res := range DerivedSysctlResources(router) {
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

func TestPackageFeaturesCoverStandaloneDataplaneResources(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want []string
	}{
		{kind: "PortForward", want: []string{"nft"}},
		{kind: "IngressService", want: []string{"nft"}},
		{kind: "LocalServiceRedirect", want: []string{"nft"}},
		{kind: "IPv4ReversePathFilter", want: []string{"nft"}},
		{kind: "EgressRoutePolicy", want: []string{"base", "nft"}},
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
	for _, res := range DerivedSysctlResources(router) {
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
