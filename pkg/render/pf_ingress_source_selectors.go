// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"slices"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// The normal ingress view selects the first failover backend. Capability
// validation must also cover later selections made by the runtime controller,
// without changing which backend the actual ruleset currently renders.
func pfIngressSourceSelectorRules(router *api.Router, aliases map[string]string, current []ingressNATRule, firewall []api.Resource) ([]ingressNATRule, error) {
	needed := false
	for _, resource := range firewall {
		spec, err := resource.FirewallRuleSpec()
		if err != nil {
			return nil, err
		}
		if spec.ToZone != "self" && (len(spec.SourceCIDRs) > 0 || len(spec.SourcePorts) > 0) && pfRuleHasIPv4(spec) {
			needed = true
			break
		}
	}
	if !needed {
		return current, nil
	}
	possible := slices.Clone(current)
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "IngressService" {
			continue
		}
		spec, err := resource.IngressServiceSpec()
		if err != nil {
			return nil, err
		}
		if defaultString(spec.Policy.Selection, "failover") != "failover" {
			continue
		}
		for _, backend := range spec.Backends {
			candidate, ok, err := ingressRuleFromEndpoints(router, aliases, resource, spec.Listen, []api.IngressBackendSpec{backend}, spec.Hairpin, "failover", true, "")
			if err == nil && ok {
				possible = append(possible, candidate)
				continue
			}
			// A runtime-resolved backend is not evidence that hairpin translation is
			// impossible. Preserve unknown destination overlap on its possible native
			// interfaces; the existing zone validator rejects only affected rules.
			candidate = ingressNATRule{ResourceID: resource.ID(), ResourceKind: resource.Kind, Name: resource.Metadata.Name}
			mode := strings.TrimSpace(spec.Hairpin.Mode)
			if mode == "off" {
				continue
			}
			if mode == "" {
				mode = "auto"
				if spec.Hairpin.Enabled {
					mode = "manual"
				}
			}
			if mode == "auto" && (spec.Listen.Address != "" || spec.Listen.AddressFrom.Resource != "") {
				if ifname := aliases[spec.Listen.Interface]; ifname != "" {
					candidate.HairpinInterfaces = append(candidate.HairpinInterfaces, ifname)
				}
			}
			if spec.Hairpin.Enabled || len(spec.Hairpin.Interfaces) > 0 {
				for _, ref := range spec.Hairpin.Interfaces {
					if ifname := aliases[ref]; ifname != "" {
						candidate.HairpinInterfaces = append(candidate.HairpinInterfaces, ifname)
					}
				}
			}
			possible = append(possible, candidate)
		}
	}
	return possible, nil
}
