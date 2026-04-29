package api

import "fmt"

type LogSinkSpec struct {
	Type     string            `yaml:"type" json:"type" jsonschema:"enum=syslog,enum=plugin"`
	Enabled  *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinLevel string            `yaml:"minLevel,omitempty" json:"minLevel,omitempty" jsonschema:"enum=debug,enum=info,enum=warning,enum=error"`
	Syslog   LogSinkSyslogSpec `yaml:"syslog,omitempty" json:"syslog,omitempty"`
	Plugin   LogSinkPluginSpec `yaml:"plugin,omitempty" json:"plugin,omitempty"`
}

type ApplyPolicySpec struct {
	Mode                string   `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=strict,enum=progressive"`
	ProtectedInterfaces []string `yaml:"protectedInterfaces,omitempty" json:"protectedInterfaces,omitempty"`
	ProtectedZones      []string `yaml:"protectedZones,omitempty" json:"protectedZones,omitempty"`
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

type NTPClientSpec struct {
	Provider  string   `yaml:"provider,omitempty" json:"provider,omitempty" jsonschema:"enum=systemd-timesyncd"`
	Managed   bool     `yaml:"managed,omitempty" json:"managed,omitempty"`
	Source    string   `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"enum=static"`
	Interface string   `yaml:"interface,omitempty" json:"interface,omitempty"`
	Servers   []string `yaml:"servers,omitempty" json:"servers,omitempty"`
}

type NixOSHostSpec struct {
	Hostname              string                  `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Domain                string                  `yaml:"domain,omitempty" json:"domain,omitempty"`
	StateVersion          string                  `yaml:"stateVersion,omitempty" json:"stateVersion,omitempty"`
	Boot                  NixOSBootSpec           `yaml:"boot,omitempty" json:"boot,omitempty"`
	Users                 []NixOSUserSpec         `yaml:"users,omitempty" json:"users,omitempty"`
	SSH                   NixOSSSHSpec            `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Sudo                  NixOSSudoSpec           `yaml:"sudo,omitempty" json:"sudo,omitempty"`
	RouterdService        NixOSRouterdServiceSpec `yaml:"routerdService,omitempty" json:"routerdService,omitempty"`
	DebugSystemPackages   bool                    `yaml:"debugSystemPackages,omitempty" json:"debugSystemPackages,omitempty"`
	AdditionalPackages    []string                `yaml:"additionalPackages,omitempty" json:"additionalPackages,omitempty"`
	AdditionalServicePath []string                `yaml:"additionalServicePath,omitempty" json:"additionalServicePath,omitempty"`
}

type NixOSBootSpec struct {
	Loader     string `yaml:"loader,omitempty" json:"loader,omitempty" jsonschema:"enum=,enum=grub"`
	GrubDevice string `yaml:"grubDevice,omitempty" json:"grubDevice,omitempty"`
}

type NixOSUserSpec struct {
	Name              string   `yaml:"name" json:"name"`
	Description       string   `yaml:"description,omitempty" json:"description,omitempty"`
	Groups            []string `yaml:"groups,omitempty" json:"groups,omitempty"`
	InitialPassword   string   `yaml:"initialPassword,omitempty" json:"initialPassword,omitempty"`
	SSHAuthorizedKeys []string `yaml:"sshAuthorizedKeys,omitempty" json:"sshAuthorizedKeys,omitempty"`
}

type NixOSSSHSpec struct {
	Enabled                *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	PasswordAuthentication *bool  `yaml:"passwordAuthentication,omitempty" json:"passwordAuthentication,omitempty"`
	PermitRootLogin        string `yaml:"permitRootLogin,omitempty" json:"permitRootLogin,omitempty" jsonschema:"enum=,enum=no,enum=yes,enum=prohibit-password,enum=forced-commands-only"`
}

type NixOSSudoSpec struct {
	WheelNeedsPassword *bool `yaml:"wheelNeedsPassword,omitempty" json:"wheelNeedsPassword,omitempty"`
}

type NixOSRouterdServiceSpec struct {
	Enabled       *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	BinaryPath    string   `yaml:"binaryPath,omitempty" json:"binaryPath,omitempty"`
	ConfigFile    string   `yaml:"configFile,omitempty" json:"configFile,omitempty"`
	Socket        string   `yaml:"socket,omitempty" json:"socket,omitempty"`
	ApplyInterval string   `yaml:"applyInterval,omitempty" json:"applyInterval,omitempty"`
	ExtraFlags    []string `yaml:"extraFlags,omitempty" json:"extraFlags,omitempty"`
}

type InterfaceSpec struct {
	IfName  string `yaml:"ifname" json:"ifname"`
	AdminUp bool   `yaml:"adminUp,omitempty" json:"adminUp,omitempty"`
	Managed bool   `yaml:"managed" json:"managed"`
	Owner   string `yaml:"owner,omitempty" json:"owner,omitempty" jsonschema:"enum=routerd,enum=external"`
}

type PPPoEInterfaceSpec struct {
	Interface      string `yaml:"interface" json:"interface"`
	IfName         string `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	Username       string `yaml:"username" json:"username"`
	Password       string `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFile   string `yaml:"passwordFile,omitempty" json:"passwordFile,omitempty"`
	ServiceName    string `yaml:"serviceName,omitempty" json:"serviceName,omitempty"`
	ACName         string `yaml:"acName,omitempty" json:"acName,omitempty"`
	DefaultRoute   bool   `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	UsePeerDNS     bool   `yaml:"usePeerDNS,omitempty" json:"usePeerDNS,omitempty"`
	Persist        *bool  `yaml:"persist,omitempty" json:"persist,omitempty"`
	MTU            int    `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=576,maximum=1500"`
	MRU            int    `yaml:"mru,omitempty" json:"mru,omitempty" jsonschema:"minimum=576,maximum=1500"`
	LCPInterval    int    `yaml:"lcpInterval,omitempty" json:"lcpInterval,omitempty" jsonschema:"minimum=0"`
	LCPFailure     int    `yaml:"lcpFailure,omitempty" json:"lcpFailure,omitempty" jsonschema:"minimum=0"`
	IPv6           bool   `yaml:"ipv6,omitempty" json:"ipv6,omitempty"`
	Managed        bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
	SecretEncoding string `yaml:"secretEncoding,omitempty" json:"secretEncoding,omitempty" jsonschema:"enum=plain"`
}

type IPv4StaticAddressSpec struct {
	Interface          string `yaml:"interface" json:"interface"`
	Address            string `yaml:"address" json:"address"`
	Exclusive          bool   `yaml:"exclusive,omitempty" json:"exclusive,omitempty"`
	AllowOverlap       bool   `yaml:"allowOverlap,omitempty" json:"allowOverlap,omitempty"`
	AllowOverlapReason string `yaml:"allowOverlapReason,omitempty" json:"allowOverlapReason,omitempty"`
}

type IPv4DHCPAddressSpec struct {
	Interface   string `yaml:"interface" json:"interface"`
	Client      string `yaml:"client,omitempty" json:"client,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	UseRoutes   *bool  `yaml:"useRoutes,omitempty" json:"useRoutes,omitempty"`
	UseDNS      *bool  `yaml:"useDNS,omitempty" json:"useDNS,omitempty"`
	RouteMetric int    `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
}

type IPv4DHCPServerSpec struct {
	Server           string                `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq,enum=kea,enum=dhcpd"`
	Managed          bool                  `yaml:"managed,omitempty" json:"managed,omitempty"`
	ListenInterfaces []string              `yaml:"listenInterfaces,omitempty" json:"listenInterfaces,omitempty"`
	DNS              IPv4DHCPServerDNSSpec `yaml:"dns,omitempty" json:"dns,omitempty"`
}

type IPv4DHCPServerDNSSpec struct {
	Enabled           bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	UpstreamSource    string   `yaml:"upstreamSource,omitempty" json:"upstreamSource,omitempty" jsonschema:"enum=dhcp4,enum=static,enum=system,enum=none"`
	UpstreamInterface string   `yaml:"upstreamInterface,omitempty" json:"upstreamInterface,omitempty"`
	UpstreamServers   []string `yaml:"upstreamServers,omitempty" json:"upstreamServers,omitempty"`
	CacheSize         int      `yaml:"cacheSize,omitempty" json:"cacheSize,omitempty" jsonschema:"minimum=0"`
}

type IPv4DHCPScopeSpec struct {
	Server        string           `yaml:"server" json:"server"`
	Interface     string           `yaml:"interface" json:"interface"`
	RangeStart    string           `yaml:"rangeStart" json:"rangeStart"`
	RangeEnd      string           `yaml:"rangeEnd" json:"rangeEnd"`
	LeaseTime     string           `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	RouterSource  string           `yaml:"routerSource,omitempty" json:"routerSource,omitempty" jsonschema:"enum=interfaceAddress,enum=static,enum=none"`
	Router        string           `yaml:"router,omitempty" json:"router,omitempty"`
	DNSSource     string           `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=dhcp4,enum=static,enum=self,enum=none"`
	DNSInterface  string           `yaml:"dnsInterface,omitempty" json:"dnsInterface,omitempty"`
	DNSServers    []string         `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	Authoritative bool             `yaml:"authoritative,omitempty" json:"authoritative,omitempty"`
	When          ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
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
	IAID         string `yaml:"iaid,omitempty" json:"iaid,omitempty"`
	DUIDType     string `yaml:"duidType,omitempty" json:"duidType,omitempty" jsonschema:"enum=,enum=vendor,enum=uuid,enum=link-layer-time,enum=link-layer"`
	DUIDRawData  string `yaml:"duidRawData,omitempty" json:"duidRawData,omitempty"`
	Required     bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type IPv6DelegatedAddressSpec struct {
	PrefixDelegation string           `yaml:"prefixDelegation" json:"prefixDelegation"`
	Interface        string           `yaml:"interface" json:"interface"`
	SubnetID         string           `yaml:"subnetID,omitempty" json:"subnetID,omitempty"`
	AddressSuffix    string           `yaml:"addressSuffix" json:"addressSuffix"`
	SendRA           bool             `yaml:"sendRA,omitempty" json:"sendRA,omitempty"`
	Announce         bool             `yaml:"announce,omitempty" json:"announce,omitempty"`
	When             ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type IPv6DHCPServerSpec struct {
	Server           string   `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq"`
	Managed          bool     `yaml:"managed,omitempty" json:"managed,omitempty"`
	ListenInterfaces []string `yaml:"listenInterfaces,omitempty" json:"listenInterfaces,omitempty"`
}

type IPv6DHCPScopeSpec struct {
	Server            string           `yaml:"server" json:"server"`
	DelegatedAddress  string           `yaml:"delegatedAddress" json:"delegatedAddress"`
	Mode              string           `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=stateless,enum=ra-only"`
	LeaseTime         string           `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	DefaultRoute      bool             `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	DNSSource         string           `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=self,enum=static,enum=none"`
	SelfAddressPolicy string           `yaml:"selfAddressPolicy,omitempty" json:"selfAddressPolicy,omitempty"`
	DNSServers        []string         `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	When              ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type ResourceWhenSpec struct {
	State map[string]StateMatchSpec `yaml:"state,omitempty" json:"state,omitempty"`
}

type StateMatchSpec struct {
	Exists   *bool    `yaml:"exists,omitempty" json:"exists,omitempty"`
	Equals   string   `yaml:"equals,omitempty" json:"equals,omitempty"`
	In       []string `yaml:"in,omitempty" json:"in,omitempty"`
	Contains string   `yaml:"contains,omitempty" json:"contains,omitempty"`
	Status   string   `yaml:"status,omitempty" json:"status,omitempty" jsonschema:"enum=set,enum=unset,enum=unknown"`
	For      string   `yaml:"for,omitempty" json:"for,omitempty"`
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
	Interface             string           `yaml:"interface" json:"interface"`
	TunnelName            string           `yaml:"tunnelName,omitempty" json:"tunnelName,omitempty"`
	AFTRFQDN              string           `yaml:"aftrFQDN,omitempty" json:"aftrFQDN,omitempty"`
	AFTRDNSServers        []string         `yaml:"aftrDNSServers,omitempty" json:"aftrDNSServers,omitempty"`
	AFTRAddressOrdinal    int              `yaml:"aftrAddressOrdinal,omitempty" json:"aftrAddressOrdinal,omitempty" jsonschema:"minimum=1"`
	AFTRAddressSelection  string           `yaml:"aftrAddressSelection,omitempty" json:"aftrAddressSelection,omitempty" jsonschema:"enum=ordinal,enum=ordinalModulo"`
	RemoteAddress         string           `yaml:"remoteAddress,omitempty" json:"remoteAddress,omitempty"`
	LocalAddress          string           `yaml:"localAddress,omitempty" json:"localAddress,omitempty"`
	LocalAddressSource    string           `yaml:"localAddressSource,omitempty" json:"localAddressSource,omitempty" jsonschema:"enum=interface,enum=static,enum=delegatedAddress"`
	LocalDelegatedAddress string           `yaml:"localDelegatedAddress,omitempty" json:"localDelegatedAddress,omitempty"`
	LocalAddressSuffix    string           `yaml:"localAddressSuffix,omitempty" json:"localAddressSuffix,omitempty"`
	DefaultRoute          bool             `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	RouteMetric           int              `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	MTU                   int              `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=1280,maximum=65535"`
	EncapsulationLimit    string           `yaml:"encapsulationLimit,omitempty" json:"encapsulationLimit,omitempty"`
	When                  ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type StatePolicySpec struct {
	Variable  string           `yaml:"variable" json:"variable"`
	Interface string           `yaml:"interface,omitempty" json:"interface,omitempty"`
	Values    []StateValueSpec `yaml:"values" json:"values"`
}

type StateValueSpec struct {
	Value string             `yaml:"value" json:"value"`
	When  StateConditionSpec `yaml:"when" json:"when"`
}

type StateConditionSpec struct {
	IPv6PrefixDelegation StateIPv6PrefixDelegationCondition `yaml:"ipv6PrefixDelegation,omitempty" json:"ipv6PrefixDelegation,omitempty"`
	IPv6Address          StateIPv6AddressCondition          `yaml:"ipv6Address,omitempty" json:"ipv6Address,omitempty"`
	DNSResolve           StateDNSResolveCondition           `yaml:"dnsResolve,omitempty" json:"dnsResolve,omitempty"`
}

type StateIPv6PrefixDelegationCondition struct {
	Resource       string `yaml:"resource,omitempty" json:"resource,omitempty"`
	Available      *bool  `yaml:"available,omitempty" json:"available,omitempty"`
	UnavailableFor string `yaml:"unavailableFor,omitempty" json:"unavailableFor,omitempty"`
}

type StateIPv6AddressCondition struct {
	Interface string `yaml:"interface,omitempty" json:"interface,omitempty"`
	Global    *bool  `yaml:"global,omitempty" json:"global,omitempty"`
}

type StateDNSResolveCondition struct {
	Name              string   `yaml:"name,omitempty" json:"name,omitempty"`
	Type              string   `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=AAAA"`
	UpstreamSource    string   `yaml:"upstreamSource,omitempty" json:"upstreamSource,omitempty" jsonschema:"enum=static,enum=dhcp4,enum=dhcp6,enum=system"`
	UpstreamInterface string   `yaml:"upstreamInterface,omitempty" json:"upstreamInterface,omitempty"`
	UpstreamServers   []string `yaml:"upstreamServers,omitempty" json:"upstreamServers,omitempty"`
}

type HealthCheckSpec struct {
	Type               string           `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=ping"`
	Role               string           `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=link,enum=next-hop,enum=internet,enum=service,enum=policy"`
	AddressFamily      string           `yaml:"addressFamily,omitempty" json:"addressFamily,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
	Target             string           `yaml:"target,omitempty" json:"target,omitempty"`
	TargetSource       string           `yaml:"targetSource,omitempty" json:"targetSource,omitempty" jsonschema:"enum=auto,enum=static,enum=defaultGateway,enum=dsliteRemote"`
	Interface          string           `yaml:"interface,omitempty" json:"interface,omitempty"`
	Interval           string           `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout            string           `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	HealthyThreshold   int              `yaml:"healthyThreshold,omitempty" json:"healthyThreshold,omitempty" jsonschema:"minimum=1"`
	UnhealthyThreshold int              `yaml:"unhealthyThreshold,omitempty" json:"unhealthyThreshold,omitempty" jsonschema:"minimum=1"`
	When               ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type IPv4DefaultRoutePolicySpec struct {
	Mode             string                            `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=priority"`
	SourceCIDRs      []string                          `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs []string                          `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	Candidates       []IPv4DefaultRoutePolicyCandidate `yaml:"candidates" json:"candidates"`
}

type IPv4DefaultRoutePolicyCandidate struct {
	Name          string           `yaml:"name,omitempty" json:"name,omitempty"`
	Interface     string           `yaml:"interface,omitempty" json:"interface,omitempty"`
	RouteSet      string           `yaml:"routeSet,omitempty" json:"routeSet,omitempty"`
	GatewaySource string           `yaml:"gatewaySource,omitempty" json:"gatewaySource,omitempty" jsonschema:"enum=none,enum=dhcp4,enum=static"`
	Gateway       string           `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	Priority      int              `yaml:"priority" json:"priority" jsonschema:"minimum=1"`
	Table         int              `yaml:"table,omitempty" json:"table,omitempty" jsonschema:"minimum=1,maximum=4294967295"`
	Mark          int              `yaml:"mark,omitempty" json:"mark,omitempty" jsonschema:"minimum=1,maximum=4294967295"`
	RouteMetric   int              `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	HealthCheck   string           `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
	When          ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type IPv4SourceNATSpec struct {
	OutboundInterface string                 `yaml:"outboundInterface" json:"outboundInterface"`
	SourceCIDRs       []string               `yaml:"sourceCIDRs" json:"sourceCIDRs"`
	Translation       IPv4NATTranslationSpec `yaml:"translation" json:"translation"`
	When              ResourceWhenSpec       `yaml:"when,omitempty" json:"when,omitempty"`
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
	When             ResourceWhenSpec        `yaml:"when,omitempty" json:"when,omitempty"`
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

type PathMTUPolicySpec struct {
	FromInterface string                  `yaml:"fromInterface" json:"fromInterface"`
	ToInterfaces  []string                `yaml:"toInterfaces" json:"toInterfaces"`
	MTU           PathMTUPolicyMTUSpec    `yaml:"mtu,omitempty" json:"mtu,omitempty"`
	IPv6RA        PathMTUPolicyIPv6RASpec `yaml:"ipv6RA,omitempty" json:"ipv6RA,omitempty"`
	TCPMSSClamp   PathMTUPolicyTCPMSSSpec `yaml:"tcpMSSClamp,omitempty" json:"tcpMSSClamp,omitempty"`
}

type PathMTUPolicyMTUSpec struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"enum=minInterface,enum=static"`
	Value  int    `yaml:"value,omitempty" json:"value,omitempty" jsonschema:"minimum=1280,maximum=65535"`
}

type PathMTUPolicyIPv6RASpec struct {
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Scope   string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

type PathMTUPolicyTCPMSSSpec struct {
	Enabled  bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Families []string `yaml:"families,omitempty" json:"families,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
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

type ZoneSpec struct {
	Interfaces []string `yaml:"interfaces" json:"interfaces"`
}

type FirewallPolicySpec struct {
	Preset       string                  `yaml:"preset,omitempty" json:"preset,omitempty" jsonschema:"enum=home-router"`
	Input        FirewallChainPolicySpec `yaml:"input,omitempty" json:"input,omitempty"`
	Forward      FirewallChainPolicySpec `yaml:"forward,omitempty" json:"forward,omitempty"`
	RouterAccess RouterAccessSpec        `yaml:"routerAccess,omitempty" json:"routerAccess,omitempty"`
}

type FirewallChainPolicySpec struct {
	Default string `yaml:"default,omitempty" json:"default,omitempty" jsonschema:"enum=accept,enum=drop"`
}

type RouterAccessSpec struct {
	SSH  FirewallRouterServiceSpec `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	DNS  FirewallRouterServiceSpec `yaml:"dns,omitempty" json:"dns,omitempty"`
	DHCP FirewallRouterServiceSpec `yaml:"dhcp,omitempty" json:"dhcp,omitempty"`
}

type FirewallRouterServiceSpec struct {
	FromZones []string                    `yaml:"fromZones,omitempty" json:"fromZones,omitempty"`
	WAN       FirewallRouterWANAccessSpec `yaml:"wan,omitempty" json:"wan,omitempty"`
}

type FirewallRouterWANAccessSpec struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type ExposeServiceSpec struct {
	Family          string   `yaml:"family,omitempty" json:"family,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
	FromZone        string   `yaml:"fromZone" json:"fromZone"`
	ViaInterface    string   `yaml:"viaInterface,omitempty" json:"viaInterface,omitempty"`
	Protocol        string   `yaml:"protocol" json:"protocol" jsonschema:"enum=tcp,enum=udp"`
	ExternalPort    int      `yaml:"externalPort" json:"externalPort" jsonschema:"minimum=1,maximum=65535"`
	InternalAddress string   `yaml:"internalAddress" json:"internalAddress"`
	InternalPort    int      `yaml:"internalPort" json:"internalPort" jsonschema:"minimum=1,maximum=65535"`
	Sources         []string `yaml:"sources,omitempty" json:"sources,omitempty"`
	Hairpin         bool     `yaml:"hairpin,omitempty" json:"hairpin,omitempty"`
}

type HostnameSpec struct {
	Hostname string `yaml:"hostname" json:"hostname"`
	Managed  bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
}

func (r Resource) SysctlSpec() (SysctlSpec, error) {
	return specAs[SysctlSpec](r)
}

func (r Resource) NTPClientSpec() (NTPClientSpec, error) {
	return specAs[NTPClientSpec](r)
}

func (r Resource) NixOSHostSpec() (NixOSHostSpec, error) {
	return specAs[NixOSHostSpec](r)
}

func (r Resource) InterfaceSpec() (InterfaceSpec, error) {
	return specAs[InterfaceSpec](r)
}

func (r Resource) PPPoEInterfaceSpec() (PPPoEInterfaceSpec, error) {
	return specAs[PPPoEInterfaceSpec](r)
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

func (r Resource) StatePolicySpec() (StatePolicySpec, error) {
	return specAs[StatePolicySpec](r)
}

func (r Resource) HealthCheckSpec() (HealthCheckSpec, error) {
	return specAs[HealthCheckSpec](r)
}

func (r Resource) IPv4DefaultRoutePolicySpec() (IPv4DefaultRoutePolicySpec, error) {
	return specAs[IPv4DefaultRoutePolicySpec](r)
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

func (r Resource) PathMTUPolicySpec() (PathMTUPolicySpec, error) {
	return specAs[PathMTUPolicySpec](r)
}

func (r Resource) ZoneSpec() (ZoneSpec, error) {
	return specAs[ZoneSpec](r)
}

func (r Resource) FirewallPolicySpec() (FirewallPolicySpec, error) {
	return specAs[FirewallPolicySpec](r)
}

func (r Resource) ExposeServiceSpec() (ExposeServiceSpec, error) {
	return specAs[ExposeServiceSpec](r)
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
