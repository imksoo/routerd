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
	ConfigAPIVersion        = "config.routerd.net/v1alpha1"
	PluginAPIVersion        = "plugin.routerd.net/v1alpha1"
	HybridAPIVersion        = "hybrid.routerd.net/v1alpha1"
	FederationAPIVersion    = "federation.routerd.net/v1alpha1"
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
	case "Plugin":
		var spec PluginSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DynamicConfigSource":
		var spec DynamicConfigSourceSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DynamicOverridePolicy":
		var spec DynamicOverridePolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "LogSink":
		if hasMappingKey(&raw.Spec, "plugin") {
			return fmt.Errorf("%s spec.plugin is not supported; use type webhook, file, journald, or otlp for log forwarding sinks", r.ID())
		}
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
		if hasMappingKey(&raw.Spec, "targets") {
			return fmt.Errorf("%s spec.targets is not supported; use spec.retention with spec.signals and spec.sinks", r.ID())
		}
		if hasMappingKey(&raw.Spec, "incrementalVacuum") {
			return fmt.Errorf("%s spec.incrementalVacuum is not supported; use spec.vacuum", r.ID())
		}
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
	case "ManagementAccess":
		var spec ManagementAccessSpec
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
		for _, field := range []string{"fwmark", "table"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; routerd derives WireGuard fwmark and routing table ownership automatically", r.ID(), field)
			}
		}
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
		for _, field := range []string{"operator", "binaryPath"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; routerd derives the tailscale binary path and operator handling from the platform", r.ID(), field)
			}
		}
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
		if vrrp := mappingValueNode(&raw.Spec, "vrrp"); vrrp != nil {
			for _, field := range []string{"advertInterval", "preemptDelay"} {
				if hasMappingKey(vrrp, field) {
					return fmt.Errorf("%s spec.vrrp.%s is not supported; choose VRRP/CARP behavior with resource intent and routerd profile defaults", r.ID(), field)
				}
			}
		}
		var spec VirtualAddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "BGPRouter":
		if timers := mappingValueNode(&raw.Spec, "timers"); timers != nil {
			for _, field := range []string{"keepalive", "holdTime", "connectRetry"} {
				if hasMappingKey(timers, field) {
					return fmt.Errorf("%s spec.timers.%s is not supported; use spec.timers.profile or routerd BGP defaults", r.ID(), field)
				}
			}
		}
		var spec BGPRouterSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "BGPPeer":
		if timers := mappingValueNode(&raw.Spec, "timers"); timers != nil {
			for _, field := range []string{"keepalive", "holdTime", "connectRetry"} {
				if hasMappingKey(timers, field) {
					return fmt.Errorf("%s spec.timers.%s is not supported; use spec.timers.profile or routerd BGP defaults", r.ID(), field)
				}
			}
		}
		if bfd := mappingValueNode(&raw.Spec, "bfd"); bfd != nil {
			if bfd.Kind != yaml.ScalarNode || bfd.Tag != "!!str" {
				return fmt.Errorf("%s spec.bfd inline BFD settings are not supported; create a BFD/<name> resource and reference it with spec.bfd", r.ID())
			}
		}
		var spec BGPPeerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "BFD":
		var spec BFDSpec
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
		return RemovedLegacyKindError(raw.Kind, r.ID())
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
		for _, field := range []string{"iaid", "duidType"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; use spec.profile and let routerd derive DHCPv6 client identity details", r.ID(), field)
			}
		}
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
		return RemovedLegacyKindError(raw.Kind, r.ID())
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
		if hasMappingKey(&raw.Spec, "includeNDPI") {
			return fmt.Errorf("%s spec.includeNDPI is not supported; use spec.includeApplicationLayer", r.ID())
		}
		if hasMappingKey(&raw.Spec, "retention") {
			return fmt.Errorf("%s spec.retention is not supported; declare a LogRetention resource for traffic flow retention", r.ID())
		}
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
	case "OverlayPeer":
		var spec OverlayPeerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "HybridRoute":
		var spec HybridRouteSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "AddressMobilityDomain":
		var spec AddressMobilityDomainSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "CloudProviderProfile":
		var spec CloudProviderProfileSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "RemoteAddressClaim":
		var spec RemoteAddressClaimSpec
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
		for _, field := range []string{"daemon", "socketSource", "fwmark", "sourceInterface", "sourceAddress", "sourceAddressFrom", "via"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; routerd derives health-check daemon, source binding, and fwmark from referenced route/interface resources", r.ID(), field)
			}
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
	case "EventGroup":
		var spec EventGroupSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "EventPeer":
		var spec EventPeerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4DefaultRoutePolicy":
		return RemovedLegacyKindError(raw.Kind, r.ID())
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
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "IPv4PolicyRouteSet":
		return RemovedLegacyKindError(raw.Kind, r.ID())
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
	case "FirewallEventLog":
		var spec FirewallEventLogSpec
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
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "KernelModule":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "NetworkAdoption":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "NixOSHost":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "Link":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "StatePolicy":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "DHCPv4Lease":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "PPPoEInterface":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "IPv4SourceNAT":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "VirtualIPv4Address":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "VirtualIPv6Address":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "FirewallLog":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "IPv4ReversePathFilter":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "PathMTUPolicy":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	default:
		return fmt.Errorf("unsupported resource kind %s in %s", raw.Kind, r.ID())
	}
	return nil
}

func hasMappingKey(node *yaml.Node, key string) bool {
	return mappingValueNode(node, key) != nil
}

func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
