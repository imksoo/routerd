// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"fmt"
	"sort"
	"strings"

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

type TeardownLifecycle string

const (
	TeardownLifecycleResource TeardownLifecycle = "resource"
)

type Declaration struct {
	APIVersion           string
	Kind                 string
	Class                Class
	Notes                string
	ArtifactKinds        []string
	TeardownLifecycle    TeardownLifecycle
	NoHostTeardownReason string
}

func OwnerKey(apiVersion, kind, name string) string {
	return strings.TrimSpace(apiVersion) + "/" + strings.TrimSpace(kind) + "/" + strings.TrimSpace(name)
}

func SelfOwnerRef(apiVersion, kind, name string) api.OwnerRef {
	return api.OwnerRef{APIVersion: strings.TrimSpace(apiVersion), Kind: strings.TrimSpace(kind), Name: strings.TrimSpace(name)}
}

func OwnerRefKey(ref api.OwnerRef) string {
	return OwnerKey(ref.APIVersion, ref.Kind, ref.Name)
}

func OwnerRefStatusMap(ref api.OwnerRef) map[string]any {
	out := map[string]any{
		"kind": ref.Kind,
		"name": ref.Name,
	}
	if strings.TrimSpace(ref.APIVersion) != "" {
		out["apiVersion"] = ref.APIVersion
	}
	return out
}

func OwnerRefsStatusList(refs []api.OwnerRef) []any {
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.Name) == "" {
			continue
		}
		out = append(out, OwnerRefStatusMap(ref))
	}
	return out
}

func DeclarationForResource(resource api.Resource) (Declaration, bool) {
	apiVersion := resource.APIVersion
	if apiVersion == "" {
		apiVersion = APIVersionForKind(resource.Kind)
	}
	return Lookup(apiVersion, resource.Kind)
}

func APIVersionForKind(kind string) string {
	for _, resource := range api.ConfigResourceKinds() {
		if resource.Kind == kind {
			return resource.APIVersion
		}
	}
	switch kind {
	case "Inventory", "Router":
		return api.RouterAPIVersion
	case "ServiceUnit", "NetworkAdoption", "KernelModule", "ConntrackTuning":
		return api.SystemAPIVersion
	case "ConntrackObserver":
		return api.NetAPIVersion
	default:
		return ""
	}
}

func MustOwnerKey(apiVersion, kind, name string) (string, error) {
	if strings.TrimSpace(apiVersion) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("owner key requires apiVersion, kind, and name")
	}
	return OwnerKey(apiVersion, kind, name), nil
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

type declarationOption func(*Declaration)

func declare(apiVersion, kind string, class Class, notes string, options ...declarationOption) Declaration {
	declaration := Declaration{APIVersion: apiVersion, Kind: kind, Class: class, Notes: notes}
	for _, option := range options {
		option(&declaration)
	}
	return declaration
}

func artifacts(kinds ...string) declarationOption {
	return func(declaration *Declaration) {
		declaration.ArtifactKinds = append([]string(nil), kinds...)
	}
}

func resourceLifecycle() declarationOption {
	return func(declaration *Declaration) {
		declaration.TeardownLifecycle = TeardownLifecycleResource
	}
}

func noHostTeardown(reason string) declarationOption {
	return func(declaration *Declaration) {
		declaration.NoHostTeardownReason = reason
	}
}

var declarations = []Declaration{
	declare(api.PluginAPIVersion, "Plugin", ClassManagedHost, "trusted local plugin manifest and execution wiring", resourceLifecycle()),
	declare(api.PluginAPIVersion, "DynamicConfigSource", ClassDynamicSource, "produces DynamicConfigPart resources", noHostTeardown("dynamic output is represented as DynamicConfigPart and then handled by the effective resource view")),
	declare(api.ConfigAPIVersion, "DynamicOverridePolicy", ClassDynamicSource, "filters or overrides dynamic resources", noHostTeardown("policy only influences dynamic effective view construction")),
	declare(api.SystemAPIVersion, "LogSink", ClassManagedHost, "log forwarding sink configuration", resourceLifecycle()),
	declare(api.ObservabilityAPIVersion, "Telemetry", ClassManagedHost, "OpenTelemetry SDK/exporter configuration", resourceLifecycle()),
	declare(api.SystemAPIVersion, "ObservabilityPipeline", ClassManagedHost, "local observability pipeline configuration", resourceLifecycle()),
	declare(api.SystemAPIVersion, "LogRetention", ClassManagedHost, "local retention jobs and state cleanup policy", resourceLifecycle()),
	declare(api.SystemAPIVersion, "Sysctl", ClassManagedHost, "host sysctl value", resourceLifecycle()),
	declare(api.SystemAPIVersion, "SysctlProfile", ClassRendererInput, "expands into sysctl settings", noHostTeardown("renderer input expands into Sysctl resources")),
	declare(api.SystemAPIVersion, "Package", ClassManagedHost, "host package intent", resourceLifecycle()),
	declare(api.SystemAPIVersion, "NTPClient", ClassManagedHost, "time sync client configuration", resourceLifecycle()),
	declare(api.SystemAPIVersion, "NTPServer", ClassManagedHost, "time sync server configuration", resourceLifecycle()),
	declare(api.SystemAPIVersion, "WebConsole", ClassManagedHost, "web console service and files", resourceLifecycle()),
	declare(api.SystemAPIVersion, "RouterdCluster", ClassController, "cluster coordination state", noHostTeardown("controller state does not own standalone host artifacts")),
	declare(api.NetAPIVersion, "Interface", ClassManagedHost, "network interface alias/adoption intent", resourceLifecycle()),
	declare(api.NetAPIVersion, "PPPoESession", ClassManagedHost, "PPPoE daemon, socket, and interface artifacts", artifacts("systemd.service", "file", "unix.socket", "directory")),
	declare(api.NetAPIVersion, "WireGuardInterface", ClassManagedHost, "WireGuard interface and configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "WireGuardPeer", ClassRendererInput, "peer entry rendered into a WireGuard interface", noHostTeardown("peer entry is rendered into its owning WireGuardInterface")),
	declare(api.NetAPIVersion, "TailscaleNode", ClassManagedHost, "tailscale node configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "IPsecConnection", ClassManagedHost, "IPsec connection configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "VRF", ClassManagedHost, "VRF link and routing table intent", resourceLifecycle()),
	declare(api.NetAPIVersion, "VXLANTunnel", ClassManagedHost, "VXLAN tunnel link", resourceLifecycle()),
	declare(api.NetAPIVersion, "IPv4StaticAddress", ClassManagedHost, "IPv4 address on an interface", resourceLifecycle()),
	declare(api.NetAPIVersion, "VirtualAddress", ClassManagedHost, "VRRP/CARP virtual address", resourceLifecycle()),
	declare(api.NetAPIVersion, "BGPRouter", ClassController, "GoBGP router instance", resourceLifecycle()),
	declare(api.NetAPIVersion, "BGPPeer", ClassController, "GoBGP peer", resourceLifecycle()),
	declare(api.NetAPIVersion, "BFD", ClassController, "BFD session configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv4Client", ClassManagedHost, "DHCPv4 client service", resourceLifecycle()),
	declare(api.NetAPIVersion, "ClusterNetworkRoute", ClassRendererInput, "expands cluster CIDRs into route intents", noHostTeardown("renderer input expands into route resources")),
	declare(api.NetAPIVersion, "DHCPv4Server", ClassManagedHost, "dnsmasq DHCPv4 server configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv4Reservation", ClassRendererInput, "reservation rendered into a DHCPv4 server", noHostTeardown("reservation is rendered into its DHCPv4Server")),
	declare(api.NetAPIVersion, "DHCPv6Address", ClassRendererInput, "address allocation rendered into DHCPv6 server state", noHostTeardown("address allocation is rendered into DHCPv6 server/client state")),
	declare(api.NetAPIVersion, "IPv6RAAddress", ClassRendererInput, "RA address/prefix input", noHostTeardown("RA address input is rendered into router advertisement state")),
	declare(api.NetAPIVersion, "DHCPv6PrefixDelegation", ClassManagedHost, "DHCPv6 prefix delegation client", resourceLifecycle()),
	declare(api.NetAPIVersion, "IPv6DelegatedAddress", ClassManagedHost, "address derived from delegated prefix", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv6Information", ClassManagedHost, "DHCPv6 information-request client", resourceLifecycle()),
	declare(api.NetAPIVersion, "IPv6RouterAdvertisement", ClassManagedHost, "router advertisement service configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv6Server", ClassManagedHost, "dnsmasq DHCPv6 server configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv4ServerLeaseSync", ClassManagedHost, "DHCPv4 lease synchronization job", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv6ServerLeaseSync", ClassManagedHost, "DHCPv6 lease synchronization job", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv6PrefixDelegationLeaseSync", ClassManagedHost, "DHCPv6 PD lease synchronization job", resourceLifecycle()),
	declare(api.NetAPIVersion, "DHCPv4Relay", ClassManagedHost, "DHCPv4 relay service", resourceLifecycle()),
	declare(api.NetAPIVersion, "SelfAddressPolicy", ClassRendererInput, "self-address selection input", noHostTeardown("policy input is consumed by address-selection controllers")),
	declare(api.NetAPIVersion, "DNSZone", ClassRendererInput, "DNS zone data rendered by resolver/forwarder controllers", noHostTeardown("zone data is rendered into DNS resolver state")),
	declare(api.NetAPIVersion, "DNSResolver", ClassManagedHost, "DNS resolver service configuration", resourceLifecycle()),
	declare(api.NetAPIVersion, "DNSForwarder", ClassRendererInput, "forwarding rule rendered into DNS resolver", noHostTeardown("forwarding rule is rendered into DNS resolver state")),
	declare(api.NetAPIVersion, "DNSUpstream", ClassRendererInput, "upstream rendered into DNS resolver", noHostTeardown("upstream is rendered into DNS resolver state")),
	declare(api.NetAPIVersion, "DSLiteTunnel", ClassManagedHost, "DS-Lite tunnel and policy artifacts", artifacts("linux.ipip6.tunnel", "net.ipv4.address")),
	declare(api.HybridAPIVersion, "TunnelInterface", ClassManagedHost, "hybrid tunnel interface", resourceLifecycle()),
	declare(api.HybridAPIVersion, "OverlayPeer", ClassRendererInput, "legacy SAM overlay peer input", noHostTeardown("legacy overlay peer input is rendered into transport resources")),
	declare(api.HybridAPIVersion, "HybridRoute", ClassManagedHost, "hybrid route artifact", resourceLifecycle()),
	declare(api.HybridAPIVersion, "AddressMobilityDomain", ClassDynamicSource, "legacy mobility dynamic source", noHostTeardown("dynamic source emits resources that are handled through the effective view")),
	declare(api.HybridAPIVersion, "CloudProviderProfile", ClassExternal, "cloud provider reference and credentials", noHostTeardown("external provider reference does not own local host artifacts")),
	declare(api.HybridAPIVersion, "RemoteAddressClaim", ClassDynamicSource, "remote address claim driving mobility state", resourceLifecycle()),
	declare(api.HybridAPIVersion, "ProviderActionPolicy", ClassExternal, "cloud/provider action policy", noHostTeardown("external policy reference does not own local host artifacts")),
	declare(api.FederationAPIVersion, "EventGroup", ClassController, "event federation group state", noHostTeardown("controller state does not own standalone host artifacts")),
	declare(api.FederationAPIVersion, "EventPeer", ClassController, "event federation peer state", noHostTeardown("controller state does not own standalone host artifacts")),
	declare(api.FederationAPIVersion, "EventSubscription", ClassDynamicSource, "event subscription can emit DynamicConfigPart", noHostTeardown("dynamic output is represented as DynamicConfigPart and then handled by the effective resource view")),
	declare(api.MobilityAPIVersion, "MobilityPool", ClassDynamicSource, "SAM capture/control-plane dynamic source", noHostTeardown("dynamic source emits capture resources that are handled through the effective view")),
	declare(api.MobilityAPIVersion, "SAMTransportProfile", ClassDynamicSource, "generates transport TunnelInterface/BGPPeer resources", noHostTeardown("generated transport resources own teardown through the effective view")),
	declare(api.NetAPIVersion, "IPv4Route", ClassManagedHost, "IPv4 route", resourceLifecycle()),
	declare(api.NetAPIVersion, "HealthCheck", ClassManagedHost, "health check service/socket artifacts", resourceLifecycle()),
	declare(api.NetAPIVersion, "EgressRoutePolicy", ClassManagedHost, "policy routing and nft mark artifacts", artifacts("nft.table", "linux.ipv4.fwmarkRule", "linux.ipv4.routeTable")),
	declare(api.NetAPIVersion, "EventRule", ClassController, "local event derivation rule", noHostTeardown("controller derives daemon events without owning host artifacts")),
	declare(api.NetAPIVersion, "DerivedEvent", ClassController, "derived event state", noHostTeardown("derived event state does not own host artifacts")),
	declare(api.NetAPIVersion, "NAT44Rule", ClassManagedHost, "nft NAT44 rule/table artifacts", artifacts("nft.table")),
	declare(api.NetAPIVersion, "NAT44SessionSync", ClassManagedHost, "conntrack session synchronization service", resourceLifecycle()),
	declare(api.NetAPIVersion, "ManagementAccess", ClassManagedHost, "management access firewall/service policy", resourceLifecycle()),
	declare(api.FirewallAPIVersion, "PortForward", ClassManagedHost, "nft port-forward rule artifacts", artifacts("nft.table")),
	declare(api.FirewallAPIVersion, "IngressService", ClassManagedHost, "nft ingress service rule artifacts", artifacts("nft.table")),
	declare(api.NetAPIVersion, "IPAddressSet", ClassManagedHost, "nft/ipset address set artifacts", resourceLifecycle()),
	declare(api.FirewallAPIVersion, "LocalServiceRedirect", ClassManagedHost, "local redirect nft rule artifacts", artifacts("nft.table")),
	declare(api.NetAPIVersion, "TrafficFlowLog", ClassManagedHost, "traffic flow logging service/artifacts", resourceLifecycle()),
	declare(api.FirewallAPIVersion, "FirewallZone", ClassRendererInput, "firewall rendering input", noHostTeardown("zone input is rendered into firewall policy tables")),
	declare(api.FirewallAPIVersion, "FirewallPolicy", ClassManagedHost, "firewall ruleset owner", artifacts("nft.table")),
	declare(api.FirewallAPIVersion, "ClientPolicy", ClassRendererInput, "client firewall rendering input", noHostTeardown("client policy input is rendered into firewall policy tables")),
	declare(api.FirewallAPIVersion, "FirewallRule", ClassRendererInput, "firewall rule rendered into policy tables", noHostTeardown("rule input is rendered into firewall policy tables")),
	declare(api.FirewallAPIVersion, "FirewallEventLog", ClassManagedHost, "firewall logging table/service artifacts", resourceLifecycle()),
	declare(api.NetAPIVersion, "Hostname", ClassManagedHost, "host name", resourceLifecycle()),
}
