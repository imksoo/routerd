// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestRuntimeShapeChangedReloadsDSLiteSourceChange(t *testing.T) {
	current := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
		Metadata: api.ObjectMeta{Name: "ds-lite-ra"},
		Spec: api.DSLiteTunnelSpec{
			Interface:             "wan-vmac",
			AFTRIPv6:              "2001:db8::1",
			LocalAddressSource:    "delegatedAddress",
			LocalDelegatedAddress: "ds-lite-ra-source-v6",
			LocalAddressSuffix:    "::23",
		},
	}}}}
	next := &api.Router{Spec: api.RouterSpec{Resources: append([]api.Resource(nil), current.Spec.Resources...)}}
	next.Spec.Resources[0].Spec = api.DSLiteTunnelSpec{
		Interface:          "wan-vmac",
		AFTRIPv6:           "2001:db8::1",
		LocalAddressSource: "interface",
	}

	changed, resources := runtimeShapeChanged(current, next)
	if !changed {
		t.Fatal("DSLiteTunnel source change did not require runtime generation reload")
	}
	want := "net.routerd.net/v1alpha1/DSLiteTunnel/ds-lite-ra"
	if len(resources) != 1 || resources[0] != want {
		t.Fatalf("changed resources = %v, want [%s]", resources, want)
	}
}

func TestRuntimeReloadHasFiniteDeadline(t *testing.T) {
	before := time.Now()
	wantErr := errors.New("stop after inspecting deadline")
	mutator := serveConfigMutator{
		reload: func(ctx context.Context, _ *api.Router) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("runtime reload context has no deadline")
			}
			after := time.Now()
			if deadline.Before(before.Add(time.Minute)) || deadline.After(after.Add(time.Minute)) {
				t.Fatalf("deadline %s outside creation interval [%s, %s]", deadline, before.Add(time.Minute), after.Add(time.Minute))
			}
			return wantErr
		},
	}
	if err := mutator.reloadRuntime(&api.Router{}); !errors.Is(err, wantErr) {
		t.Fatalf("reloadRuntime error = %v, want %v", err, wantErr)
	}
}

func dnsRuntimeYAML(address string) string {
	return `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: runtime-reload
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan
      spec:
        listen:
          - name: loopback
            addresses: [` + address + `]
            port: 5353
            sources: [default]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSForwarder
      metadata:
        name: default
      spec:
        resolver: DNSResolver/lan
        match: ["."]
        upstreams: [DNSUpstream/public]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSUpstream
      metadata:
        name: public
      spec:
        protocol: udp
        address: 1.1.1.1
`
}
