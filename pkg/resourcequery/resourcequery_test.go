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
		api.SystemAPIVersion + "/NetworkAdoption/wan-networkd-owned-by-routerd": {
			"phase": "Applied",
		},
	}

	if !DependencyReady(store, api.ResourceDependencySpec{
		Resource: "NetworkAdoption/wan-networkd-owned-by-routerd",
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
