// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"routerd/pkg/api"
)

func TestIngressServiceDNSResolverEndpointPrefersLoopback(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{
				Addresses: []string{"192.0.2.1/32", "127.0.0.1"},
				Port:      1053,
			}}},
		},
	}}}
	endpoint, ok := dnsResolverEndpoint(router, nil)
	if !ok || endpoint != "127.0.0.1:1053" {
		t.Fatalf("endpoint = %q, %v; want 127.0.0.1:1053, true", endpoint, ok)
	}
}

func TestIngressServiceDNSResolverEndpointUsesAddressFrom(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{
				AddressFrom: []api.StatusValueSourceSpec{{Resource: "IPv4StaticAddress/lan-base", Field: "address"}},
			}}},
		},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/IPv4StaticAddress/lan-base": {"address": "192.0.2.53/32"},
	}
	endpoint, ok := dnsResolverEndpoint(router, store)
	if !ok || endpoint != "192.0.2.53:53" {
		t.Fatalf("endpoint = %q, %v; want 192.0.2.53:53, true", endpoint, ok)
	}
}
