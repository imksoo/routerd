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
	v4, v6 := dnsmasqInterfaceFamilies(router, aliases)
	return dnsmasqFamilyOnlyInterfaces(v6, v4)
}

// DnsmasqIPv4OnlyInterfaces excludes DHCPv6 only where IPv4 service is
// declared without IPv6 service intent. Pending IPv6 scopes still protect their
// interfaces: dnsmasq 2.90 also applies exclusions when constructing IPv6 ranges.
// An unresolved delegated scope prevents exclusions until its interface is known.
func DnsmasqIPv4OnlyInterfaces(router *api.Router, aliases map[string]string) []string {
	v4, v6 := dnsmasqInterfaceFamilies(router, aliases)
	if router != nil {
		for _, resource := range router.Spec.Resources {
			if resource.Kind == "DHCPv6Server" {
				if spec, err := resource.DHCPv6ServerSpec(); err == nil && spec.Mode != "ra-only" {
					for _, iface := range spec.ListenInterfaces {
						v6[defaultString(aliases[iface], iface)] = true
					}
				}
			}
		}
	}
	if v6[""] {
		return nil
	}
	return dnsmasqFamilyOnlyInterfaces(v4, v6)
}

func dnsmasqInterfaceFamilies(router *api.Router, aliases map[string]string) (map[string]bool, map[string]bool) {
	v4, v6 := map[string]bool{}, map[string]bool{}
	if router == nil {
		return v4, v6
	}
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
			if err == nil && spec.Mode != "ra-only" {
				if spec.DelegatedAddress != "" {
					v6[delegated[spec.DelegatedAddress]] = true
				} else if spec.Interface != "" {
					v6[defaultString(aliases[spec.Interface], spec.Interface)] = true
				}
			}
		}
	}
	return v4, v6
}

func dnsmasqFamilyOnlyInterfaces(included, excluded map[string]bool) []string {
	var out []string
	for name := range included {
		if name != "" && !excluded[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
