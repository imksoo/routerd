// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"testing"

	"routerd/pkg/api"
)

type mapStore map[string]map[string]any

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func TestDependencyReadyUsesKindAPIVersion(t *testing.T) {
	store := mapStore{
		api.SystemAPIVersion + "/Package/router-runtime": {
			"phase": "Applied",
		},
	}

	if !DependencyReady(store, api.ResourceDependencySpec{
		Resource: "Package/router-runtime",
		Phase:    "Applied",
	}) {
		t.Fatalf("expected system resource dependency to be ready")
	}
}

func TestValueUsesKindAPIVersion(t *testing.T) {
	store := mapStore{
		api.SystemAPIVersion + "/Package/router-runtime": {
			"phase":       "Applied",
			"packageList": []string{"nftables", "conntrack"},
		},
	}

	values := Values(store, api.StatusValueSourceSpec{
		Resource: "Package/router-runtime",
		Field:    "packageList",
	})
	if len(values) != 2 || values[0] != "nftables" || values[1] != "conntrack" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

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

func TestSourceReadyUsesKindAPIVersion(t *testing.T) {
	store := mapStore{
		api.FirewallAPIVersion + "/FirewallPolicy/default": {
			"phase": "Applied",
		},
	}

	if !SourceReady(store, "FirewallPolicy/default") {
		t.Fatalf("expected firewall resource source to be ready")
	}
}
