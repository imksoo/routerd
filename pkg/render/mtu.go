// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"sort"

	"routerd/pkg/api"
)

type pathMTUPolicy struct {
	ResourceID string
	Spec       pathMTUPolicySpec
	MTU        int
}

type pathMTUPolicySpec struct {
	FromInterface string
	ToInterfaces  []string
	IPv6RA        pathMTUPolicyIPv6RASpec
	TCPMSSClamp   pathMTUPolicyTCPMSSSpec
}

type pathMTUPolicyIPv6RASpec struct {
	Enabled bool
	Scope   string
}

type pathMTUPolicyTCPMSSSpec struct {
	Enabled  bool
	Families []string
}

type pathMTUTunnel struct {
	Name     string
	Underlay string
	MTU      int
}

func pathMTUPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	mtus, err := resourceMTUs(router)
	if err != nil {
		return nil, err
	}
	var policies []pathMTUPolicy
	for _, spec := range derivedPathMTUPolicySpecs(router) {
		if len(spec.ToInterfaces) == 0 {
			continue
		}
		mtu := 0
		for _, name := range spec.ToInterfaces {
			candidate := mtus[name]
			if candidate == 0 {
				return nil, fmt.Errorf("%s references interface with unknown MTU %q", specResourceID(spec), name)
			}
			if mtu == 0 || candidate < mtu {
				mtu = candidate
			}
		}
		if mtu < 1280 {
			return nil, fmt.Errorf("%s computed MTU %d is below the IPv6 minimum MTU 1280", specResourceID(spec), mtu)
		}
		policies = append(policies, pathMTUPolicy{ResourceID: specResourceID(spec), Spec: spec, MTU: mtu})
	}
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].ResourceID < policies[j].ResourceID
	})
	return policies, nil
}

func resourceMTUs(router *api.Router) (map[string]int, error) {
	mtus := map[string]int{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			mtus[res.Metadata.Name] = 1500
		case "PPPoESession":
			spec, err := res.PPPoESessionSpec()
			if err != nil {
				return nil, err
			}
			mtus[res.Metadata.Name] = defaultInt(spec.MTU, 1454)
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			mtus[res.Metadata.Name] = defaultInt(spec.MTU, 1454)
		case "WireGuardInterface":
			spec, err := res.WireGuardInterfaceSpec()
			if err != nil {
				return nil, err
			}
			mtus[res.Metadata.Name] = defaultInt(spec.MTU, 1420)
		}
	}
	return mtus, nil
}

func derivedPathMTUPolicySpecs(router *api.Router) []pathMTUPolicySpec {
	tunnels := pathMTUTunnels(router)
	if len(tunnels) == 0 {
		return nil
	}
	sources := pathMTUSourceInterfaces(router)
	if len(sources) == 0 {
		return nil
	}
	untrust := pathMTUUntrustInterfaces(router)
	var tunnelTargets []string
	for _, tunnel := range tunnels {
		if len(untrust) > 0 && !untrust[tunnel.Name] {
			continue
		}
		tunnelTargets = append(tunnelTargets, tunnel.Name)
		if tunnel.Underlay != "" && (len(untrust) == 0 || untrust[tunnel.Underlay]) {
			tunnelTargets = append(tunnelTargets, tunnel.Underlay)
		}
	}
	tunnelTargets = compactStrings(sortedStrings(tunnelTargets))
	if len(tunnelTargets) == 0 {
		return nil
	}
	raScopes := pathMTURAScopesByInterface(router)
	var policies []pathMTUPolicySpec
	for _, source := range sources {
		spec := pathMTUPolicySpec{
			FromInterface: source,
			ToInterfaces:  tunnelTargets,
			TCPMSSClamp: pathMTUPolicyTCPMSSSpec{
				Enabled:  true,
				Families: []string{"ipv4", "ipv6"},
			},
		}
		if scope := raScopes[source]; scope != "" {
			spec.IPv6RA = pathMTUPolicyIPv6RASpec{Enabled: true, Scope: scope}
		}
		policies = append(policies, spec)
	}
	return policies
}

func pathMTUTunnels(router *api.Router) []pathMTUTunnel {
	var tunnels []pathMTUTunnel
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				continue
			}
			tunnels = append(tunnels, pathMTUTunnel{Name: res.Metadata.Name, Underlay: spec.Interface, MTU: defaultInt(spec.MTU, 1454)})
		case "PPPoESession":
			spec, err := res.PPPoESessionSpec()
			if err != nil {
				continue
			}
			tunnels = append(tunnels, pathMTUTunnel{Name: res.Metadata.Name, Underlay: spec.Interface, MTU: defaultInt(spec.MTU, 1454)})
		case "WireGuardInterface":
			spec, err := res.WireGuardInterfaceSpec()
			if err != nil {
				continue
			}
			tunnels = append(tunnels, pathMTUTunnel{Name: res.Metadata.Name, MTU: defaultInt(spec.MTU, 1420)})
		}
	}
	sort.Slice(tunnels, func(i, j int) bool { return tunnels[i].Name < tunnels[j].Name })
	return tunnels
}

func pathMTUSourceInterfaces(router *api.Router) []string {
	var sources []string
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallZone" {
			continue
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil || spec.Role != "trust" {
			continue
		}
		for _, ref := range spec.Interfaces {
			_, name := splitResourceRef(ref)
			sources = append(sources, name)
		}
	}
	return compactStrings(sortedStrings(sources))
}

func pathMTUUntrustInterfaces(router *api.Router) map[string]bool {
	out := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallZone" {
			continue
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil || spec.Role != "untrust" {
			continue
		}
		for _, ref := range spec.Interfaces {
			_, name := splitResourceRef(ref)
			out[name] = true
		}
	}
	return out
}

func pathMTURAScopesByInterface(router *api.Router) map[string]string {
	out := map[string]string{}
	delegatedInterface := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err == nil {
			delegatedInterface[res.Metadata.Name] = spec.Interface
		}
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv6Server":
			spec, err := res.DHCPv6ServerSpec()
			if err != nil {
				continue
			}
			if iface := delegatedInterface[spec.DelegatedAddress]; iface != "" && out[iface] == "" {
				out[iface] = res.Metadata.Name
			}
		case "IPv6RouterAdvertisement":
			spec, err := res.IPv6RouterAdvertisementSpec()
			if err != nil {
				continue
			}
			if out[spec.Interface] == "" {
				out[spec.Interface] = res.Metadata.Name
			}
		}
	}
	return out
}

func pathMTURAByScope(router *api.Router) (map[string]int, error) {
	policies, err := pathMTUPolicies(router)
	if err != nil {
		return nil, err
	}
	result := map[string]int{}
	for _, policy := range policies {
		if !policy.Spec.IPv6RA.Enabled {
			continue
		}
		scope := policy.Spec.IPv6RA.Scope
		if scope == "" {
			continue
		}
		if existing := result[scope]; existing == 0 || policy.MTU < existing {
			result[scope] = policy.MTU
		}
	}
	return result, nil
}

func PathMTURAByScope(router *api.Router) (map[string]int, error) {
	return pathMTURAByScope(router)
}

func pathMTUMSSPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	policies, err := pathMTUPolicies(router)
	if err != nil {
		return nil, err
	}
	var result []pathMTUPolicy
	for _, policy := range policies {
		if policy.Spec.TCPMSSClamp.Enabled {
			result = append(result, policy)
		}
	}
	return result, nil
}

func pathMTUFamilyEnabled(families []string, family string) bool {
	if len(families) == 0 {
		return true
	}
	for _, candidate := range families {
		if candidate == family {
			return true
		}
	}
	return false
}

func specResourceID(spec pathMTUPolicySpec) string {
	return "routerd.net/v1alpha1/Router/derived-path-mtu-" + spec.FromInterface
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
