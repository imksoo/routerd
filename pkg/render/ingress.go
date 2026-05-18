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
	HairpinInterfaces []string
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
	// Static nftables rendering uses the first backend as the initial active
	// endpoint. The controller-owned runtime state can update the active
	// endpoint as health checks change without regenerating the whole ruleset.
	return ingressRuleFromEndpoint(router, aliases, res, spec.Listen, spec.Backends[0], spec.Hairpin)
}

func ingressRuleFromEndpoint(router *api.Router, aliases map[string]string, res api.Resource, listen api.IngressListenSpec, target api.IngressBackendSpec, hairpin api.IngressHairpinSpec) (ingressNATRule, bool, error) {
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
	targetAddress, ok, err := ingressAddress(router, target.Address, target.AddressFrom, false)
	if err != nil {
		return ingressNATRule{}, false, fmt.Errorf("%s target address: %w", res.ID(), err)
	}
	if !ok {
		return ingressNATRule{}, false, nil
	}
	hairpinIfnames, err := ingressHairpinInterfaces(aliases, res, listen, hairpin, listenAddress)
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
		TargetAddress:     targetAddress,
		TargetPort:        target.Port,
		HairpinInterfaces: hairpinIfnames,
	}, true, nil
}

func ingressHairpinInterfaces(aliases map[string]string, res api.Resource, listen api.IngressListenSpec, hairpin api.IngressHairpinSpec, listenAddress string) ([]string, error) {
	if !hairpin.Enabled {
		return nil, nil
	}
	if listenAddress == "" {
		return nil, fmt.Errorf("%s hairpin requires resolved listen address", res.ID())
	}
	ifnames := make([]string, 0, len(hairpin.Interfaces))
	for _, name := range hairpin.Interfaces {
		ifname := aliases[name]
		if ifname == "" {
			return nil, fmt.Errorf("%s references hairpin interface with empty ifname %q", res.ID(), name)
		}
		if name == listen.Interface || ifname == aliases[listen.Interface] {
			return nil, fmt.Errorf("%s hairpin interface %q must not be the listen interface", res.ID(), name)
		}
		ifnames = append(ifnames, ifname)
	}
	sort.Strings(ifnames)
	return compactStrings(ifnames), nil
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
