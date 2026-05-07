package render

import (
	"sort"
	"strings"

	"routerd/pkg/api"
)

// InternalFirewallHoles returns the small service exceptions that routerd-owned
// resources need for their own control-plane traffic. The filter renderers use
// this as an input so Linux nftables and FreeBSD pf stay in sync.
func InternalFirewallHoles(router *api.Router) []FirewallHole {
	zones := internalFirewallZoneIndex(router)
	var holes []FirewallHole
	add := func(name, from, to, proto string, port int, comment string) {
		if from == "" || to == "" {
			return
		}
		holes = append(holes, FirewallHole{Name: name, FromZone: from, ToZone: to, Protocol: proto, Port: port, Action: "accept", Comment: comment})
	}
	if router == nil {
		return nil
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv6PrefixDelegation":
			spec, _ := resource.DHCPv6PrefixDelegationSpec()
			add(resource.Metadata.Name+"-dhcpv6-client", zones.byResource(spec.Interface), "self", "udp", 546, resource.ID())
		case "DHCPv6Information":
			spec, _ := resource.DHCPv6InformationSpec()
			add(resource.Metadata.Name+"-dhcpv6-info", zones.byResource(spec.Interface), "self", "udp", 546, resource.ID())
		case "DHCPv4Lease":
			spec, _ := resource.DHCPv4LeaseSpec()
			add(resource.Metadata.Name+"-dhcpv4-client", zones.byResource(spec.Interface), "self", "udp", 68, resource.ID())
		case "DSLiteTunnel":
			spec, _ := resource.DSLiteTunnelSpec()
			add(resource.Metadata.Name+"-dslite-ipip", "self", zones.byResource(spec.Interface), "ipip", 0, resource.ID())
		case "DHCPv4Server":
			spec, _ := resource.DHCPv4ServerSpec()
			for _, iface := range resourceInterfaces(spec.Interface, spec.ListenInterfaces) {
				add(resource.Metadata.Name+"-dhcpv4-server-"+iface, zones.byResource(iface), "self", "udp", 67, resource.ID())
			}
		case "DHCPv6Server":
			spec, _ := resource.DHCPv6ServerSpec()
			for _, iface := range resourceInterfaces(spec.Interface, spec.ListenInterfaces) {
				add(resource.Metadata.Name+"-dhcpv6-server-"+iface, zones.byResource(iface), "self", "udp", 547, resource.ID())
			}
		case "DNSResolver":
			spec, _ := resource.DNSResolverSpec()
			for _, listen := range spec.Listen {
				for _, zone := range zones.byListenAddress(listen.Addresses) {
					add(resource.Metadata.Name+"-dns-udp-"+zone, zone, "self", "udp", listen.Port, resource.ID())
					add(resource.Metadata.Name+"-dns-tcp-"+zone, zone, "self", "tcp", listen.Port, resource.ID())
				}
			}
		case "IPv6RouterAdvertisement":
			spec, _ := resource.IPv6RouterAdvertisementSpec()
			add(resource.Metadata.Name+"-ra", "self", zones.byResource(spec.Interface), "icmpv6", 0, resource.ID())
		case "WireGuardInterface":
			spec, _ := resource.WireGuardInterfaceSpec()
			if spec.ListenPort != 0 {
				add(resource.Metadata.Name+"-wireguard", zones.firstUntrust(), "self", "udp", spec.ListenPort, resource.ID())
			}
		case "VXLANSegment":
			spec, _ := resource.VXLANSegmentSpec()
			if port := defaultInt(spec.UDPPort, 4789); port != 0 {
				add(resource.Metadata.Name+"-vxlan", zones.byResource(spec.UnderlayInterface), "self", "udp", port, resource.ID())
			}
		case "HealthCheck":
			spec, _ := resource.HealthCheckSpec()
			if spec.Protocol == "tcp" || spec.Protocol == "dns" || spec.Protocol == "http" {
				proto := "tcp"
				if spec.Protocol == "dns" {
					proto = "udp"
				}
				add(resource.Metadata.Name+"-healthcheck", "self", zones.byResource(spec.Interface), proto, spec.Port, resource.ID())
			}
		}
	}
	sort.Slice(holes, func(i, j int) bool { return holes[i].Name < holes[j].Name })
	return holes
}

func resourceInterfaces(primary string, listen []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range append([]string{primary}, listen...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

type internalFirewallZonesByRef struct {
	resource map[string]string
	role     map[string]string
}

func internalFirewallZoneIndex(router *api.Router) internalFirewallZonesByRef {
	out := internalFirewallZonesByRef{resource: map[string]string{}, role: map[string]string{}}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallZone" {
			continue
		}
		spec, err := resource.FirewallZoneSpec()
		if err != nil {
			continue
		}
		out.role[resource.Metadata.Name] = spec.Role
		for _, ref := range spec.Interfaces {
			kind, name := splitResourceRef(ref)
			out.resource[name] = resource.Metadata.Name
			out.resource[kind+"/"+name] = resource.Metadata.Name
		}
	}
	return out
}

func (z internalFirewallZonesByRef) byResource(name string) string {
	if zone := z.resource[name]; zone != "" {
		return zone
	}
	if _, short, ok := strings.Cut(name, "/"); ok {
		return z.resource[short]
	}
	return ""
}

func (z internalFirewallZonesByRef) firstUntrust() string {
	var names []string
	for name, role := range z.role {
		if role == "untrust" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func (z internalFirewallZonesByRef) byListenAddress(addresses []string) []string {
	var out []string
	for zone, role := range z.role {
		if role == "untrust" {
			continue
		}
		for _, address := range addresses {
			if address == "127.0.0.1" || address == "::1" {
				continue
			}
			if zone != "" && !stringSliceContains(out, zone) {
				out = append(out, zone)
			}
		}
	}
	sort.Strings(out)
	return out
}

func splitResourceRef(ref string) (string, string) {
	if kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/"); ok {
		return kind, name
	}
	return "Interface", strings.TrimSpace(ref)
}

func stringSliceContains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
