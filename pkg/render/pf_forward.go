// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"bytes"
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
	"net/netip"
	"slices"
)

func pfIngressTag(ifname string) string { return "routerd_ingress_" + ifname }

// FreeBSD 14.3 has no received-on selector. A stateless ingress pass records
// the actual interface; only an egress-zone decision creates forwarding state.
// Input anchors already terminate local delivery before this staging pass.
func writePFZoneForwards(buf *bytes.Buffer, zones map[string]firewallZone, rules []api.Resource, holes []FirewallHole, clients []pfClientPolicy, policy firewallPolicy, logging firewallLogging) error {
	for _, from := range sortedFirewallZones(zones) {
		for _, client := range clients {
			for _, expr := range pfClientPolicyForwardExprs(from, client, logging) {
				buf.WriteString(expr + "\n")
			}
		}
		for _, ifname := range from.IfNames {
			buf.WriteString("pass in quick on " + pfQuote(ifname) + " all no state tag " + pfQuote(pfIngressTag(ifname)) + "\n")
		}
	}
	for _, from := range sortedFirewallZones(zones) {
		for _, ifname := range from.IfNames {
			tag := pfQuote(pfIngressTag(ifname))
			for _, to := range sortedFirewallZones(zones) {
				if len(to.IfNames) == 0 {
					continue
				}
				buf.WriteString("anchor " + pfQuote("routerd_forward_"+from.Name+"_"+to.Name+"_"+ifname) + " out on $" + pfZoneMacro(to) + " tagged " + tag + " {\n")
				for _, hole := range holes {
					if hole.FromZone == from.Name && hole.ToZone == to.Name && (len(hole.IfNames) == 0 || slices.Contains(hole.IfNames, ifname)) {
						name := hole.Name
						if name == "" {
							name = hole.Comment
						}
						spec := api.FirewallRuleSpec{FromZone: hole.FromZone, ToZone: hole.ToZone, Protocol: hole.Protocol, Port: hole.Port, Action: defaultString(hole.Action, "accept")}
						buf.WriteString(pfFirewallRuleExprDirection("out", "$"+pfZoneMacro(to), "any", name, spec, logging) + "\n")
					}
				}
				for _, resource := range rules {
					spec, err := resource.FirewallRuleSpec()
					if err != nil {
						return err
					}
					if spec.FromZone == from.Name && spec.ToZone == to.Name {
						buf.WriteString(pfFirewallRuleExprDirection("out", "$"+pfZoneMacro(to), "any", resource.Metadata.Name, spec, logging) + "\n")
					}
				}
				if implicitFirewallAction(from.Role, to.Role, policy) == "accept" {
					buf.WriteString("pass out quick on $" + pfZoneMacro(to) + " all keep state label " + pfQuote("routerd:"+from.Name+"-to-"+to.Name) + "\n")
				} else {
					log := ""
					if policy.LogDeny {
						log = " " + pfLogExpr(logging)
					}
					buf.WriteString("block drop out" + log + " quick on $" + pfZoneMacro(to) + " all label " + pfQuote("routerd:"+from.Name+"-to-"+to.Name+"-deny") + "\n")
				}
				buf.WriteString("}\n")
			}
			// An ingress-tagged packet must never fall into the local-output default.
			buf.WriteString("block drop out quick tagged " + tag + " label " + pfQuote("routerd:"+from.Name+":unmatched-forward") + "\n")
		}
	}
	for _, resource := range rules {
		spec, err := resource.FirewallRuleSpec()
		if err != nil {
			return err
		}
		if spec.FromZone != "self" {
			continue
		}
		to, ok := zones[spec.ToZone]
		if !ok {
			return fmt.Errorf("%s references missing PF output zone %q", resource.ID(), spec.ToZone)
		}
		if len(to.IfNames) == 0 {
			continue
		}
		buf.WriteString(pfFirewallRuleExprDirection("out", "$"+pfZoneMacro(to), "any", resource.Metadata.Name, spec, logging) + "\n")
	}
	buf.WriteString("pass out quick all keep state\n")
	return nil
}

func validatePFZoneRules(zones map[string]firewallZone, rules []api.Resource, nats []api.Resource, ingress []ingressNATRule, aliases map[string]string) error {
	for _, resource := range rules {
		spec, err := resource.FirewallRuleSpec()
		if err != nil {
			return err
		}
		if spec.FromZone != "self" {
			if _, ok := zones[spec.FromZone]; !ok {
				return fmt.Errorf("%s references missing PF source zone %q", resource.ID(), spec.FromZone)
			}
		}
		if spec.ToZone == "self" {
			continue
		}
		to, ok := zones[spec.ToZone]
		if !ok {
			return fmt.Errorf("%s references missing PF destination zone %q", resource.ID(), spec.ToZone)
		}
		if len(spec.SourceCIDRs) == 0 && len(spec.SourcePorts) == 0 {
			continue
		}
		if !pfRuleHasIPv4(spec) {
			continue
		}
		// SNAT precedes outbound filtering in pf_test_rule. The original source
		// address/port cannot be reconstructed by an outbound PF expression.
		translated := false
		for _, r := range nats {
			nat, err := r.NAT44RuleSpec()
			if err != nil {
				return err
			}
			ref := defaultString(nat.OutboundInterface, nat.EgressInterface)
			if nat.EgressPolicyRef != "" || slices.Contains(to.IfNames, aliases[ref]) {
				translated = true
			}
		}
		for _, rule := range ingress {
			for _, ifname := range rule.HairpinInterfaces {
				if slices.Contains(to.IfNames, ifname) {
					translated = true
				}
			}
		}
		if translated {
			return fmt.Errorf("%s FreeBSD PF cannot preserve original srcCIDRs/sourcePorts with outbound SNAT on destination zone %s", resource.ID(), to.Name)
		}
	}
	return nil
}
func pfRuleHasIPv4(spec api.FirewallRuleSpec) bool {
	if spec.Protocol == "icmpv6" || spec.Protocol == "ipv6-icmp" || spec.Protocol == "ipip" {
		return false
	}
	for _, set := range [][]string{spec.SourceCIDRs, spec.DestinationCIDRs} {
		if len(set) == 0 {
			continue
		}
		ipv4 := false
		for _, value := range set {
			prefix, err := netip.ParsePrefix(value)
			if err == nil && prefix.Addr().Is4() {
				ipv4 = true
			}
		}
		if !ipv4 {
			return false
		}
	}
	return true
}
