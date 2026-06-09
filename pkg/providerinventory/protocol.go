// SPDX-License-Identifier: BSD-3-Clause

// Package providerinventory implements the read-only cloud private-IP
// inventory path used by CloudEdge mobility ownership discovery.
package providerinventory

import "github.com/imksoo/routerd/pkg/plugin"

const (
	ProtocolAPIVersion = "providerinventory.routerd.net/v1alpha1"

	KindObservePrivateIPsRequest = "ObservePrivateIPsRequest"
	KindObservePrivateIPsResult  = "ObservePrivateIPsResult"

	CapabilityObserveProviderPrivateIPs = "observe.providerPrivateIPs"

	ResultSucceeded = "succeeded"
	ResultFailed    = "failed"
	ResultSkipped   = "skipped"
)

type TypeMeta struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
}

type ObservePrivateIPsRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Spec     ObservePrivateIPsRequestSpec `json:"spec" yaml:"spec"`
}

type ObservePrivateIPsRequestSpec struct {
	Provider      string               `json:"provider" yaml:"provider"`
	ProviderRef   string               `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	Strategy      string               `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	SelfNode      string               `json:"selfNode" yaml:"selfNode"`
	Pool          string               `json:"pool" yaml:"pool"`
	Prefix        string               `json:"prefix" yaml:"prefix"`
	SelfNICRef    string               `json:"selfNicRef" yaml:"selfNicRef"`
	SubnetRef     string               `json:"subnetRef,omitempty" yaml:"subnetRef,omitempty"`
	RouteTableRef string               `json:"routeTableRef,omitempty" yaml:"routeTableRef,omitempty"`
	Target        map[string]string    `json:"target,omitempty" yaml:"target,omitempty"`
	Selector      InventorySelector    `json:"selector,omitempty" yaml:"selector,omitempty"`
	Context       plugin.PluginContext `json:"context,omitempty" yaml:"context,omitempty"`
}

type InventorySelector struct {
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type ObservePrivateIPsResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Status   ObservePrivateIPsResultStatus `json:"status" yaml:"status"`
}

type ObservePrivateIPsResultStatus struct {
	Status             string            `json:"status" yaml:"status"`
	Message            string            `json:"message,omitempty" yaml:"message,omitempty"`
	Error              string            `json:"error,omitempty" yaml:"error,omitempty"`
	Self               *PrivateIPSelf    `json:"self,omitempty" yaml:"self,omitempty"`
	IPs                []PrivateIPRecord `json:"ips,omitempty" yaml:"ips,omitempty"`
	ObservedCandidates []PrivateIPRecord `json:"observedCandidates,omitempty" yaml:"observedCandidates,omitempty"`
	LocalIPs           []PrivateIPRecord `json:"localIPs,omitempty" yaml:"localIPs,omitempty"`
	Routes             []RouteRecord     `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type PrivateIPSelf struct {
	NICRef            string   `json:"nicRef,omitempty" yaml:"nicRef,omitempty"`
	SubnetRef         string   `json:"subnetRef,omitempty" yaml:"subnetRef,omitempty"`
	PrivateIPs        []string `json:"privateIPs,omitempty" yaml:"privateIPs,omitempty"`
	CapturedAddresses []string `json:"capturedAddresses,omitempty" yaml:"capturedAddresses,omitempty"`
	ForwardingEnabled *bool    `json:"forwardingEnabled,omitempty" yaml:"forwardingEnabled,omitempty"`
}

type PrivateIPRecord struct {
	Address       string            `json:"address" yaml:"address"`
	NICRef        string            `json:"nicRef,omitempty" yaml:"nicRef,omitempty"`
	SubnetRef     string            `json:"subnetRef,omitempty" yaml:"subnetRef,omitempty"`
	VPCRef        string            `json:"vpcRef,omitempty" yaml:"vpcRef,omitempty"`
	ProviderRef   string            `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	ResourceRef   string            `json:"resourceRef,omitempty" yaml:"resourceRef,omitempty"`
	ResourceType  string            `json:"resourceType,omitempty" yaml:"resourceType,omitempty"`
	Primary       bool              `json:"primary,omitempty" yaml:"primary,omitempty"`
	Tags          map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	InstanceState string            `json:"instanceState,omitempty" yaml:"instanceState,omitempty"`
}

type RouteRecord struct {
	Address       string            `json:"address" yaml:"address"`
	RouteTableRef string            `json:"routeTableRef,omitempty" yaml:"routeTableRef,omitempty"`
	NextHopNICRef string            `json:"nextHopNicRef,omitempty" yaml:"nextHopNicRef,omitempty"`
	NextHopRef    string            `json:"nextHopRef,omitempty" yaml:"nextHopRef,omitempty"`
	Tags          map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	InstanceState string            `json:"instanceState,omitempty" yaml:"instanceState,omitempty"`
}

func NewObservePrivateIPsRequest(spec ObservePrivateIPsRequestSpec) ObservePrivateIPsRequest {
	return ObservePrivateIPsRequest{
		TypeMeta: TypeMeta{APIVersion: ProtocolAPIVersion, Kind: KindObservePrivateIPsRequest},
		Spec:     spec,
	}
}

func (s ObservePrivateIPsResultStatus) ObservedCandidateRecords() []PrivateIPRecord {
	if len(s.ObservedCandidates) > 0 {
		return append([]PrivateIPRecord(nil), s.ObservedCandidates...)
	}
	return append([]PrivateIPRecord(nil), s.IPs...)
}

func (s ObservePrivateIPsResultStatus) LocalInventoryRecords() []PrivateIPRecord {
	if len(s.LocalIPs) > 0 {
		return append([]PrivateIPRecord(nil), s.LocalIPs...)
	}
	if len(s.ObservedCandidates) > 0 {
		return append([]PrivateIPRecord(nil), s.ObservedCandidates...)
	}
	return append([]PrivateIPRecord(nil), s.IPs...)
}

func hasCapability(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}
