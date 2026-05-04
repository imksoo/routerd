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
	RouterAPIVersion   = "routerd.net/v1alpha1"
	NetAPIVersion      = "net.routerd.net/v1alpha1"
	SystemAPIVersion   = "system.routerd.net/v1alpha1"
	FirewallAPIVersion = "firewall.routerd.net/v1alpha1"
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
	case "NetworkAdoption":
		var spec NetworkAdoptionSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "SystemdUnit":
		var spec SystemdUnitSpec
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
	case "WebConsole":
		var spec WebConsoleSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "NixOSHost":
		var spec NixOSHostSpec
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
	case "Link":
		var spec LinkSpec
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
	case "PPPoEInterface":
		var spec PPPoEInterfaceSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "PPPoESession":
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
	case "DHCPv4Address":
		var spec DHCPv4AddressSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv4Lease":
		var spec DHCPv4LeaseSpec
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
		var spec DHCPv4ScopeSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
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
		var spec DHCPv6ScopeSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
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
		var spec DNSResolverSpec
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
	case "StatePolicy":
		var spec StatePolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "HealthCheck":
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
		var spec IPv4DefaultRoutePolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4SourceNAT":
		var spec IPv4SourceNATSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "NAT44Rule":
		var spec NAT44RuleSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4PolicyRoute":
		var spec IPv4PolicyRouteSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4PolicyRouteSet":
		var spec IPv4PolicyRouteSetSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4ReversePathFilter":
		var spec IPv4ReversePathFilterSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "PathMTUPolicy":
		var spec PathMTUPolicySpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
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
	default:
		var spec map[string]any
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	}
	return nil
}
