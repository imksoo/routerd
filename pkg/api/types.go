// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"encoding/json"
	"fmt"
	"maps"

	"gopkg.in/yaml.v3"
)

type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

type ObjectMeta struct {
	Name      string     `yaml:"name" json:"name"`
	OwnerRefs []OwnerRef `yaml:"ownerRefs,omitempty" json:"ownerRefs,omitempty"`
	// Annotations carry non-identifying provenance metadata. They are
	// omitempty so resources without annotations serialize identically to
	// before this field existed. The EventSubscriptionController stamps
	// routerd.net/* provenance keys on dynamic resources it produces.
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
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

// ResourceSpec is the closed set of router configuration variants. The methods
// copy mutable configuration fields without serialization.
// Generation runs from the repository root via make generate-spec.
type ResourceSpec interface {
	cloneSpec() ResourceSpec
}

type Resource struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta     `yaml:"metadata" json:"metadata"`
	Spec     ResourceSpec   `yaml:"spec" json:"spec"`
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
	MobilityAPIVersion      = "mobility.routerd.net/v1alpha1"
)

func (r *Resource) UnmarshalJSON(data []byte) error {
	var raw struct {
		TypeMeta
		Metadata ObjectMeta      `json:"metadata"`
		Spec     json.RawMessage `json:"spec"`
		Status   map[string]any  `json:"status,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	spec, err := decodeResourceSpec(raw.Kind, func(spec ResourceSpec) error { return json.Unmarshal(raw.Spec, spec) })
	if err != nil {
		return fmt.Errorf("%s spec: %w", raw.Kind, err)
	}
	*r = Resource{TypeMeta: raw.TypeMeta, Metadata: raw.Metadata, Spec: spec, Status: raw.Status}
	return nil
}

// Clone copies operator-owned mutable configuration. Status observations are
// read-only shared input, not a second mutable configuration representation.
func (r Resource) Clone() Resource {
	r.Metadata.OwnerRefs = append([]OwnerRef(nil), r.Metadata.OwnerRefs...)
	r.Metadata.Annotations = maps.Clone(r.Metadata.Annotations)
	if r.Spec != nil {
		r.Spec = r.Spec.cloneSpec()
	}
	return r
}

func (r Router) Clone() Router {
	r.Metadata.OwnerRefs = append([]OwnerRef(nil), r.Metadata.OwnerRefs...)
	r.Metadata.Annotations = maps.Clone(r.Metadata.Annotations)
	r.Spec.Apply = cloneApplyPolicySpec(r.Spec.Apply)
	r.Spec.Resources = append([]Resource(nil), r.Spec.Resources...)
	for i := range r.Spec.Resources {
		r.Spec.Resources[i] = r.Spec.Resources[i].Clone()
	}
	return r
}

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
		if hasMappingKey(&raw.Spec, "plugin") {
			return fmt.Errorf("%s spec.plugin is not supported; use type webhook, file, journald, or otlp for log forwarding sinks", r.ID())
		}
	case "LogRetention":
		if hasMappingKey(&raw.Spec, "targets") {
			return fmt.Errorf("%s spec.targets is not supported; use spec.retention with spec.signals and spec.sinks", r.ID())
		}
		if hasMappingKey(&raw.Spec, "incrementalVacuum") {
			return fmt.Errorf("%s spec.incrementalVacuum is not supported; use spec.vacuum", r.ID())
		}
	case "WireGuardInterface":
		for _, field := range []string{"fwmark", "table"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; routerd derives WireGuard fwmark and routing table ownership automatically", r.ID(), field)
			}
		}
	case "TailscaleNode":
		for _, field := range []string{"operator", "binaryPath"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; routerd derives the tailscale binary path and operator handling from the platform", r.ID(), field)
			}
		}
	case "IPsecConnection":
		if err := normalizeIPsecProposalFields(&raw.Spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
	case "PPPoESession":
		if hasMappingKey(&raw.Spec, "socketSource") {
			return fmt.Errorf("%s spec.socketSource is not supported; routerd derives daemon sockets automatically", r.ID())
		}
	case "VirtualAddress":
		if vrrp := mappingValueNode(&raw.Spec, "vrrp"); vrrp != nil {
			for _, field := range []string{"advertInterval", "preemptDelay"} {
				if hasMappingKey(vrrp, field) {
					return fmt.Errorf("%s spec.vrrp.%s is not supported; choose VRRP/CARP behavior with resource intent and routerd profile defaults", r.ID(), field)
				}
			}
		}
	case "BGPRouter":
		if timers := mappingValueNode(&raw.Spec, "timers"); timers != nil {
			for _, field := range []string{"keepalive", "holdTime", "connectRetry"} {
				if hasMappingKey(timers, field) {
					return fmt.Errorf("%s spec.timers.%s is not supported; use spec.timers.profile or routerd BGP defaults", r.ID(), field)
				}
			}
		}
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
	case "BGPDynamicPeer":
		if timers := mappingValueNode(&raw.Spec, "timers"); timers != nil {
			for _, field := range []string{"keepalive", "holdTime", "connectRetry"} {
				if hasMappingKey(timers, field) {
					return fmt.Errorf("%s spec.timers.%s is not supported; use spec.timers.profile or routerd BGP defaults", r.ID(), field)
				}
			}
		}
	case "SAMEnrollmentPolicy":
		if hasMappingKey(&raw.Spec, "revokeAfterInactive") {
			return fmt.Errorf("%s spec.revokeAfterInactive is not supported; use spec.ttl for lease expiry or explicitly revoke accepted claims", r.ID())
		}
	case "DHCPv4Scope":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "DHCPv6PrefixDelegation":
		for _, field := range []string{"iaid", "duidType"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; use spec.profile and let routerd derive DHCPv6 client identity details", r.ID(), field)
			}
		}
	case "DHCPv6Scope":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "DNSResolver":
		if hasMappingKey(&raw.Spec, "sources") {
			return fmt.Errorf("%s spec.sources is not supported; split DNS source intent into DNSForwarder and DNSUpstream resources that reference this DNSResolver", r.ID())
		}
		if queryLog := mappingValueNode(&raw.Spec, "queryLog"); queryLog != nil && hasMappingKey(queryLog, "retention") {
			return fmt.Errorf("%s spec.queryLog.retention is not supported; declare a LogRetention resource for DNS query retention", r.ID())
		}
	case "TrafficFlowLog":
		if hasMappingKey(&raw.Spec, "includeNDPI") {
			return fmt.Errorf("%s spec.includeNDPI is not supported; use spec.includeApplicationLayer", r.ID())
		}
		if hasMappingKey(&raw.Spec, "retention") {
			return fmt.Errorf("%s spec.retention is not supported; declare a LogRetention resource for traffic flow retention", r.ID())
		}
	case "HealthCheck":
		for _, field := range []string{"daemon", "socketSource", "fwmark", "sourceInterface", "sourceAddress", "sourceAddressFrom", "via"} {
			if hasMappingKey(&raw.Spec, field) {
				return fmt.Errorf("%s spec.%s is not supported; routerd derives health-check daemon, source binding, and fwmark from referenced route/interface resources", r.ID(), field)
			}
		}
	case "MobilityPool":
		if err := rejectMobilityPoolRemovedFields(&raw.Spec); err != nil {
			return fmt.Errorf("%s spec: %w", r.ID(), err)
		}
	case "IPv4DefaultRoutePolicy":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "IPv4PolicyRoute":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "IPv4PolicyRouteSet":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "SystemdUnit":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "KernelModule":
		return RemovedLegacyKindError(raw.Kind, r.ID())
	case "NetworkAdoption":
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
	}
	spec, err := decodeResourceSpec(raw.Kind, func(spec ResourceSpec) error { return raw.Spec.Decode(spec) })
	if err != nil {
		return fmt.Errorf("%s spec: %w", r.ID(), err)
	}
	r.Spec = spec

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

func rejectMobilityPoolRemovedFields(spec *yaml.Node) error {
	if hasMappingKey(spec, "deliveryPolicy") {
		return fmt.Errorf("deliveryPolicy is not supported; MobilityPool always uses BGP delivery")
	}
	for _, field := range []string{"mode", "capturePolicy", "authority", "ipOwnershipPolicy", "publishMemberSet"} {
		if hasMappingKey(spec, field) {
			return fmt.Errorf("%s is not supported; MobilityPool derives capture, ownership, and placement from members, discovery, and BGP state", field)
		}
	}
	members := mappingValueNode(spec, "members")
	if members == nil || members.Kind != yaml.SequenceNode {
		return nil
	}
	allowedMemberFields := map[string]bool{
		"nodeRef":              true,
		"profileRef":           true,
		"capture":              true,
		"staticOwnedAddresses": true,
		"ownershipDiscovery":   true,
	}
	topologyFields := map[string]bool{
		"site":            true,
		"role":            true,
		"placement":       true,
		"maintenance":     true,
		"maxSecondaryIPs": true,
		"eventEndpoint":   true,
		"samEndpoint":     true,
		"macAddresses":    true,
		"routeReflector":  true,
		"wireGuard":       true,
	}
	for i, member := range members.Content {
		if member.Kind != yaml.MappingNode {
			continue
		}
		for fieldIndex := 0; fieldIndex+1 < len(member.Content); fieldIndex += 2 {
			field := member.Content[fieldIndex].Value
			switch field {
			case "delivery", "deliveryTo", "deliveryTargets":
				return fmt.Errorf("members[%d].%s is not supported; BGP mobility derives transport from SAMTransportProfile and OverlayPeer resources", i, field)
			}
			if topologyFields[field] {
				return fmt.Errorf("members[%d].%s is not supported; shared identity and topology belong in SAMNodeSet", i, field)
			}
			if !allowedMemberFields[field] {
				return fmt.Errorf("members[%d].%s is not supported; MobilityPool members are local provider/L2 overlays only", i, field)
			}
		}
	}
	return nil
}

// normalizeIPsecProposalFields accepts the historical psPhase* spelling while
// publishing the canonical phase* API names.  Renaming the YAML node before
// decoding keeps existing Linux and FreeBSD configurations usable without
// letting both spellings silently choose one value.
func normalizeIPsecProposalFields(node *yaml.Node) error {
	for legacy, canonical := range map[string]string{
		"psPhase1Proposals": "phase1Proposals",
		"psPhase2Proposals": "phase2Proposals",
	} {
		if !hasMappingKey(node, legacy) {
			continue
		}
		if hasMappingKey(node, canonical) {
			return fmt.Errorf("%s and deprecated %s cannot both be set", canonical, legacy)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == legacy {
				node.Content[i].Value = canonical
				break
			}
		}
	}
	return nil
}
