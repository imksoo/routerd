// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

type ObjectMeta struct {
	Name      string     `yaml:"name" json:"name"`
	OwnerRefs []OwnerRef `yaml:"ownerRefs,omitempty" json:"ownerRefs,omitempty"`
}

type OwnerRef struct {
	APIVersion string `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

type Router struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta `yaml:"metadata" json:"metadata"`
	Spec     RouterSpec `yaml:"spec" json:"spec"`
}

type RouterSpec struct {
	Apply     ApplyPolicySpec `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`
	Resources []Resource      `yaml:"resources" json:"resources"`
}

type Resource struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta     `yaml:"metadata" json:"metadata"`
	Spec     any            `yaml:"spec" json:"spec"`
	Status   map[string]any `yaml:"status,omitempty" json:"status,omitempty"`
}

func (r Resource) ID() string {
	return r.APIVersion + "/" + r.Kind + "/" + r.Metadata.Name
}

const (
	RouterAPIVersion        = "routerd.net/v1alpha1"
	NetAPIVersion           = "net.routerd.net/v1alpha1"
	SystemAPIVersion        = "system.routerd.net/v1alpha1"
	FirewallAPIVersion      = "firewall.routerd.net/v1alpha1"
	ObservabilityAPIVersion = "observability.routerd.net/v1alpha1"
)

func (r *Resource) UnmarshalYAML(value *yaml.Node) error {
	type rawResource struct {
		TypeMeta `yaml:",inline"`
		Metadata ObjectMeta     `yaml:"metadata"`
		Spec     yaml.Node      `yaml:"spec"`
		Status   map[string]any `yaml:"status,omitempty"`
	}

	var raw rawResource
	if err := value.Decode(&raw); err != nil {
		return err
	}
	r.TypeMeta = raw.TypeMeta
	r.Metadata = raw.Metadata
	r.Status = raw.Status

	switch raw.Kind {
	case "LogSink":
		var spec LogSinkSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Telemetry":
		var spec TelemetrySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "ObservabilityPipeline":
		var spec ObservabilityPipelineSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "LogRetention":
		var spec LogRetentionSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Sysctl":
		var spec SysctlSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "SysctlProfile":
		var spec SysctlProfileSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Package":
		var spec PackageSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "NTPClient":
		var spec NTPClientSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "NTPServer":
		var spec NTPServerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "WebConsole":
		var spec WebConsoleSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "RouterdCluster":
		var spec RouterdClusterSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Inventory":
		var spec InventorySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Interface":
		var spec InterfaceSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Bridge":
		var spec BridgeSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "VXLANSegment":
		var spec VXLANSegmentSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "WireGuardInterface":
		var spec WireGuardInterfaceSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "WireGuardPeer":
		var spec WireGuardPeerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "TailscaleNode":
		var spec TailscaleNodeSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPsecConnection":
		var spec IPsecConnectionSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "VRF":
		var spec VRFSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "VXLANTunnel":
		var spec VXLANTunnelSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "PPPoESession":
		if hasMappingKey(&raw.Spec, "socketSource") {
			return fmt.Errorf("%s spec.socketSource is not supported; routerd derives daemon sockets automatically", r.ID())
		}
		var spec PPPoESessionSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4StaticAddress":
		var spec IPv4StaticAddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "VirtualAddress":
		var spec VirtualAddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "BGPRouter":
		var spec BGPRouterSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "BGPPeer":
		var spec BGPPeerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv4Client":
		var spec DHCPv4ClientSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4StaticRoute":
		var spec IPv4StaticRouteSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "ClusterNetworkRoute":
		var spec ClusterNetworkRouteSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv6StaticRoute":
		var spec IPv6StaticRouteSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv4Server":
		var spec DHCPv4ServerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv4Scope":
		return fmt.Errorf("%s is not supported; put the DHCPv4 address pool directly on DHCPv4Server", r.ID())
	case "DHCPv4Reservation":
		var spec DHCPv4ReservationSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv6Address":
		var spec DHCPv6AddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv6RAAddress":
		var spec IPv6RAAddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv6PrefixDelegation":
		var spec DHCPv6PrefixDelegationSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv6DelegatedAddress":
		var spec IPv6DelegatedAddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv6Information":
		var spec DHCPv6InformationSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv6RouterAdvertisement":
		var spec IPv6RouterAdvertisementSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv6Server":
		var spec DHCPv6ServerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv6Scope":
		return fmt.Errorf("%s is not supported; put the DHCPv6 delegatedAddress and options directly on DHCPv6Server", r.ID())
	case "DHCPv4Relay":
		var spec DHCPv4RelaySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DNSZone":
		var spec DNSZoneSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "SelfAddressPolicy":
		var spec SelfAddressPolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DNSResolver":
		if hasMappingKey(&raw.Spec, "sources") {
			return fmt.Errorf("%s spec.sources is not supported; split DNS source intent into DNSForwarder and DNSUpstream resources that reference this DNSResolver", r.ID())
		}
		var spec DNSResolverSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DNSForwarder":
		var spec DNSForwarderSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DNSUpstream":
		var spec DNSUpstreamSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "TrafficFlowLog":
		var spec TrafficFlowLogSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DSLiteTunnel":
		var spec DSLiteTunnelSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4Route":
		var spec IPv4RouteSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "HealthCheck":
		if hasMappingKey(&raw.Spec, "socketSource") {
			return fmt.Errorf("%s spec.socketSource is not supported; routerd derives daemon sockets automatically", r.ID())
		}
		var spec HealthCheckSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "EgressRoutePolicy":
		var spec EgressRoutePolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "EventRule":
		var spec EventRuleSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DerivedEvent":
		var spec DerivedEventSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4DefaultRoutePolicy":
		return fmt.Errorf("%s is not supported; use EgressRoutePolicy with candidates directly", r.ID())
	case "NAT44Rule":
		var spec NAT44RuleSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "PortForward":
		var spec PortForwardSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IngressService":
		var spec IngressServiceSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPAddressSet":
		var spec IPAddressSetSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "LocalServiceRedirect":
		var spec LocalServiceRedirectSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4PolicyRoute":
		return fmt.Errorf("%s is not supported; use EgressRoutePolicy with one marked candidate", r.ID())
	case "IPv4PolicyRouteSet":
		return fmt.Errorf("%s is not supported; put hashFields and targets under EgressRoutePolicy candidates", r.ID())
	case "FirewallZone":
		var spec FirewallZoneSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "FirewallPolicy":
		var spec FirewallPolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "FirewallLog":
		var spec FirewallLogSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "ClientPolicy":
		var spec ClientPolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "FirewallRule":
		var spec FirewallRuleSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "Hostname":
		var spec HostnameSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "SystemdUnit":
		return fmt.Errorf("%s is not supported; declare router intent and let routerd generate service units", r.ID())
	case "KernelModule":
		return fmt.Errorf("%s is not supported; routerd derives required kernel modules from declared resources", r.ID())
	case "NetworkAdoption":
		return fmt.Errorf("%s is not supported; routerd derives networkd/resolved adoption from Interface and service resources", r.ID())
	case "NixOSHost":
		return fmt.Errorf("%s is not supported; use router resources and platform renderers instead of host implementation resources", r.ID())
	case "Link":
		return fmt.Errorf("%s is not supported; use Interface resources as link status providers", r.ID())
	case "StatePolicy":
		return fmt.Errorf("%s is not supported; use spec.when any/all predicates on the dependent resources", r.ID())
	case "DHCPv4Lease":
		return fmt.Errorf("%s is not supported; use DHCPv4Client for routerd-managed DHCPv4 client intent", r.ID())
	case "PPPoEInterface":
		return fmt.Errorf("%s is not supported; use PPPoESession for routerd-managed PPPoE session intent", r.ID())
	case "IPv4SourceNAT":
		return fmt.Errorf("%s is not supported; use NAT44Rule for IPv4 source NAT intent", r.ID())
	case "VirtualIPv4Address":
		return fmt.Errorf("%s is not supported; use VirtualAddress with spec.family: ipv4", r.ID())
	case "VirtualIPv6Address":
		return fmt.Errorf("%s is not supported; use VirtualAddress with spec.family: ipv6", r.ID())
	case "IPv4ReversePathFilter":
		return fmt.Errorf("%s is not supported; routerd derives reverse path filter sysctls automatically", r.ID())
	case "PathMTUPolicy":
		return fmt.Errorf("%s is not supported; routerd derives path MTU and TCP MSS handling from tunnel and interface resources", r.ID())
	default:
		return fmt.Errorf("unsupported resource kind %s in %s", raw.Kind, r.ID())
	}
	return nil
}

func hasMappingKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}
