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
	case "IPv4StaticAddress":
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.ipv4.address", aliases[spec.Interface]+":"+spec.Address, resource.ActionEnsure, "platform-network", nil)}
	case "IPv4DHCPAddress":
		spec, err := res.IPv4DHCPAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dhcp.ipv4.client", aliases[spec.Interface], resource.ActionEnsure, defaultString(spec.Client, "dhcpcd"), nil)}
	case "IPv4DHCPServer", "IPv6DHCPServer":
		return []resource.Intent{
			artifact("dnsmasq.config", "routerd", resource.ActionEnsure, "dnsmasq", nil),
			artifact("systemd.service", "routerd-dnsmasq.service", resource.ActionEnsure, "systemctl", nil),
		}
	case "IPv4DHCPScope":
		spec, err := res.IPv4DHCPScopeSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dnsmasq.dhcpv4.scope", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", map[string]string{"server": spec.Server})}
	case "IPv6DHCPAddress":
		spec, err := res.IPv6DHCPAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dhcp.ipv6.client", aliases[spec.Interface], resource.ActionEnsure, defaultString(spec.Client, "networkd"), nil)}
	case "IPv6RAAddress":
		spec, err := res.IPv6RAAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.ipv6.ra.client", aliases[spec.Interface], resource.ActionEnsure, "platform-network", nil)}
	case "IPv6PrefixDelegation":
		spec, err := res.IPv6PrefixDelegationSpec()
		if err != nil {
			return nil
		}
		profile := defaultString(spec.Profile, api.IPv6PDProfileDefault)
		client := defaultString(spec.Client, "networkd")
		intents := []resource.Intent{artifact("dhcp.ipv6.prefixDelegation", aliases[spec.Interface], resource.ActionEnsure, client, nil)}
		if client == "dhcp6c" && api.EffectiveIPv6PDDUIDType(profile, spec.DUIDType) == "link-layer" {
			intents = append(intents, artifact("file", "/var/db/dhcp6c_duid", resource.ActionEnsure, "dhcp6c", map[string]string{"purpose": "dhcpv6-client-duid"}))
		}
		return intents
	case "IPv6DelegatedAddress":
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("net.ipv6.address", aliases[spec.Interface]+":"+spec.AddressSuffix, resource.ActionEnsure, "platform-network", nil)}
	case "IPv6DHCPScope":
		spec, err := res.IPv6DHCPScopeSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dnsmasq.dhcpv6.scope", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", map[string]string{"server": spec.Server})}
	case "SelfAddressPolicy":
		return []resource.Intent{artifact("routerd.selfAddressPolicy", res.Metadata.Name, resource.ActionEnsure, "routerd", nil)}
	case "DNSConditionalForwarder":
		return []resource.Intent{artifact("dnsmasq.conditionalForwarder", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", nil)}
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
	case "Zone":
		return []resource.Intent{artifact("routerd.firewall.zone", res.Metadata.Name, resource.ActionEnsure, "nft", nil)}
	case "FirewallPolicy":
		return []resource.Intent{artifact("nft.table", "routerd_filter", resource.ActionEnsure, "nft", nil)}
	case "ExposeService":
		return []resource.Intent{artifact("nft.table", "routerd_dnat", resource.ActionEnsure, "nft", nil)}
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
	case "DHCPv4HostReservation":
		spec, err := res.DHCPv4HostReservationSpec()
		if err != nil {
			return nil
		}
		return []resource.Intent{artifact("dnsmasq.dhcpv4.host", res.Metadata.Name, resource.ActionEnsure, "dnsmasq", map[string]string{"scope": spec.Scope, "mac": spec.MACAddress, "ip": spec.IPAddress})}
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
