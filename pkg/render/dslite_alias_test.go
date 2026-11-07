// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func dsliteAliasRouter(tunnel string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "em1", MTU: 1500}),
		netResource("Interface", "uplink", api.InterfaceSpec{IfName: "em0", MTU: 1500}),
		netResource("DSLiteTunnel", "ds", api.DSLiteTunnelSpec{Interface: "uplink", TunnelName: tunnel, MTU: 1454}),
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"DSLiteTunnel/ds"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		netResource("NAT44Rule", "nat", api.NAT44RuleSpec{OutboundInterface: "ds", SourceCIDRs: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "interfaceAddress"}}),
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "PortForward"}, Metadata: api.ObjectMeta{Name: "ingress"}, Spec: api.PortForwardSpec{Listen: api.IngressListenSpec{Interface: "ds", Address: "198.51.100.1", Protocol: "tcp", Port: 8443}, Target: api.IngressTargetSpec{Address: "192.0.2.10", Port: 443}}},
	}}}
}
func TestPFDSLiteAliasesMatchNativeOwner(t *testing.T) {
	for _, configured := range []string{"ds-transix", "gif41", ""} {
		t.Run(configured, func(t *testing.T) {
			router := dsliteAliasRouter(configured)
			native := configured
			if native == "" {
				native = "uplink"
			}
			native = api.DSLiteRuntimeIfName(native, "freebsd")
			data, err := PF(router, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`wan_if = "` + native + `"`, `nat on ` + native + ` from 192.0.2.0/24`, `scrub out on ` + native + ` proto tcp max-mss 1414`, `rdr pass on ` + native + ` inet proto tcp`} {
				if !strings.Contains(string(data), want) {
					t.Errorf("actual PF missing native owner interface %q:\n%s", want, data)
				}
			}
		})
	}
}

func TestPFRuntimeDSLiteAliasesMatchNativeOwner(t *testing.T) {
	router := dsliteAliasRouter("ds-transix")
	native := api.DSLiteRuntimeIfName("ds-transix", "freebsd")
	root, err := PFRuntimeFirewall(router, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`wan_if = "` + native + `"`, `scrub out on ` + native, `nat-anchor "routerd_nat"`} {
		if !strings.Contains(string(root), want) {
			t.Errorf("runtime root missing %q:\n%s", want, root)
		}
	}
	anchor, err := PFNAT44RulesForRouter(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `rdr pass on ` + native + ` inet proto tcp`; !strings.Contains(string(anchor), want) {
		t.Fatalf("runtime NAT ingress anchor missing %q:\n%s", want, anchor)
	}
}

func TestNftablesDSLiteAliasesMatchNativeOwner(t *testing.T) {
	for _, configured := range []string{"ds-transix", "gif41", ""} {
		t.Run(configured, func(t *testing.T) {
			router := dsliteAliasRouter(configured)
			native := configured
			if native == "" {
				native = "uplink"
			}
			data, err := NftablesNAT44(router)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `oifname "`+native+`"`) {
				t.Fatalf("Linux renderer missed native egress %q:\n%s", native, data)
			}
		})
	}
}
