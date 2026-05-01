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
	Name string `yaml:"name" json:"name"`
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
	case "Sysctl":
		var spec SysctlSpec
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
	case "PPPoEInterface":
		var spec PPPoEInterfaceSpec
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
	case "IPv4DHCPAddress":
		var spec IPv4DHCPAddressSpec
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
	case "IPv4DHCPServer":
		var spec IPv4DHCPServerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv4DHCPScope":
		var spec IPv4DHCPScopeSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "DHCPv4HostReservation":
		var spec DHCPv4HostReservationSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv6DHCPAddress":
		var spec IPv6DHCPAddressSpec
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
	case "IPv6PrefixDelegation":
		var spec IPv6PrefixDelegationSpec
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
	case "IPv6DHCPServer":
		var spec IPv6DHCPServerSpec
		if err := raw.Spec.Decode(&spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
		r.Spec = spec
	case "IPv6DHCPScope":
		var spec IPv6DHCPScopeSpec
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
	case "DNSConditionalForwarder":
		var spec DNSConditionalForwarderSpec
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
	case "Zone":
		var spec ZoneSpec
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
	case "ExposeService":
		var spec ExposeServiceSpec
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
