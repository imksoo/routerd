package apply

import (
	"fmt"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/resource"
)

func resourceArtifactIntents(res api.Resource, aliases map[string]string) []resource.Intent {
	owner := res.ID()
	artifact := func(kind, name, action, applyWith string, attrs map[string]string) resource.Intent {
		if attrs == nil {
			attrs = map[string]string{}
		}
		return resource.Intent{
			Artifact:  resource.Artifact{Kind: kind, Name: name, Owner: owner, Attributes: attrs},
			Action:    action,
			ApplyWith: applyWith,
		}
	}
	switch res.Kind {
	case "LogSink":
		return []resource.Intent{artifact("routerd.logSink", res.Metadata.Name, resource.ActionEnsure, "eventlog", nil)}
	case "Sysctl":
		spec, err := res.SysctlSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("host.sysctl", spec.Key, resource.ActionEnsure, "sysctl", map[string]string{"value": spec.Value})}
	case "NTPClient":
		return []resource.Intent{artifact("systemd.timesyncd.config", "routerd.conf", resource.ActionEnsure, "timesyncd", nil)}
	case "Interface":
		spec, err := res.InterfaceSpec()
		if err != nil {
			return nil
		}
		action := resource.ActionObserve
		if spec.Managed {
			action = resource.ActionEnsure
		}
		return []resource.Intent{artifact("net.link", spec.IfName, action, "platform-network", nil)}
	case "PPPoEInterface":
		spec, err := res.PPPoEInterfaceSpec()
		if err != nil {
			return nil
		}
		ifname := defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
		return []resource.Intent{
			artifact("ppp.interface", ifname, resource.ActionEnsure, "pppd", nil),
			artifact("systemd.service", "routerd-pppoe-"+res.Metadata.Name+".service", resource.ActionEnsure, "systemctl", nil),
			artifact("file", "/etc/ppp/chap-secrets", resource.ActionEnsure, "file", nil),
			artifact("file", "/etc/ppp/pap-secrets", resource.ActionEnsure, "file", nil),
			artifact("file", "/usr/local/etc/mpd5/mpd.conf", resource.ActionEnsure, "mpd5", nil),
			artifact("rc.d.service", "mpd5", resource.ActionEnsure, "service", nil),
		}
	case "PPPoESession":
		spec, err := res.PPPoESessionSpec()
		if err != nil {
			return nil
		}
		ifname := defaultString(spec.Interface, res.Metadata.Name)
		return []resource.Intent{
			artifact("routerd.pppoe.client", res.Metadata.Name, resource.ActionEnsure, "routerd-pppoe-client", map[string]string{"interface": ifname}),
			artifact("unix.socket", "/run/routerd/pppoe-client/"+res.Metadata.Name+".sock", resource.ActionEnsure, "routerd-pppoe-client", nil),
		}
	case "IPv4StaticAddress":
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.ipv4.address", aliases[spec.Interface]+":"+spec.Address, resource.ActionEnsure, "platform-network", nil)}
	case "DHCPv4Address":
		spec, err := res.DHCPv4AddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("routerd.dhcpv4.client", aliases[spec.Interface], resource.ActionEnsure, "routerd-dhcpv4-client", nil)}
	case "DHCPv4Lease":
		return []resource.Intent{artifact("routerd.dhcpv4.client", res.Metadata.Name, resource.ActionEnsure, "routerd-dhcpv4-client", nil)}
	case "WireGuardInterface":
		return []resource.Intent{artifact("net.wireguard.interface", res.Metadata.Name, resource.ActionEnsure, "wg", nil)}
	case "WireGuardPeer":
		spec, err := res.WireGuardPeerSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.wireguard.peer", spec.Interface+"/"+res.Metadata.Name, resource.ActionEnsure, "wg", map[string]string{"interface": spec.Interface})}
	case "IPsecConnection":
		return []resource.Intent{
			artifact("ipsec.swanctl.connection", res.Metadata.Name, resource.ActionEnsure, "swanctl", nil),
			artifact("file", "/usr/local/etc/swanctl/conf.d/routerd-"+res.Metadata.Name+".conf", resource.ActionEnsure, "file", nil),
		}
	case "VRF":
		spec, err := res.VRFSpec()
		if err != nil {
			return nil
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		return []resource.Intent{artifact("net.vrf", ifname, resource.ActionEnsure, "ip-link", map[string]string{"routeTable": fmt.Sprintf("%d", spec.RouteTable)})}
	case "VXLANTunnel":
		spec, err := res.VXLANTunnelSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.vxlan.tunnel", defaultString(spec.IfName, res.Metadata.Name), resource.ActionEnsure, "ip-link", map[string]string{"vni": fmt.Sprintf("%d", spec.VNI)})}
	case "DHCPv4Server", "DHCPv6Server":
		return []resource.Intent{
			artifact("dnsmasq.config", "routerd", resource.ActionEnsure, "dnsmasq", nil),
			artifact("systemd.service", "routerd-dnsmasq.service", resource.ActionEnsure, "systemctl", nil),
		}
	case "DHCPv4Reservation":
		spec, err := res.DHCPv4ReservationSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dnsmasq.dhcpv4.host", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", map[string]string{"server": spec.Server, "scope": spec.Scope, "mac": spec.MACAddress, "ip": spec.IPAddress})}
	case "DHCPv4Scope":
		spec, err := res.DHCPv4ScopeSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dnsmasq.dhcpv4.scope", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", map[string]string{"server": spec.Server})}
	case "DHCPv6Address":
		spec, err := res.DHCPv6AddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("routerd.dhcpv6.addressClient", aliases[spec.Interface], resource.ActionEnsure, "routerd-dhcpv6-client", nil)}
	case "IPv6RAAddress":
		spec, err := res.IPv6RAAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.ipv6.ra.client", aliases[spec.Interface], resource.ActionEnsure, "platform-network", nil)}
	case "DHCPv6PrefixDelegation":
		return []resource.Intent{artifact("systemd.service", "routerd-dhcpv6-client@"+res.Metadata.Name+".service", resource.ActionEnsure, "systemctl", map[string]string{"purpose": "dhcpv6-prefix-delegation"})}
	case "IPv6DelegatedAddress":
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.ipv6.address", aliases[spec.Interface]+":"+spec.AddressSuffix, resource.ActionEnsure, "platform-network", nil)}
	case "DHCPv6Scope":
		spec, err := res.DHCPv6ScopeSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dnsmasq.dhcpv6.scope", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", map[string]string{"server": spec.Server})}
	case "DHCPv4Relay":
		return []resource.Intent{artifact("dnsmasq.dhcp.relay", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", nil)}
	case "SelfAddressPolicy":
		return []resource.Intent{artifact("routerd.selfAddressPolicy", res.Metadata.Name, resource.ActionEnsure, "routerd", nil)}
	case "DNSZone":
		return []resource.Intent{artifact("routerd.dns.zone", res.Metadata.Name, resource.ActionEnsure, "routerd-dns-resolver", nil)}
	case "DNSResolver":
		return []resource.Intent{artifact("systemd.service", "routerd-dns-resolver@"+res.Metadata.Name+".service", resource.ActionEnsure, "systemctl", nil)}
	case "DSLiteTunnel":
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("linux.ipip6.tunnel", defaultString(spec.TunnelName, res.Metadata.Name), resource.ActionEnsure, "ip-link", nil)}
	case "HealthCheck":
		return []resource.Intent{artifact("routerd.healthCheck", res.Metadata.Name, resource.ActionEnsure, "routerd-scheduler", nil)}
	case "IPv4DefaultRoutePolicy":
		return ipv4DefaultRoutePolicyArtifacts(res, aliases)
	case "IPv4SourceNAT":
		return []resource.Intent{artifact("nft.table", "routerd_nat", resource.ActionEnsure, "nft", nil)}
	case "NAT44Rule":
		return []resource.Intent{artifact("nft.table", "routerd_nat", resource.ActionEnsure, "nft", nil)}
	case "IPv4PolicyRoute":
		return ipv4PolicyRouteArtifacts(res, aliases)
	case "IPv4PolicyRouteSet":
		return ipv4PolicyRouteSetArtifacts(res, aliases)
	case "IPv4ReversePathFilter":
		spec, err := res.IPv4ReversePathFilterSpec()
		if err != nil {
			return nil
		}
		target := spec.Target
		if target == "interface" {
			target = aliases[spec.Interface]
		}
		return []resource.Intent{artifact("host.sysctl", "net.ipv4.conf."+target+".rp_filter", resource.ActionEnsure, "sysctl", nil)}
	case "PathMTUPolicy":
		return []resource.Intent{
			artifact("nft.table", "routerd_mss", resource.ActionEnsure, "nft", nil),
			artifact("dnsmasq.ra.mtu", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", nil),
		}
	case "FirewallZone":
		return []resource.Intent{artifact("routerd.firewall.zone", res.Metadata.Name, resource.ActionEnsure, "nft", nil)}
	case "FirewallPolicy", "FirewallRule":
		return []resource.Intent{artifact("nft.table", "routerd_filter", resource.ActionEnsure, "nft", nil)}
	case "VXLANSegment":
		spec, err := res.VXLANSegmentSpec()
		if err != nil {
			return nil
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		intents := []resource.Intent{artifact("net.link", ifname, resource.ActionEnsure, "platform-network", nil)}
		if defaultString(spec.L2Filter, "default") != "none" {
			intents = append(intents, artifact("nft.table", "routerd_l2_filter", resource.ActionEnsure, "nft", nil))
		}
		return intents
	case "Bridge":
		spec, err := res.BridgeSpec()
		if err != nil {
			return nil
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		return []resource.Intent{artifact("net.link", ifname, resource.ActionEnsure, "platform-network", nil)}
	case "IPv4StaticRoute":
		spec, err := res.IPv4StaticRouteSpec()
		if err != nil {
			return nil
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		return []resource.Intent{artifact("net.ipv4.route", ifname+":"+spec.Destination, resource.ActionEnsure, "platform-network", map[string]string{"via": spec.Via})}
	case "IPv6StaticRoute":
		spec, err := res.IPv6StaticRouteSpec()
		if err != nil {
			return nil
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		return []resource.Intent{artifact("net.ipv6.route", ifname+":"+spec.Destination, resource.ActionEnsure, "platform-network", map[string]string{"via": spec.Via})}
	case "Hostname":
		spec, err := res.HostnameSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("host.hostname", "system", resource.ActionEnsure, "platform-hostname", map[string]string{"hostname": spec.Hostname})}
	default:
		return nil
	}
}

func ipv4PolicyRouteArtifacts(res api.Resource, aliases map[string]string) []resource.Intent {
	spec, err := res.IPv4PolicyRouteSpec()
	if err != nil {
		return nil
	}
	target := api.IPv4PolicyRouteTarget{
		Name:              res.Metadata.Name,
		OutboundInterface: spec.OutboundInterface,
		Table:             spec.Table,
		Priority:          spec.Priority,
		Mark:              spec.Mark,
		RouteMetric:       spec.RouteMetric,
	}
	return ipv4PolicyTargetArtifacts(res.ID(), target, aliases)
}

func ipv4PolicyRouteSetArtifacts(res api.Resource, aliases map[string]string) []resource.Intent {
	spec, err := res.IPv4PolicyRouteSetSpec()
	if err != nil {
		return nil
	}
	var intents []resource.Intent
	intents = append(intents, resource.Intent{
		Artifact:  resource.Artifact{Kind: "nft.table", Name: "routerd_policy", Owner: res.ID()},
		Action:    resource.ActionEnsure,
		ApplyWith: "nft",
	})
	for i, target := range spec.Targets {
		if target.Name == "" {
			target.Name = fmt.Sprintf("%s-%d", res.Metadata.Name, i)
		}
		intents = append(intents, ipv4PolicyTargetArtifacts(res.ID(), target, aliases)...)
	}
	return intents
}

func ipv4DefaultRoutePolicyArtifacts(res api.Resource, aliases map[string]string) []resource.Intent {
	spec, err := res.IPv4DefaultRoutePolicySpec()
	if err != nil {
		return nil
	}
	var intents []resource.Intent
	intents = append(intents, resource.Intent{
		Artifact:  resource.Artifact{Kind: "nft.table", Name: "routerd_default_route", Owner: res.ID()},
		Action:    resource.ActionEnsure,
		ApplyWith: "nft",
	})
	for _, candidate := range spec.Candidates {
		if candidate.RouteSet != "" {
			continue
		}
		target := api.IPv4PolicyRouteTarget{
			Name:              defaultString(candidate.Name, candidate.Interface),
			OutboundInterface: candidate.Interface,
			Table:             candidate.Table,
			Priority:          candidate.Priority,
			Mark:              candidate.Mark,
			RouteMetric:       candidate.RouteMetric,
		}
		intents = append(intents, ipv4PolicyTargetArtifacts(res.ID(), target, aliases)...)
	}
	return intents
}

func ipv4PolicyTargetArtifacts(owner string, target api.IPv4PolicyRouteTarget, aliases map[string]string) []resource.Intent {
	if target.Priority == 0 || target.Mark == 0 || target.Table == 0 {
		return nil
	}
	ifname := aliases[target.OutboundInterface]
	if ifname == "" {
		ifname = target.OutboundInterface
	}
	routeName := fmt.Sprintf("table=%d", target.Table)
	return []resource.Intent{
		{
			Artifact: resource.Artifact{
				Kind:  "linux.ipv4.routeTable",
				Name:  routeName,
				Owner: owner,
				Attributes: map[string]string{
					"table":  fmt.Sprintf("%d", target.Table),
					"ifname": ifname,
				},
			},
			Action:    resource.ActionEnsure,
			ApplyWith: "ip-route",
		},
		{
			Artifact:  newIPv4FwmarkRuleArtifact(owner, target.Priority, target.Mark, target.Table),
			Action:    resource.ActionEnsure,
			ApplyWith: "ip-rule",
		},
	}
}

func artifactIntentsForResult(intents []resource.Intent) []ArtifactIntent {
	out := make([]ArtifactIntent, 0, len(intents))
	for _, intent := range intents {
		name := intent.Artifact.Name
		if name == "" && len(intent.Artifact.Attributes) > 0 {
			var parts []string
			for key, value := range intent.Artifact.Attributes {
				parts = append(parts, key+"="+value)
			}
			name = strings.Join(parts, ",")
		}
		out = append(out, ArtifactIntent{
			Kind:      intent.Artifact.Kind,
			Name:      name,
			Action:    defaultString(intent.Action, resource.ActionEnsure),
			ApplyWith: intent.ApplyWith,
		})
	}
	return out
}
