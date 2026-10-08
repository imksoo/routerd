// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type consoleAddressSet struct {
	Name      string
	Addresses map[netip.Addr]struct{}
}

type localRedirectDisplayRule struct {
	ResourceName      string
	RuleName          string
	DestinationSetRef string
	DestinationPort   int
	RedirectPort      int
	Protocols         map[string]struct{}
}

func (h Handler) enrichConnectionsWithLocalRedirect(table *observe.ConnectionTable) {
	if table == nil || len(table.Entries) == 0 || h.opts.Router == nil {
		return
	}
	sets := h.consoleAddressSets()
	rules := h.localRedirectDisplayRules()
	if len(rules) == 0 {
		return
	}
	for i := range table.Entries {
		if match := matchConnectionLocalRedirect(table.Entries[i], rules, sets); match != nil {
			table.Entries[i].LocalRedirect = match
		}
	}
}

func (h Handler) enrichFirewallLogsWithAddressSets(logs []logstore.FirewallLogEntry) {
	if len(logs) == 0 || h.opts.Router == nil {
		return
	}
	sets := h.consoleAddressSets()
	if len(sets) == 0 {
		return
	}
	ruleRefs := h.firewallRuleDestinationSetRefs()
	for i := range logs {
		log := &logs[i]
		seen := map[string]int{}
		seenSets := map[*consoleAddressSet]int{}
		for _, ref := range ruleRefs[strings.TrimSpace(log.RuleName)] {
			set, ok := sets[ref]
			if !ok {
				continue
			}
			current := set.contains(log.DstAddress)
			log.DestinationSets = append(log.DestinationSets, logstore.AddressSetMatch{
				ResourceName: set.Name,
				SetName:      ref,
				Source:       "firewall-rule",
				Current:      current,
			})
			seen[ref] = len(log.DestinationSets) - 1
			seenSets[set] = len(log.DestinationSets) - 1
		}
		for _, ref := range sortedConsoleAddressSetRefs(sets) {
			set := sets[ref]
			if !set.contains(log.DstAddress) {
				continue
			}
			if index, ok := seenSets[set]; ok {
				log.DestinationSets[index].Current = true
				continue
			}
			if index, ok := seen[ref]; ok {
				log.DestinationSets[index].Current = true
				continue
			}
			log.DestinationSets = append(log.DestinationSets, logstore.AddressSetMatch{
				ResourceName: set.Name,
				SetName:      ref,
				Source:       "current-destination",
				Current:      true,
			})
		}
	}
}

func matchConnectionLocalRedirect(entry observe.ConnectionEntry, rules []localRedirectDisplayRule, sets map[string]*consoleAddressSet) *observe.LocalRedirect {
	protocol := strings.ToLower(strings.TrimSpace(entry.Protocol))
	dstPort := atoiDefault(entry.Original.DestinationPort, 0)
	replySourcePort := atoiDefault(entry.Reply.SourcePort, 0)
	originalAddress := normalizedIPAddrString(entry.Original.Destination)
	replyAddress := normalizedIPAddrString(entry.Reply.Source)
	if protocol == "" || dstPort == 0 || originalAddress == "" || replyAddress == "" || originalAddress == replyAddress {
		return nil
	}
	var tupleCandidates []localRedirectDisplayRule
	for _, rule := range rules {
		if _, ok := rule.Protocols[protocol]; !ok {
			continue
		}
		if rule.DestinationPort != dstPort || rule.RedirectPort != replySourcePort {
			continue
		}
		tupleCandidates = append(tupleCandidates, rule)
		if set := sets[rule.DestinationSetRef]; set != nil && set.contains(originalAddress) {
			return &observe.LocalRedirect{
				ResourceName:      rule.ResourceName,
				RuleName:          rule.RuleName,
				DestinationSetRef: rule.DestinationSetRef,
				OriginalAddress:   originalAddress,
				RedirectAddress:   replyAddress,
				RedirectPort:      rule.RedirectPort,
				Match:             "destination-set",
			}
		}
	}
	if len(tupleCandidates) == 1 {
		rule := tupleCandidates[0]
		return &observe.LocalRedirect{
			ResourceName:      rule.ResourceName,
			RuleName:          rule.RuleName,
			DestinationSetRef: rule.DestinationSetRef,
			OriginalAddress:   originalAddress,
			RedirectAddress:   replyAddress,
			RedirectPort:      rule.RedirectPort,
			Match:             "tuple",
		}
	}
	return nil
}

func (h Handler) localRedirectDisplayRules() []localRedirectDisplayRule {
	if h.opts.Router == nil {
		return nil
	}
	var out []localRedirectDisplayRule
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "LocalServiceRedirect" {
			continue
		}
		spec, err := resource.LocalServiceRedirectSpec()
		if err != nil {
			continue
		}
		for i, rule := range spec.Rules {
			protocols := map[string]struct{}{}
			for _, protocol := range rule.Protocols {
				protocol = strings.ToLower(strings.TrimSpace(protocol))
				if protocol != "" {
					protocols[protocol] = struct{}{}
				}
			}
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				name = strconv.Itoa(i)
			}
			out = append(out, localRedirectDisplayRule{
				ResourceName:      resource.Metadata.Name,
				RuleName:          name,
				DestinationSetRef: strings.TrimSpace(rule.DestinationSetRef),
				DestinationPort:   rule.DestinationPort,
				RedirectPort:      rule.RedirectPort,
				Protocols:         protocols,
			})
		}
	}
	return out
}

func (h Handler) firewallRuleDestinationSetRefs() map[string][]string {
	out := map[string][]string{}
	if h.opts.Router == nil {
		return out
	}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallRule" {
			continue
		}
		spec, err := resource.FirewallRuleSpec()
		if err != nil {
			continue
		}
		for _, ref := range spec.DestinationSetRefs {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				out[resource.Metadata.Name] = appendUnique(out[resource.Metadata.Name], ref)
			}
		}
	}
	return out
}

func (h Handler) consoleAddressSets() map[string]*consoleAddressSet {
	if h.opts.Router == nil {
		return nil
	}
	statuses := h.objectStatusMap()
	out := map[string]*consoleAddressSet{}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPAddressSet" {
			continue
		}
		spec, err := resource.IPAddressSetSpec()
		if err != nil {
			continue
		}
		set := &consoleAddressSet{
			Name:      resource.Metadata.Name,
			Addresses: map[netip.Addr]struct{}{},
		}
		for _, address := range spec.Addresses {
			set.addAddress(address)
		}
		status := statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name]
		for _, key := range []string{"addresses", "ipv4Addresses", "ipv6Addresses"} {
			for _, address := range stringSliceFromMap(status, key) {
				set.addAddress(address)
			}
		}
		out[resource.Metadata.Name] = set
		out["IPAddressSet/"+resource.Metadata.Name] = set
	}
	return out
}

func (h Handler) objectStatusMap() map[string]map[string]any {
	out := map[string]map[string]any{}
	lister, ok := h.opts.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return out
	}
	resources, err := lister.ListObjectStatuses()
	if err != nil {
		return out
	}
	for _, resource := range h.filterStaleObjectStatuses(resources) {
		out[resource.APIVersion+"/"+resource.Kind+"/"+resource.Name] = resource.Status
	}
	return out
}

func (s *consoleAddressSet) addAddress(value string) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return
	}
	s.Addresses[addr.Unmap()] = struct{}{}
}

func (s *consoleAddressSet) contains(value string) bool {
	if s == nil {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	_, ok := s.Addresses[addr.Unmap()]
	return ok
}

func sortedConsoleAddressSetRefs(sets map[string]*consoleAddressSet) []string {
	seen := map[*consoleAddressSet]struct{}{}
	var out []string
	for ref, set := range sets {
		if strings.Contains(ref, "/") {
			continue
		}
		if _, ok := seen[set]; ok {
			continue
		}
		seen[set] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}
