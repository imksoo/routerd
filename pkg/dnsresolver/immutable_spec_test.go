// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNormalizeSpecPreservesDeclaredSlices(t *testing.T) {
	declared := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{}},
		Sources: []api.DNSResolverSourceSpec{{Kind: "forward", Upstreams: []string{"udp://192.0.2.53"}}},
	}
	want := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{}},
		Sources: []api.DNSResolverSourceSpec{{Kind: "forward", Upstreams: []string{"udp://192.0.2.53"}}},
	}
	normalized := NormalizeSpec(declared)
	if normalized.Listen[0].Port != 5053 || normalized.Sources[0].QueryTimeout != "3s" {
		t.Fatalf("defaults not applied: %+v", normalized)
	}
	if !reflect.DeepEqual(declared, want) {
		t.Fatalf("normalization changed declaration: got %+v, want %+v", declared, want)
	}
	if err := Validate(declared); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(declared, want) {
		t.Fatal("validation changed declaration")
	}
}

func TestValidateRejectsInvalidCacheBounds(t *testing.T) {
	for _, cache := range []api.DNSResolverCacheSpec{
		{MaxEntries: -1}, {MinTTL: "-1s"}, {MaxTTL: "invalid"}, {NegativeTTL: "-1s"},
	} {
		spec := api.DNSResolverSpec{Cache: cache, Sources: []api.DNSResolverSourceSpec{{Kind: "forward", Upstreams: []string{"udp://192.0.2.53"}}}}
		if err := Validate(spec); err == nil {
			t.Fatalf("invalid cache accepted: %+v", cache)
		}
	}
	spec := api.DNSResolverSpec{Cache: api.DNSResolverCacheSpec{MinTTL: "0s", MaxTTL: "0s", NegativeTTL: "0s"}, Sources: []api.DNSResolverSourceSpec{{Kind: "forward", Upstreams: []string{"udp://192.0.2.53"}}}}
	if err := Validate(spec); err != nil {
		t.Fatalf("explicit zero cache TTL rejected: %v", err)
	}
}
