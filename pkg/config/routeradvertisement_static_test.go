// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestStaticRAPrefixAdmissionMatchesDnsmasq(t *testing.T) {
	for _, prefix := range []string{"2001:db8:1::123/64", "2001:db8:1::/56", "192.0.2.0/24", "invalid"} {
		router := routerWithResources(
			testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "eth0"}),
			testResource(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan", api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: prefix}),
		)
		err := Validate(router)
		if prefix == "2001:db8:1::123/64" && err != nil {
			t.Fatalf("supported static dnsmasq RA rejected: %v", err)
		}
		if prefix != "2001:db8:1::123/64" && err == nil {
			t.Fatalf("unsupported static dnsmasq RA admitted: %s", prefix)
		}
	}
	router := routerWithResources(
		testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "eth0"}),
		testResource(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan", api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8::/64", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "address"}}),
	)
	if err := Validate(router); err == nil {
		t.Fatal("two competing RA prefix sources were admitted")
	}
}
