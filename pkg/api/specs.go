package api

import "fmt"

type SysctlSpec struct {
	Key        string `yaml:"key" json:"key" jsonschema:"title=Key"`
	Value      string `yaml:"value" json:"value" jsonschema:"title=Value"`
	Runtime    *bool  `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Persistent bool   `yaml:"persistent,omitempty" json:"persistent,omitempty"`
}

type InterfaceSpec struct {
	IfName  string `yaml:"ifname" json:"ifname"`
	AdminUp bool   `yaml:"adminUp,omitempty" json:"adminUp,omitempty"`
	Managed bool   `yaml:"managed" json:"managed"`
	Owner   string `yaml:"owner,omitempty" json:"owner,omitempty" jsonschema:"enum=routerd,enum=external"`
}

type IPv4StaticAddressSpec struct {
	Interface          string `yaml:"interface" json:"interface"`
	Address            string `yaml:"address" json:"address"`
	Exclusive          bool   `yaml:"exclusive,omitempty" json:"exclusive,omitempty"`
	AllowOverlap       bool   `yaml:"allowOverlap,omitempty" json:"allowOverlap,omitempty"`
	AllowOverlapReason string `yaml:"allowOverlapReason,omitempty" json:"allowOverlapReason,omitempty"`
}

type IPv4DHCPAddressSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	Client    string `yaml:"client,omitempty" json:"client,omitempty"`
	Required  bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type IPv4DHCPServerSpec struct {
	Server  string `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq,enum=kea,enum=dhcpd"`
	Managed bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
}

type IPv4DHCPScopeSpec struct {
	Server        string   `yaml:"server" json:"server"`
	Interface     string   `yaml:"interface" json:"interface"`
	RangeStart    string   `yaml:"rangeStart" json:"rangeStart"`
	RangeEnd      string   `yaml:"rangeEnd" json:"rangeEnd"`
	LeaseTime     string   `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	RouterSource  string   `yaml:"routerSource,omitempty" json:"routerSource,omitempty" jsonschema:"enum=interfaceAddress,enum=static,enum=none"`
	Router        string   `yaml:"router,omitempty" json:"router,omitempty"`
	DNSSource     string   `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=dhcp4,enum=static,enum=self,enum=none"`
	DNSInterface  string   `yaml:"dnsInterface,omitempty" json:"dnsInterface,omitempty"`
	DNSServers    []string `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	Authoritative bool     `yaml:"authoritative,omitempty" json:"authoritative,omitempty"`
}

type IPv6DHCPAddressSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	Client    string `yaml:"client,omitempty" json:"client,omitempty"`
	Required  bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type IPv4DefaultRouteSpec struct {
	Interface     string `yaml:"interface" json:"interface"`
	GatewaySource string `yaml:"gatewaySource" json:"gatewaySource" jsonschema:"enum=dhcp4,enum=static"`
	Gateway       string `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	Required      bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type IPv4SourceNATSpec struct {
	OutboundInterface string                 `yaml:"outboundInterface" json:"outboundInterface"`
	SourceCIDRs       []string               `yaml:"sourceCIDRs" json:"sourceCIDRs"`
	Translation       IPv4NATTranslationSpec `yaml:"translation" json:"translation"`
}

type IPv4NATTranslationSpec struct {
	Type        string                 `yaml:"type" json:"type" jsonschema:"enum=interfaceAddress,enum=address,enum=pool"`
	Address     string                 `yaml:"address,omitempty" json:"address,omitempty"`
	Addresses   []string               `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	PortMapping IPv4NATPortMappingSpec `yaml:"portMapping,omitempty" json:"portMapping,omitempty"`
}

type IPv4NATPortMappingSpec struct {
	Type  string `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=auto,enum=preserve,enum=range"`
	Start int    `yaml:"start,omitempty" json:"start,omitempty" jsonschema:"minimum=1,maximum=65535"`
	End   int    `yaml:"end,omitempty" json:"end,omitempty" jsonschema:"minimum=1,maximum=65535"`
}

type HostnameSpec struct {
	Hostname string `yaml:"hostname" json:"hostname"`
	Managed  bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
}

func (r Resource) SysctlSpec() (SysctlSpec, error) {
	return specAs[SysctlSpec](r)
}

func (r Resource) InterfaceSpec() (InterfaceSpec, error) {
	return specAs[InterfaceSpec](r)
}

func (r Resource) IPv4StaticAddressSpec() (IPv4StaticAddressSpec, error) {
	return specAs[IPv4StaticAddressSpec](r)
}

func (r Resource) IPv4DHCPAddressSpec() (IPv4DHCPAddressSpec, error) {
	return specAs[IPv4DHCPAddressSpec](r)
}

func (r Resource) IPv4DHCPServerSpec() (IPv4DHCPServerSpec, error) {
	return specAs[IPv4DHCPServerSpec](r)
}

func (r Resource) IPv4DHCPScopeSpec() (IPv4DHCPScopeSpec, error) {
	return specAs[IPv4DHCPScopeSpec](r)
}

func (r Resource) IPv6DHCPAddressSpec() (IPv6DHCPAddressSpec, error) {
	return specAs[IPv6DHCPAddressSpec](r)
}

func (r Resource) IPv4DefaultRouteSpec() (IPv4DefaultRouteSpec, error) {
	return specAs[IPv4DefaultRouteSpec](r)
}

func (r Resource) IPv4SourceNATSpec() (IPv4SourceNATSpec, error) {
	return specAs[IPv4SourceNATSpec](r)
}

func (r Resource) HostnameSpec() (HostnameSpec, error) {
	return specAs[HostnameSpec](r)
}

func specAs[T any](r Resource) (T, error) {
	spec, ok := r.Spec.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s has unexpected spec type %T", r.ID(), r.Spec)
	}
	return spec, nil
}

func BoolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
