// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"sort"

	"github.com/imksoo/routerd/pkg/api"
)

type Class string

const (
	ClassManagedHost   Class = "managed-host"
	ClassRendererInput Class = "renderer-input"
	ClassController    Class = "controller"
	ClassDynamicSource Class = "dynamic-source"
	ClassExternal      Class = "external"
	ClassConfigOnly    Class = "config-only"
)

type Declaration struct {
	APIVersion string
	Kind       string
	Class      Class
	Notes      string
}

func AllDeclarations() []Declaration {
	out := append([]Declaration(nil), declarations...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].APIVersion != out[j].APIVersion {
			return out[i].APIVersion < out[j].APIVersion
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func Lookup(apiVersion, kind string) (Declaration, bool) {
	for _, declaration := range declarations {
		if declaration.APIVersion == apiVersion && declaration.Kind == kind {
			return declaration, true
		}
	}
	return Declaration{}, false
}

var declarations = []Declaration{
	{api.PluginAPIVersion, "Plugin", ClassManagedHost, "trusted local plugin manifest and execution wiring"},
	{api.PluginAPIVersion, "DynamicConfigSource", ClassDynamicSource, "produces DynamicConfigPart resources"},
	{api.ConfigAPIVersion, "DynamicOverridePolicy", ClassDynamicSource, "filters or overrides dynamic resources"},
	{api.SystemAPIVersion, "LogSink", ClassManagedHost, "log forwarding sink configuration"},
	{api.ObservabilityAPIVersion, "Telemetry", ClassManagedHost, "OpenTelemetry SDK/exporter configuration"},
	{api.SystemAPIVersion, "ObservabilityPipeline", ClassManagedHost, "local observability pipeline configuration"},
	{api.SystemAPIVersion, "LogRetention", ClassManagedHost, "local retention jobs and state cleanup policy"},
	{api.SystemAPIVersion, "Sysctl", ClassManagedHost, "host sysctl value"},
	{api.SystemAPIVersion, "SysctlProfile", ClassRendererInput, "expands into sysctl settings"},
	{api.SystemAPIVersion, "Package", ClassManagedHost, "host package intent"},
	{api.SystemAPIVersion, "NTPClient", ClassManagedHost, "time sync client configuration"},
	{api.SystemAPIVersion, "NTPServer", ClassManagedHost, "time sync server configuration"},
	{api.SystemAPIVersion, "WebConsole", ClassManagedHost, "web console service and files"},
	{api.SystemAPIVersion, "RouterdCluster", ClassController, "cluster coordination state"},
	{api.NetAPIVersion, "Interface", ClassManagedHost, "network interface alias/adoption intent"},
	{api.NetAPIVersion, "PPPoESession", ClassManagedHost, "PPPoE daemon, socket, and interface artifacts"},
	{api.NetAPIVersion, "WireGuardInterface", ClassManagedHost, "WireGuard interface and configuration"},
	{api.NetAPIVersion, "WireGuardPeer", ClassRendererInput, "peer entry rendered into a WireGuard interface"},
	{api.NetAPIVersion, "TailscaleNode", ClassManagedHost, "tailscale node configuration"},
	{api.NetAPIVersion, "IPsecConnection", ClassManagedHost, "IPsec connection configuration"},
	{api.NetAPIVersion, "VRF", ClassManagedHost, "VRF link and routing table intent"},
	{api.NetAPIVersion, "VXLANTunnel", ClassManagedHost, "VXLAN tunnel link"},
	{api.NetAPIVersion, "IPv4StaticAddress", ClassManagedHost, "IPv4 address on an interface"},
	{api.NetAPIVersion, "VirtualAddress", ClassManagedHost, "VRRP/CARP virtual address"},
	{api.NetAPIVersion, "BGPRouter", ClassController, "GoBGP router instance"},
	{api.NetAPIVersion, "BGPPeer", ClassController, "GoBGP peer"},
	{api.NetAPIVersion, "BFD", ClassController, "BFD session configuration"},
	{api.NetAPIVersion, "DHCPv4Client", ClassManagedHost, "DHCPv4 client service"},
	{api.NetAPIVersion, "ClusterNetworkRoute", ClassRendererInput, "expands cluster CIDRs into route intents"},
	{api.NetAPIVersion, "DHCPv4Server", ClassManagedHost, "dnsmasq DHCPv4 server configuration"},
	{api.NetAPIVersion, "DHCPv4Reservation", ClassRendererInput, "reservation rendered into a DHCPv4 server"},
	{api.NetAPIVersion, "DHCPv6Address", ClassRendererInput, "address allocation rendered into DHCPv6 server state"},
	{api.NetAPIVersion, "IPv6RAAddress", ClassRendererInput, "RA address/prefix input"},
	{api.NetAPIVersion, "DHCPv6PrefixDelegation", ClassManagedHost, "DHCPv6 prefix delegation client"},
	{api.NetAPIVersion, "IPv6DelegatedAddress", ClassManagedHost, "address derived from delegated prefix"},
	{api.NetAPIVersion, "DHCPv6Information", ClassManagedHost, "DHCPv6 information-request client"},
	{api.NetAPIVersion, "IPv6RouterAdvertisement", ClassManagedHost, "router advertisement service configuration"},
	{api.NetAPIVersion, "DHCPv6Server", ClassManagedHost, "dnsmasq DHCPv6 server configuration"},
	{api.NetAPIVersion, "DHCPv4ServerLeaseSync", ClassManagedHost, "DHCPv4 lease synchronization job"},
	{api.NetAPIVersion, "DHCPv6ServerLeaseSync", ClassManagedHost, "DHCPv6 lease synchronization job"},
	{api.NetAPIVersion, "DHCPv6PrefixDelegationLeaseSync", ClassManagedHost, "DHCPv6 PD lease synchronization job"},
	{api.NetAPIVersion, "DHCPv4Relay", ClassManagedHost, "DHCPv4 relay service"},
	{api.NetAPIVersion, "SelfAddressPolicy", ClassRendererInput, "self-address selection input"},
	{api.NetAPIVersion, "DNSZone", ClassRendererInput, "DNS zone data rendered by resolver/forwarder controllers"},
	{api.NetAPIVersion, "DNSResolver", ClassManagedHost, "DNS resolver service configuration"},
	{api.NetAPIVersion, "DNSForwarder", ClassRendererInput, "forwarding rule rendered into DNS resolver"},
	{api.NetAPIVersion, "DNSUpstream", ClassRendererInput, "upstream rendered into DNS resolver"},
	{api.NetAPIVersion, "DSLiteTunnel", ClassManagedHost, "DS-Lite tunnel and policy artifacts"},
	{api.HybridAPIVersion, "TunnelInterface", ClassManagedHost, "hybrid tunnel interface"},
	{api.HybridAPIVersion, "OverlayPeer", ClassRendererInput, "legacy SAM overlay peer input"},
	{api.HybridAPIVersion, "HybridRoute", ClassManagedHost, "hybrid route artifact"},
	{api.HybridAPIVersion, "AddressMobilityDomain", ClassDynamicSource, "legacy mobility dynamic source"},
	{api.HybridAPIVersion, "CloudProviderProfile", ClassExternal, "cloud provider reference and credentials"},
	{api.HybridAPIVersion, "RemoteAddressClaim", ClassDynamicSource, "remote address claim driving mobility state"},
	{api.HybridAPIVersion, "ProviderActionPolicy", ClassExternal, "cloud/provider action policy"},
	{api.FederationAPIVersion, "EventGroup", ClassController, "event federation group state"},
	{api.FederationAPIVersion, "EventPeer", ClassController, "event federation peer state"},
	{api.FederationAPIVersion, "EventSubscription", ClassDynamicSource, "event subscription can emit DynamicConfigPart"},
	{api.MobilityAPIVersion, "MobilityPool", ClassDynamicSource, "SAM capture/control-plane dynamic source"},
	{api.MobilityAPIVersion, "SAMTransportProfile", ClassDynamicSource, "generates transport TunnelInterface/BGPPeer resources"},
	{api.NetAPIVersion, "IPv4Route", ClassManagedHost, "IPv4 route"},
	{api.NetAPIVersion, "HealthCheck", ClassManagedHost, "health check service/socket artifacts"},
	{api.NetAPIVersion, "EgressRoutePolicy", ClassManagedHost, "policy routing and nft mark artifacts"},
	{api.NetAPIVersion, "EventRule", ClassController, "local event derivation rule"},
	{api.NetAPIVersion, "DerivedEvent", ClassController, "derived event state"},
	{api.NetAPIVersion, "NAT44Rule", ClassManagedHost, "nft NAT44 rule/table artifacts"},
	{api.NetAPIVersion, "NAT44SessionSync", ClassManagedHost, "conntrack session synchronization service"},
	{api.NetAPIVersion, "ManagementAccess", ClassManagedHost, "management access firewall/service policy"},
	{api.FirewallAPIVersion, "PortForward", ClassManagedHost, "nft port-forward rule artifacts"},
	{api.FirewallAPIVersion, "IngressService", ClassManagedHost, "nft ingress service rule artifacts"},
	{api.NetAPIVersion, "IPAddressSet", ClassManagedHost, "nft/ipset address set artifacts"},
	{api.FirewallAPIVersion, "LocalServiceRedirect", ClassManagedHost, "local redirect nft rule artifacts"},
	{api.NetAPIVersion, "TrafficFlowLog", ClassManagedHost, "traffic flow logging service/artifacts"},
	{api.FirewallAPIVersion, "FirewallZone", ClassRendererInput, "firewall rendering input"},
	{api.FirewallAPIVersion, "FirewallPolicy", ClassManagedHost, "firewall ruleset owner"},
	{api.FirewallAPIVersion, "ClientPolicy", ClassRendererInput, "client firewall rendering input"},
	{api.FirewallAPIVersion, "FirewallRule", ClassRendererInput, "firewall rule rendered into policy tables"},
	{api.FirewallAPIVersion, "FirewallEventLog", ClassManagedHost, "firewall logging table/service artifacts"},
	{api.NetAPIVersion, "Hostname", ClassManagedHost, "host name"},
}
