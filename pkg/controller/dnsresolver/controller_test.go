package dnsresolver

import (
	"context"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestReconcileResolvesListenAddressStatusRef(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {"address": "2409:10:3d60:1200::1"},
	}
	controller := Controller{
		Router: dnsResolverRouter(nil, []api.DNSResolverListenAddressSourceSpec{{Field: "${IPv6DelegatedAddress/lan-base.status.address}"}}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
	resolved, _ := status["listenAddresses"].([]string)
	if len(resolved) != 1 || resolved[0] != "2409:10:3d60:1200::1" {
		t.Fatalf("listenAddresses = %#v", status["listenAddresses"])
	}
}

func TestReconcilePendingWhenListenAddressUnresolved(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouter(nil, []api.DNSResolverListenAddressSourceSpec{{Field: "${IPv6DelegatedAddress/lan-base.status.address}"}}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v", status)
	}
	if !strings.Contains(strings.TrimSpace(status["message"].(string)), "AddressUnresolved") {
		t.Fatalf("message = %#v", status["message"])
	}
}

func TestDNSResolverDependsOnExplicitAddressSource(t *testing.T) {
	router := dnsResolverRouter([]string{"172.18.0.1"}, []api.DNSResolverListenAddressSourceSpec{{Field: "${IPv6DelegatedAddress/lan-base.status.address}"}})
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: "lan-base"}) {
		t.Fatal("expected dependency on IPv6DelegatedAddress/lan-base")
	}
	if dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: "other"}) {
		t.Fatal("unexpected dependency on IPv6DelegatedAddress/other")
	}
}

func dnsResolverRouter(addresses []string, addressSources []api.DNSResolverListenAddressSourceSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: addresses, AddressSources: addressSources, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{
					Name:      "default",
					Kind:      "upstream",
					Match:     []string{"."},
					Upstreams: []string{"udp://1.1.1.1:53"},
				}},
			},
		},
	}}}
}
