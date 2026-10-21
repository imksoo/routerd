// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"sort"
)

// dnsmasqIPv6OnlyFirewallInterfaces retains DHCPv4 whenever a server or relay
// is declared, including currently pending scopes. The firewall must not depend
// on transient VIP readiness. Compare OS names because aliases can overlap.
func dnsmasqIPv6OnlyFirewallInterfaces(router *api.Router, aliases map[string]string) []string {
	v4, v6 := map[string]bool{}, map[string]bool{}
	delegated := map[string]string{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "IPv6DelegatedAddress" {
			spec, err := resource.IPv6DelegatedAddressSpec()
			if err == nil {
				delegated[resource.Metadata.Name] = aliases[spec.Interface]
			}
		}
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Server":
			spec, err := resource.DHCPv4ServerSpec()
			if err == nil && spec.Interface != "" {
				v4[aliases[spec.Interface]] = true
			}
		case "DHCPv4Relay":
			spec, err := resource.DHCPv4RelaySpec()
			if err == nil {
				for _, iface := range spec.Interfaces {
					v4[aliases[iface]] = true
				}
			}
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err == nil {
				if spec.DelegatedAddress != "" {
					v6[delegated[spec.DelegatedAddress]] = true
				} else if spec.Interface != "" {
					v6[defaultString(aliases[spec.Interface], spec.Interface)] = true
				}
			}
		case "IPv6RouterAdvertisement":
			spec, err := resource.IPv6RouterAdvertisementSpec()
			if err == nil {
				v6[defaultString(aliases[spec.Interface], spec.Interface)] = true
			}
		}
	}
	var out []string
	for name := range v6 {
		if name != "" && !v4[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
