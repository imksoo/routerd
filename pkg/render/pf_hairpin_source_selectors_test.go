// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func TestPFRejectsOriginalSourceRuleForEveryFailoverHairpinBackend(t *testing.T) {
	router := pfForwardRouter()
	rule := router.Spec.Resources[6]
	spec, _ := rule.FirewallRuleSpec()
	spec.ToZone = "lan"
	spec.SourceCIDRs = []string{"192.0.2.42/32"}
	spec.DestinationCIDRs = []string{"192.0.2.20/32"}
	rule.Spec = spec
	router.Spec.Resources[6] = rule
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "service"}, Spec: api.IngressServiceSpec{
		Listen:   api.IngressListenSpec{Interface: "wan", Address: "198.51.100.10", Protocol: "tcp", Port: 443},
		Hairpin:  api.IngressHairpinSpec{Mode: "manual", Interfaces: []string{"lan"}},
		Backends: []api.IngressBackendSpec{{Name: "first", Address: "192.0.2.10", Port: 443}, {Name: "second", Address: "192.0.2.20", Port: 443}},
	}})
	firewall, err := PFRuntimeFirewall(router, nil, nil)
	nat, natErr := PFNAT44RulesForRouter(router, nil, IngressServiceState{Name: "service", Backends: []api.IngressBackendSpec{{Name: "second", Address: "192.0.2.20", Port: 443}}, Selection: "failover"})
	if natErr != nil {
		t.Fatal(natErr)
	}
	if !strings.Contains(string(nat), "nat on em0 inet proto tcp from (em0:network) to 192.0.2.20 port 443 -> (em0)") {
		t.Fatalf("actual runtime second-backend SNAT missing:\n%s", nat)
	}
	if err == nil {
		t.Fatalf("original-source rule accepted despite current SNAT on selected failover target; firewall:\n%s\nactual runtime NAT:\n%s", firewall, nat)
	}
	if !strings.Contains(err.Error(), "original srcCIDRs/sourcePorts") {
		t.Fatalf("wrong capability rejection: %v", err)
	}
	// Moving the selector to a destination outside every candidate remains valid.
	spec.DestinationCIDRs = []string{"203.0.113.0/24"}
	rule.Spec = spec
	router.Spec.Resources[6] = rule
	if _, err := PFRuntimeFirewall(router, nil, nil); err != nil {
		t.Fatalf("provably unrelated destination rejected: %v", err)
	}
	// Validation must not rewrite actual default failover selection.
	selected, err := PFNAT44RulesForRouter(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(selected), "-> 192.0.2.10 port 443") || strings.Contains(string(selected), "-> 192.0.2.20 port 443") {
		t.Fatalf("guard changed actual first-backend failover: %s", selected)
	}
}
