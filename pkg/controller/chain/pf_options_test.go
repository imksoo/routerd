// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestPFLiveNATRequiresAttachedRuntimeAnchor(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		kind string
	}{
		{"NAT with dry-run firewall", Options{DryRunFirewall: true, DryRunIngress: true}, "NAT44Rule"},
		{"ingress with dry-run firewall", Options{DryRunFirewall: true, DryRunNAT: true}, "IngressService"},
		{"disabled firewall", Options{FirewallDisabled: true}, "IngressService"},
		{"filtered firewall", Options{EnabledControllers: []string{"nat44"}}, "NAT44Rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: tc.kind}, Metadata: api.ObjectMeta{Name: "example"}, Spec: api.IngressServiceSpec{}}
			if tc.kind == "NAT44Rule" {
				resource.APIVersion, resource.Spec = api.NetAPIVersion, api.NAT44RuleSpec{}
			}
			store := mapStore{}
			runner := &Runner{Opts: tc.opts, Store: store, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}}
			if err := runner.validatePFNATOptions(platform.OSFreeBSD, tc.opts); err == nil || !strings.Contains(err.Error(), "routerd_nat PF anchor") {
				t.Fatalf("unattached live PF anchor was accepted: %v", err)
			}
			if err := runner.validatePFNATOptions(platform.OSLinux, tc.opts); err != nil {
				t.Fatalf("PF requirement affected Linux: %v", err)
			}
			if len(store) != 0 {
				t.Fatal("invalid PF options caused status effects")
			}
		})
	}
	for _, opts := range []Options{
		{},
		{DryRunFirewall: true, DryRunNAT: true, DryRunIngress: true},
		{EnabledControllers: []string{"log-retention"}, DryRunFirewall: true},
		{FirewallDisabled: true},
	} {
		if err := (&Runner{Opts: opts, Router: &api.Router{}, Store: mapStore{}}).validatePFNATOptions(platform.OSFreeBSD, opts); err != nil {
			t.Fatalf("coherent PF runtime options rejected: %v", err)
		}
	}
}

func TestPFNATOptionsIgnoreUnrelatedOrDryRunResources(t *testing.T) {
	for _, tc := range []struct {
		resource api.Resource
		opts     Options
	}{
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Spec: api.DNSResolverSpec{}}, Options{FirewallDisabled: true}},
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Spec: api.NAT44RuleSpec{}}, Options{FirewallDisabled: true, DryRunNAT: true}},
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Spec: api.IngressServiceSpec{}}, Options{FirewallDisabled: true, DryRunIngress: true}},
	} {
		runner := &Runner{Opts: tc.opts, Store: mapStore{}, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{tc.resource}}}}
		if err := runner.validatePFNATOptions(platform.OSFreeBSD, tc.opts); err != nil {
			t.Fatalf("unrelated/dry-run %s required live firewall: %v", tc.resource.Kind, err)
		}
	}
}
