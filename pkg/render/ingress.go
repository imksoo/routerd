// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"routerd/pkg/api"
)

type ingressNATRule struct {
	ResourceID        string
	ResourceKind      string
	Name              string
	ListenInterface   string
	ListenAddress     string
	Protocol          string
	ListenPort        int
	TargetAddress     string
	TargetPort        int
	Targets           []ingressNATTarget
	Selection         string
	HairpinInterfaces []string
}

type ingressNATTarget struct {
	Name    string
	Address string
	Port    int
}

func ingressNATRules(router *api.Router, aliases map[string]string) ([]ingressNATRule, error) {
	var rules []ingressNATRule
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion {
			continue
		}
		switch res.Kind {
		case "PortForward":
			spec, err := res.PortForwardSpec()
			if err != nil {
				return nil, err
			}
			rule, ok, err := ingressRuleFromPortForward(router, aliases, res, spec)
			if err != nil {
				return nil, err
			}
			if ok {
				rules = append(rules, rule)
			}
		case "IngressService":
			spec, err := res.IngressServiceSpec()
			if err != nil {
				return nil, err
			}
			rule, ok, err := ingressRuleFromService(router, aliases, res, spec)
			if err != nil {
				return nil, err
			}
			if ok {
				rules = append(rules, rule)
			}
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].ResourceKind != rules[j].ResourceKind {
			return rules[i].ResourceKind < rules[j].ResourceKind
		}
		return rules[i].Name < rules[j].Name
	})
	return rules, nil
}

func ingressRuleFromPortForward(router *api.Router, aliases map[string]string, res api.Resource, spec api.PortForwardSpec) (ingressNATRule, bool, error) {
	target := api.IngressBackendSpec{Address: spec.Target.Address, AddressFrom: spec.Target.AddressFrom, Port: spec.Target.Port}
	return ingressRuleFromEndpoint(router, aliases, res, spec.Listen, target, spec.Hairpin)
}

func ingressRuleFromService(router *api.Router, aliases map[string]string, res api.Resource, spec api.IngressServiceSpec) (ingressNATRule, bool, error) {
	if len(spec.Backends) == 0 {
		return ingressNATRule{}, false, fmt.Errorf("%s needs at least one backend", res.ID())
	}
	return ingressRuleFromEndpoints(router, aliases, res, spec.Listen, spec.Backends, spec.Hairpin, defaultString(spec.Policy.Selection, "failover"), true)
}

func ingressRuleFromEndpoint(router *api.Router, aliases map[string]string, res api.Resource, listen api.IngressListenSpec, target api.IngressBackendSpec, hairpin api.IngressHairpinSpec) (ingressNATRule, bool, error) {
	return ingressRuleFromEndpoints(router, aliases, res, listen, []api.IngressBackendSpec{target}, hairpin, "failover", false)
}

func ingressRuleFromEndpoints(router *api.Router, aliases map[string]string, res api.Resource, listen api.IngressListenSpec, backends []api.IngressBackendSpec, hairpin api.IngressHairpinSpec, selection string, autoHairpinDefault bool) (ingressNATRule, bool, error) {
	ifname := aliases[listen.Interface]
	if ifname == "" {
		return ingressNATRule{}, false, fmt.Errorf("%s references listen interface with empty ifname %q", res.ID(), listen.Interface)
	}
	listenAddress, ok, err := ingressAddress(router, listen.Address, listen.AddressFrom, true)
	if err != nil {
		return ingressNATRule{}, false, fmt.Errorf("%s spec.listen.addressFrom: %w", res.ID(), err)
	}
	if !ok {
		return ingressNATRule{}, false, nil
	}
	var targets []ingressNATTarget
	for i, backend := range backends {
		targetAddress, ok, err := ingressAddress(router, backend.Address, backend.AddressFrom, false)
		if err != nil {
			return ingressNATRule{}, false, fmt.Errorf("%s backend %d address: %w", res.ID(), i, err)
		}
		if !ok {
			return ingressNATRule{}, false, nil
		}
		targets = append(targets, ingressNATTarget{Name: defaultString(backend.Name, fmt.Sprintf("backend-%d", i)), Address: targetAddress, Port: backend.Port})
		if selection == "failover" {
			break
		}
	}
	if len(targets) == 0 {
		return ingressNATRule{}, false, nil
	}
	hairpinIfnames, err := ingressHairpinInterfaces(router, aliases, res, listen, hairpin, listenAddress, targets, autoHairpinDefault)
	if err != nil {
		return ingressNATRule{}, false, err
	}
	return ingressNATRule{
		ResourceID:        res.ID(),
		ResourceKind:      res.Kind,
		Name:              res.Metadata.Name,
		ListenInterface:   ifname,
		ListenAddress:     listenAddress,
		Protocol:          listen.Protocol,
		ListenPort:        listen.Port,
		TargetAddress:     targets[0].Address,
		TargetPort:        targets[0].Port,
		Targets:           targets,
		Selection:         selection,
		HairpinInterfaces: hairpinIfnames,
	}, true, nil
}

func ingressHairpinInterfaces(router *api.Router, aliases map[string]string, res api.Resource, listen api.IngressListenSpec, hairpin api.IngressHairpinSpec, listenAddress string, targets []ingressNATTarget, autoDefault bool) ([]string, error) {
	mode := strings.TrimSpace(hairpin.Mode)
	if mode == "" {
		if autoDefault {
			mode = "auto"
		} else if hairpin.Enabled {
			mode = "manual"
		} else {
			mode = "off"
		}
	}
	if mode == "off" {
		return nil, nil
	}
	if listenAddress == "" {
		if mode == "auto" && !hairpin.Enabled && len(hairpin.Interfaces) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("%s hairpin requires resolved listen address", res.ID())
	}
	ifnames := make([]string, 0, len(hairpin.Interfaces)+1)
	if mode == "auto" && ingressNeedsSameInterfaceHairpin(router, listen.Interface, listenAddress, targets) {
		if ifname := aliases[listen.Interface]; ifname != "" {
			ifnames = append(ifnames, ifname)
		}
	}
	if hairpin.Enabled || len(hairpin.Interfaces) > 0 {
		for _, name := range hairpin.Interfaces {
			ifname := aliases[name]
			if ifname == "" {
				return nil, fmt.Errorf("%s references hairpin interface with empty ifname %q", res.ID(), name)
			}
			ifnames = append(ifnames, ifname)
		}
	}
	sort.Strings(ifnames)
	return compactStrings(ifnames), nil
}

func ingressNeedsSameInterfaceHairpin(router *api.Router, listenInterface, listenAddress string, targets []ingressNATTarget) bool {
	listenAddr, err := netip.ParseAddr(strings.TrimSpace(listenAddress))
	if err != nil || !listenAddr.Is4() {
		return false
	}
	for _, prefix := range ingressInterfaceIPv4Prefixes(router, listenInterface) {
		if !prefix.Contains(listenAddr) {
			continue
		}
		for _, target := range targets {
			targetAddr, err := netip.ParseAddr(strings.TrimSpace(target.Address))
			if err == nil && targetAddr.Is4() && prefix.Contains(targetAddr) {
				return true
			}
		}
	}
	return false
}

func ingressInterfaceIPv4Prefixes(router *api.Router, interfaceName string) []netip.Prefix {
	if router == nil {
		return nil
	}
	var prefixes []netip.Prefix
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "IPv4StaticAddress" {
			continue
		}
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil || spec.Interface != interfaceName {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Address))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

func ingressAddress(router *api.Router, address string, source api.StatusValueSourceSpec, allowEmpty bool) (string, bool, error) {
	address = strings.TrimSpace(address)
	if address == "" && strings.TrimSpace(source.Resource) != "" {
		resolved, err := renderAddressFromResource(router, source)
		if err != nil {
			return "", false, err
		}
		if resolved == "" && source.Optional {
			return "", false, nil
		}
		address = strings.TrimSpace(resolved)
	}
	if address == "" {
		return "", allowEmpty, nil
	}
	addr, err := netip.ParseAddr(address)
	if err != nil || !addr.Is4() {
		return "", false, fmt.Errorf("must resolve to an IPv4 address")
	}
	return addr.String(), true, nil
}
