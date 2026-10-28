// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// Filtering is used by both controllers and read-only queries. It must omit
// inactive references from the result without erasing the reusable intent.
func TestFilterRouterByWhenPreservesDNSZoneSourcesAcrossRoleChange(t *testing.T) {
	for _, representation := range []string{"typed", "pointer", "json"} {
		t.Run(representation, func(t *testing.T) {
			when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/lan-vip.role": {Equals: "master"}}}
			declaredRecord := api.DNSZoneRecordSpec{
				Hostname: "router",
				IPv4From: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"},
				IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
			}
			zoneSpec := api.DNSZoneSpec{Zone: "example.test", Records: []api.DNSZoneRecordSpec{declaredRecord}}
			zone := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: zoneSpec}
			switch representation {
			case "pointer":
				zone.Spec = &zoneSpec
			case "json":
				data, err := json.Marshal(zone)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &zone); err != nil {
					t.Fatal(err)
				}
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "lan-vip"}, Spec: api.VirtualAddressSpec{Address: "192.0.2.1/32"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{When: when}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv6DelegatedAddressSpec{When: when}},
				zone,
			}}}
			store := statefulMapStore{mapStore: mapStore{}}
			for _, role := range []string{"backup", "master"} {
				t.Run(role, func(t *testing.T) {
					store.mapStore[api.NetAPIVersion+"/VirtualAddress/lan-vip"] = map[string]any{"role": role}
					filtered := FilterRouterByWhen(router, store)
					result, ok := findResource(filtered, "DNSZone", "home")
					if !ok {
						t.Fatal("DNSZone/home missing")
					}
					spec, err := result.DNSZoneSpec()
					if err != nil {
						t.Fatal(err)
					}
					want := declaredRecord
					if role == "backup" {
						want.IPv4From = api.StatusValueSourceSpec{}
						want.IPv6From = api.StatusValueSourceSpec{}
					}
					if !reflect.DeepEqual(spec.Records, []api.DNSZoneRecordSpec{want}) {
						t.Errorf("filtered records = %#v, want %#v", spec.Records, want)
					}
					original, err := zone.DNSZoneSpec()
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(original.Records, []api.DNSZoneRecordSpec{declaredRecord}) {
						t.Errorf("declared records mutated: %#v", original.Records)
					}
				})
			}
		})
	}
}
