package reconcile

import (
	"bytes"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/config"
)

type Engine struct {
	Command      func(name string, args ...string) ([]byte, error)
	OSNetworking *osNetworking
}

func New() *Engine {
	return &Engine{Command: runCommand}
}

func (e *Engine) Validate(router *api.Router) error {
	return config.Validate(router)
}

func (e *Engine) Observe(router *api.Router) (*Result, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	return e.evaluate(router, false)
}

func (e *Engine) Plan(router *api.Router) (*Result, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	return e.evaluate(router, true)
}

func (e *Engine) evaluate(router *api.Router, includePlan bool) (*Result, error) {
	aliases := interfaceAliases(router)
	kinds := resourceKinds(router)
	osNet := e.detectOSNetworking()
	policies := interfacePolicies(router, osNet)
	observedV4 := e.observedIPv4Prefixes(policies)
	observedV4ByInterface := ipv4AssignmentsByInterface(observedV4)
	desiredV4 := desiredIPv4Prefixes(router, aliases)
	overlaps := ipv4Overlaps(desiredV4, observedV4)
	result := &Result{
		Generation: time.Now().Unix(),
		Timestamp:  time.Now().UTC(),
		Phase:      "Healthy",
	}

	for _, res := range router.Spec.Resources {
		rr := ResourceResult{
			ID:       res.ID(),
			Phase:    "Healthy",
			Observed: map[string]string{},
		}

		switch res.Kind {
		case "LogSink":
			e.observeLogSink(res, includePlan, &rr)
		case "Sysctl":
			e.observeSysctl(res, includePlan, &rr)
		case "NTPClient":
			e.observeNTPClient(res, aliases, includePlan, &rr)
		case "Interface":
			e.observeInterface(res, policies[res.Metadata.Name], observedV4ByInterface[res.Metadata.Name], includePlan, &rr)
		case "PPPoEInterface":
			e.observePPPoEInterface(res, aliases, includePlan, &rr)
		case "IPv4StaticAddress":
			e.observeIPv4Static(res, aliases, policies, overlaps[res.ID()], includePlan, &rr)
		case "IPv4DHCPAddress":
			e.observeDHCP(res, aliases, policies, "ipv4", includePlan, &rr)
		case "IPv4DHCPServer":
			e.observeIPv4DHCPServer(res, includePlan, &rr)
		case "IPv4DHCPScope":
			e.observeIPv4DHCPScope(res, aliases, policies, includePlan, &rr)
		case "IPv6DHCPAddress":
			e.observeDHCP(res, aliases, policies, "ipv6", includePlan, &rr)
		case "IPv6PrefixDelegation":
			e.observeIPv6PrefixDelegation(res, aliases, includePlan, &rr)
		case "IPv6DelegatedAddress":
			e.observeIPv6DelegatedAddress(res, aliases, includePlan, &rr)
		case "IPv6DHCPServer":
			e.observeIPv6DHCPServer(res, includePlan, &rr)
		case "IPv6DHCPScope":
			e.observeIPv6DHCPScope(res, includePlan, &rr)
		case "SelfAddressPolicy":
			e.observeSelfAddressPolicy(res, includePlan, &rr)
		case "DNSConditionalForwarder":
			e.observeDNSConditionalForwarder(res, aliases, includePlan, &rr)
		case "DSLiteTunnel":
			e.observeDSLiteTunnel(res, aliases, includePlan, &rr)
		case "HealthCheck":
			e.observeHealthCheck(res, aliases, kinds, includePlan, &rr)
		case "IPv4DefaultRoutePolicy":
			e.observeIPv4DefaultRoutePolicy(res, aliases, includePlan, &rr)
		case "IPv4SourceNAT":
			e.observeIPv4SourceNAT(res, aliases, policies, includePlan, &rr)
		case "IPv4PolicyRoute":
			e.observeIPv4PolicyRoute(res, aliases, policies, includePlan, &rr)
		case "IPv4PolicyRouteSet":
			e.observeIPv4PolicyRouteSet(res, aliases, policies, includePlan, &rr)
		case "IPv4ReversePathFilter":
			e.observeIPv4ReversePathFilter(res, aliases, includePlan, &rr)
		case "Hostname":
			e.observeHostname(res, osNet, includePlan, &rr)
		}

		if rr.Phase == "RequiresAdoption" || rr.Phase == "Blocked" {
			result.Phase = "Blocked"
		}
		result.Resources = append(result.Resources, rr)
	}

	return result, nil
}

func (e *Engine) observeNTPClient(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.NTPClientSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	provider := defaultString(spec.Provider, "systemd-timesyncd")
	source := defaultString(spec.Source, "static")
	rr.Observed["provider"] = provider
	rr.Observed["managed"] = fmt.Sprintf("%t", spec.Managed)
	rr.Observed["source"] = source
	rr.Observed["servers"] = strings.Join(spec.Servers, ",")
	if spec.Interface != "" {
		rr.Observed["interface"] = spec.Interface
		rr.Observed["ifname"] = aliases[spec.Interface]
	}
	if out, err := e.Command("timedatectl", "show", "-p", "NTPSynchronized", "--value"); err == nil {
		rr.Observed["synchronized"] = strings.TrimSpace(string(out))
	}
	if !includePlan {
		return
	}
	if !spec.Managed {
		rr.Plan = append(rr.Plan, "observe only; NTP client is not managed")
		return
	}
	if spec.Interface != "" {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s uses static NTP servers on %s", provider, aliases[spec.Interface]))
	} else {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s uses static global NTP servers", provider))
	}
}

func (e *Engine) observeLogSink(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.LogSinkSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	enabled := api.BoolDefault(spec.Enabled, true)
	minLevel := defaultString(spec.MinLevel, "info")
	rr.Observed["type"] = spec.Type
	rr.Observed["enabled"] = fmt.Sprintf("%t", enabled)
	rr.Observed["minLevel"] = minLevel
	switch spec.Type {
	case "syslog":
		rr.Observed["facility"] = defaultString(spec.Syslog.Facility, "local6")
		rr.Observed["tag"] = defaultString(spec.Syslog.Tag, "routerd")
		if spec.Syslog.Network != "" {
			rr.Observed["network"] = spec.Syslog.Network
		}
		if spec.Syslog.Address != "" {
			rr.Observed["address"] = spec.Syslog.Address
		}
	case "plugin":
		rr.Observed["pluginPath"] = spec.Plugin.Path
		rr.Observed["timeout"] = defaultString(spec.Plugin.Timeout, "5s")
	}
	if !includePlan {
		return
	}
	if !enabled {
		rr.Plan = append(rr.Plan, "log sink is disabled")
		return
	}
	switch spec.Type {
	case "syslog":
		rr.Plan = append(rr.Plan, fmt.Sprintf("send routerd events to syslog facility %s", defaultString(spec.Syslog.Facility, "local6")))
	case "plugin":
		rr.Plan = append(rr.Plan, fmt.Sprintf("send routerd events to local log plugin %s", spec.Plugin.Path))
	}
}

func (e *Engine) observeIPv6PrefixDelegation(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv6PrefixDelegationSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := aliases[spec.Interface]
	client := defaultString(spec.Client, "networkd")
	profile := defaultString(spec.Profile, "default")
	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = ifname
	rr.Observed["client"] = client
	rr.Observed["profile"] = profile
	prefixLength := effectiveIPv6PDPrefixLength(profile, spec.PrefixLength)
	if prefixLength != 0 {
		rr.Observed["prefixLength"] = fmt.Sprintf("%d", prefixLength)
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("request DHCPv6 prefix delegation on %s with %s", ifname, client))
		switch profile {
		case "ntt-ngn-direct-hikari-denwa":
			rr.Plan = append(rr.Plan, "use NTT NGN direct Hikari Denwa DHCPv6-PD profile quirks")
		case "ntt-hgw-lan-pd":
			rr.Plan = append(rr.Plan, "use NTT HGW LAN-side DHCPv6-PD profile quirks")
		}
	}
}

func (e *Engine) observeIPv6DelegatedAddress(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv6DelegatedAddressSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := aliases[spec.Interface]
	subnetID := defaultString(spec.SubnetID, "0")
	rr.Observed["prefixDelegation"] = spec.PrefixDelegation
	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = ifname
	rr.Observed["subnetID"] = subnetID
	rr.Observed["addressSuffix"] = spec.AddressSuffix
	rr.Observed["sendRA"] = fmt.Sprintf("%t", spec.SendRA)
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("derive IPv6 address %s from delegated prefix subnet %s on %s", spec.AddressSuffix, subnetID, ifname))
		if spec.SendRA {
			rr.Plan = append(rr.Plan, fmt.Sprintf("send IPv6 router advertisements for delegated prefix on %s", ifname))
		}
	}
}

func effectiveIPv6PDPrefixLength(profile string, configured int) int {
	if configured != 0 {
		return configured
	}
	if profile == "ntt-ngn-direct-hikari-denwa" || profile == "ntt-hgw-lan-pd" {
		return 60
	}
	return 0
}

func (e *Engine) observeIPv6DHCPServer(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv6DHCPServerSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	server := defaultString(spec.Server, "dnsmasq")
	rr.Observed["server"] = server
	rr.Observed["managed"] = fmt.Sprintf("%t", spec.Managed)
	rr.Observed["listenInterfaces"] = strings.Join(spec.ListenInterfaces, ",")
	if _, err := exec.LookPath(server); err == nil {
		rr.Observed["serverAvailable"] = "true"
	} else {
		rr.Observed["serverAvailable"] = "false"
		if includePlan {
			rr.Warnings = append(rr.Warnings, fmt.Sprintf("%s is required to ensure DHCPv6 server on this host", server))
		}
	}
	if !includePlan {
		return
	}
	if !spec.Managed {
		rr.Plan = append(rr.Plan, "observe only; DHCPv6 server instance is not managed")
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv6 DHCP server instance %s is available", server))
	if len(spec.ListenInterfaces) > 0 {
		rr.Plan = append(rr.Plan, fmt.Sprintf("serve dnsmasq RA/DHCPv6 only on %s", strings.Join(spec.ListenInterfaces, ",")))
	}
}

func (e *Engine) observeIPv6DHCPScope(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv6DHCPScopeSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	mode := defaultString(spec.Mode, "stateless")
	dnsSource := defaultString(spec.DNSSource, "self")
	rr.Observed["server"] = spec.Server
	rr.Observed["delegatedAddress"] = spec.DelegatedAddress
	rr.Observed["mode"] = mode
	rr.Observed["defaultRoute"] = fmt.Sprintf("%t", spec.DefaultRoute)
	rr.Observed["dnsSource"] = dnsSource
	if spec.SelfAddressPolicy != "" {
		rr.Observed["selfAddressPolicy"] = spec.SelfAddressPolicy
	}
	if spec.LeaseTime != "" {
		rr.Observed["leaseTime"] = spec.LeaseTime
	}
	if len(spec.DNSServers) > 0 {
		rr.Observed["dnsServers"] = strings.Join(spec.DNSServers, ",")
	}
	if !includePlan {
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv6 DHCP scope %s uses delegated address %s", spec.Server, spec.DelegatedAddress))
	if spec.DefaultRoute {
		rr.Plan = append(rr.Plan, "advertise IPv6 default route by router advertisement")
	}
	switch dnsSource {
	case "self":
		rr.Plan = append(rr.Plan, "advertise this router's delegated IPv6 address as DNS server")
	case "static":
		rr.Plan = append(rr.Plan, fmt.Sprintf("advertise IPv6 DNS servers %s", strings.Join(spec.DNSServers, ",")))
	case "none":
		rr.Plan = append(rr.Plan, "do not advertise IPv6 DNS servers")
	}
}

func (e *Engine) observeSelfAddressPolicy(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.SelfAddressPolicySpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["addressFamily"] = spec.AddressFamily
	rr.Observed["candidates"] = fmt.Sprintf("%d", len(spec.Candidates))
	if !includePlan {
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("select %s self address from %d ordered candidates", spec.AddressFamily, len(spec.Candidates)))
}

func (e *Engine) observeDNSConditionalForwarder(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.DNSConditionalForwarderSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	source := defaultString(spec.UpstreamSource, "static")
	rr.Observed["domain"] = spec.Domain
	rr.Observed["upstreamSource"] = source
	if spec.UpstreamInterface != "" {
		rr.Observed["upstreamInterface"] = spec.UpstreamInterface
		rr.Observed["upstreamIfname"] = aliases[spec.UpstreamInterface]
	}
	if len(spec.UpstreamServers) > 0 {
		rr.Observed["upstreamServers"] = strings.Join(spec.UpstreamServers, ",")
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("forward DNS queries for %s using %s upstreams", spec.Domain, source))
	}
}

func (e *Engine) observeDSLiteTunnel(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.DSLiteTunnelSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	tunnelName := defaultString(spec.TunnelName, res.Metadata.Name)
	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = aliases[spec.Interface]
	rr.Observed["tunnelName"] = tunnelName
	if spec.AFTRFQDN != "" {
		rr.Observed["aftrFQDN"] = spec.AFTRFQDN
	}
	if spec.RemoteAddress != "" {
		rr.Observed["remoteAddress"] = spec.RemoteAddress
	}
	if spec.AFTRAddressOrdinal != 0 {
		rr.Observed["aftrAddressOrdinal"] = fmt.Sprintf("%d", spec.AFTRAddressOrdinal)
	}
	localSource := defaultString(spec.LocalAddressSource, "interface")
	rr.Observed["localAddressSource"] = localSource
	if spec.LocalAddress != "" {
		rr.Observed["localAddress"] = spec.LocalAddress
	}
	if spec.LocalDelegatedAddress != "" {
		rr.Observed["localDelegatedAddress"] = spec.LocalDelegatedAddress
	}
	if spec.LocalAddressSuffix != "" {
		rr.Observed["localAddressSuffix"] = spec.LocalAddressSuffix
	}
	if len(spec.AFTRDNSServers) > 0 {
		rr.Observed["aftrDNSServers"] = strings.Join(spec.AFTRDNSServers, ",")
	}
	rr.Observed["defaultRoute"] = fmt.Sprintf("%t", spec.DefaultRoute)
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure DS-Lite ipip6 tunnel %s on %s", tunnelName, aliases[spec.Interface]))
		if spec.AFTRFQDN != "" && spec.AFTRAddressOrdinal != 0 {
			rr.Plan = append(rr.Plan, fmt.Sprintf("select sorted AFTR AAAA record #%d", spec.AFTRAddressOrdinal))
		}
		if localSource == "delegatedAddress" {
			rr.Plan = append(rr.Plan, fmt.Sprintf("use delegated LAN IPv6 address %s%s as tunnel source", spec.LocalDelegatedAddress, spec.LocalAddressSuffix))
		}
		if spec.DefaultRoute {
			rr.Plan = append(rr.Plan, "route IPv4 default traffic through DS-Lite tunnel")
		}
	}
}

func (e *Engine) observePPPoEInterface(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.PPPoEInterfaceSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
	lowerIfName := aliases[spec.Interface]
	rr.Observed["interface"] = spec.Interface
	rr.Observed["lowerIfname"] = lowerIfName
	rr.Observed["ifname"] = ifname
	rr.Observed["username"] = spec.Username
	rr.Observed["managed"] = fmt.Sprintf("%t", spec.Managed)
	rr.Observed["defaultRoute"] = fmt.Sprintf("%t", spec.DefaultRoute)
	rr.Observed["usePeerDNS"] = fmt.Sprintf("%t", spec.UsePeerDNS)
	if spec.ServiceName != "" {
		rr.Observed["serviceName"] = spec.ServiceName
	}
	if spec.ACName != "" {
		rr.Observed["acName"] = spec.ACName
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure PPPoE interface %s over %s", ifname, lowerIfName))
		if spec.DefaultRoute {
			rr.Plan = append(rr.Plan, "install IPv4 default route from PPPoE peer")
		}
		if spec.UsePeerDNS {
			rr.Plan = append(rr.Plan, "accept DNS servers from PPPoE peer")
		}
		if spec.Managed {
			rr.Plan = append(rr.Plan, fmt.Sprintf("manage systemd unit routerd-pppoe-%s.service", res.Metadata.Name))
		}
	}
}

func (e *Engine) observeIPv4SourceNAT(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4SourceNATSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	outIfName := aliases[spec.OutboundInterface]
	policy := policies[spec.OutboundInterface]

	rr.Observed["outboundInterface"] = spec.OutboundInterface
	rr.Observed["outboundIfname"] = outIfName
	rr.Observed["sourceCIDRs"] = strings.Join(spec.SourceCIDRs, ",")
	rr.Observed["translationType"] = spec.Translation.Type
	if spec.Translation.Address != "" {
		rr.Observed["translationAddress"] = spec.Translation.Address
	}
	if len(spec.Translation.Addresses) > 0 {
		rr.Observed["translationAddresses"] = strings.Join(spec.Translation.Addresses, ",")
	}
	portMapping := defaultString(spec.Translation.PortMapping.Type, "auto")
	rr.Observed["portMapping"] = portMapping
	if portMapping == "range" {
		rr.Observed["portRange"] = fmt.Sprintf("%d-%d", spec.Translation.PortMapping.Start, spec.Translation.PortMapping.End)
	}

	if !includePlan {
		return
	}
	if !policy.Managed || policy.Owner == "external" {
		rr.Plan = append(rr.Plan, "plan NAT rule for externally managed outbound interface")
	} else if policy.RequiresAdoption {
		rr.Phase = "RequiresAdoption"
		rr.Plan = append(rr.Plan, "blocked: outbound interface requires adoption before routerd manages NAT")
		return
	}
	switch spec.Translation.Type {
	case "interfaceAddress":
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 source NAT for %s via interface address on %s", strings.Join(spec.SourceCIDRs, ","), outIfName))
	case "address":
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 source NAT for %s to %s via %s", strings.Join(spec.SourceCIDRs, ","), spec.Translation.Address, outIfName))
	case "pool":
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 source NAT for %s to pool %s via %s", strings.Join(spec.SourceCIDRs, ","), strings.Join(spec.Translation.Addresses, ","), outIfName))
	}
	switch portMapping {
	case "auto":
		rr.Plan = append(rr.Plan, "use automatic source port mapping")
	case "preserve":
		rr.Plan = append(rr.Plan, "preserve source ports when supported")
	case "range":
		rr.Plan = append(rr.Plan, fmt.Sprintf("map source ports to %d-%d", spec.Translation.PortMapping.Start, spec.Translation.PortMapping.End))
	}
}

func (e *Engine) observeIPv4PolicyRoute(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4PolicyRouteSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	outIfName := aliases[spec.OutboundInterface]
	policy := policies[spec.OutboundInterface]

	rr.Observed["outboundInterface"] = spec.OutboundInterface
	rr.Observed["outboundIfname"] = outIfName
	rr.Observed["table"] = fmt.Sprintf("%d", spec.Table)
	rr.Observed["priority"] = fmt.Sprintf("%d", spec.Priority)
	rr.Observed["mark"] = fmt.Sprintf("0x%x", spec.Mark)
	if len(spec.SourceCIDRs) > 0 {
		rr.Observed["sourceCIDRs"] = strings.Join(spec.SourceCIDRs, ",")
	}
	if len(spec.DestinationCIDRs) > 0 {
		rr.Observed["destinationCIDRs"] = strings.Join(spec.DestinationCIDRs, ",")
	}
	if spec.RouteMetric != 0 {
		rr.Observed["routeMetric"] = fmt.Sprintf("%d", spec.RouteMetric)
	}

	if !includePlan {
		return
	}
	if !policy.Managed || policy.Owner == "external" || policy.Owner == "" {
		rr.Plan = append(rr.Plan, "plan policy route for externally managed outbound interface")
	} else if policy.RequiresAdoption {
		rr.Phase = "RequiresAdoption"
		rr.Plan = append(rr.Plan, "blocked: outbound interface requires adoption before routerd manages policy routing")
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("mark matching IPv4 packets with 0x%x", spec.Mark))
	rr.Plan = append(rr.Plan, fmt.Sprintf("route fwmark 0x%x via table %d default dev %s", spec.Mark, spec.Table, outIfName))
}

func (e *Engine) observeIPv4PolicyRouteSet(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4PolicyRouteSetSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	mode := defaultString(spec.Mode, "hash")
	rr.Observed["mode"] = mode
	rr.Observed["hashFields"] = strings.Join(spec.HashFields, ",")
	if len(spec.SourceCIDRs) > 0 {
		rr.Observed["sourceCIDRs"] = strings.Join(spec.SourceCIDRs, ",")
	}
	if len(spec.DestinationCIDRs) > 0 {
		rr.Observed["destinationCIDRs"] = strings.Join(spec.DestinationCIDRs, ",")
	}
	var targets []string
	for _, target := range spec.Targets {
		outIfName := aliases[target.OutboundInterface]
		targetName := target.Name
		if targetName == "" {
			targetName = target.OutboundInterface
		}
		targets = append(targets, fmt.Sprintf("%s:%s:table=%d:mark=0x%x", targetName, outIfName, target.Table, target.Mark))
		if includePlan {
			policy := policies[target.OutboundInterface]
			if policy.RequiresAdoption {
				rr.Phase = "RequiresAdoption"
				rr.Plan = append(rr.Plan, fmt.Sprintf("blocked: outbound interface %s requires adoption before routerd manages policy routing", target.OutboundInterface))
				return
			}
		}
	}
	rr.Observed["targets"] = strings.Join(targets, ",")
	if !includePlan {
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("hash IPv4 packets by %s and select one of %d policy route targets", strings.Join(spec.HashFields, ","), len(spec.Targets)))
	rr.Plan = append(rr.Plan, "store selected mark in conntrack mark so each flow keeps the same route")
}

func (e *Engine) observeIPv4ReversePathFilter(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4ReversePathFilterSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	target := spec.Target
	targetName := target
	if target == "interface" {
		targetName = aliases[spec.Interface]
	}
	key := "net.ipv4.conf." + targetName + ".rp_filter"
	rr.Observed["target"] = target
	if spec.Interface != "" {
		rr.Observed["interface"] = spec.Interface
		rr.Observed["ifname"] = targetName
	}
	rr.Observed["mode"] = spec.Mode
	rr.Observed["key"] = key
	if current, err := e.Command("sysctl", "-n", key); err == nil {
		rr.Observed["current"] = strings.TrimSpace(string(current))
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 reverse path filtering is %s for %s", spec.Mode, targetName))
	}
}

func (e *Engine) observeIPv4DHCPServer(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4DHCPServerSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	server := spec.Server
	if server == "" {
		server = "dnsmasq"
	}

	rr.Observed["server"] = server
	rr.Observed["managed"] = fmt.Sprintf("%t", spec.Managed)
	rr.Observed["listenInterfaces"] = strings.Join(spec.ListenInterfaces, ",")
	rr.Observed["dnsEnabled"] = fmt.Sprintf("%t", spec.DNS.Enabled)
	if spec.DNS.UpstreamSource != "" {
		rr.Observed["dnsUpstreamSource"] = spec.DNS.UpstreamSource
	}
	if spec.DNS.UpstreamInterface != "" {
		rr.Observed["dnsUpstreamInterface"] = spec.DNS.UpstreamInterface
	}
	if len(spec.DNS.UpstreamServers) > 0 {
		rr.Observed["dnsUpstreamServers"] = strings.Join(spec.DNS.UpstreamServers, ",")
	}

	if _, err := exec.LookPath(server); err == nil {
		rr.Observed["serverAvailable"] = "true"
	} else {
		rr.Observed["serverAvailable"] = "false"
		if includePlan {
			rr.Warnings = append(rr.Warnings, fmt.Sprintf("%s is required to ensure DHCP server on this host", server))
		}
	}

	if !includePlan {
		return
	}
	if !spec.Managed {
		rr.Plan = append(rr.Plan, "observe only; DHCP server instance is not managed")
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 DHCP server instance %s is available", server))
	if len(spec.ListenInterfaces) > 0 {
		rr.Plan = append(rr.Plan, fmt.Sprintf("serve dnsmasq only on %s", strings.Join(spec.ListenInterfaces, ",")))
	}
	if spec.DNS.Enabled {
		upstreamSource := defaultString(spec.DNS.UpstreamSource, "system")
		switch upstreamSource {
		case "dhcp4":
			rr.Plan = append(rr.Plan, fmt.Sprintf("run dnsmasq DNS forwarder/cache using DHCPv4 DNS from %s", spec.DNS.UpstreamInterface))
		case "static":
			rr.Plan = append(rr.Plan, fmt.Sprintf("run dnsmasq DNS forwarder/cache using static upstreams %s", strings.Join(spec.DNS.UpstreamServers, ",")))
		case "system":
			rr.Plan = append(rr.Plan, "run dnsmasq DNS forwarder/cache using system resolver configuration")
		case "none":
			rr.Plan = append(rr.Plan, "run dnsmasq DNS service without upstream forwarders")
		}
	}
}

func (e *Engine) observeIPv4DHCPScope(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4DHCPScopeSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := aliases[spec.Interface]
	policy := policies[spec.Interface]
	routerSource := defaultString(spec.RouterSource, "interfaceAddress")
	dnsSource := defaultString(spec.DNSSource, "self")

	rr.Observed["server"] = spec.Server
	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = ifname
	rr.Observed["rangeStart"] = spec.RangeStart
	rr.Observed["rangeEnd"] = spec.RangeEnd
	rr.Observed["routerSource"] = routerSource
	rr.Observed["dnsSource"] = dnsSource
	if spec.LeaseTime != "" {
		rr.Observed["leaseTime"] = spec.LeaseTime
	}
	if spec.Router != "" {
		rr.Observed["router"] = spec.Router
	}
	if spec.DNSInterface != "" {
		rr.Observed["dnsInterface"] = spec.DNSInterface
	}
	if len(spec.DNSServers) > 0 {
		rr.Observed["dnsServers"] = strings.Join(spec.DNSServers, ",")
	}

	if !includePlan {
		return
	}
	if !policy.Managed || policy.Owner == "external" {
		rr.Plan = append(rr.Plan, "observe only; referenced interface is externally managed")
		return
	}
	if policy.RequiresAdoption {
		rr.Phase = "RequiresAdoption"
		rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd manages DHCP scope")
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 DHCP scope %s serves %s-%s on %s", spec.Server, spec.RangeStart, spec.RangeEnd, ifname))
	switch routerSource {
	case "interfaceAddress":
		rr.Plan = append(rr.Plan, fmt.Sprintf("advertise router option from IPv4 address on %s", ifname))
	case "static":
		rr.Plan = append(rr.Plan, fmt.Sprintf("advertise router option %s", spec.Router))
	case "none":
		rr.Plan = append(rr.Plan, "do not advertise router option")
	}
	switch dnsSource {
	case "dhcp4":
		rr.Plan = append(rr.Plan, fmt.Sprintf("advertise DNS servers learned from DHCPv4 on %s", aliases[spec.DNSInterface]))
	case "static":
		rr.Plan = append(rr.Plan, fmt.Sprintf("advertise DNS servers %s", strings.Join(spec.DNSServers, ",")))
	case "self":
		rr.Plan = append(rr.Plan, fmt.Sprintf("advertise this router as DNS server on %s", ifname))
	case "none":
		rr.Plan = append(rr.Plan, "do not advertise DNS servers")
	}
}

func (e *Engine) observeHealthCheck(res api.Resource, aliases map[string]string, kinds map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.HealthCheckSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	checkType := defaultString(spec.Type, "ping")
	role := defaultString(spec.Role, "next-hop")
	targetSource := defaultString(spec.TargetSource, "auto")
	addressFamily := spec.AddressFamily
	if addressFamily == "" {
		if targetSource == "dsliteRemote" || (targetSource == "auto" && kinds[spec.Interface] == "DSLiteTunnel") {
			addressFamily = "ipv6"
		} else {
			addressFamily = "ipv4"
		}
	}
	interval := defaultString(spec.Interval, "60s")
	timeout := defaultString(spec.Timeout, "3s")
	rr.Observed["type"] = checkType
	rr.Observed["role"] = role
	rr.Observed["addressFamily"] = addressFamily
	rr.Observed["targetSource"] = targetSource
	if spec.Target != "" {
		rr.Observed["target"] = spec.Target
	}
	rr.Observed["interval"] = interval
	rr.Observed["timeout"] = timeout
	if spec.Interface != "" {
		rr.Observed["interface"] = spec.Interface
		rr.Observed["ifname"] = aliases[spec.Interface]
	}
	if includePlan {
		target := spec.Target
		if target == "" {
			target = targetSource
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("check %s %s reachability to %s every %s", role, addressFamily, target, interval))
	}
}

func (e *Engine) observeIPv4DefaultRoutePolicy(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPv4DefaultRoutePolicySpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	mode := defaultString(spec.Mode, "priority")
	rr.Observed["mode"] = mode
	rr.Observed["candidates"] = fmt.Sprintf("%d", len(spec.Candidates))
	currentGateway, currentDev, currentProto := e.defaultIPv4Route()
	if currentGateway != "" {
		rr.Observed["currentGateway"] = currentGateway
	}
	if currentDev != "" {
		rr.Observed["currentIfname"] = currentDev
	}
	if currentProto != "" {
		rr.Observed["currentProto"] = currentProto
	}
	var candidates []string
	for _, candidate := range sortedDefaultRouteCandidates(spec.Candidates) {
		name := defaultString(candidate.Name, defaultString(candidate.RouteSet, candidate.Interface))
		if candidate.RouteSet != "" {
			candidates = append(candidates, fmt.Sprintf("%s:routeSet=%s:priority=%d", name, candidate.RouteSet, candidate.Priority))
			continue
		}
		ifname := aliases[candidate.Interface]
		candidates = append(candidates, fmt.Sprintf("%s:%s:priority=%d", name, ifname, candidate.Priority))
	}
	rr.Observed["candidateOrder"] = strings.Join(candidates, ",")
	if !includePlan {
		return
	}
	rr.Plan = append(rr.Plan, "select the first healthy IPv4 default route candidate by priority")
	for _, candidate := range sortedDefaultRouteCandidates(spec.Candidates) {
		health := "no health check"
		if candidate.HealthCheck != "" {
			health = "healthCheck=" + candidate.HealthCheck
		}
		name := defaultString(candidate.Name, defaultString(candidate.RouteSet, candidate.Interface))
		if candidate.RouteSet != "" {
			rr.Plan = append(rr.Plan, fmt.Sprintf("candidate %s priority %d via routeSet=%s %s", name, candidate.Priority, candidate.RouteSet, health))
			continue
		}
		ifname := aliases[candidate.Interface]
		source := defaultString(candidate.GatewaySource, "none")
		rr.Plan = append(rr.Plan, fmt.Sprintf("candidate %s priority %d via %s gatewaySource=%s %s", name, candidate.Priority, ifname, source, health))
	}
}

func (e *Engine) observeSysctl(res api.Resource, includePlan bool, rr *ResourceResult) {
	key := stringSpec(res, "key")
	desired := stringSpec(res, "value")
	runtime := boolSpecDefault(res, "runtime", true)
	persistent := boolSpec(res, "persistent")

	rr.Observed["key"] = key
	rr.Observed["desired"] = desired
	rr.Observed["runtime"] = fmt.Sprintf("%t", runtime)
	rr.Observed["persistent"] = fmt.Sprintf("%t", persistent)

	if out, err := e.Command("sysctl", "-n", key); err == nil {
		current := strings.TrimSpace(string(out))
		rr.Observed["current"] = current
		if current != desired {
			rr.Phase = "Drifted"
		}
	} else {
		rr.Phase = "Drifted"
		rr.Warnings = append(rr.Warnings, fmt.Sprintf("could not observe sysctl %s: %v", key, err))
	}

	if !includePlan {
		return
	}
	if runtime {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure runtime sysctl %s=%s", key, desired))
	}
	if persistent {
		rr.Plan = append(rr.Plan, fmt.Sprintf("persistent sysctl %s=%s is not implemented yet", key, desired))
		rr.Warnings = append(rr.Warnings, "persistent sysctl rendering is pending OS-specific implementation")
	}
}

func (e *Engine) observeInterface(res api.Resource, policy interfacePolicy, observedV4 []ipv4Assignment, includePlan bool, rr *ResourceResult) {
	rr.Observed["ifname"] = policy.IfName
	rr.Observed["managed"] = fmt.Sprintf("%t", policy.Managed)
	rr.Observed["owner"] = policy.Owner

	if exists, up := e.interfaceState(policy.IfName); exists {
		rr.Observed["exists"] = "true"
		rr.Observed["up"] = fmt.Sprintf("%t", up)
	} else {
		rr.Observed["exists"] = "false"
		rr.Phase = "Drifted"
	}

	if policy.OS.CloudInit {
		rr.Observed["cloudInit"] = "present"
	}
	if policy.OS.Netplan {
		rr.Observed["netplan"] = "present"
	}
	if policy.OS.Networkd {
		rr.Observed["networkd"] = "present"
	}
	if len(observedV4) > 0 {
		var prefixes []string
		for _, assignment := range observedV4 {
			prefixes = append(prefixes, assignment.Prefix.String())
		}
		rr.Observed["ipv4Prefixes"] = strings.Join(prefixes, ",")
	}

	if !includePlan {
		return
	}
	if !policy.Managed || policy.Owner == "external" {
		rr.Plan = append(rr.Plan, "observe only; interface is externally managed")
		return
	}
	if policy.RequiresAdoption {
		rr.Phase = "RequiresAdoption"
		rr.Plan = append(rr.Plan, "blocked: existing cloud-init/netplan networking detected; run an explicit adoption workflow before routerd manages this interface")
		rr.Conditions = append(rr.Conditions, Condition{
			Type:    "AdoptionRequired",
			Status:  "True",
			Reason:  "ExistingOSNetworking",
			Message: "routerd will not override cloud-init/netplan-managed networking automatically",
		})
		return
	}
	if boolSpec(res, "adminUp") {
		rr.Plan = append(rr.Plan, "ensure link is administratively up")
	}
}

func (e *Engine) observeIPv4Static(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, overlaps []addressOverlap, includePlan bool, rr *ResourceResult) {
	iface := stringSpec(res, "interface")
	ifname := aliases[iface]
	policy := policies[iface]
	addr := stringSpec(res, "address")

	rr.Observed["interface"] = iface
	rr.Observed["ifname"] = ifname
	rr.Observed["address"] = addr

	if has := e.hasAddress(ifname, addr, "-4"); has {
		rr.Observed["present"] = "true"
	} else {
		rr.Observed["present"] = "false"
		rr.Phase = "Drifted"
	}

	if includePlan {
		if len(overlaps) > 0 {
			if boolSpec(res, "allowOverlap") {
				for _, overlap := range overlaps {
					rr.Warnings = append(rr.Warnings, overlap.Message)
				}
			} else {
				rr.Phase = "Blocked"
				for _, overlap := range overlaps {
					rr.Plan = append(rr.Plan, "blocked: "+overlap.Message)
				}
				rr.Conditions = append(rr.Conditions, Condition{
					Type:    "AddressOverlap",
					Status:  "True",
					Reason:  "OverlappingIPv4Prefix",
					Message: "IPv4 overlap is blocked by default; set allowOverlap with a reason only for intentional NAT/HA cases",
				})
				return
			}
		}
		if !policy.Managed || policy.Owner == "external" {
			rr.Plan = append(rr.Plan, "observe only; referenced interface is externally managed")
			return
		}
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd manages addresses")
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 address %s on %s", addr, ifname))
		if boolSpec(res, "exclusive") {
			rr.Plan = append(rr.Plan, fmt.Sprintf("remove other IPv4 addresses on %s", ifname))
		}
	}
}

func (e *Engine) observeDHCP(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, family string, includePlan bool, rr *ResourceResult) {
	iface := stringSpec(res, "interface")
	ifname := aliases[iface]
	policy := policies[iface]
	client := stringSpecDefault(res, "client", "dhcpcd")

	rr.Observed["interface"] = iface
	rr.Observed["ifname"] = ifname
	rr.Observed["family"] = family
	rr.Observed["client"] = client

	if dhcpClientAvailable(client) {
		rr.Observed["clientAvailable"] = "true"
	} else {
		rr.Observed["clientAvailable"] = "false"
		if includePlan {
			rr.Warnings = append(rr.Warnings, fmt.Sprintf("%s is required to ensure DHCP on this host", client))
		}
	}
	if includePlan {
		if !policy.Managed || policy.Owner == "external" {
			rr.Plan = append(rr.Plan, "observe only; referenced interface is externally managed")
			return
		}
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd manages DHCP")
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s DHCP client %s is running for %s", family, client, ifname))
	}
}

func dhcpClientAvailable(client string) bool {
	if client == "networkd" {
		_, err := exec.LookPath("networkctl")
		return err == nil
	}
	_, err := exec.LookPath(client)
	return err == nil
}

func (e *Engine) observeHostname(res api.Resource, osNet osNetworking, includePlan bool, rr *ResourceResult) {
	desired := stringSpec(res, "hostname")
	rr.Observed["desired"] = desired
	if out, err := e.Command("hostname"); err == nil {
		current := strings.TrimSpace(string(out))
		rr.Observed["current"] = current
		if current != desired {
			rr.Phase = "Drifted"
		}
	}
	if includePlan {
		if osNet.CloudInit {
			rr.Warnings = append(rr.Warnings, "cloud-init is present and may reset hostname unless configured not to manage it")
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure hostname is %s", desired))
	}
}

type osNetworking struct {
	CloudInit bool
	Netplan   bool
	Networkd  bool
}

func (e *Engine) detectOSNetworking() osNetworking {
	if e.OSNetworking != nil {
		return *e.OSNetworking
	}
	var osNet osNetworking
	if _, err := os.Stat("/etc/cloud/cloud.cfg"); err == nil {
		osNet.CloudInit = true
	}
	if entries, err := os.ReadDir("/etc/netplan"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
				osNet.Netplan = true
				break
			}
		}
	}
	if _, err := e.Command("networkctl", "list", "--no-pager"); err == nil {
		osNet.Networkd = true
	}
	return osNet
}

func (e *Engine) interfaceState(ifname string) (bool, bool) {
	out, err := e.Command("ip", "-brief", "link", "show", "dev", ifname)
	if err != nil {
		return false, false
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return true, false
	}
	return true, fields[1] == "UP" || strings.Contains(fields[1], "UP")
}

func (e *Engine) hasAddress(ifname, address, family string) bool {
	out, err := e.Command("ip", "-brief", family, "addr", "show", "dev", ifname)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), address)
}

func (e *Engine) defaultIPv4Route() (gateway, dev, proto string) {
	out, err := e.Command("ip", "-4", "route", "show", "default")
	if err != nil {
		return "", "", ""
	}
	fields := strings.Fields(string(out))
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "via":
			if i+1 < len(fields) {
				gateway = fields[i+1]
			}
		case "dev":
			if i+1 < len(fields) {
				dev = fields[i+1]
			}
		case "proto":
			if i+1 < len(fields) {
				proto = fields[i+1]
			}
		}
	}
	return gateway, dev, proto
}

func (e *Engine) observedIPv4Prefixes(policies map[string]interfacePolicy) []ipv4Assignment {
	var assignments []ipv4Assignment
	for name, policy := range policies {
		out, err := e.Command("ip", "-brief", "-4", "addr", "show", "dev", policy.IfName)
		if err != nil {
			continue
		}
		for _, prefix := range parseIPv4Prefixes(string(out)) {
			assignments = append(assignments, ipv4Assignment{
				ResourceID: "observed/" + name + "/" + prefix.String(),
				Interface:  name,
				IfName:     policy.IfName,
				Prefix:     prefix,
				Source:     "observed",
			})
		}
	}
	return assignments
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			aliases[res.Metadata.Name] = stringSpec(res, "ifname")
		case "PPPoEInterface":
			aliases[res.Metadata.Name] = defaultString(stringSpec(res, "ifname"), "ppp-"+res.Metadata.Name)
		case "DSLiteTunnel":
			aliases[res.Metadata.Name] = defaultString(stringSpec(res, "tunnelName"), res.Metadata.Name)
		}
	}
	return aliases
}

func resourceKinds(router *api.Router) map[string]string {
	kinds := map[string]string{}
	for _, res := range router.Spec.Resources {
		kinds[res.Metadata.Name] = res.Kind
	}
	return kinds
}

type ipv4Assignment struct {
	ResourceID         string
	Interface          string
	IfName             string
	Prefix             netip.Prefix
	Source             string
	AllowOverlap       bool
	AllowOverlapReason string
}

type addressOverlap struct {
	Other   ipv4Assignment
	Message string
}

func desiredIPv4Prefixes(router *api.Router, aliases map[string]string) []ipv4Assignment {
	var assignments []ipv4Assignment
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4StaticAddress" {
			continue
		}
		prefix, err := netip.ParsePrefix(stringSpec(res, "address"))
		if err != nil {
			continue
		}
		iface := stringSpec(res, "interface")
		assignments = append(assignments, ipv4Assignment{
			ResourceID:         res.ID(),
			Interface:          iface,
			IfName:             aliases[iface],
			Prefix:             prefix.Masked(),
			Source:             "desired",
			AllowOverlap:       boolSpec(res, "allowOverlap"),
			AllowOverlapReason: stringSpec(res, "allowOverlapReason"),
		})
	}
	return assignments
}

func ipv4Overlaps(desired, observed []ipv4Assignment) map[string][]addressOverlap {
	result := map[string][]addressOverlap{}
	all := append([]ipv4Assignment{}, observed...)
	all = append(all, desired...)

	for _, current := range desired {
		for _, other := range all {
			if current.ResourceID == other.ResourceID {
				continue
			}
			if current.Interface == other.Interface {
				continue
			}
			if !prefixesOverlap(current.Prefix, other.Prefix) {
				continue
			}
			result[current.ResourceID] = append(result[current.ResourceID], addressOverlap{
				Other: other,
				Message: fmt.Sprintf(
					"IPv4 prefix %s on %s overlaps with %s prefix %s on %s",
					current.Prefix,
					current.IfName,
					other.Source,
					other.Prefix,
					other.IfName,
				),
			})
		}
	}
	return result
}

func ipv4AssignmentsByInterface(assignments []ipv4Assignment) map[string][]ipv4Assignment {
	result := map[string][]ipv4Assignment{}
	for _, assignment := range assignments {
		result[assignment.Interface] = append(result[assignment.Interface], assignment)
	}
	return result
}

func prefixesOverlap(a, b netip.Prefix) bool {
	a = a.Masked()
	b = b.Masked()
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func parseIPv4Prefixes(output string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, field := range strings.Fields(output) {
		if !strings.Contains(field, "/") {
			continue
		}
		prefix, err := netip.ParsePrefix(field)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

type interfacePolicy struct {
	Name             string
	IfName           string
	Managed          bool
	Owner            string
	RequiresAdoption bool
	OS               osNetworking
}

func interfacePolicies(router *api.Router, osNet osNetworking) map[string]interfacePolicy {
	policies := map[string]interfacePolicy{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		managed := boolSpec(res, "managed")
		owner := stringSpecDefault(res, "owner", ownerFromManaged(managed))
		policies[res.Metadata.Name] = interfacePolicy{
			Name:             res.Metadata.Name,
			IfName:           stringSpec(res, "ifname"),
			Managed:          managed,
			Owner:            owner,
			RequiresAdoption: managed && owner != "external" && osNet.CloudInit && !osNet.Netplan,
			OS:               osNet,
		}
	}
	return policies
}

func runCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return out, err
	}
	return out, nil
}

func ownerFromManaged(managed bool) string {
	if managed {
		return "routerd"
	}
	return "external"
}

func sortedDefaultRouteCandidates(candidates []api.IPv4DefaultRoutePolicyCandidate) []api.IPv4DefaultRoutePolicyCandidate {
	result := append([]api.IPv4DefaultRoutePolicyCandidate{}, candidates...)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Priority < result[j].Priority
	})
	return result
}

func stringSpec(res api.Resource, key string) string {
	switch spec := res.Spec.(type) {
	case api.SysctlSpec:
		switch key {
		case "key":
			return spec.Key
		case "value":
			return spec.Value
		}
	case api.InterfaceSpec:
		switch key {
		case "ifname":
			return spec.IfName
		case "owner":
			return spec.Owner
		}
	case api.PPPoEInterfaceSpec:
		switch key {
		case "ifname":
			return spec.IfName
		case "interface":
			return spec.Interface
		}
	case api.IPv4StaticAddressSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "address":
			return spec.Address
		case "allowOverlapReason":
			return spec.AllowOverlapReason
		}
	case api.IPv4DHCPAddressSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "client":
			return spec.Client
		}
	case api.IPv4DHCPServerSpec:
		switch key {
		case "server":
			return spec.Server
		}
	case api.IPv4DHCPScopeSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "server":
			return spec.Server
		case "rangeStart":
			return spec.RangeStart
		case "rangeEnd":
			return spec.RangeEnd
		case "leaseTime":
			return spec.LeaseTime
		case "router":
			return spec.Router
		}
	case api.IPv6DHCPAddressSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "client":
			return spec.Client
		}
	case api.DSLiteTunnelSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "tunnelName":
			return spec.TunnelName
		}
	case api.HostnameSpec:
		switch key {
		case "hostname":
			return spec.Hostname
		}
	}
	return ""
}

func stringSpecDefault(res api.Resource, key, fallback string) string {
	if value := stringSpec(res, key); value != "" {
		return value
	}
	return fallback
}

func boolSpec(res api.Resource, key string) bool {
	switch spec := res.Spec.(type) {
	case api.SysctlSpec:
		switch key {
		case "runtime":
			return api.BoolDefault(spec.Runtime, false)
		case "persistent":
			return spec.Persistent
		}
	case api.InterfaceSpec:
		switch key {
		case "adminUp":
			return spec.AdminUp
		case "managed":
			return spec.Managed
		}
	case api.IPv4StaticAddressSpec:
		switch key {
		case "exclusive":
			return spec.Exclusive
		case "allowOverlap":
			return spec.AllowOverlap
		}
	case api.IPv4DHCPAddressSpec:
		if key == "required" {
			return spec.Required
		}
	case api.IPv6DHCPAddressSpec:
		if key == "required" {
			return spec.Required
		}
	case api.HostnameSpec:
		if key == "managed" {
			return spec.Managed
		}
	}
	return false
}

func boolSpecDefault(res api.Resource, key string, fallback bool) bool {
	if spec, ok := res.Spec.(api.SysctlSpec); ok && key == "runtime" {
		return api.BoolDefault(spec.Runtime, fallback)
	}
	return boolSpec(res, key)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
