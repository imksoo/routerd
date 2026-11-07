// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestIngressUnavailablePolicyBlocksOnlySelectedListener(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, policy, verdict string
	}{
		{"default", "tcp", "", "drop"},
		{"drop", "udp", "drop", "drop"},
		{"tcp reject", "tcp", "reject", "reject with tcp reset"},
		{"udp reject", "udp", "reject", "reject with icmp type port-unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
					Listen:   api.IngressListenSpec{Interface: "wan", Address: "192.0.2.10", Protocol: tc.protocol, Port: 6443},
					Backends: []api.IngressBackendSpec{{Name: "declared", Address: "10.0.0.11", Port: 6443}},
					Hairpin:  api.IngressHairpinSpec{Mode: "manual", Enabled: true, Interfaces: []string{"lan"}},
					Policy:   api.IngressServicePolicySpec{OnNoHealthyBackends: tc.policy},
				}},
				{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "other"}, Spec: api.IngressServiceSpec{
					Listen:   api.IngressListenSpec{Interface: "wan", Address: "192.0.2.10", Protocol: "tcp", Port: 8080},
					Backends: []api.IngressBackendSpec{{Name: "other", Address: "10.0.0.99", Port: 8080}},
				}},
			}}}
			states := []IngressServiceState{{Name: "api"}}
			data, err := NftablesNAT44RulesForRouter(router, nil, states...)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			predicate := `iifname { "ens18", "ens19" } meta l4proto ` + tc.protocol + ` ct direction original ct original ip daddr 192.0.2.10 ct original proto-dst 6443 counter ` + tc.verdict + ` comment "routerd IngressService api no healthy backends"`
			if strings.Count(text, predicate) != 2 || !strings.Contains(text, "type filter hook input priority filter - 1; policy accept;") || !strings.Contains(text, "type filter hook forward priority filter - 1; policy accept;") {
				t.Fatalf("listener policy missing input/forward scope:\n%s", text)
			}
			if strings.Contains(text, "10.0.0.11") || strings.Contains(text, "dnat to :0") || !strings.Contains(text, "dnat to 10.0.0.99:8080") {
				t.Fatalf("unavailable service leaked its backend or altered other listener:\n%s", text)
			}
			pfData, err := PFNAT44RulesForRouter(router, nil, states...)
			if err != nil {
				t.Fatal(err)
			}
			pfAction := "drop"
			if tc.policy == "reject" {
				pfAction = "return-icmp"
				if tc.protocol == "tcp" {
					pfAction = "return-rst"
				}
			}
			for _, ifname := range []string{"ens18", "ens19"} {
				wantPF := "block " + pfAction + " in quick on \"" + ifname + "\" inet proto " + tc.protocol + ` from any to 192.0.2.10 port 6443 label "routerd:IngressService:api:unavailable"`
				if !strings.Contains(string(pfData), wantPF) || strings.Contains(string(pfData), "10.0.0.11") || !strings.Contains(string(pfData), "-> 10.0.0.99 port 8080") {
					t.Fatalf("PF unavailable policy changed wrong listener:\n%s", pfData)
				}
			}
			states[0].Backends = []api.IngressBackendSpec{{Name: "recovered", Address: "10.0.0.12", Port: 6443}}
			states[0].Selection = "failover"
			data, err = NftablesNAT44RulesForRouter(router, nil, states...)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "ingress_unavailable") || !strings.Contains(string(data), "dnat to 10.0.0.12:6443") {
				t.Fatalf("recovered service did not replace unavailable policy:\n%s", data)
			}
			pfData, err = PFNAT44RulesForRouter(router, nil, states...)
			if err != nil || strings.Contains(string(pfData), "unavailable") || !strings.Contains(string(pfData), "-> 10.0.0.12 port 6443") {
				t.Fatalf("PF recovered service did not replace unavailable policy:\n%s (%v)", pfData, err)
			}
		})
	}
}

func TestPFRejectsUnsupportedIndependentBackendDistribution(t *testing.T) {
	for _, selection := range []string{"sourceHash", "random"} {
		router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "wan", Protocol: "tcp", Port: 6443},
				Backends: []api.IngressBackendSpec{{Address: "10.0.0.11", Port: 6443}, {Address: "10.0.0.12", Port: 6443}},
				Policy:   api.IngressServicePolicySpec{Selection: selection},
			}},
		}}}
		if _, err := PFNAT44RulesForRouter(router, nil); err == nil || !strings.Contains(err.Error(), selection) {
			t.Fatalf("PF silently selected first %s backend: %v", selection, err)
		}
	}
}
