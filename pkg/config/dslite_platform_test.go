// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDSLiteDefaultRouteMetricMatchesPlatformOwner(t *testing.T) {
	for _, test := range []struct {
		os        platform.OS
		metric    int
		wantError bool
	}{
		{platform.OSLinux, 20, false}, {platform.OSFreeBSD, 0, false}, {platform.OSFreeBSD, 20, true}, {platform.OSLinux, -1, true},
	} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "gif77", LocalAddressSource: "static", LocalAddress: "2001:db8::1", AFTRIPv6: "2001:db8::2", DefaultRoute: true, RouteMetric: test.metric}},
		}}}
		err := ValidateForOS(router, test.os)
		if (err != nil) != test.wantError || err != nil && !strings.Contains(err.Error(), "routeMetric") {
			t.Fatalf("os=%s metric=%d err=%v", test.os, test.metric, err)
		}
	}
}

func TestDSLiteRejectsRemovedLocalIPv6StatusExpression(t *testing.T) {
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", AFTRIPv6: "2001:db8::2", LocalIPv6Source: "Interface/wan.ipv6Addresses"}}
	if _, err := validateWANResource(resource, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "localIPv6Source was removed") {
		t.Fatalf("legacy ambiguous source admitted: %v", err)
	}
}

func TestDSLiteEncapsulationLimitMatchesPlatformOwner(t *testing.T) {
	for _, test := range []struct {
		os       platform.OS
		limit    string
		rejected bool
	}{{platform.OSLinux, "4", false}, {platform.OSFreeBSD, "none", false}, {platform.OSFreeBSD, "4", true}} {
		resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", AFTRIPv6: "2001:db8::2", EncapsulationLimit: test.limit}}
		_, err := validateWANResource(resource, test.os)
		if (err != nil) != test.rejected || err != nil && !strings.Contains(err.Error(), "encapsulationLimit") {
			t.Fatalf("os=%s limit=%s err=%v", test.os, test.limit, err)
		}
	}
}
