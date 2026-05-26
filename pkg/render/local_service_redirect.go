// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"bytes"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

type nftIPAddressSet struct {
	ResourceID      string
	Name            string
	SetName         string
	Addresses       []string
	IPv4Addresses   []string
	IPv6Addresses   []string
	IPv4Enabled     bool
	IPv6Enabled     bool
	Names           []string
	RefreshInterval string
}

func NftIPAddressSetName(name string) string {
	return nftSetName("ip_address_set_" + name)
}

func NftFirewallIPAddressSetName(name, addressFamily string) string {
	switch addressFamily {
	case "ip6":
		return NftIPAddressSetName(name) + "_v6"
	default:
		return NftIPAddressSetName(name) + "_v4"
	}
}

type localServiceRedirectRule struct {
	ResourceID         string
	ResourceName       string
	Name               string
	Interface          string
	DestinationSetName string
	HasIPv4Destination bool
	HasIPv6Destination bool
	DestinationPort    int
	RedirectPort       int
	Protocols          []string
}

func nftIPAddressSets(router *api.Router) (map[string]nftIPAddressSet, error) {
	sets := map[string]nftIPAddressSet{}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "IPAddressSet" {
			continue
		}
		spec, err := res.IPAddressSetSpec()
		if err != nil {
			return nil, err
		}
		names, err := normalizedAddressSetNames(spec.Names)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		addresses, ipv4Addresses, ipv6Addresses, ipv4Enabled, ipv6Enabled, err := renderedIPAddressSetAddresses(spec.Addresses, names)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		set := nftIPAddressSet{
			ResourceID:      res.ID(),
			Name:            res.Metadata.Name,
			SetName:         NftIPAddressSetName(res.Metadata.Name),
			Addresses:       addresses,
			IPv4Addresses:   ipv4Addresses,
			IPv6Addresses:   ipv6Addresses,
			IPv4Enabled:     ipv4Enabled,
			IPv6Enabled:     ipv6Enabled,
			Names:           names,
			RefreshInterval: strings.TrimSpace(spec.RefreshInterval),
		}
		sets[res.Metadata.Name] = set
		sets["IPAddressSet/"+res.Metadata.Name] = set
	}
	return sets, nil
}

func localServiceRedirectRules(router *api.Router, aliases map[string]string, sets map[string]nftIPAddressSet) ([]localServiceRedirectRule, error) {
	var rules []localServiceRedirectRule
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "LocalServiceRedirect" {
			continue
		}
		spec, err := res.LocalServiceRedirectSpec()
		if err != nil {
			return nil, err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return nil, fmt.Errorf("%s references interface with empty ifname %q", res.ID(), spec.Interface)
		}
		for i, rule := range spec.Rules {
			protocols, err := normalizedRedirectProtocols(rule.Protocols)
			if err != nil {
				return nil, fmt.Errorf("%s spec.rules[%d]: %w", res.ID(), i, err)
			}
			set, ok := sets[strings.TrimSpace(rule.DestinationSetRef)]
			if !ok {
				return nil, fmt.Errorf("%s spec.rules[%d].destinationSetRef references missing IPAddressSet %q", res.ID(), i, rule.DestinationSetRef)
			}
			if rule.DestinationPort < 1 || rule.DestinationPort > 65535 {
				return nil, fmt.Errorf("%s spec.rules[%d].destinationPort must be within 1-65535", res.ID(), i)
			}
			if rule.RedirectPort < 1 || rule.RedirectPort > 65535 {
				return nil, fmt.Errorf("%s spec.rules[%d].redirectPort must be within 1-65535", res.ID(), i)
			}
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				name = strconv.Itoa(i)
			}
			rules = append(rules, localServiceRedirectRule{
				ResourceID:         res.ID(),
				ResourceName:       res.Metadata.Name,
				Name:               name,
				Interface:          ifname,
				DestinationSetName: set.SetName,
				HasIPv4Destination: set.IPv4Enabled,
				HasIPv6Destination: set.IPv6Enabled,
				DestinationPort:    rule.DestinationPort,
				RedirectPort:       rule.RedirectPort,
				Protocols:          protocols,
			})
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].ResourceID != rules[j].ResourceID {
			return rules[i].ResourceID < rules[j].ResourceID
		}
		return rules[i].Name < rules[j].Name
	})
	return rules, nil
}

func referencedIPAddressSets(rules []localServiceRedirectRule, sets map[string]nftIPAddressSet) []nftIPAddressSet {
	bySetName := map[string]nftIPAddressSet{}
	for _, set := range sets {
		bySetName[set.SetName] = set
	}
	seen := map[string]bool{}
	var out []nftIPAddressSet
	for _, rule := range rules {
		if seen[rule.DestinationSetName] {
			continue
		}
		if set, ok := bySetName[rule.DestinationSetName]; ok {
			out = append(out, set)
			seen[rule.DestinationSetName] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetName < out[j].SetName })
	return out
}

func referencedIPAddressSetsByRefs(refs []string, sets map[string]nftIPAddressSet) []nftIPAddressSet {
	seen := map[string]bool{}
	var out []nftIPAddressSet
	for _, ref := range refs {
		set, ok := sets[strings.TrimSpace(ref)]
		if !ok || seen[set.SetName] {
			continue
		}
		seen[set.SetName] = true
		out = append(out, set)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetName < out[j].SetName })
	return out
}

func mergeIPAddressSets(groups ...[]nftIPAddressSet) []nftIPAddressSet {
	seen := map[string]bool{}
	var out []nftIPAddressSet
	for _, group := range groups {
		for _, set := range group {
			if seen[set.SetName] {
				continue
			}
			seen[set.SetName] = true
			out = append(out, set)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetName < out[j].SetName })
	return out
}

func referencedNAT44IPAddressSets(rules []NAT44RenderRule, sets map[string]nftIPAddressSet) []nftIPAddressSet {
	var refs []string
	for _, rule := range rules {
		refs = append(refs, rule.DestinationSetRefs...)
		refs = append(refs, rule.ExcludeDestinationSetRefs...)
	}
	return referencedIPAddressSetsByRefs(refs, sets)
}

func referencedNAT44ResourceIPAddressSets(resources []api.Resource, sets map[string]nftIPAddressSet) []nftIPAddressSet {
	var refs []string
	for _, res := range resources {
		spec, err := res.NAT44RuleSpec()
		if err != nil {
			continue
		}
		refs = append(refs, spec.DestinationSetRefs...)
		refs = append(refs, spec.ExcludeDestinationSetRefs...)
	}
	return referencedIPAddressSetsByRefs(refs, sets)
}

func referencedIPv4PolicyIPAddressSets(policies []api.Resource, sets map[string]nftIPAddressSet) []nftIPAddressSet {
	var refs []string
	for _, res := range policies {
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			continue
		}
		refs = append(refs, spec.DestinationSetRefs...)
		refs = append(refs, spec.ExcludeDestinationSetRefs...)
	}
	return referencedIPAddressSetsByRefs(refs, sets)
}

func referencedFirewallIPAddressSets(rules []api.Resource, sets map[string]nftIPAddressSet) []nftIPAddressSet {
	var refs []string
	for _, res := range rules {
		spec, err := res.FirewallRuleSpec()
		if err != nil {
			continue
		}
		refs = append(refs, spec.DestinationSetRefs...)
		refs = append(refs, spec.ExcludeDestinationSetRefs...)
	}
	return referencedIPAddressSetsByRefs(refs, sets)
}

func writeIPv4AddressSets(buf *bytes.Buffer, sets []nftIPAddressSet) {
	for _, set := range sets {
		if !set.IPv4Enabled {
			continue
		}
		buf.WriteString("  set " + set.SetName + " { type ipv4_addr;")
		if len(set.IPv4Addresses) > 0 {
			buf.WriteString(" elements = { " + strings.Join(set.IPv4Addresses, ", ") + " };")
		}
		buf.WriteString(" }\n")
	}
}

func writeIPv6AddressSets(buf *bytes.Buffer, sets []nftIPAddressSet) {
	for _, set := range sets {
		if !set.IPv6Enabled {
			continue
		}
		buf.WriteString("  set " + set.SetName + " { type ipv6_addr;")
		if len(set.IPv6Addresses) > 0 {
			buf.WriteString(" elements = { " + strings.Join(set.IPv6Addresses, ", ") + " };")
		}
		buf.WriteString(" }\n")
	}
}

func writeFirewallAddressSets(buf *bytes.Buffer, sets []nftIPAddressSet) {
	for _, set := range sets {
		if set.IPv4Enabled {
			buf.WriteString("  set " + NftFirewallIPAddressSetName(set.Name, "ip") + " { type ipv4_addr;")
			if len(set.IPv4Addresses) > 0 {
				buf.WriteString(" elements = { " + strings.Join(set.IPv4Addresses, ", ") + " };")
			}
			buf.WriteString(" }\n")
		}
		if set.IPv6Enabled {
			buf.WriteString("  set " + NftFirewallIPAddressSetName(set.Name, "ip6") + " { type ipv6_addr;")
			if len(set.IPv6Addresses) > 0 {
				buf.WriteString(" elements = { " + strings.Join(set.IPv6Addresses, ", ") + " };")
			}
			buf.WriteString(" }\n")
		}
	}
}

func writeFirewallAddressSetResets(buf *bytes.Buffer, sets []nftIPAddressSet) {
	for _, set := range sets {
		if set.IPv4Enabled {
			writeNftSetReset(buf, "inet", "routerd_filter", NftFirewallIPAddressSetName(set.Name, "ip"))
		}
		if set.IPv6Enabled {
			writeNftSetReset(buf, "inet", "routerd_filter", NftFirewallIPAddressSetName(set.Name, "ip6"))
		}
	}
}

func writeLocalServiceRedirectRules(buf *bytes.Buffer, rules []localServiceRedirectRule, family string) {
	for _, rule := range rules {
		match := "ip daddr @" + rule.DestinationSetName
		if family == "ip" && !rule.HasIPv4Destination {
			continue
		}
		if family == "ip6" {
			if !rule.HasIPv6Destination {
				continue
			}
			match = "ip6 daddr @" + rule.DestinationSetName
		}
		for _, proto := range rule.Protocols {
			parts := []string{
				"iifname " + nftQuote(rule.Interface),
				match,
				proto,
				"dport " + strconv.Itoa(rule.DestinationPort),
				"counter redirect to :" + strconv.Itoa(rule.RedirectPort),
				"comment " + nftQuote("routerd LocalServiceRedirect "+rule.ResourceName+" "+rule.Name),
			}
			buf.WriteString("    " + strings.Join(parts, " ") + "\n")
		}
	}
}

func hasIPv4LocalServiceRedirectRules(rules []localServiceRedirectRule) bool {
	for _, rule := range rules {
		if rule.HasIPv4Destination {
			return true
		}
	}
	return false
}

func hasIPv6LocalServiceRedirectRules(rules []localServiceRedirectRule) bool {
	for _, rule := range rules {
		if rule.HasIPv6Destination {
			return true
		}
	}
	return false
}

func normalizedRedirectProtocols(values []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		switch value {
		case "tcp", "udp":
		default:
			return nil, fmt.Errorf("protocols entries must be tcp or udp")
		}
		if seen[value] {
			return nil, fmt.Errorf("protocols duplicate %q", value)
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("protocols is required")
	}
	sort.Strings(out)
	return out, nil
}

func renderedIPAddressSetAddresses(addresses, names []string) ([]string, []string, []string, bool, bool, error) {
	seen := map[string]bool{}
	var out []string
	var ipv4 []string
	var ipv6 []string
	ipv4Enabled := false
	ipv6Enabled := false
	for _, value := range addresses {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return nil, nil, nil, false, false, fmt.Errorf("addresses entries must be IP addresses")
		}
		addr = addr.Unmap()
		key := addr.String()
		if seen[key] {
			return nil, nil, nil, false, false, fmt.Errorf("addresses duplicate %q", key)
		}
		seen[key] = true
		out = append(out, key)
		if addr.Is4() {
			ipv4Enabled = true
			ipv4 = append(ipv4, key)
		} else if addr.Is6() {
			ipv6Enabled = true
			ipv6 = append(ipv6, key)
		}
	}
	if len(names) > 0 {
		ipv4Enabled = true
		ipv6Enabled = true
		sort.Strings(out)
		sort.Strings(ipv4)
		sort.Strings(ipv6)
		return out, ipv4, ipv6, ipv4Enabled, ipv6Enabled, nil
	}
	if len(out) == 0 {
		return nil, nil, nil, false, false, fmt.Errorf("addresses or names must resolve to at least one IP address")
	}
	sort.Strings(out)
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	return out, ipv4, ipv6, ipv4Enabled, ipv6Enabled, nil
}

func normalizedAddressSetNames(values []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("names entries must not be empty")
		}
		if seen[value] {
			return nil, fmt.Errorf("names duplicate %q", value)
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}
