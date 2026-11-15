// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

type ObjectMeta struct {
	Name string `yaml:"name" json:"name"`
	// Annotations carry non-identifying provenance metadata. They are
	// omitempty so resources without annotations serialize identically to
	// before this field existed. The EventSubscriptionController stamps
	// routerd.net/* provenance keys on dynamic resources it produces.
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

type Router struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta `yaml:"metadata" json:"metadata"`
	Spec     RouterSpec `yaml:"spec" json:"spec"`
}

type RouterSpec struct {
	SSHHostKeys []SSHHostKeySpec `yaml:"sshHostKeys,omitempty" json:"sshHostKeys,omitempty"`
	Apply       ApplyPolicySpec  `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`
	Resources   []Resource       `yaml:"resources" json:"resources"`
}

type SSHHostKeySpec struct {
	PrivateKey string `yaml:"privateKey" json:"privateKey"`
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
	MobilityAPIVersion      = "mobility.routerd.net/v1alpha1"
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

	specType, ok := specTypesByKind[raw.Kind]
	if !ok {
		return fmt.Errorf("unsupported resource kind %s in %s", raw.Kind, r.ID())
	}
	spec := reflect.New(specType)
	if err := raw.Spec.Decode(spec.Interface()); err != nil {
		return fmt.Errorf("%s spec: %w", r.ID(), err)
	}
	r.Spec = spec.Elem().Interface()
	return nil
}

// specTypesByKind maps each decodable Kind to its spec type. The configurable
// Kinds come from ConfigResourceKinds; the rest are decoded but not listed there.
var specTypesByKind = func() map[string]reflect.Type {
	types := map[string]reflect.Type{}
	for _, kind := range ConfigResourceKinds() {
		types[kind.Kind] = reflect.TypeOf(kind.Spec)
	}
	for kind, spec := range map[string]any{
		"Inventory":       InventorySpec{},
		"Bridge":          BridgeSpec{},
		"VXLANSegment":    VXLANSegmentSpec{},
		"IPv4StaticRoute": IPv4StaticRouteSpec{},
		"IPv6StaticRoute": IPv6StaticRouteSpec{},
		"SAMPeerGroup":    SAMPeerGroupSpec{},
		"SAMRRSet":        SAMRRSetSpec{},
	} {
		types[kind] = reflect.TypeOf(spec)
	}
	return types
}()
