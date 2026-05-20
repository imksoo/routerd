// SPDX-License-Identifier: BSD-3-Clause

package apply

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
	"routerd/pkg/healthcheck"
	"routerd/pkg/render"
	"routerd/pkg/sysctlprofile"
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
		Timestamp: time.Now().UTC(),
		Phase:     "Healthy",
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
		case "Package":
			e.observePackage(res, includePlan, &rr)
		case "NetworkAdoption":
			e.observeNetworkAdoption(res, aliases, includePlan, &rr)
		case "Sysctl":
			e.observeSysctl(res, includePlan, &rr)
		case "SysctlProfile":
			e.observeSysctlProfile(res, includePlan, &rr)
		case "NTPClient":
			e.observeNTPClient(res, aliases, includePlan, &rr)
		case "WebConsole":
			e.observeWebConsole(res, includePlan, &rr)
		case "Interface":
			e.observeInterface(res, policies[res.Metadata.Name], observedV4ByInterface[res.Metadata.Name], includePlan, &rr)
		case "PPPoESession":
			e.observePPPoESession(res, aliases, includePlan, &rr)
		case "WireGuardInterface":
			e.observeWireGuardInterface(res, includePlan, &rr)
		case "WireGuardPeer":
			e.observeWireGuardPeer(res, aliases, includePlan, &rr)
		case "TailscaleNode":
			e.observeTailscaleNode(res, includePlan, &rr)
		case "IPsecConnection":
			e.observeIPsecConnection(res, includePlan, &rr)
		case "VRF":
			e.observeVRF(res, aliases, includePlan, &rr)
		case "VXLANTunnel":
			e.observeVXLANTunnel(res, aliases, includePlan, &rr)
		case "IPv4StaticAddress":
			e.observeIPv4Static(res, aliases, policies, overlaps[res.ID()], includePlan, &rr)
		case "BFD":
			e.observeBFD(res, includePlan, &rr)
		case "DHCPv4Client":
			e.observeDHCPv4Client(res, aliases, policies, includePlan, &rr)
		case "DHCPv4Server":
			e.observeDHCPv4Server(res, includePlan, &rr)
		case "DHCPv4Reservation":
			e.observeDHCPv4Reservation(res, includePlan, &rr)
		case "DHCPv6Address":
			e.observeDHCP(res, aliases, policies, "ipv6", includePlan, &rr)
		case "IPv6RAAddress":
			e.observeIPv6RAAddress(res, aliases, policies, includePlan, &rr)
		case "DHCPv6PrefixDelegation":
			e.observeDHCPv6PrefixDelegation(res, aliases, includePlan, &rr)
		case "IPv6DelegatedAddress":
			e.observeIPv6DelegatedAddress(res, aliases, includePlan, &rr)
		case "DHCPv6Server":
			e.observeDHCPv6Server(res, includePlan, &rr)
		case "SelfAddressPolicy":
			e.observeSelfAddressPolicy(res, includePlan, &rr)
		case "DNSZone":
			e.observeDNSZone(res, includePlan, &rr)
		case "DNSResolver":
			e.observeDNSResolver(res, includePlan, &rr)
		case "DNSForwarder":
			e.observeDNSForwarder(res, includePlan, &rr)
		case "DNSUpstream":
			e.observeDNSUpstream(res, includePlan, &rr)
		case "DHCPv4Relay":
			e.observeDHCPv4Relay(res, aliases, includePlan, &rr)
		case "DSLiteTunnel":
			e.observeDSLiteTunnel(res, aliases, includePlan, &rr)
		case "HealthCheck":
			e.observeHealthCheck(router, res, aliases, kinds, includePlan, &rr)
		case "EgressRoutePolicy":
			e.observeEgressRoutePolicy(res, aliases, policies, includePlan, &rr)
		case "NAT44Rule":
			e.observeNAT44Rule(res, aliases, policies, includePlan, &rr)
		case "ClusterNetworkRoute":
			e.observeClusterNetworkRoute(res, includePlan, &rr)
		case "FirewallZone":
			e.observeFirewallZone(res, aliases, includePlan, &rr)
		case "FirewallPolicy":
			e.observeFirewallPolicy(res, includePlan, &rr)
		case "FirewallRule":
			e.observeFirewallRule(res, includePlan, &rr)
		case "Hostname":
			e.observeHostname(res, osNet, includePlan, &rr)
		}
		if includePlan {
			rr.Artifacts = artifactIntentsForResult(resourceArtifactIntents(res, aliases))
		}

		if rr.Phase == "RequiresAdoption" || rr.Phase == "Blocked" {
			result.Phase = "Blocked"
		}
		result.Resources = append(result.Resources, rr)
	}
	if includePlan {
		if intents := routerArtifactIntents(router, aliases); len(intents) > 0 {
			result.Resources = append(result.Resources, ResourceResult{
				ID:        api.RouterAPIVersion + "/Router/" + router.Metadata.Name + "/derived-host-runtime",
				Phase:     "Healthy",
				Observed:  map[string]string{"source": "resource graph"},
				Plan:      []string{"derive host packages, kernel modules, sysctls, and network adoption artifacts from declared resources"},
				Artifacts: artifactIntentsForResult(intents),
			})
		}
		result.Orphans = append(result.Orphans, e.observeManagedOrphans(router, aliases)...)
		if len(result.Orphans) > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%d orphaned managed artifacts found", len(result.Orphans)))
			if result.Phase == "Healthy" {
				result.Phase = "Drifted"
			}
		}
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
	if len(spec.FallbackServers) > 0 {
		rr.Observed["fallbackServers"] = strings.Join(spec.FallbackServers, ",")
	}
	if len(spec.ServerFrom) > 0 {
		rr.Observed["serverFrom"] = fmt.Sprintf("%d sources", len(spec.ServerFrom))
	}
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
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s uses %s NTP servers on %s", provider, source, aliases[spec.Interface]))
	} else {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s uses %s global NTP servers", provider, source))
	}
}

func (e *Engine) observeWebConsole(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.WebConsoleSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	enabled := spec.Enabled == nil || *spec.Enabled
	address := spec.ListenAddress
	if address == "" {
		address = "127.0.0.1"
	}
	port := spec.Port
	if port == 0 {
		port = 8080
	}
	basePath := spec.BasePath
	if basePath == "" {
		basePath = "/"
	}
	rr.Observed["enabled"] = fmt.Sprintf("%t", enabled)
	rr.Observed["listen"] = fmt.Sprintf("%s:%d", address, port)
	rr.Observed["basePath"] = basePath
	if !includePlan {
		return
	}
	if enabled {
		rr.Plan = append(rr.Plan, fmt.Sprintf("serve read-only web console on %s:%d%s", address, port, basePath))
	} else {
		rr.Plan = append(rr.Plan, "web console disabled")
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
	case "otlp":
		rr.Observed["telemetryRef"] = spec.OTLP.TelemetryRef
		rr.Observed["endpoint"] = spec.OTLP.Endpoint
	case "webhook":
		rr.Observed["url"] = spec.Webhook.URL
		rr.Observed["timeout"] = defaultString(spec.Webhook.Timeout, "5s")
	case "file":
		rr.Observed["name"] = defaultString(spec.File.Name, res.Metadata.Name)
	case "journald":
		rr.Observed["identifier"] = defaultString(spec.Journald.Identifier, "routerd")
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
	case "otlp":
		target := spec.OTLP.TelemetryRef
		if target == "" {
			target = spec.OTLP.Endpoint
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("send routerd log events to OTLP sink %s", target))
	case "webhook":
		rr.Plan = append(rr.Plan, fmt.Sprintf("send routerd log events to webhook %s", spec.Webhook.URL))
	case "file":
		rr.Plan = append(rr.Plan, fmt.Sprintf("write routerd log events to logical file sink %s", defaultString(spec.File.Name, res.Metadata.Name)))
	case "journald":
		rr.Plan = append(rr.Plan, "send routerd log events to journald")
	}
}

func (e *Engine) observePackage(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.PackageSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["state"] = defaultString(spec.State, "present")
	var sets []string
	var total int
	for _, set := range spec.Packages {
		manager := set.Manager
		if manager == "" {
			manager = "default"
		}
		sets = append(sets, fmt.Sprintf("%s:%s:%s", set.OS, manager, strings.Join(set.Names, ",")))
		total += len(set.Names)
	}
	rr.Observed["sets"] = strings.Join(sets, ";")
	rr.Observed["packages"] = fmt.Sprintf("%d", total)
	if includePlan {
		rr.Plan = append(rr.Plan, "ensure OS packages declared for the current host OS are installed")
		for _, set := range spec.Packages {
			manager := set.Manager
			if manager == "" {
				manager = "OS default"
			}
			rr.Plan = append(rr.Plan, fmt.Sprintf("%s via %s: %s", set.OS, manager, strings.Join(set.Names, ", ")))
		}
	}
}

func (e *Engine) observeDHCPv6PrefixDelegation(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.DHCPv6PrefixDelegationSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := aliases[spec.Interface]
	client := defaultString(spec.Client, "networkd")
	profile := defaultString(spec.Profile, api.IPv6PDProfileDefault)
	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = ifname
	rr.Observed["client"] = client
	rr.Observed["profile"] = profile
	prefixLength := api.EffectiveIPv6PDPrefixLength(profile, spec.PrefixLength)
	if prefixLength != 0 {
		rr.Observed["prefixLength"] = fmt.Sprintf("%d", prefixLength)
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("request DHCPv6 prefix delegation on %s with %s", ifname, client))
		switch profile {
		case api.IPv6PDProfileNTTNGNDirectHikariDenwa:
			rr.Plan = append(rr.Plan, "use NTT NGN direct Hikari Denwa DHCPv6-PD profile defaults")
		case api.IPv6PDProfileNTTHGWLANPD:
			rr.Plan = append(rr.Plan, "use NTT HGW LAN-side DHCPv6-PD profile defaults")
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

func (e *Engine) observeDHCPv6Server(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DHCPv6ServerSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	server := defaultString(spec.Server, "dnsmasq")
	mode := defaultString(spec.Mode, "stateless")
	rr.Observed["server"] = server
	rr.Observed["managed"] = fmt.Sprintf("%t", spec.Managed)
	rr.Observed["listenInterfaces"] = strings.Join(spec.ListenInterfaces, ",")
	rr.Observed["interface"] = spec.Interface
	rr.Observed["mode"] = mode
	if spec.DelegatedAddress != "" {
		rr.Observed["delegatedAddress"] = spec.DelegatedAddress
		rr.Observed["defaultRoute"] = fmt.Sprintf("%t", spec.DefaultRoute)
		rr.Observed["dnsSource"] = defaultString(spec.DNSSource, "self")
	}
	if spec.SelfAddressPolicy != "" {
		rr.Observed["selfAddressPolicy"] = spec.SelfAddressPolicy
	}
	if spec.LeaseTime != "" {
		rr.Observed["leaseTime"] = spec.LeaseTime
	}
	if len(spec.DNSServers) > 0 {
		rr.Observed["dnsServers"] = strings.Join(spec.DNSServers, ",")
	}
	if len(spec.SNTPServers) > 0 {
		rr.Observed["sntpServers"] = strings.Join(spec.SNTPServers, ",")
	}
	if len(spec.DomainSearch) > 0 {
		rr.Observed["domainSearch"] = strings.Join(spec.DomainSearch, ",")
	}
	if spec.AddressPool.Start != "" {
		rr.Observed["rangeStart"] = spec.AddressPool.Start
		rr.Observed["rangeEnd"] = spec.AddressPool.End
	}
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
	if spec.Interface != "" {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure dnsmasq DHCPv6 %s service on %s", mode, spec.Interface))
	}
	if spec.DelegatedAddress != "" {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv6 DHCP/RA service uses delegated address %s", spec.DelegatedAddress))
		if spec.DefaultRoute {
			rr.Plan = append(rr.Plan, "advertise IPv6 default route by router advertisement")
		}
		switch defaultString(spec.DNSSource, "self") {
		case "self":
			rr.Plan = append(rr.Plan, "advertise this router's delegated IPv6 address as DNS server")
		case "static":
			rr.Plan = append(rr.Plan, fmt.Sprintf("advertise IPv6 DNS servers %s", strings.Join(spec.DNSServers, ",")))
		case "none":
			rr.Plan = append(rr.Plan, "do not advertise IPv6 DNS servers")
		}
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

func (e *Engine) observeDNSZone(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DNSZoneSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["zone"] = spec.Zone
	rr.Observed["records"] = fmt.Sprintf("%d", len(spec.Records))
	rr.Observed["dhcpSources"] = strings.Join(spec.DHCPDerived.Sources, ",")
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("maintain DNS zone %s with %d manual records", spec.Zone, len(spec.Records)))
	}
}

func (e *Engine) observeDNSResolver(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DNSResolverSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["listeners"] = fmt.Sprintf("%d", len(spec.Listen))
	rr.Observed["sources"] = fmt.Sprintf("%d", len(spec.Sources))
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("run routerd-dns-resolver for %d listen profiles", len(spec.Listen)))
	}
}

func (e *Engine) observeDNSForwarder(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DNSForwarderSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["resolver"] = spec.Resolver
	rr.Observed["matches"] = fmt.Sprintf("%d", len(spec.Match))
	rr.Observed["upstreams"] = fmt.Sprintf("%d", len(spec.Upstreams))
	rr.Observed["zones"] = fmt.Sprintf("%d", len(spec.ZoneRefs))
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("attach DNS forwarder to %s", spec.Resolver))
	}
}

func (e *Engine) observeDNSUpstream(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DNSUpstreamSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["protocol"] = spec.Protocol
	rr.Observed["address"] = spec.Address
	rr.Observed["addressFrom"] = fmt.Sprintf("%d", len(spec.AddressFrom))
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("provide DNS %s upstream", spec.Protocol))
	}
}

func (e *Engine) observeDHCPv4Relay(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.DHCPv4RelaySpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	var ifnames []string
	for _, name := range spec.Interfaces {
		ifnames = append(ifnames, aliases[name])
	}
	rr.Observed["interfaces"] = strings.Join(spec.Interfaces, ",")
	rr.Observed["ifnames"] = strings.Join(ifnames, ",")
	rr.Observed["upstream"] = spec.Upstream
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("relay DHCP from %s to %s", strings.Join(ifnames, ","), spec.Upstream))
	}
}

func (e *Engine) observeBFD(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.BFDSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["peer"] = spec.Peer
	rr.Observed["profile"] = defaultString(spec.Profile, "normal")
	if spec.Interface != "" {
		rr.Observed["interface"] = spec.Interface
	}
	if includePlan {
		rr.Plan = append(rr.Plan, "render FRR BFD session")
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

func (e *Engine) observePPPoESession(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.PPPoESessionSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	lowerIfName := aliases[spec.Interface]
	ifname := defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
	auth := defaultString(spec.AuthMethod, "chap")
	rr.Observed["interface"] = spec.Interface
	rr.Observed["lowerIfname"] = lowerIfName
	rr.Observed["ifname"] = ifname
	rr.Observed["client"] = "routerd-pppoe-client"
	rr.Observed["authMethod"] = auth
	rr.Observed["username"] = spec.Username
	rr.Observed["disabled"] = fmt.Sprintf("%t", spec.Disabled)
	rr.Observed["defaultRoute"] = fmt.Sprintf("%t", spec.DefaultRoute)
	rr.Observed["usePeerDNS"] = fmt.Sprintf("%t", spec.UsePeerDNS)
	if spec.Disabled {
		rr.Phase = "Disabled"
	}
	if spec.ServiceName != "" {
		rr.Observed["serviceName"] = spec.ServiceName
	}
	if spec.ACName != "" {
		rr.Observed["acName"] = spec.ACName
	}
	if includePlan {
		if spec.Disabled {
			rr.Plan = append(rr.Plan, fmt.Sprintf("keep routerd-pppoe-client for %s disabled", lowerIfName))
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("run routerd-pppoe-client for %s and observe PPPoE IPCP status from daemon", lowerIfName))
	}
}

func (e *Engine) observeWireGuardInterface(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.WireGuardInterfaceSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["ifname"] = res.Metadata.Name
	if spec.ListenPort != 0 {
		rr.Observed["listenPort"] = fmt.Sprintf("%d", spec.ListenPort)
	}
	if spec.MTU != 0 {
		rr.Observed["mtu"] = fmt.Sprintf("%d", spec.MTU)
	}
	if spec.FwMark != 0 {
		rr.Observed["fwmark"] = fmt.Sprintf("%d", spec.FwMark)
	}
	if spec.Table != 0 {
		rr.Observed["table"] = fmt.Sprintf("%d", spec.Table)
	}
	if out, err := e.Command("wg", "show", res.Metadata.Name, "dump"); err == nil && len(bytes.TrimSpace(out)) > 0 {
		rr.Observed["wgAvailable"] = "true"
	} else if includePlan {
		rr.Warnings = append(rr.Warnings, "wg is required to observe WireGuard runtime status")
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure WireGuard interface %s", res.Metadata.Name))
	}
}

func (e *Engine) observeWireGuardPeer(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.WireGuardPeerSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = aliases[spec.Interface]
	rr.Observed["publicKey"] = spec.PublicKey
	rr.Observed["allowedIPs"] = strings.Join(spec.AllowedIPs, ",")
	if spec.Endpoint != "" {
		rr.Observed["endpoint"] = spec.Endpoint
	}
	if spec.PersistentKeepalive != 0 {
		rr.Observed["persistentKeepalive"] = fmt.Sprintf("%d", spec.PersistentKeepalive)
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure WireGuard peer %s on %s", res.Metadata.Name, aliases[spec.Interface]))
	}
}

func (e *Engine) observeIPsecConnection(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.IPsecConnectionSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["localAddress"] = spec.LocalAddress
	rr.Observed["remoteAddress"] = spec.RemoteAddress
	rr.Observed["leftSubnet"] = spec.LeftSubnet
	rr.Observed["rightSubnet"] = spec.RightSubnet
	if spec.CloudProviderHint != "" {
		rr.Observed["cloudProviderHint"] = spec.CloudProviderHint
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("render swanctl connection %s for cloud VPN peer %s", res.Metadata.Name, spec.RemoteAddress))
	}
}

func (e *Engine) observeTailscaleNode(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.TailscaleNodeSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	unitName := render.TailscaleUnitName(res.Metadata.Name)
	rr.Observed["unitName"] = unitName
	rr.Observed["state"] = defaultString(spec.State, "present")
	if spec.Hostname != "" {
		rr.Observed["hostname"] = spec.Hostname
	}
	rr.Observed["advertiseExitNode"] = fmt.Sprintf("%t", spec.AdvertiseExitNode)
	rr.Observed["advertiseRoutes"] = strings.Join(spec.AdvertiseRoutes, ",")
	if defaultString(spec.State, "present") == "absent" {
		if includePlan {
			rr.Plan = append(rr.Plan, "remove Tailscale systemd unit "+unitName)
		}
		return
	}
	if out, err := e.Command("tailscale", "status", "--json"); err == nil && len(bytes.TrimSpace(out)) > 0 {
		rr.Observed["tailscaleStatus"] = "available"
	} else if includePlan {
		rr.Warnings = append(rr.Warnings, "tailscale is required to observe Tailscale runtime status")
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure Tailscale node %s via %s", res.Metadata.Name, unitName))
		if spec.AdvertiseExitNode {
			rr.Plan = append(rr.Plan, "advertise Tailscale exit node capability")
		}
		if len(spec.AdvertiseRoutes) > 0 {
			rr.Plan = append(rr.Plan, "advertise Tailscale subnet routes "+strings.Join(spec.AdvertiseRoutes, ","))
		}
	}
}

func (e *Engine) observeVRF(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.VRFSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := defaultString(spec.IfName, res.Metadata.Name)
	rr.Observed["ifname"] = ifname
	rr.Observed["routeTable"] = fmt.Sprintf("%d", spec.RouteTable)
	rr.Observed["members"] = strings.Join(spec.Members, ",")
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure Linux VRF %s with route table %d", ifname, spec.RouteTable))
		for _, member := range spec.Members {
			rr.Plan = append(rr.Plan, fmt.Sprintf("enslave %s to VRF %s", aliases[member], ifname))
		}
	}
}

func (e *Engine) observeVXLANTunnel(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.VXLANTunnelSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := defaultString(spec.IfName, res.Metadata.Name)
	rr.Observed["ifname"] = ifname
	rr.Observed["vni"] = fmt.Sprintf("%d", spec.VNI)
	rr.Observed["localAddress"] = spec.LocalAddress
	rr.Observed["underlayInterface"] = spec.UnderlayInterface
	rr.Observed["underlayIfname"] = aliases[spec.UnderlayInterface]
	rr.Observed["peers"] = strings.Join(spec.Peers, ",")
	if spec.Bridge != "" {
		rr.Observed["bridge"] = spec.Bridge
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure VXLAN tunnel %s vni %d over %s", ifname, spec.VNI, aliases[spec.UnderlayInterface]))
	}
}

func (e *Engine) observeNAT44SourceNATFields(res api.Resource, spec api.NAT44RuleSpec, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
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

func (e *Engine) observeNAT44Rule(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.NAT44RuleSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	if spec.OutboundInterface != "" || len(spec.SourceCIDRs) > 0 || spec.Translation.Type != "" {
		e.observeNAT44SourceNATFields(res, spec, aliases, policies, includePlan, rr)
		return
	}
	if spec.EgressInterface != "" {
		rr.Observed["egressInterface"] = spec.EgressInterface
		if ifname := aliases[spec.EgressInterface]; ifname != "" {
			rr.Observed["egressIfname"] = ifname
		}
	}
	if spec.EgressPolicyRef != "" {
		rr.Observed["egressPolicyRef"] = spec.EgressPolicyRef
	}
	rr.Observed["type"] = spec.Type
	rr.Observed["sourceRanges"] = strings.Join(spec.SourceRanges, ",")
	if spec.SNATAddress != "" {
		rr.Observed["snatAddress"] = spec.SNATAddress
	}
	if spec.SNATAddressFrom.Resource != "" {
		rr.Observed["snatAddressFrom"] = fmt.Sprintf("%s.%s", spec.SNATAddressFrom.Resource, defaultString(spec.SNATAddressFrom.Field, "address"))
	}
	if !includePlan {
		return
	}
	if spec.EgressInterface != "" {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure NAT44 %s for %s via %s", spec.Type, strings.Join(spec.SourceRanges, ","), defaultString(aliases[spec.EgressInterface], spec.EgressInterface)))
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("ensure NAT44 %s for %s via selected device from EgressRoutePolicy/%s", spec.Type, strings.Join(spec.SourceRanges, ","), spec.EgressPolicyRef))
}

func (e *Engine) observeEgressRoutePolicy(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.EgressRoutePolicySpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	mode := defaultString(spec.Mode, "selection")
	rr.Observed["mode"] = mode
	rr.Observed["family"] = defaultString(spec.Family, "ipv4")
	rr.Observed["candidates"] = fmt.Sprintf("%d", len(spec.Candidates))
	if len(spec.SourceCIDRs) > 0 {
		rr.Observed["sourceCIDRs"] = strings.Join(spec.SourceCIDRs, ",")
	}
	if len(spec.DestinationCIDRs) > 0 {
		rr.Observed["destinationCIDRs"] = strings.Join(spec.DestinationCIDRs, ",")
	}
	if len(spec.HashFields) > 0 {
		rr.Observed["hashFields"] = strings.Join(spec.HashFields, ",")
	}
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
	for _, candidate := range sortedEgressRouteCandidates(spec.Candidates) {
		name := defaultString(candidate.Name, candidate.EffectiveInterface())
		if len(candidate.Targets) > 0 {
			candidates = append(candidates, fmt.Sprintf("%s:targets=%d:priority=%d", name, len(candidate.Targets), candidate.Priority))
			continue
		}
		ifname := aliases[candidate.EffectiveInterface()]
		candidates = append(candidates, fmt.Sprintf("%s:%s:priority=%d:mark=0x%x", name, ifname, candidate.Priority, candidate.Mark))
	}
	rr.Observed["candidateOrder"] = strings.Join(candidates, ",")
	if !includePlan {
		return
	}
	switch mode {
	case "priority":
		rr.Plan = append(rr.Plan, "select the first healthy IPv4 default route candidate by priority")
	case "hash":
		rr.Plan = append(rr.Plan, fmt.Sprintf("hash IPv4 packets by %s and select one of the target route tables", strings.Join(spec.HashFields, ",")))
		rr.Plan = append(rr.Plan, "store selected mark in conntrack mark so each flow keeps the same route")
	case "mark":
		rr.Plan = append(rr.Plan, "mark matching IPv4 packets and route them through configured route tables")
	default:
		rr.Plan = append(rr.Plan, "select egress route candidate and publish status for dependent resources")
	}
	for _, candidate := range sortedEgressRouteCandidates(spec.Candidates) {
		if len(candidate.Targets) > 0 {
			for _, target := range candidate.Targets {
				policy := policies[target.EffectiveInterface()]
				if policy.RequiresAdoption {
					rr.Phase = "RequiresAdoption"
					rr.Plan = append(rr.Plan, fmt.Sprintf("blocked: outbound interface %s requires adoption before routerd manages policy routing", target.EffectiveInterface()))
					return
				}
			}
			continue
		}
		if candidate.Mark == 0 {
			continue
		}
		policy := policies[candidate.EffectiveInterface()]
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, fmt.Sprintf("blocked: outbound interface %s requires adoption before routerd manages policy routing", candidate.EffectiveInterface()))
			return
		}
		if mode == "priority" {
			rr.Plan = append(rr.Plan, fmt.Sprintf("candidate %s priority %d via %s gatewaySource=%s", defaultString(candidate.Name, candidate.EffectiveInterface()), candidate.Priority, aliases[candidate.EffectiveInterface()], defaultString(candidate.GatewaySource, "none")))
			continue
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("candidate %s marks packets with 0x%x and routes via table %d", defaultString(candidate.Name, candidate.EffectiveInterface()), candidate.Mark, candidate.EffectiveTable()))
	}
}

func (e *Engine) observeClusterNetworkRoute(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.ClusterNetworkRouteSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["podCIDRs"] = strings.Join(spec.Pods.CIDRs, ",")
	rr.Observed["serviceCIDRs"] = strings.Join(spec.Services.CIDRs, ",")
	rr.Observed["nextHops"] = fmt.Sprintf("%d", len(spec.Via))
	if !includePlan {
		return
	}
	expanded := api.ExpandClusterNetworkRoutes(&api.Router{Spec: api.RouterSpec{Resources: []api.Resource{res}}})
	rr.Plan = append(rr.Plan, fmt.Sprintf("expand into %d IPv4StaticRoute resources", len(expanded.Spec.Resources)-1))
}

func (e *Engine) observeFirewallZone(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.FirewallZoneSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	var ifnames []string
	for _, name := range spec.Interfaces {
		ref := name
		if _, refName, ok := strings.Cut(name, "/"); ok {
			ref = refName
		}
		ifnames = append(ifnames, aliases[ref])
	}
	rr.Observed["role"] = spec.Role
	rr.Observed["interfaces"] = strings.Join(spec.Interfaces, ",")
	rr.Observed["ifnames"] = strings.Join(ifnames, ",")
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("define firewall zone %s role=%s for %s", res.Metadata.Name, spec.Role, strings.Join(ifnames, ",")))
	}
}

func (e *Engine) observeFirewallPolicy(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.FirewallPolicySpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["logDeny"] = fmt.Sprintf("%t", spec.LogDeny)
	rr.Observed["sameRoleAccept"] = fmt.Sprintf("%t", spec.SameRoleAccept)
	if includePlan {
		rr.Plan = append(rr.Plan, "ensure stateful firewall policy matrix for untrust/trust/mgmt roles")
		rr.Plan = append(rr.Plan, "allow established/related traffic, loopback, and required ICMPv6")
	}
}

func (e *Engine) observeFirewallRule(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.FirewallRuleSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["fromZone"] = spec.FromZone
	rr.Observed["toZone"] = spec.ToZone
	rr.Observed["protocol"] = spec.Protocol
	if spec.Port != 0 {
		rr.Observed["port"] = fmt.Sprintf("%d", spec.Port)
	}
	rr.Observed["action"] = spec.Action
	rr.Observed["srcCIDRs"] = strings.Join(spec.SourceCIDRs, ",")
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("%s %s/%d from %s to %s", spec.Action, defaultString(spec.Protocol, "any"), spec.Port, spec.FromZone, spec.ToZone))
	}
}

func (e *Engine) observeDHCPv4Server(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DHCPv4ServerSpec()
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
	if spec.Interface != "" {
		rr.Observed["interface"] = spec.Interface
		rr.Observed["rangeStart"] = defaultString(spec.AddressPool.Start, spec.RangeStart)
		rr.Observed["rangeEnd"] = defaultString(spec.AddressPool.End, spec.RangeEnd)
	}
	if spec.LeaseTime != "" || spec.AddressPool.LeaseTime != "" {
		rr.Observed["leaseTime"] = defaultString(spec.AddressPool.LeaseTime, spec.LeaseTime)
	}
	if spec.RouterSource != "" {
		rr.Observed["routerSource"] = spec.RouterSource
	}
	if spec.Router != "" {
		rr.Observed["router"] = spec.Router
	}
	if spec.DNSSource != "" {
		rr.Observed["dnsSource"] = spec.DNSSource
	}
	if spec.DNSInterface != "" {
		rr.Observed["dnsInterface"] = spec.DNSInterface
	}
	if len(spec.DNSServers) > 0 {
		rr.Observed["dnsServers"] = strings.Join(spec.DNSServers, ",")
	}
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
	if spec.Interface != "" {
		rr.Plan = append(rr.Plan, fmt.Sprintf("serve IPv4 DHCP pool %s-%s on %s", defaultString(spec.AddressPool.Start, spec.RangeStart), defaultString(spec.AddressPool.End, spec.RangeEnd), spec.Interface))
	}
	if spec.RangeStart != "" || spec.RangeEnd != "" || spec.RouterSource != "" || spec.Router != "" || spec.DNSSource != "" || spec.DNSInterface != "" {
		switch defaultString(spec.RouterSource, "interfaceAddress") {
		case "interfaceAddress":
			rr.Plan = append(rr.Plan, fmt.Sprintf("advertise router option from IPv4 address on %s", spec.Interface))
		case "static":
			rr.Plan = append(rr.Plan, fmt.Sprintf("advertise router option %s", spec.Router))
		case "none":
			rr.Plan = append(rr.Plan, "do not advertise router option")
		}
		switch defaultString(spec.DNSSource, "self") {
		case "dhcpv4":
			rr.Plan = append(rr.Plan, fmt.Sprintf("advertise DNS servers learned from DHCPv4 on %s", spec.DNSInterface))
		case "static":
			rr.Plan = append(rr.Plan, fmt.Sprintf("advertise DNS servers %s", strings.Join(spec.DNSServers, ",")))
		case "self":
			rr.Plan = append(rr.Plan, fmt.Sprintf("advertise this router as DNS server on %s", spec.Interface))
		case "none":
			rr.Plan = append(rr.Plan, "do not advertise DNS servers")
		}
	}
	if spec.DNS.Enabled {
		upstreamSource := defaultString(spec.DNS.UpstreamSource, "system")
		switch upstreamSource {
		case "dhcpv4":
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

func (e *Engine) observeDHCPv4Reservation(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.DHCPv4ReservationSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["server"] = spec.Server
	rr.Observed["macAddress"] = spec.MACAddress
	rr.Observed["ipAddress"] = spec.IPAddress
	if spec.Hostname != "" {
		rr.Observed["hostname"] = spec.Hostname
	}
	if includePlan {
		rr.Plan = append(rr.Plan, fmt.Sprintf("reserve %s for %s", spec.IPAddress, spec.MACAddress))
	}
}

func (e *Engine) observeHealthCheck(router *api.Router, res api.Resource, aliases map[string]string, kinds map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.HealthCheckSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	spec = healthcheck.ResolveSpecForResource(router, res.Metadata.Name, spec)
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
	rr.Observed["disabled"] = fmt.Sprintf("%t", spec.Disabled)
	if spec.Disabled {
		rr.Phase = "Disabled"
	}
	rr.Observed["type"] = checkType
	rr.Observed["role"] = role
	rr.Observed["addressFamily"] = addressFamily
	rr.Observed["targetSource"] = targetSource
	if spec.Target != "" {
		rr.Observed["target"] = spec.Target
	}
	if spec.Protocol != "" {
		rr.Observed["protocol"] = spec.Protocol
	}
	if spec.Port != 0 {
		rr.Observed["port"] = fmt.Sprintf("%d", spec.Port)
	}
	if spec.FwMark != 0 {
		rr.Observed["fwmark"] = fmt.Sprintf("0x%x", spec.FwMark)
	}
	rr.Observed["interval"] = interval
	rr.Observed["timeout"] = timeout
	if spec.Interface != "" {
		rr.Observed["interface"] = spec.Interface
		rr.Observed["ifname"] = aliases[spec.Interface]
	}
	if spec.SourceInterface != "" {
		rr.Observed["sourceInterface"] = spec.SourceInterface
		if ifname := aliases[spec.SourceInterface]; ifname != "" {
			rr.Observed["sourceIfname"] = ifname
		}
	}
	if spec.SourceAddress != "" {
		rr.Observed["sourceAddress"] = spec.SourceAddress
	}
	if includePlan {
		if spec.Disabled {
			rr.Plan = append(rr.Plan, fmt.Sprintf("disable health check %s", res.Metadata.Name))
			return
		}
		target := spec.Target
		if target == "" {
			target = targetSource
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("check %s %s reachability to %s every %s", role, addressFamily, target, interval))
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
		current := normalizeSysctlValue(string(out))
		rr.Observed["current"] = current
		if current != normalizeSysctlValue(desired) {
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

func (e *Engine) observeSysctlProfile(res api.Resource, includePlan bool, rr *ResourceResult) {
	spec, err := res.SysctlProfileSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	rr.Observed["profile"] = spec.Profile
	rr.Observed["entries"] = fmt.Sprintf("%d", len(entries))
	rr.Observed["runtime"] = fmt.Sprintf("%t", api.BoolDefault(spec.Runtime, true))
	rr.Observed["persistent"] = fmt.Sprintf("%t", spec.Persistent)
	for _, entry := range entries {
		if out, err := e.Command("sysctl", "-n", entry.Key); err == nil {
			current := normalizeSysctlValue(string(out))
			rr.Observed[entry.Key] = current
			if current != normalizeSysctlValue(entry.Value) && !entry.Optional {
				rr.Phase = "Drifted"
			}
		} else if !entry.Optional {
			rr.Phase = "Drifted"
			rr.Warnings = append(rr.Warnings, fmt.Sprintf("could not observe sysctl %s: %v", entry.Key, err))
		}
	}
	if !includePlan {
		return
	}
	rr.Plan = append(rr.Plan, fmt.Sprintf("ensure sysctl profile %s with %d entries", spec.Profile, len(entries)))
	for _, entry := range entries {
		note := ""
		if entry.Optional {
			note = " (optional)"
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s=%s%s", entry.Key, entry.Value, note))
	}
}

func normalizeSysctlValue(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func (e *Engine) observeNetworkAdoption(res api.Resource, aliases map[string]string, includePlan bool, rr *ResourceResult) {
	spec, err := res.NetworkAdoptionSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := spec.IfName
	if ifname == "" {
		ifname = aliases[spec.Interface]
	}
	rr.Observed["ifname"] = ifname
	rr.Observed["state"] = defaultString(spec.State, "present")
	if spec.SystemdNetworkd.DisableDHCPv4 || spec.SystemdNetworkd.DisableDHCPv6 || spec.SystemdNetworkd.DisableIPv6RA {
		rr.Observed["systemdNetworkd"] = "managed"
	}
	if spec.SystemdResolved.DisableDNSStubListener {
		rr.Observed["systemdResolved"] = "managed"
	}
	if includePlan {
		rr.Plan = append(rr.Plan, "ensure systemd-networkd/resolved adoption drop-ins")
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

func (e *Engine) observeDHCPv4Client(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	spec, err := res.DHCPv4ClientSpec()
	if err != nil {
		rr.Phase = "Blocked"
		rr.Warnings = append(rr.Warnings, err.Error())
		return
	}
	ifname := aliases[spec.Interface]
	policy := policies[spec.Interface]

	rr.Observed["interface"] = spec.Interface
	rr.Observed["ifname"] = ifname
	rr.Observed["client"] = "routerd-dhcpv4-client"
	if spec.Hostname != "" {
		rr.Observed["hostname"] = spec.Hostname
	}
	if spec.RequestedAddress != "" {
		rr.Observed["requestedAddress"] = spec.RequestedAddress
	}
	if spec.ClassID != "" {
		rr.Observed["classID"] = spec.ClassID
	}
	if spec.ClientID != "" {
		rr.Observed["clientID"] = spec.ClientID
	}

	if includePlan {
		if !policy.Managed || policy.Owner == "external" {
			rr.Plan = append(rr.Plan, "observe daemon lease only; referenced interface is externally managed")
			return
		}
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd applies DHCPv4 lease")
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("run routerd-dhcpv4-client for %s and apply DHCPv4 address/default route from daemon status", ifname))
	}
}

func (e *Engine) observeIPv6RAAddress(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, includePlan bool, rr *ResourceResult) {
	iface := stringSpec(res, "interface")
	ifname := aliases[iface]
	policy := policies[iface]
	managed := boolSpecDefault(res, "managed", true)

	rr.Observed["interface"] = iface
	rr.Observed["ifname"] = ifname
	rr.Observed["managed"] = fmt.Sprintf("%t", managed)
	rr.Observed["family"] = "ipv6"
	rr.Observed["source"] = "routerAdvertisement"

	if includePlan {
		if !managed {
			rr.Plan = append(rr.Plan, "observe IPv6 RA/SLAAC address and default route")
			return
		}
		if !policy.Managed || policy.Owner == "external" {
			rr.Plan = append(rr.Plan, "configure IPv6 RA/SLAAC through host network renderer for externally managed interface")
			return
		}
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd manages IPv6 RA/SLAAC")
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv6 RA/SLAAC is accepted on %s", ifname))
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
		out, err = e.Command("ifconfig", ifname)
		if err != nil {
			return false, false
		}
		return true, ifconfigStatusActive(string(out))
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
		out, err = e.Command("ifconfig", ifname)
		if err != nil {
			return false
		}
	}
	if strings.Contains(string(out), address) {
		return true
	}
	if addr, _, ok := strings.Cut(address, "/"); ok {
		return strings.Contains(string(out), addr)
	}
	return false
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
			out, err = e.Command("ifconfig", policy.IfName)
			if err != nil {
				continue
			}
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

func ifconfigStatusActive(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "status:" {
			return fields[1] == "active"
		}
	}
	firstLine, _, _ := strings.Cut(output, "\n")
	return strings.Contains(firstLine, "<") && strings.Contains(firstLine, "UP")
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			aliases[res.Metadata.Name] = stringSpec(res, "ifname")
		case "Bridge":
			aliases[res.Metadata.Name] = stringSpecDefault(res, "ifname", res.Metadata.Name)
		case "VXLANSegment":
			aliases[res.Metadata.Name] = stringSpecDefault(res, "ifname", res.Metadata.Name)
		case "WireGuardInterface":
			aliases[res.Metadata.Name] = res.Metadata.Name
		case "VRF":
			aliases[res.Metadata.Name] = stringSpecDefault(res, "ifname", res.Metadata.Name)
		case "VXLANTunnel":
			aliases[res.Metadata.Name] = stringSpecDefault(res, "ifname", res.Metadata.Name)
		case "PPPoESession":
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
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet" {
			continue
		}
		prefixLen := ""
		for i, field := range fields {
			switch {
			case field == "netmask" && i+1 < len(fields):
				prefixLen = freeBSDIPv4MaskPrefix(fields[i+1])
			case strings.HasPrefix(field, "netmask ") && len(field) > len("netmask "):
				prefixLen = freeBSDIPv4MaskPrefix(strings.TrimPrefix(field, "netmask "))
			}
		}
		if prefixLen == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[1] + "/" + prefixLen)
		if err == nil && prefix.Addr().Is4() {
			prefixes = append(prefixes, prefix.Masked())
		}
	}
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

func sortedEgressRouteCandidates(candidates []api.EgressRoutePolicyCandidate) []api.EgressRoutePolicyCandidate {
	result := append([]api.EgressRoutePolicyCandidate{}, candidates...)
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
	case api.BridgeSpec:
		if key == "ifname" {
			return spec.IfName
		}
	case api.VXLANSegmentSpec:
		if key == "ifname" {
			return spec.IfName
		}
	case api.VRFSpec:
		if key == "ifname" {
			return spec.IfName
		}
	case api.VXLANTunnelSpec:
		if key == "ifname" {
			return spec.IfName
		}
	case api.PPPoESessionSpec:
		switch key {
		case "ifname":
			return spec.IfName
		case "interface":
			return spec.Interface
		case "authMethod":
			return spec.AuthMethod
		case "username":
			return spec.Username
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
	case api.DHCPv4ClientSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "hostname":
			return spec.Hostname
		case "requestedAddress":
			return spec.RequestedAddress
		case "classID":
			return spec.ClassID
		case "clientID":
			return spec.ClientID
		case "routeMetric":
			return fmt.Sprint(spec.RouteMetric)
		}
	case api.DHCPv4ServerSpec:
		switch key {
		case "server":
			return spec.Server
		case "interface":
			return spec.Interface
		case "rangeStart":
			return defaultString(spec.AddressPool.Start, spec.RangeStart)
		case "rangeEnd":
			return defaultString(spec.AddressPool.End, spec.RangeEnd)
		case "leaseTime":
			return defaultString(spec.AddressPool.LeaseTime, spec.LeaseTime)
		case "router":
			return spec.Router
		}
	case api.DHCPv6AddressSpec:
		switch key {
		case "interface":
			return spec.Interface
		case "client":
			return spec.Client
		}
	case api.IPv6RAAddressSpec:
		if key == "interface" {
			return spec.Interface
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
	case api.DHCPv6AddressSpec:
		if key == "required" {
			return spec.Required
		}
	case api.IPv6RAAddressSpec:
		switch key {
		case "managed":
			return api.BoolDefault(spec.Managed, true)
		case "required":
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
