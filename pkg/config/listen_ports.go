// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

type listenEndpoint struct {
	Interface string
	Address   string
	Protocol  string
	Port      int
	Role      string
	Group     string
	Owner     string
}

type listenPortRegistry struct {
	endpoints []listenEndpoint
}

func validateListenPortCollisions(router *api.Router) error {
	var registry listenPortRegistry
	for _, res := range router.Spec.Resources {
		if err := registry.addResource(router, res); err != nil {
			return err
		}
	}
	return nil
}

func (r *listenPortRegistry) addResource(router *api.Router, res api.Resource) error {
	switch {
	case res.APIVersion == api.FirewallAPIVersion && res.Kind == "IngressService":
		spec, err := res.IngressServiceSpec()
		if err != nil {
			return err
		}
		return r.add(spec.Listen.Interface, spec.Listen.Protocol, spec.Listen.Port, "ingress", "", res.ID()+".spec.listen")
	case res.APIVersion == api.FirewallAPIVersion && res.Kind == "LocalServiceRedirect":
		spec, err := res.LocalServiceRedirectSpec()
		if err != nil {
			return err
		}
		for i, rule := range spec.Rules {
			for _, proto := range rule.Protocols {
				if err := r.add(spec.Interface, proto, rule.RedirectPort, "redirect", "", fmt.Sprintf("%s.spec.rules[%d].redirectPort", res.ID(), i)); err != nil {
					return err
				}
			}
		}
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Server":
		spec, err := res.DHCPv4ServerSpec()
		if err != nil {
			return err
		}
		for _, iface := range resourceListenInterfaces(spec.Interface, spec.ListenInterfaces) {
			if err := r.add(iface, "udp", 67, "daemon", "", res.ID()+".dhcpv4-server"); err != nil {
				return err
			}
			if spec.DNS.Enabled {
				if err := r.add(iface, "udp", 53, "daemon", "", res.ID()+".dns"); err != nil {
					return err
				}
				if err := r.add(iface, "tcp", 53, "daemon", "", res.ID()+".dns"); err != nil {
					return err
				}
			}
		}
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Client":
		spec, err := res.DHCPv4ClientSpec()
		if err != nil {
			return err
		}
		return r.add(spec.Interface, "udp", 68, "daemon", "", res.ID()+".dhcpv4-client")
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6PrefixDelegation":
		spec, err := res.DHCPv6PrefixDelegationSpec()
		if err != nil {
			return err
		}
		return r.add(spec.Interface, "udp", 546, "daemon", "dhcpv6-client", res.ID()+".dhcpv6-client")
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Information":
		spec, err := res.DHCPv6InformationSpec()
		if err != nil {
			return err
		}
		return r.add(spec.Interface, "udp", 546, "daemon", "dhcpv6-client", res.ID()+".dhcpv6-information")
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Address":
		spec, err := res.DHCPv6AddressSpec()
		if err != nil {
			return err
		}
		return r.add(spec.Interface, "udp", 546, "daemon", "dhcpv6-client", res.ID()+".dhcpv6-address")
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Server":
		spec, err := res.DHCPv6ServerSpec()
		if err != nil {
			return err
		}
		for _, iface := range resourceListenInterfaces(spec.Interface, spec.ListenInterfaces) {
			if err := r.add(iface, "udp", 547, "daemon", "", res.ID()+".dhcpv6-server"); err != nil {
				return err
			}
		}
	case res.APIVersion == api.NetAPIVersion && res.Kind == "DNSResolver":
		spec, err := res.DNSResolverSpec()
		if err != nil {
			return err
		}
		for i, listen := range spec.Listen {
			port := listen.Port
			if port == 0 {
				port = 53
			}
			for _, iface := range dnsListenInterfaces(router, listen) {
				for _, proto := range []string{"udp", "tcp"} {
					if err := r.add(iface, proto, port, "daemon", "", fmt.Sprintf("%s.spec.listen[%d]", res.ID(), i)); err != nil {
						return err
					}
				}
			}
		}
	case res.APIVersion == api.NetAPIVersion && res.Kind == "BGPRouter":
		spec, err := res.BGPRouterSpec()
		if err != nil {
			return err
		}
		port := spec.Listen.Port
		if port == 0 {
			port = 179
		}
		return r.addAddress("", spec.Listen.Address, "tcp", port, "daemon", "", res.ID()+".bgp-listen")
	case res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6RouterAdvertisement":
		spec, err := res.IPv6RouterAdvertisementSpec()
		if err != nil {
			return err
		}
		return r.add(spec.Interface, "icmpv6", 0, "daemon", "", res.ID()+".router-advertisement")
	}
	return nil
}

func (r *listenPortRegistry) add(iface, protocol string, port int, role, group, owner string) error {
	return r.addAddress(iface, "", protocol, port, role, group, owner)
}

func (r *listenPortRegistry) addAddress(iface, address, protocol string, port int, role, group, owner string) error {
	protocol = strings.TrimSpace(protocol)
	if protocol == "" || port < 0 {
		return nil
	}
	endpoint := listenEndpoint{
		Interface: strings.TrimSpace(iface),
		Address:   strings.TrimSpace(address),
		Protocol:  protocol,
		Port:      port,
		Role:      role,
		Group:     group,
		Owner:     owner,
	}
	for _, existing := range r.endpoints {
		if !listenEndpointsConflict(existing, endpoint) {
			continue
		}
		return fmt.Errorf("%s conflicts with %s on %s", endpoint.Owner, existing.Owner, listenEndpointLabel(endpoint))
	}
	r.endpoints = append(r.endpoints, endpoint)
	return nil
}

func listenEndpointsConflict(a, b listenEndpoint) bool {
	if a.Protocol != b.Protocol || a.Port != b.Port {
		return false
	}
	if a.Group != "" && a.Group == b.Group {
		return false
	}
	if a.Role == "redirect" && b.Role == "redirect" {
		return false
	}
	if listenRedirectsToDaemon(a, b) {
		return false
	}
	interfaceMatch := a.Interface == "" || b.Interface == "" || a.Interface == b.Interface
	addressMatch := a.Address == "" || b.Address == "" || a.Address == b.Address
	return interfaceMatch && addressMatch
}

func listenRedirectsToDaemon(a, b listenEndpoint) bool {
	return (a.Role == "redirect" && b.Role == "daemon") || (a.Role == "daemon" && b.Role == "redirect")
}

func listenEndpointLabel(endpoint listenEndpoint) string {
	if endpoint.Port == 0 {
		if endpoint.Interface == "" {
			return "all interfaces proto " + endpoint.Protocol
		}
		return "interface " + endpoint.Interface + " proto " + endpoint.Protocol
	}
	port := endpoint.Protocol + "/" + strconv.Itoa(endpoint.Port)
	if endpoint.Address != "" {
		return "address " + endpoint.Address + " " + port
	}
	if endpoint.Interface == "" {
		return "all interfaces " + port
	}
	return "interface " + endpoint.Interface + " " + port
}

func resourceListenInterfaces(primary string, additional []string) []string {
	values := compactListenStrings(append([]string{primary}, additional...))
	if len(values) == 0 {
		return nil
	}
	return values
}

func dnsListenInterfaces(router *api.Router, listen api.DNSResolverListenSpec) []string {
	var out []string
	for _, source := range listen.AddressFrom {
		if iface := statusSourceInterface(router, source); iface != "" {
			out = append(out, iface)
		}
	}
	for _, address := range listen.Addresses {
		switch strings.TrimSpace(address) {
		case "0.0.0.0", "::", "[::]":
			out = append(out, "")
		}
	}
	seen := map[string]bool{}
	compact := make([]string, 0, len(out))
	for _, value := range out {
		value = strings.TrimSpace(value)
		if seen[value] {
			continue
		}
		seen[value] = true
		compact = append(compact, value)
	}
	return compact
}

func statusSourceInterface(router *api.Router, source api.StatusValueSourceSpec) string {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || name == "" {
		return ""
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != kind || res.Metadata.Name != name {
			continue
		}
		if iface, err := interfaceRef(res); err == nil {
			return strings.TrimSpace(iface)
		}
	}
	return ""
}

func compactListenStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
