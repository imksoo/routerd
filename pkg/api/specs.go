package api

import "fmt"

type LogSinkSpec struct {
	Type     string            `yaml:"type" json:"type" jsonschema:"enum=syslog,enum=plugin"`
	Enabled  *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinLevel string            `yaml:"minLevel,omitempty" json:"minLevel,omitempty" jsonschema:"enum=debug,enum=info,enum=warning,enum=error"`
	Syslog   LogSinkSyslogSpec `yaml:"syslog,omitempty" json:"syslog,omitempty"`
	Plugin   LogSinkPluginSpec `yaml:"plugin,omitempty" json:"plugin,omitempty"`
}

type LogSinkSyslogSpec struct {
	Network  string `yaml:"network,omitempty" json:"network,omitempty" jsonschema:"enum=,enum=unix,enum=unixgram,enum=tcp,enum=udp"`
	Address  string `yaml:"address,omitempty" json:"address,omitempty"`
	Facility string `yaml:"facility,omitempty" json:"facility,omitempty" jsonschema:"enum=kern,enum=user,enum=mail,enum=daemon,enum=auth,enum=syslog,enum=lpr,enum=news,enum=uucp,enum=cron,enum=authpriv,enum=ftp,enum=local0,enum=local1,enum=local2,enum=local3,enum=local4,enum=local5,enum=local6,enum=local7"`
	Tag      string `yaml:"tag,omitempty" json:"tag,omitempty"`
}

type LogSinkPluginSpec struct {
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

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
	Server  string                `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq,enum=kea,enum=dhcpd"`
	Managed bool                  `yaml:"managed,omitempty" json:"managed,omitempty"`
	DNS     IPv4DHCPServerDNSSpec `yaml:"dns,omitempty" json:"dns,omitempty"`
}

type IPv4DHCPServerDNSSpec struct {
	Enabled           bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	UpstreamSource    string   `yaml:"upstreamSource,omitempty" json:"upstreamSource,omitempty" jsonschema:"enum=dhcp4,enum=static,enum=system,enum=none"`
	UpstreamInterface string   `yaml:"upstreamInterface,omitempty" json:"upstreamInterface,omitempty"`
	UpstreamServers   []string `yaml:"upstreamServers,omitempty" json:"upstreamServers,omitempty"`
	CacheSize         int      `yaml:"cacheSize,omitempty" json:"cacheSize,omitempty" jsonschema:"minimum=0"`
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

type IPv6PrefixDelegationSpec struct {
	Interface    string `yaml:"interface" json:"interface"`
	Client       string `yaml:"client,omitempty" json:"client,omitempty"`
	Profile      string `yaml:"profile,omitempty" json:"profile,omitempty" jsonschema:"enum=default,enum=ntt-ngn-direct-hikari-denwa,enum=ntt-hgw-lan-pd"`
	PrefixLength int    `yaml:"prefixLength,omitempty" json:"prefixLength,omitempty" jsonschema:"minimum=1,maximum=128"`
	Required     bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type IPv6DelegatedAddressSpec struct {
	PrefixDelegation string `yaml:"prefixDelegation" json:"prefixDelegation"`
	Interface        string `yaml:"interface" json:"interface"`
	SubnetID         string `yaml:"subnetID,omitempty" json:"subnetID,omitempty"`
	AddressSuffix    string `yaml:"addressSuffix" json:"addressSuffix"`
	SendRA           bool   `yaml:"sendRA,omitempty" json:"sendRA,omitempty"`
	Announce         bool   `yaml:"announce,omitempty" json:"announce,omitempty"`
}

type IPv6DHCPServerSpec struct {
	Server  string `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq"`
	Managed bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
}

type IPv6DHCPScopeSpec struct {
	Server            string   `yaml:"server" json:"server"`
	DelegatedAddress  string   `yaml:"delegatedAddress" json:"delegatedAddress"`
	Mode              string   `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=stateless,enum=ra-only"`
	LeaseTime         string   `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	DefaultRoute      bool     `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	DNSSource         string   `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=self,enum=static,enum=none"`
	SelfAddressPolicy string   `yaml:"selfAddressPolicy,omitempty" json:"selfAddressPolicy,omitempty"`
	DNSServers        []string `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
}

type SelfAddressPolicySpec struct {
	AddressFamily string                       `yaml:"addressFamily" json:"addressFamily" jsonschema:"enum=ipv4,enum=ipv6"`
	Candidates    []SelfAddressPolicyCandidate `yaml:"candidates" json:"candidates"`
}

type SelfAddressPolicyCandidate struct {
	Source           string `yaml:"source" json:"source" jsonschema:"enum=delegatedAddress,enum=interfaceAddress,enum=static"`
	Interface        string `yaml:"interface,omitempty" json:"interface,omitempty"`
	DelegatedAddress string `yaml:"delegatedAddress,omitempty" json:"delegatedAddress,omitempty"`
	Address          string `yaml:"address,omitempty" json:"address,omitempty"`
	AddressSuffix    string `yaml:"addressSuffix,omitempty" json:"addressSuffix,omitempty"`
	MatchSuffix      string `yaml:"matchSuffix,omitempty" json:"matchSuffix,omitempty"`
	Ordinal          int    `yaml:"ordinal,omitempty" json:"ordinal,omitempty" jsonschema:"minimum=1"`
}

type DNSConditionalForwarderSpec struct {
	Domain            string   `yaml:"domain" json:"domain"`
	UpstreamSource    string   `yaml:"upstreamSource,omitempty" json:"upstreamSource,omitempty" jsonschema:"enum=static,enum=dhcp4,enum=dhcp6"`
	UpstreamInterface string   `yaml:"upstreamInterface,omitempty" json:"upstreamInterface,omitempty"`
	UpstreamServers   []string `yaml:"upstreamServers,omitempty" json:"upstreamServers,omitempty"`
}

type DSLiteTunnelSpec struct {
	Interface             string   `yaml:"interface" json:"interface"`
	TunnelName            string   `yaml:"tunnelName,omitempty" json:"tunnelName,omitempty"`
	AFTRFQDN              string   `yaml:"aftrFQDN,omitempty" json:"aftrFQDN,omitempty"`
	AFTRDNSServers        []string `yaml:"aftrDNSServers,omitempty" json:"aftrDNSServers,omitempty"`
	AFTRAddressOrdinal    int      `yaml:"aftrAddressOrdinal,omitempty" json:"aftrAddressOrdinal,omitempty" jsonschema:"minimum=1"`
	AFTRAddressSelection  string   `yaml:"aftrAddressSelection,omitempty" json:"aftrAddressSelection,omitempty" jsonschema:"enum=ordinal,enum=ordinalModulo"`
	RemoteAddress         string   `yaml:"remoteAddress,omitempty" json:"remoteAddress,omitempty"`
	LocalAddress          string   `yaml:"localAddress,omitempty" json:"localAddress,omitempty"`
	LocalAddressSource    string   `yaml:"localAddressSource,omitempty" json:"localAddressSource,omitempty" jsonschema:"enum=interface,enum=static,enum=delegatedAddress"`
	LocalDelegatedAddress string   `yaml:"localDelegatedAddress,omitempty" json:"localDelegatedAddress,omitempty"`
	LocalAddressSuffix    string   `yaml:"localAddressSuffix,omitempty" json:"localAddressSuffix,omitempty"`
	DefaultRoute          bool     `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	RouteMetric           int      `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	MTU                   int      `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=1280,maximum=65535"`
	EncapsulationLimit    string   `yaml:"encapsulationLimit,omitempty" json:"encapsulationLimit,omitempty"`
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

type IPv4PolicyRouteSpec struct {
	OutboundInterface   string   `yaml:"outboundInterface" json:"outboundInterface"`
	Table               int      `yaml:"table" json:"table" jsonschema:"minimum=1,maximum=4294967295"`
	Priority            int      `yaml:"priority" json:"priority" jsonschema:"minimum=1,maximum=32765"`
	Mark                int      `yaml:"mark" json:"mark" jsonschema:"minimum=1,maximum=4294967295"`
	SourceCIDRs         []string `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs    []string `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	RouteMetric         int      `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	AllowLocalSourceNAT bool     `yaml:"allowLocalSourceNAT,omitempty" json:"allowLocalSourceNAT,omitempty"`
}

type IPv4PolicyRouteSetSpec struct {
	Mode             string                  `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=hash"`
	HashFields       []string                `yaml:"hashFields,omitempty" json:"hashFields,omitempty"`
	SourceCIDRs      []string                `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs []string                `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	Targets          []IPv4PolicyRouteTarget `yaml:"targets" json:"targets"`
}

type IPv4PolicyRouteTarget struct {
	Name              string `yaml:"name,omitempty" json:"name,omitempty"`
	OutboundInterface string `yaml:"outboundInterface" json:"outboundInterface"`
	Table             int    `yaml:"table" json:"table" jsonschema:"minimum=1,maximum=4294967295"`
	Priority          int    `yaml:"priority" json:"priority" jsonschema:"minimum=1,maximum=32765"`
	Mark              int    `yaml:"mark" json:"mark" jsonschema:"minimum=1,maximum=4294967295"`
	RouteMetric       int    `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
}

type IPv4ReversePathFilterSpec struct {
	Target    string `yaml:"target" json:"target" jsonschema:"enum=all,enum=default,enum=interface"`
	Interface string `yaml:"interface,omitempty" json:"interface,omitempty"`
	Mode      string `yaml:"mode" json:"mode" jsonschema:"enum=disabled,enum=strict,enum=loose"`
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

func (r Resource) IPv6PrefixDelegationSpec() (IPv6PrefixDelegationSpec, error) {
	return specAs[IPv6PrefixDelegationSpec](r)
}

func (r Resource) IPv6DelegatedAddressSpec() (IPv6DelegatedAddressSpec, error) {
	return specAs[IPv6DelegatedAddressSpec](r)
}

func (r Resource) IPv6DHCPServerSpec() (IPv6DHCPServerSpec, error) {
	return specAs[IPv6DHCPServerSpec](r)
}

func (r Resource) IPv6DHCPScopeSpec() (IPv6DHCPScopeSpec, error) {
	return specAs[IPv6DHCPScopeSpec](r)
}

func (r Resource) SelfAddressPolicySpec() (SelfAddressPolicySpec, error) {
	return specAs[SelfAddressPolicySpec](r)
}

func (r Resource) DNSConditionalForwarderSpec() (DNSConditionalForwarderSpec, error) {
	return specAs[DNSConditionalForwarderSpec](r)
}

func (r Resource) DSLiteTunnelSpec() (DSLiteTunnelSpec, error) {
	return specAs[DSLiteTunnelSpec](r)
}

func (r Resource) IPv4DefaultRouteSpec() (IPv4DefaultRouteSpec, error) {
	return specAs[IPv4DefaultRouteSpec](r)
}

func (r Resource) IPv4SourceNATSpec() (IPv4SourceNATSpec, error) {
	return specAs[IPv4SourceNATSpec](r)
}

func (r Resource) IPv4PolicyRouteSpec() (IPv4PolicyRouteSpec, error) {
	return specAs[IPv4PolicyRouteSpec](r)
}

func (r Resource) IPv4PolicyRouteSetSpec() (IPv4PolicyRouteSetSpec, error) {
	return specAs[IPv4PolicyRouteSetSpec](r)
}

func (r Resource) IPv4ReversePathFilterSpec() (IPv4ReversePathFilterSpec, error) {
	return specAs[IPv4ReversePathFilterSpec](r)
}

func (r Resource) LogSinkSpec() (LogSinkSpec, error) {
	return specAs[LogSinkSpec](r)
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
