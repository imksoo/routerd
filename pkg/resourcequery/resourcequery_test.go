// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestValuesFromRouterResolvesDNSZone(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.DNSZoneSpec{Zone: "home.internal"}},
	}}}

	values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "DNSZone/home", Field: "zone"})
	if len(values) != 1 || values[0] != "home.internal" {
		t.Fatalf("unexpected values: %#v", values)
	}
	if values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "DNSZone/home"}); len(values) != 0 {
		t.Fatalf("expected field to be explicit, got %#v", values)
	}
}

func TestValuesFromRouterResolvesIPv4StaticAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "172.18.0.2/16"}},
	}}}

	values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"})
	if len(values) != 1 || values[0] != "172.18.0.2/16" {
		t.Fatalf("unexpected values: %#v", values)
	}
	if values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base"}); len(values) != 0 {
		t.Fatalf("expected field to be explicit, got %#v", values)
	}
}
