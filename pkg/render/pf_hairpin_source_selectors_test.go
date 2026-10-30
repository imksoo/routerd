// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
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

func TestPFUnknownBackupRetainsBlankModeAutomaticHairpinOnListenInterface(t *testing.T) {
	router := pfForwardRouter()
	rule := router.Spec.Resources[6]
	spec, _ := rule.FirewallRuleSpec()
	spec.FromZone = "wan"
	spec.ToZone = "wan"
	spec.SourceCIDRs = []string{"198.51.100.42/32"}
	spec.DestinationCIDRs = []string{"198.51.100.20/32"}
	rule.Spec = spec
	router.Spec.Resources[6] = rule
	router.Spec.Resources = append(router.Spec.Resources,
		netResource("IPv4StaticAddress", "wan-address", api.IPv4StaticAddressSpec{Interface: "wan", Address: "198.51.100.1/24"}),
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "automatic"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "wan", Address: "198.51.100.10", Protocol: "tcp", Port: 443},
			Hairpin:  api.IngressHairpinSpec{Enabled: true},
			Backends: []api.IngressBackendSpec{{Name: "first", Address: "192.0.2.10", Port: 443}, {Name: "later", AddressFrom: api.StatusValueSourceSpec{Resource: "VirtualAddress/later", Field: "address", Optional: true}, Port: 443}},
		}},
	)
	// Actual runtime selection can resolve the backup onto the listener subnet.
	// Blank mode stays automatic for IngressService even with Enabled=true.
	nat, err := PFNAT44RulesForRouter(router, nil, IngressServiceState{Name: "automatic", Selection: "failover", Backends: []api.IngressBackendSpec{{Name: "later", Address: "198.51.100.20", Port: 443}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nat), "nat on em1 inet proto tcp from (em1:network) to 198.51.100.20 port 443 -> (em1)") {
		t.Fatalf("actual automatic runtime hairpin missing: %s", nat)
	}
	if _, err := PFRuntimeFirewall(router, nil, nil); err == nil || !strings.Contains(err.Error(), "original srcCIDRs/sourcePorts") {
		t.Fatalf("unknown backup's possible automatic listen-interface SNAT was missed: %v", err)
	}
}
