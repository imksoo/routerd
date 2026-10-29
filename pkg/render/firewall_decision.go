// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// FirewallNewFlowQuery describes a new TCP/UDP flow after zone classification.
// A zero destination port means that the operator did not supply that field.
type FirewallNewFlowQuery struct {
	FromZone        string
	ToZone          string
	Protocol        string
	DestinationPort int
}

type FirewallDecision struct {
	Action string
	Source string
	Reason string
}

func sortFirewallRules(rules []api.Resource) {
	sort.Slice(rules, func(i, j int) bool { return rules[i].Metadata.Name < rules[j].Metadata.Name })
}

// NftablesNewFlowDecision reports only statically decidable new-flow verdicts.
// It uses the renderer's zone resolution, policy, rule order and service holes.
// Missing packet selectors and runtime state produce an indeterminate result;
// this is neither a live packet probe nor a PF policy evaluation. The caller
// must supply configuration whose when/effective-state gates are resolved.
func NftablesNewFlowDecision(router *api.Router, query FirewallNewFlowQuery) (FirewallDecision, error) {
	unknown := func(source, reason string) (FirewallDecision, error) {
		return FirewallDecision{Action: "indeterminate", Source: source, Reason: reason}, nil
	}
	if router == nil {
		return FirewallDecision{}, fmt.Errorf("router configuration is unavailable")
	}
	if query.DestinationPort < 0 || query.DestinationPort > 65535 {
		return FirewallDecision{}, fmt.Errorf("destination port must be between 1 and 65535 when supplied")
	}
	aliases, err := nftOutboundAliases(router)
	if err != nil {
		return FirewallDecision{}, err
	}
	var zones, policies, rules []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion {
			switch resource.Kind {
			case "FirewallZone":
				zones = append(zones, resource)
			case "FirewallPolicy":
				policies = append(policies, resource)
			case "FirewallRule":
				rules = append(rules, resource)
			case "ClientPolicy", "FirewallFlowPinhole":
				return unknown(resource.ID(), "client classification or learned flow state is unavailable")
			}
		}
		switch resource.Kind {
		case "IngressService", "LocalServiceRedirect", "NAT44FlowDNATPinhole":
			return unknown(resource.ID(), "address translation and ingress packet selectors are unavailable")
		}
	}
	zoneMap, err := nftFirewallZones(aliases, zones)
	if err != nil {
		return FirewallDecision{}, err
	}
	if query.FromZone == "self" {
		return unknown("output", "the nftables filter has no output zone chain")
	}
	from, exists := zoneMap[query.FromZone]
	if !exists {
		return FirewallDecision{}, fmt.Errorf("unknown source zone %q", query.FromZone)
	}
	to := firewallZone{Name: "self", Role: "self"}
	if query.ToZone != "self" {
		to, exists = zoneMap[query.ToZone]
		if !exists {
			return FirewallDecision{}, fmt.Errorf("unknown destination zone %q", query.ToZone)
		}
	}
	if query.Protocol != "tcp" && query.Protocol != "udp" {
		return unknown("packet", "a TCP or UDP protocol is required; other protocols need family/header selectors")
	}
	if to.Name == "self" && query.Protocol == "udp" && (query.DestinationPort == 0 || query.DestinationPort == 67) && len(dnsmasqIPv6OnlyFirewallInterfaces(router, aliases)) > 0 {
		return unknown("input/dhcpv4-exclusion", "input interface is required for DHCPv4 listener exclusions")
	}
	if to.Name == "self" && slices.Contains(from.IfNames, "lo") {
		if len(from.IfNames) == 1 {
			return FirewallDecision{Action: "accept", Source: "input/loopback"}, nil
		}
		return unknown("input/loopback", "input interface is required for a zone containing loopback and other interfaces")
	}
	if len(from.IfNames) == 0 || (to.Name != "self" && len(to.IfNames) == 0) {
		return FirewallDecision{Action: "drop", Source: "zone-dispatch"}, nil
	}
	policy := firewallPolicyOptions(policies)
	for _, hole := range InternalFirewallHoles(router) {
		if hole.FromZone != from.Name || hole.ToZone != to.Name || (to.Name == "self" && implicitFirewallAction(from.Role, to.Role, policy) == "accept") {
			continue
		}
		if hole.Protocol != "" && hole.Protocol != query.Protocol {
			continue
		}
		if hole.Port != 0 {
			if query.DestinationPort == 0 {
				return unknown("internal/"+hole.Name, "destination port is required")
			}
			if hole.Port != query.DestinationPort {
				continue
			}
		}
		if len(hole.IfNames) > 0 {
			for _, ifname := range from.IfNames {
				if !slices.Contains(hole.IfNames, ifname) {
					return unknown("internal/"+hole.Name, "input interface is required for the service exception")
				}
			}
		}
		return FirewallDecision{Action: defaultString(hole.Action, "accept"), Source: "internal/" + hole.Name}, nil
	}
	sortFirewallRules(rules)
	for _, resource := range rules {
		spec, err := resource.FirewallRuleSpec()
		if err != nil {
			return FirewallDecision{}, err
		}
		if spec.FromZone != from.Name || spec.ToZone != to.Name {
			continue
		}
		if spec.Protocol != "" && spec.Protocol != query.Protocol {
			continue
		}
		source := "user/" + resource.Metadata.Name
		ports := firewallDestinationPorts(spec)
		if len(ports) > 0 {
			if query.DestinationPort == 0 {
				return unknown(source, "destination port is required")
			}
			matched, unavailable := false, false
			for _, port := range ports {
				value, err := strconv.Atoi(strings.TrimSpace(string(port)))
				if err != nil {
					unavailable = true // Do not invent a second range/set parser.
				} else if value == query.DestinationPort {
					matched = true
				}
			}
			if !matched {
				if unavailable {
					return unknown(source, "destination port ranges require packet rule evaluation")
				}
				continue
			}
		}
		if len(spec.SourceCIDRs)+len(spec.DestinationCIDRs)+len(spec.DestinationSetRefs)+len(spec.ExcludeDestinationSetRefs) > 0 || len(firewallSourcePorts(spec)) > 0 || nftICMPTypeMatch(spec) != "" {
			return unknown(source, "source/destination address, source port or ICMP selectors are unavailable")
		}
		if nftFirewallRateLimitExpr(spec.RateLimit) != "" || spec.ConnLimit.MaxPerSource > 0 {
			return unknown(source, "rate or connection-limit state is unavailable")
		}
		switch spec.Action {
		case "accept", "drop", "reject":
			return FirewallDecision{Action: spec.Action, Source: source}, nil
		default:
			return FirewallDecision{}, fmt.Errorf("%s has invalid action %q", resource.ID(), spec.Action)
		}
	}
	return FirewallDecision{Action: implicitFirewallAction(from.Role, to.Role, policy), Source: "implicit/matrix"}, nil
}
