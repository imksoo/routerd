// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestRuntimeShapeChangedRejectsLifecycleResources(t *testing.T) {
	current := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: map[string]any{"listen": []any{"127.0.0.1"}}},
	}}}
	next := &api.Router{Spec: api.RouterSpec{Resources: append([]api.Resource(nil), current.Spec.Resources...)}}
	next.Spec.Resources[0].Spec = map[string]any{"listen": []any{"127.0.0.2"}}

	changed, resources := runtimeShapeChanged(current, next)
	if !changed || len(resources) != 1 {
		t.Fatalf("changed=%v resources=%v", changed, resources)
	}
}

func TestRuntimeShapeChangedAllowsDataplaneOnlyChange(t *testing.T) {
	current := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow"}},
	}}}
	next := &api.Router{Spec: api.RouterSpec{Resources: nil}}
	if changed, resources := runtimeShapeChanged(current, next); changed {
		t.Fatalf("changed=%v resources=%v", changed, resources)
	}
}
