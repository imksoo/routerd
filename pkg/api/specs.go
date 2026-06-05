// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

type LogSinkSpec struct {
	Type     string              `yaml:"type" json:"type" jsonschema:"enum=syslog,enum=otlp,enum=webhook,enum=file,enum=journald"`
	Enabled  *bool               `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinLevel string              `yaml:"minLevel,omitempty" json:"minLevel,omitempty" jsonschema:"enum=debug,enum=info,enum=warning,enum=error"`
	Syslog   LogSinkSyslogSpec   `yaml:"syslog,omitempty" json:"syslog,omitempty"`
	OTLP     LogSinkOTLPSpec     `yaml:"otlp,omitempty" json:"otlp,omitempty"`
	Webhook  LogSinkWebhookSpec  `yaml:"webhook,omitempty" json:"webhook,omitempty"`
	File     LogSinkFileSpec     `yaml:"file,omitempty" json:"file,omitempty"`
	Journald LogSinkJournaldSpec `yaml:"journald,omitempty" json:"journald,omitempty"`
}

type TelemetrySpec struct {
	OTLP             TelemetryOTLPSpec `yaml:"otlp" json:"otlp"`
	ServiceNamespace string            `yaml:"serviceNamespace,omitempty" json:"serviceNamespace,omitempty"`
	Attributes       map[string]string `yaml:"attributes,omitempty" json:"attributes,omitempty"`
	Signals          []string          `yaml:"signals,omitempty" json:"signals,omitempty" jsonschema:"enum=logs,enum=metrics,enum=traces"`
}

type TelemetryOTLPSpec struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	Insecure bool   `yaml:"insecure,omitempty" json:"insecure,omitempty"`
}

type ObservabilityPipelineSpec struct {
	OTLP             ObservabilityPipelineOTLPSpec     `yaml:"otlp,omitempty" json:"otlp,omitempty"`
	ServiceNamespace string                            `yaml:"serviceNamespace,omitempty" json:"serviceNamespace,omitempty"`
	Attributes       map[string]string                 `yaml:"attributes,omitempty" json:"attributes,omitempty"`
	Signals          []string                          `yaml:"signals,omitempty" json:"signals,omitempty" jsonschema:"enum=logs,enum=metrics,enum=traces"`
	Sampling         ObservabilityPipelineSamplingSpec `yaml:"sampling,omitempty" json:"sampling,omitempty"`
	Logs             ObservabilityPipelineLogsSpec     `yaml:"logs,omitempty" json:"logs,omitempty"`
	When             ResourceWhenSpec                  `yaml:"when,omitempty" json:"when,omitempty"`
}

type ObservabilityPipelineOTLPSpec struct {
	Endpoint string            `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Insecure bool              `yaml:"insecure,omitempty" json:"insecure,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	TLS      OTELTLSSpec       `yaml:"tls,omitempty" json:"tls,omitempty"`
}

type OTELTLSSpec struct {
	CAFile             string `yaml:"caFile,omitempty" json:"caFile,omitempty"`
	CertFile           string `yaml:"certFile,omitempty" json:"certFile,omitempty"`
	KeyFile            string `yaml:"keyFile,omitempty" json:"keyFile,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify,omitempty" json:"insecureSkipVerify,omitempty"`
}

type ObservabilityPipelineSamplingSpec struct {
	Rate float64 `yaml:"rate,omitempty" json:"rate,omitempty"`
}

type ObservabilityPipelineLogsSpec struct {
	Enabled *bool                          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Sinks   []ObservabilityPipelineLogSink `yaml:"sinks,omitempty" json:"sinks,omitempty"`
}

type ObservabilityPipelineLogSink struct {
	Name     string                     `yaml:"name,omitempty" json:"name,omitempty"`
	Type     string                     `yaml:"type" json:"type" jsonschema:"enum=stdout,enum=syslog,enum=loki,enum=kafka"`
	Enabled  *bool                      `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinLevel string                     `yaml:"minLevel,omitempty" json:"minLevel,omitempty" jsonschema:"enum=debug,enum=info,enum=warning,enum=error"`
	Labels   map[string]string          `yaml:"labels,omitempty" json:"labels,omitempty"`
	Loki     ObservabilityLokiSinkSpec  `yaml:"loki,omitempty" json:"loki,omitempty"`
	Syslog   LogSinkSyslogSpec          `yaml:"syslog,omitempty" json:"syslog,omitempty"`
	Kafka    ObservabilityKafkaSinkSpec `yaml:"kafka,omitempty" json:"kafka,omitempty"`
}

type ObservabilityLokiSinkSpec struct {
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Tenant  string            `yaml:"tenant,omitempty" json:"tenant,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

type ObservabilityKafkaSinkSpec struct {
	Brokers []string `yaml:"brokers,omitempty" json:"brokers,omitempty"`
	Topic   string   `yaml:"topic,omitempty" json:"topic,omitempty"`
}

type RouterdClusterSpec struct {
	Peers     []string         `yaml:"peers" json:"peers"`
	LeaseTTL  string           `yaml:"leaseTTL,omitempty" json:"leaseTTL,omitempty"`
	LeasePath string           `yaml:"leasePath,omitempty" json:"leasePath,omitempty"`
	Identity  string           `yaml:"identity,omitempty" json:"identity,omitempty"`
	When      ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type LogRetentionSpec struct {
	Retention string   `yaml:"retention" json:"retention"`
	Signals   []string `yaml:"signals,omitempty" json:"signals,omitempty" jsonschema:"enum=events,enum=dnsQueries,enum=trafficFlows,enum=firewallEvents"`
	Sinks     []string `yaml:"sinks,omitempty" json:"sinks,omitempty"`
	Vacuum    bool     `yaml:"vacuum,omitempty" json:"vacuum,omitempty"`
	Schedule  string   `yaml:"schedule,omitempty" json:"schedule,omitempty" jsonschema:"enum=,enum=daily"`
}

type LogRetentionTargetSpec struct {
	File      string `yaml:"file" json:"file"`
	Retention string `yaml:"retention" json:"retention"`
}

type ApplyPolicySpec struct {
	// Mode defaults to strict; progressive mode records recoverable apply errors and continues with later stages.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=strict,enum=progressive"`
	// ProtectedInterfaces excludes interfaces from routerd-generated adoption and management-path disruption checks.
	ProtectedInterfaces []string `yaml:"protectedInterfaces,omitempty" json:"protectedInterfaces,omitempty"`
	ProtectedZones      []string `yaml:"protectedZones,omitempty" json:"protectedZones,omitempty"`
	AutoTuneConntrack   bool     `yaml:"autoTuneConntrack,omitempty" json:"autoTuneConntrack,omitempty"`
}

type PluginSpec struct {
	Executable   string            `yaml:"executable" json:"executable"`
	Timeout      string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Capabilities []string          `yaml:"capabilities,omitempty" json:"capabilities,omitempty" jsonschema:"enum=observe.cloud,enum=observe.providerPrivateIPs,enum=propose.dynamicConfig,enum=propose.providerAction,enum=execute.providerAction"`
	Triggers     []PluginTrigger   `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	// Context is the least-privilege allowlist of config resources the plugin
	// may read on stdin. Empty/absent = the plugin receives no configuration
	// (default-deny). Secrets are ALWAYS redacted from whatever is passed.
	Context PluginContextSpec `yaml:"context,omitempty" json:"context,omitempty"`
}

// PluginContextSpec is the allowlist of config resources the plugin may read.
// Empty = the plugin receives no configuration (default-deny). Secrets are
// ALWAYS redacted from whatever is passed; there is no opt-out.
type PluginContextSpec struct {
	Resources []PluginContextResourceRef `yaml:"resources,omitempty" json:"resources,omitempty"`
}

type PluginContextResourceRef struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

type PluginTrigger struct {
	Type  string `yaml:"type" json:"type" jsonschema:"enum=interval,enum=event"`
	Every string `yaml:"every,omitempty" json:"every,omitempty"`
	Topic string `yaml:"topic,omitempty" json:"topic,omitempty"`
}

type DynamicConfigSourceSpec struct {
	PluginRef   string          `yaml:"pluginRef" json:"pluginRef"`
	TTL         string          `yaml:"ttl,omitempty" json:"ttl,omitempty"`
	MergePolicy *MergePolicy    `yaml:"mergePolicy,omitempty" json:"mergePolicy,omitempty"`
	Triggers    []PluginTrigger `yaml:"triggers,omitempty" json:"triggers,omitempty"`
}

type MergePolicy struct {
	Conflict string `yaml:"conflict,omitempty" json:"conflict,omitempty" jsonschema:"enum=,enum=reject"`
}

// DynamicOverridePolicySpec is defined in api because DynamicOverridePolicy is
// authored in startup config. pkg/dynamicconfig keeps its runtime policy type
// and converts this shape there to avoid an api -> dynamicconfig import cycle.
type DynamicOverridePolicySpec struct {
	Allow []DynamicOverrideAllowRule `yaml:"allow" json:"allow"`
}

type DynamicOverrideAllowRule struct {
	Source     string                  `yaml:"source" json:"source"`
	Operations []string                `yaml:"operations" json:"operations"`
	Targets    []DynamicOverrideTarget `yaml:"targets" json:"targets"`
}

type DynamicOverrideTarget struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

type LogSinkSyslogSpec struct {
	Network  string `yaml:"network,omitempty" json:"network,omitempty" jsonschema:"enum=,enum=unix,enum=unixgram,enum=tcp,enum=udp"`
	Address  string `yaml:"address,omitempty" json:"address,omitempty"`
	Facility string `yaml:"facility,omitempty" json:"facility,omitempty" jsonschema:"enum=kern,enum=user,enum=mail,enum=daemon,enum=auth,enum=syslog,enum=lpr,enum=news,enum=uucp,enum=cron,enum=authpriv,enum=ftp,enum=local0,enum=local1,enum=local2,enum=local3,enum=local4,enum=local5,enum=local6,enum=local7"`
	Tag      string `yaml:"tag,omitempty" json:"tag,omitempty"`
}

type LogSinkOTLPSpec struct {
	TelemetryRef string `yaml:"telemetryRef,omitempty" json:"telemetryRef,omitempty"`
	Endpoint     string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
}

type LogSinkWebhookSpec struct {
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Timeout string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

type LogSinkFileSpec struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
}

type LogSinkJournaldSpec struct {
	Identifier string `yaml:"identifier,omitempty" json:"identifier,omitempty"`
}

type SysctlSpec struct {
	Key           string `yaml:"key" json:"key" jsonschema:"title=Key"`
	Value         string `yaml:"value" json:"value" jsonschema:"title=Value"`
	ExpectedValue string `yaml:"expectedValue,omitempty" json:"expectedValue,omitempty"`
	Compare       string `yaml:"compare,omitempty" json:"compare,omitempty" jsonschema:"enum=,enum=exact,enum=atLeast"`
	Runtime       *bool  `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Persistent    bool   `yaml:"persistent,omitempty" json:"persistent,omitempty"`
	Optional      bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type SysctlProfileSpec struct {
	Profile    string            `yaml:"profile" json:"profile" jsonschema:"enum=router-linux"`
	Runtime    *bool             `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Persistent bool              `yaml:"persistent,omitempty" json:"persistent,omitempty"`
	Overrides  map[string]string `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

type KernelModuleSpec struct {
	State      string   `yaml:"state,omitempty" json:"state,omitempty" jsonschema:"enum=,enum=present"`
	Modules    []string `yaml:"modules" json:"modules"`
	Runtime    *bool    `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Persistent bool     `yaml:"persistent,omitempty" json:"persistent,omitempty"`
	Optional   bool     `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type PackageSpec struct {
	State    string             `yaml:"state,omitempty" json:"state,omitempty" jsonschema:"enum=,enum=present"`
	Packages []OSPackageSetSpec `yaml:"packages" json:"packages"`
}

type NetworkAdoptionSpec struct {
	State           string                      `yaml:"state,omitempty" json:"state,omitempty" jsonschema:"enum=,enum=present,enum=absent"`
	Interface       string                      `yaml:"interface,omitempty" json:"interface,omitempty"`
	IfName          string                      `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	SystemdNetworkd NetworkAdoptionNetworkdSpec `yaml:"systemdNetworkd,omitempty" json:"systemdNetworkd,omitempty"`
	SystemdResolved NetworkAdoptionResolvedSpec `yaml:"systemdResolved,omitempty" json:"systemdResolved,omitempty"`
	Reload          *bool                       `yaml:"reload,omitempty" json:"reload,omitempty"`
}

type NetworkAdoptionNetworkdSpec struct {
	DisableDHCPv4     bool   `yaml:"disableDHCPv4,omitempty" json:"disableDHCPv4,omitempty"`
	DisableDHCPv6     bool   `yaml:"disableDHCPv6,omitempty" json:"disableDHCPv6,omitempty"`
	DisableIPv6RA     bool   `yaml:"disableIPv6RA,omitempty" json:"disableIPv6RA,omitempty"`
	DHCPv4UseRoutes   *bool  `yaml:"dhcpv4UseRoutes,omitempty" json:"dhcpv4UseRoutes,omitempty"`
	DHCPv4UseDNS      *bool  `yaml:"dhcpv4UseDNS,omitempty" json:"dhcpv4UseDNS,omitempty"`
	DHCPv4RouteMetric int    `yaml:"dhcpv4RouteMetric,omitempty" json:"dhcpv4RouteMetric,omitempty" jsonschema:"minimum=0"`
	DropinName        string `yaml:"dropinName,omitempty" json:"dropinName,omitempty"`
}

type NetworkAdoptionResolvedSpec struct {
	DisableDNSStubListener bool     `yaml:"disableDNSStubListener,omitempty" json:"disableDNSStubListener,omitempty"`
	DNSServers             []string `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	FallbackDNSServers     []string `yaml:"fallbackDNSServers,omitempty" json:"fallbackDNSServers,omitempty"`
	DropinName             string   `yaml:"dropinName,omitempty" json:"dropinName,omitempty"`
}

type SystemdUnitSpec struct {
	State                    string   `yaml:"state,omitempty" json:"state,omitempty" jsonschema:"enum=,enum=present,enum=absent"`
	UnitName                 string   `yaml:"unitName,omitempty" json:"unitName,omitempty"`
	Description              string   `yaml:"description,omitempty" json:"description,omitempty"`
	Type                     string   `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=,enum=simple,enum=oneshot"`
	ExecStartPre             []string `yaml:"execStartPre,omitempty" json:"execStartPre,omitempty"`
	ExecStart                []string `yaml:"execStart,omitempty" json:"execStart,omitempty"`
	Environment              []string `yaml:"environment,omitempty" json:"environment,omitempty"`
	EnvironmentFiles         []string `yaml:"environmentFiles,omitempty" json:"environmentFiles,omitempty"`
	Wants                    []string `yaml:"wants,omitempty" json:"wants,omitempty"`
	After                    []string `yaml:"after,omitempty" json:"after,omitempty"`
	WantedBy                 []string `yaml:"wantedBy,omitempty" json:"wantedBy,omitempty"`
	Restart                  string   `yaml:"restart,omitempty" json:"restart,omitempty" jsonschema:"enum=,enum=no,enum=on-failure,enum=always"`
	RestartSec               string   `yaml:"restartSec,omitempty" json:"restartSec,omitempty"`
	User                     string   `yaml:"user,omitempty" json:"user,omitempty"`
	Group                    string   `yaml:"group,omitempty" json:"group,omitempty"`
	SupplementaryGroups      []string `yaml:"supplementaryGroups,omitempty" json:"supplementaryGroups,omitempty"`
	WorkingDirectory         string   `yaml:"workingDirectory,omitempty" json:"workingDirectory,omitempty"`
	RuntimeDirectory         []string `yaml:"runtimeDirectory,omitempty" json:"runtimeDirectory,omitempty"`
	RuntimeDirectoryPreserve string   `yaml:"runtimeDirectoryPreserve,omitempty" json:"runtimeDirectoryPreserve,omitempty" jsonschema:"enum=,enum=no,enum=yes,enum=restart"`
	StateDirectory           []string `yaml:"stateDirectory,omitempty" json:"stateDirectory,omitempty"`
	LogsDirectory            []string `yaml:"logsDirectory,omitempty" json:"logsDirectory,omitempty"`
	ReadWritePaths           []string `yaml:"readWritePaths,omitempty" json:"readWritePaths,omitempty"`
	AmbientCapabilities      []string `yaml:"ambientCapabilities,omitempty" json:"ambientCapabilities,omitempty"`
	CapabilityBoundingSet    []string `yaml:"capabilityBoundingSet,omitempty" json:"capabilityBoundingSet,omitempty"`
	RestrictAddressFamilies  []string `yaml:"restrictAddressFamilies,omitempty" json:"restrictAddressFamilies,omitempty"`
	ProtectSystem            string   `yaml:"protectSystem,omitempty" json:"protectSystem,omitempty" jsonschema:"enum=,enum=no,enum=false,enum=true,enum=full,enum=strict"`
	ProtectHome              string   `yaml:"protectHome,omitempty" json:"protectHome,omitempty" jsonschema:"enum=,enum=true,enum=read-only,enum=tmpfs"`
	NoNewPrivileges          *bool    `yaml:"noNewPrivileges,omitempty" json:"noNewPrivileges,omitempty"`
	PrivateTmp               *bool    `yaml:"privateTmp,omitempty" json:"privateTmp,omitempty"`
	RemainAfterExit          *bool    `yaml:"remainAfterExit,omitempty" json:"remainAfterExit,omitempty"`
	Enabled                  *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Started                  *bool    `yaml:"started,omitempty" json:"started,omitempty"`
}

type OSPackageSetSpec struct {
	OS       string   `yaml:"os" json:"os" jsonschema:"enum=ubuntu,enum=debian,enum=alpine,enum=fedora,enum=rhel,enum=rocky,enum=almalinux,enum=nixos,enum=freebsd"`
	Manager  string   `yaml:"manager,omitempty" json:"manager,omitempty" jsonschema:"enum=,enum=apt,enum=apk,enum=dnf,enum=nix,enum=pkg"`
	Names    []string `yaml:"names" json:"names"`
	Optional bool     `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type NTPClientSpec struct {
	Provider        string                  `yaml:"provider,omitempty" json:"provider,omitempty" jsonschema:"enum=systemd-timesyncd,enum=chrony,enum=ntpd"`
	Managed         bool                    `yaml:"managed,omitempty" json:"managed,omitempty"`
	Source          string                  `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"enum=static,enum=auto,enum=dhcp,enum=dhcpv6"`
	Interface       string                  `yaml:"interface,omitempty" json:"interface,omitempty"`
	Servers         []string                `yaml:"servers,omitempty" json:"servers,omitempty"`
	ServerFrom      []StatusValueSourceSpec `yaml:"serverFrom,omitempty" json:"serverFrom,omitempty"`
	FallbackServers []string                `yaml:"fallbackServers,omitempty" json:"fallbackServers,omitempty"`
}

type NTPServerSpec struct {
	Provider          string                  `yaml:"provider,omitempty" json:"provider,omitempty" jsonschema:"enum=chrony,enum=ntpd"`
	Managed           bool                    `yaml:"managed,omitempty" json:"managed,omitempty"`
	Source            string                  `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"enum=static,enum=auto,enum=dhcp,enum=dhcpv6"`
	ListenAddresses   []string                `yaml:"listenAddresses,omitempty" json:"listenAddresses,omitempty"`
	ListenAddressFrom []StatusValueSourceSpec `yaml:"listenAddressFrom,omitempty" json:"listenAddressFrom,omitempty"`
	AllowCIDRs        []string                `yaml:"allowCIDRs,omitempty" json:"allowCIDRs,omitempty"`
	AllowCIDRFrom     []StatusValueSourceSpec `yaml:"allowCIDRFrom,omitempty" json:"allowCIDRFrom,omitempty"`
	Servers           []string                `yaml:"servers,omitempty" json:"servers,omitempty"`
	ServerFrom        []StatusValueSourceSpec `yaml:"serverFrom,omitempty" json:"serverFrom,omitempty"`
	FallbackServers   []string                `yaml:"fallbackServers,omitempty" json:"fallbackServers,omitempty"`
}

type WebConsoleSpec struct {
	Enabled           *bool                 `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ListenAddress     string                `yaml:"listenAddress,omitempty" json:"listenAddress,omitempty"`
	ListenAddressFrom StatusValueSourceSpec `yaml:"listenAddressFrom,omitempty" json:"listenAddressFrom,omitempty"`
	Port              int                   `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
	BasePath          string                `yaml:"basePath,omitempty" json:"basePath,omitempty"`
	Title             string                `yaml:"title,omitempty" json:"title,omitempty"`
}

type ManagementAccessSpec struct {
	Interfaces             []string `yaml:"interfaces" json:"interfaces" jsonschema:"title=Management interfaces"`
	AllowSourceCIDRs       []string `yaml:"allowSourceCIDRs,omitempty" json:"allowSourceCIDRs,omitempty"`
	RequireWebConsoleBound *bool    `yaml:"requireWebConsoleBound,omitempty" json:"requireWebConsoleBound,omitempty"`
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

type InventorySpec struct{}

type InterfaceSpec struct {
	IfName  string `yaml:"ifname" json:"ifname"`
	AdminUp bool   `yaml:"adminUp,omitempty" json:"adminUp,omitempty"`
	MTU     int    `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=576,maximum=9216"`
	Managed bool   `yaml:"managed" json:"managed"`
	Owner   string `yaml:"owner,omitempty" json:"owner,omitempty" jsonschema:"enum=routerd,enum=external"`
}

type LinkSpec struct {
	IfName string `yaml:"ifname,omitempty" json:"ifname,omitempty"`
}

type BridgeSpec struct {
	IfName            string   `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	Members           []string `yaml:"members,omitempty" json:"members,omitempty"`
	STP               *bool    `yaml:"stp,omitempty" json:"stp,omitempty"`
	RSTP              *bool    `yaml:"rstp,omitempty" json:"rstp,omitempty"`
	ForwardDelay      int      `yaml:"forwardDelay,omitempty" json:"forwardDelay,omitempty" jsonschema:"minimum=0"`
	HelloTime         int      `yaml:"helloTime,omitempty" json:"helloTime,omitempty" jsonschema:"minimum=0"`
	MACAddress        string   `yaml:"macAddress,omitempty" json:"macAddress,omitempty"`
	MTU               int      `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=0"`
	MulticastSnooping *bool    `yaml:"multicastSnooping,omitempty" json:"multicastSnooping,omitempty"`
}

type VXLANSegmentSpec struct {
	IfName            string   `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	VNI               int      `yaml:"vni" json:"vni" jsonschema:"minimum=1,maximum=16777215"`
	LocalAddress      string   `yaml:"localAddress" json:"localAddress"`
	Remotes           []string `yaml:"remotes,omitempty" json:"remotes,omitempty"`
	MulticastGroup    string   `yaml:"multicastGroup,omitempty" json:"multicastGroup,omitempty"`
	UnderlayInterface string   `yaml:"underlayInterface" json:"underlayInterface"`
	UDPPort           int      `yaml:"udpPort,omitempty" json:"udpPort,omitempty" jsonschema:"minimum=1,maximum=65535"`
	MTU               int      `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=0"`
	Bridge            string   `yaml:"bridge,omitempty" json:"bridge,omitempty"`
	L2Filter          string   `yaml:"l2Filter,omitempty" json:"l2Filter,omitempty" jsonschema:"enum=,enum=default,enum=none"`
}

type WireGuardInterfaceSpec struct {
	PrivateKey     string `yaml:"privateKey,omitempty" json:"privateKey,omitempty"`
	PrivateKeyFile string `yaml:"privateKeyFile,omitempty" json:"privateKeyFile,omitempty"`
	ListenPort     int    `yaml:"listenPort,omitempty" json:"listenPort,omitempty" jsonschema:"minimum=1,maximum=65535"`
	MTU            int    `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=576,maximum=9216"`
	FwMark         int    `yaml:"-" json:"-"`
	Table          int    `yaml:"-" json:"-"`
}

type TunnelInterfaceSpec struct {
	Mode            string         `yaml:"mode" json:"mode" jsonschema:"enum=ipip,enum=gre,enum=fou,enum=gue"`
	Local           string         `yaml:"local" json:"local"`
	Remote          string         `yaml:"remote" json:"remote"`
	Address         string         `yaml:"address,omitempty" json:"address,omitempty"`
	MTU             int            `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=576,maximum=9216"`
	TTL             int            `yaml:"ttl,omitempty" json:"ttl,omitempty" jsonschema:"minimum=1,maximum=255"`
	Key             int            `yaml:"key,omitempty" json:"key,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	EncapSport      int            `yaml:"encapSport,omitempty" json:"encapSport,omitempty" jsonschema:"minimum=1,maximum=65535"`
	EncapDport      int            `yaml:"encapDport,omitempty" json:"encapDport,omitempty" jsonschema:"minimum=1,maximum=65535"`
	TrustedUnderlay bool           `yaml:"trustedUnderlay" json:"trustedUnderlay"`
	PathMTU         PathMTUOptions `yaml:"pathMTU,omitempty" json:"pathMTU,omitempty"`
}

type PathMTUOptions struct {
	ForceFragmentIPv4 bool `yaml:"forceFragmentIPv4,omitempty" json:"forceFragmentIPv4,omitempty"`
}

type WireGuardPeerSpec struct {
	Interface           string   `yaml:"interface" json:"interface"`
	PublicKey           string   `yaml:"publicKey" json:"publicKey"`
	AllowedIPs          []string `yaml:"allowedIPs" json:"allowedIPs"`
	Endpoint            string   `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	PersistentKeepalive int      `yaml:"persistentKeepalive,omitempty" json:"persistentKeepalive,omitempty" jsonschema:"minimum=0,maximum=65535"`
	PresharedKey        string   `yaml:"presharedKey,omitempty" json:"presharedKey,omitempty"`
	PresharedKeyFile    string   `yaml:"presharedKeyFile,omitempty" json:"presharedKeyFile,omitempty"`
}

type TailscaleNodeSpec struct {
	State             string   `yaml:"state,omitempty" json:"state,omitempty" jsonschema:"enum=,enum=present,enum=absent"`
	Hostname          string   `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	LoginServer       string   `yaml:"loginServer,omitempty" json:"loginServer,omitempty"`
	AuthKey           string   `yaml:"authKey,omitempty" json:"authKey,omitempty"`
	AuthKeyEnv        string   `yaml:"authKeyEnv,omitempty" json:"authKeyEnv,omitempty"`
	AuthKeyFile       string   `yaml:"authKeyFile,omitempty" json:"authKeyFile,omitempty"`
	AdvertiseExitNode bool     `yaml:"advertiseExitNode,omitempty" json:"advertiseExitNode,omitempty"`
	AdvertiseRoutes   []string `yaml:"advertiseRoutes,omitempty" json:"advertiseRoutes,omitempty"`
	AdvertiseTags     []string `yaml:"advertiseTags,omitempty" json:"advertiseTags,omitempty"`
	AcceptRoutes      *bool    `yaml:"acceptRoutes,omitempty" json:"acceptRoutes,omitempty"`
	AcceptDNS         *bool    `yaml:"acceptDNS,omitempty" json:"acceptDNS,omitempty"`
	SSH               bool     `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Operator          string   `yaml:"-" json:"-"`
	ShieldsUp         *bool    `yaml:"shieldsUp,omitempty" json:"shieldsUp,omitempty"`
	BinaryPath        string   `yaml:"-" json:"-"`
}

type IPsecConnectionSpec struct {
	LocalAddress      string   `yaml:"localAddress" json:"localAddress"`
	RemoteAddress     string   `yaml:"remoteAddress" json:"remoteAddress"`
	PreSharedKey      string   `yaml:"preSharedKey,omitempty" json:"preSharedKey,omitempty"`
	CertificateRef    string   `yaml:"certificateRef,omitempty" json:"certificateRef,omitempty"`
	Phase1Proposals   []string `yaml:"psPhase1Proposals,omitempty" json:"psPhase1Proposals,omitempty"`
	Phase2Proposals   []string `yaml:"psPhase2Proposals,omitempty" json:"psPhase2Proposals,omitempty"`
	LeftSubnet        string   `yaml:"leftSubnet" json:"leftSubnet"`
	RightSubnet       string   `yaml:"rightSubnet" json:"rightSubnet"`
	CloudProviderHint string   `yaml:"cloudProviderHint,omitempty" json:"cloudProviderHint,omitempty" jsonschema:"enum=,enum=aws,enum=azure,enum=gcp"`
}

type VRFSpec struct {
	IfName     string   `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	RouteTable int      `yaml:"routeTable" json:"routeTable" jsonschema:"minimum=1,maximum=4294967295"`
	Members    []string `yaml:"members,omitempty" json:"members,omitempty"`
}

type VXLANTunnelSpec struct {
	IfName            string   `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	VNI               int      `yaml:"vni" json:"vni" jsonschema:"minimum=1,maximum=16777215"`
	LocalAddress      string   `yaml:"localAddress" json:"localAddress"`
	Peers             []string `yaml:"peers,omitempty" json:"peers,omitempty"`
	UnderlayInterface string   `yaml:"underlayInterface" json:"underlayInterface"`
	UDPPort           int      `yaml:"udpPort,omitempty" json:"udpPort,omitempty" jsonschema:"minimum=1,maximum=65535"`
	MTU               int      `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=0"`
	Bridge            string   `yaml:"bridge,omitempty" json:"bridge,omitempty"`
}

type PPPoESessionSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	IfName    string `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	// Enabled defaults to true; set enabled: false to suppress session startup.
	Enabled         *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AuthMethod      string `yaml:"authMethod,omitempty" json:"authMethod,omitempty" jsonschema:"enum=chap,enum=pap,enum=both"`
	Username        string `yaml:"username" json:"username"`
	Password        string `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFile    string `yaml:"passwordFile,omitempty" json:"passwordFile,omitempty"`
	MTU             int    `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=576,maximum=1500"`
	MRU             int    `yaml:"mru,omitempty" json:"mru,omitempty" jsonschema:"minimum=576,maximum=1500"`
	ServiceName     string `yaml:"serviceName,omitempty" json:"serviceName,omitempty"`
	ACName          string `yaml:"acName,omitempty" json:"acName,omitempty"`
	DefaultRoute    bool   `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	UsePeerDNS      bool   `yaml:"usePeerDNS,omitempty" json:"usePeerDNS,omitempty"`
	Persist         *bool  `yaml:"persist,omitempty" json:"persist,omitempty"`
	LCPInterval     int    `yaml:"lcpInterval,omitempty" json:"lcpInterval,omitempty" jsonschema:"minimum=0"`
	LCPFailure      int    `yaml:"lcpFailure,omitempty" json:"lcpFailure,omitempty" jsonschema:"minimum=0"`
	LCPEchoInterval int    `yaml:"lcpEchoInterval,omitempty" json:"lcpEchoInterval,omitempty" jsonschema:"minimum=0"`
	LCPEchoFailure  int    `yaml:"lcpEchoFailure,omitempty" json:"lcpEchoFailure,omitempty" jsonschema:"minimum=0"`
	IPv6            bool   `yaml:"ipv6,omitempty" json:"ipv6,omitempty"`
	Managed         bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
	SecretEncoding  string `yaml:"secretEncoding,omitempty" json:"secretEncoding,omitempty" jsonschema:"enum=plain"`
}

type IPv4StaticAddressSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	Address   string `yaml:"address" json:"address"`
	// Exclusive removes other IPv4 addresses from the target interface before adding this address.
	Exclusive bool `yaml:"exclusive,omitempty" json:"exclusive,omitempty"`
	// AllowOverlap permits an address prefix that overlaps another configured IPv4 prefix.
	AllowOverlap       bool   `yaml:"allowOverlap,omitempty" json:"allowOverlap,omitempty"`
	AllowOverlapReason string `yaml:"allowOverlapReason,omitempty" json:"allowOverlapReason,omitempty"`
}

type VirtualAddressSpec struct {
	Family      string                 `yaml:"family" json:"family" jsonschema:"enum=ipv4,enum=ipv6"`
	Interface   string                 `yaml:"interface" json:"interface"`
	Address     string                 `yaml:"address" json:"address"`
	Hostname    string                 `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	ExternalDNS bool                   `yaml:"externalDNS,omitempty" json:"externalDNS,omitempty"`
	Mode        string                 `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=static,enum=vrrp"`
	VRRP        VirtualAddressVRRPSpec `yaml:"vrrp,omitempty" json:"vrrp,omitempty"`
	Track       []ResourceTrackSpec    `yaml:"track,omitempty" json:"track,omitempty"`
	When        ResourceWhenSpec       `yaml:"when,omitempty" json:"when,omitempty"`
	AddressFrom StatusValueSourceSpec  `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
}

type VirtualAddressVRRPSpec struct {
	VirtualRouterID    int                   `yaml:"virtualRouterID" json:"virtualRouterID" jsonschema:"minimum=1,maximum=255"`
	Priority           int                   `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"minimum=1,maximum=254"`
	Preempt            *bool                 `yaml:"preempt,omitempty" json:"preempt,omitempty"`
	PreemptDelay       string                `yaml:"-" json:"-"`
	Peers              []string              `yaml:"peers,omitempty" json:"peers,omitempty"`
	AdvertInterval     string                `yaml:"-" json:"-"`
	Authentication     string                `yaml:"authentication,omitempty" json:"authentication,omitempty"`
	AuthenticationFrom SecretValueSourceSpec `yaml:"authenticationFrom,omitempty" json:"authenticationFrom,omitempty"`
}

type ResourceTrackSpec struct {
	Resource                    string `yaml:"resource" json:"resource"`
	UnhealthyPenalty            int    `yaml:"unhealthyPenalty,omitempty" json:"unhealthyPenalty,omitempty" jsonschema:"minimum=0,maximum=254"`
	ConfirmConsecutiveUnhealthy int    `yaml:"confirmConsecutiveUnhealthy,omitempty" json:"confirmConsecutiveUnhealthy,omitempty" jsonschema:"minimum=1,maximum=255"`
	ConfirmConsecutiveHealthy   int    `yaml:"confirmConsecutiveHealthy,omitempty" json:"confirmConsecutiveHealthy,omitempty" jsonschema:"minimum=1,maximum=255"`
}

type BGPRouterSpec struct {
	ASN                uint32                 `yaml:"asn" json:"asn" jsonschema:"minimum=1"`
	RouterID           string                 `yaml:"routerID" json:"routerID"`
	VRF                string                 `yaml:"vrf,omitempty" json:"vrf,omitempty"`
	Listen             BGPListenSpec          `yaml:"listen,omitempty" json:"listen,omitempty"`
	ImportPolicy       BGPImportPolicySpec    `yaml:"importPolicy,omitempty" json:"importPolicy,omitempty"`
	ExportPolicy       BGPExportPolicySpec    `yaml:"exportPolicy,omitempty" json:"exportPolicy,omitempty"`
	Redistribute       BGPRedistributeSpec    `yaml:"redistribute,omitempty" json:"redistribute,omitempty"`
	Communities        BGPCommunitiesSpec     `yaml:"communities,omitempty" json:"communities,omitempty"`
	ConvergenceProfile string                 `yaml:"convergenceProfile,omitempty" json:"convergenceProfile,omitempty" jsonschema:"enum=,enum=default,enum=fast,enum=stable"`
	Timers             BGPTimersSpec          `yaml:"timers,omitempty" json:"timers,omitempty"`
	GracefulRestart    BGPGracefulRestartSpec `yaml:"gracefulRestart,omitempty" json:"gracefulRestart,omitempty"`
	Watcher            BGPWatcherSpec         `yaml:"watcher,omitempty" json:"watcher,omitempty"`
	Backend            string                 `yaml:"backend,omitempty" json:"backend,omitempty" jsonschema:"enum=,enum=gobgp"`
	When               ResourceWhenSpec       `yaml:"when,omitempty" json:"when,omitempty"`
}

type BGPListenSpec struct {
	Address string `yaml:"address,omitempty" json:"address,omitempty"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
}

type BGPImportPolicySpec struct {
	AllowedPrefixes []string `yaml:"allowedPrefixes,omitempty" json:"allowedPrefixes,omitempty"`
	NextHopRewrite  string   `yaml:"nextHopRewrite,omitempty" json:"nextHopRewrite,omitempty" jsonschema:"enum=,enum=peer-address,enum=unchanged"`
}

type BGPExportPolicySpec struct {
	AllowedPrefixes []string `yaml:"allowedPrefixes,omitempty" json:"allowedPrefixes,omitempty"`
}

type BGPRedistributeSpec struct {
	Connected BGPRedistributeRouteSpec `yaml:"connected,omitempty" json:"connected,omitempty"`
	Static    BGPRedistributeRouteSpec `yaml:"static,omitempty" json:"static,omitempty"`
}

type BGPRedistributeRouteSpec struct {
	AllowedPrefixes []string `yaml:"allowedPrefixes,omitempty" json:"allowedPrefixes,omitempty"`
}

type BGPCommunitiesSpec struct {
	Send   string              `yaml:"send,omitempty" json:"send,omitempty" jsonschema:"enum=,enum=standard,enum=extended,enum=both"`
	Accept []string            `yaml:"accept,omitempty" json:"accept,omitempty"`
	Set    BGPCommunitySetSpec `yaml:"set,omitempty" json:"set,omitempty"`
}

type BGPCommunitySetSpec struct {
	In  []string `yaml:"in,omitempty" json:"in,omitempty"`
	Out []string `yaml:"out,omitempty" json:"out,omitempty"`
}

type BGPTimersSpec struct {
	Profile      string `yaml:"profile,omitempty" json:"profile,omitempty" jsonschema:"enum=,enum=default,enum=fast,enum=slow"`
	Keepalive    string `yaml:"-" json:"-"`
	HoldTime     string `yaml:"-" json:"-"`
	ConnectRetry string `yaml:"-" json:"-"`
}

type BGPGracefulRestartSpec struct {
	Enabled       *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	RestartTime   string `yaml:"restartTime,omitempty" json:"restartTime,omitempty"`
	StalePathTime string `yaml:"stalePathTime,omitempty" json:"stalePathTime,omitempty"`
}

type BGPWatcherSpec struct {
	PollInterval            string `yaml:"pollInterval,omitempty" json:"pollInterval,omitempty"`
	MaxPrefixes             int    `yaml:"maxPrefixes,omitempty" json:"maxPrefixes,omitempty" jsonschema:"minimum=1,maximum=999999"`
	PeerStateChangeThrottle string `yaml:"peerStateChangeThrottle,omitempty" json:"peerStateChangeThrottle,omitempty"`
}

type BGPPeerSpec struct {
	RouterRef               string                `yaml:"routerRef" json:"routerRef"`
	PeerASN                 uint32                `yaml:"peerASN" json:"peerASN" jsonschema:"minimum=1"`
	Peers                   []string              `yaml:"peers" json:"peers"`
	Password                string                `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFrom            SecretValueSourceSpec `yaml:"passwordFrom,omitempty" json:"passwordFrom,omitempty"`
	EbgpMultihop            int                   `yaml:"ebgpMultihop,omitempty" json:"ebgpMultihop,omitempty" jsonschema:"minimum=0,maximum=255"`
	RouteReflectorClient    bool                  `yaml:"routeReflectorClient,omitempty" json:"routeReflectorClient,omitempty"`
	RouteReflectorClusterID string                `yaml:"routeReflectorClusterID,omitempty" json:"routeReflectorClusterID,omitempty"`
	ExportPolicy            BGPExportPolicySpec   `yaml:"exportPolicy,omitempty" json:"exportPolicy,omitempty"`
	Timers                  BGPTimersSpec         `yaml:"timers,omitempty" json:"timers,omitempty"`
	Communities             BGPCommunitiesSpec    `yaml:"communities,omitempty" json:"communities,omitempty"`
	BFD                     string                `yaml:"bfd,omitempty" json:"bfd,omitempty"`
	When                    ResourceWhenSpec      `yaml:"when,omitempty" json:"when,omitempty"`
}

type BFDSpec struct {
	Peer             string           `yaml:"peer" json:"peer"`
	Interface        string           `yaml:"interface,omitempty" json:"interface,omitempty"`
	Profile          string           `yaml:"profile,omitempty" json:"profile,omitempty" jsonschema:"enum=,enum=fast,enum=normal,enum=slow"`
	MinRx            string           `yaml:"minRx,omitempty" json:"minRx,omitempty"`
	MinTx            string           `yaml:"minTx,omitempty" json:"minTx,omitempty"`
	DetectMultiplier int              `yaml:"detectMultiplier,omitempty" json:"detectMultiplier,omitempty" jsonschema:"minimum=1,maximum=50"`
	When             ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type SecretValueSourceSpec struct {
	File   string `yaml:"file,omitempty" json:"file,omitempty"`
	Env    string `yaml:"env,omitempty" json:"env,omitempty"`
	Base64 bool   `yaml:"base64,omitempty" json:"base64,omitempty"`
}

type DHCPv4ClientSpec struct {
	Interface        string `yaml:"interface" json:"interface"`
	Hostname         string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	RequestedAddress string `yaml:"requestedAddress,omitempty" json:"requestedAddress,omitempty"`
	ClassID          string `yaml:"classID,omitempty" json:"classID,omitempty"`
	ClientID         string `yaml:"clientID,omitempty" json:"clientID,omitempty"`
	UseRoutes        *bool  `yaml:"useRoutes,omitempty" json:"useRoutes,omitempty"`
	UseDNS           *bool  `yaml:"useDNS,omitempty" json:"useDNS,omitempty"`
	RouteMetric      int    `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
}

type IPv4StaticRouteSpec struct {
	Destination string `yaml:"destination" json:"destination"`
	Via         string `yaml:"via" json:"via"`
	Interface   string `yaml:"interface" json:"interface"`
	Metric      int    `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
}

type ClusterNetworkRouteSpec struct {
	Pods     ClusterNetworkRouteCIDRSpec  `yaml:"pods,omitempty" json:"pods,omitempty"`
	Services ClusterNetworkRouteCIDRSpec  `yaml:"services,omitempty" json:"services,omitempty"`
	Via      []ClusterNetworkRouteViaSpec `yaml:"via" json:"via"`
	When     ResourceWhenSpec             `yaml:"when,omitempty" json:"when,omitempty"`
}

type ClusterNetworkRouteCIDRSpec struct {
	CIDRs []string `yaml:"cidrs,omitempty" json:"cidrs,omitempty"`
}

type ClusterNetworkRouteViaSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	NextHop   string `yaml:"nextHop" json:"nextHop"`
	Weight    int    `yaml:"weight,omitempty" json:"weight,omitempty" jsonschema:"minimum=0,maximum=999"`
}

type IPv6StaticRouteSpec struct {
	Destination string `yaml:"destination" json:"destination"`
	Via         string `yaml:"via" json:"via"`
	Interface   string `yaml:"interface" json:"interface"`
	Metric      int    `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
}

type DHCPv4ServerSpec struct {
	Server           string              `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq,enum=kea,enum=dhcpd"`
	Managed          bool                `yaml:"managed,omitempty" json:"managed,omitempty"`
	Role             string              `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=server,enum=transit"`
	ListenInterfaces []string            `yaml:"listenInterfaces,omitempty" json:"listenInterfaces,omitempty"`
	LogDHCP          bool                `yaml:"logDHCP,omitempty" json:"logDHCP,omitempty"`
	StickyHoldDays   int                 `yaml:"stickyHoldDays,omitempty" json:"stickyHoldDays,omitempty" jsonschema:"minimum=0"`
	DNS              DHCPv4ServerDNSSpec `yaml:"dns,omitempty" json:"dns,omitempty"`
	Interface        string              `yaml:"interface,omitempty" json:"interface,omitempty"`
	AddressPool      DHCPAddressPoolSpec `yaml:"addressPool,omitempty" json:"addressPool,omitempty"`
	RangeStart       string              `yaml:"rangeStart,omitempty" json:"rangeStart,omitempty"`
	RangeEnd         string              `yaml:"rangeEnd,omitempty" json:"rangeEnd,omitempty"`
	LeaseTime        string              `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	// RouterSource defaults to interfaceAddress; static uses router, and none omits DHCP option 3.
	RouterSource string                `yaml:"routerSource,omitempty" json:"routerSource,omitempty" jsonschema:"enum=interfaceAddress,enum=static,enum=none"`
	Router       string                `yaml:"router,omitempty" json:"router,omitempty"`
	Gateway      string                `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	GatewayFrom  StatusValueSourceSpec `yaml:"gatewayFrom,omitempty" json:"gatewayFrom,omitempty"`
	// DNSSource defaults to self; dhcpv4 reuses DNS servers learned on dnsInterface, static uses dnsServers, and none omits DNS options.
	DNSSource     string                  `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=dhcpv4,enum=static,enum=self,enum=none"`
	DNSInterface  string                  `yaml:"dnsInterface,omitempty" json:"dnsInterface,omitempty"`
	DNSServers    []string                `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	DNSServerFrom []StatusValueSourceSpec `yaml:"dnsServerFrom,omitempty" json:"dnsServerFrom,omitempty"`
	NTPServers    []string                `yaml:"ntpServers,omitempty" json:"ntpServers,omitempty"`
	NTPServerFrom []StatusValueSourceSpec `yaml:"ntpServerFrom,omitempty" json:"ntpServerFrom,omitempty"`
	Domain        string                  `yaml:"domain,omitempty" json:"domain,omitempty"`
	DomainFrom    StatusValueSourceSpec   `yaml:"domainFrom,omitempty" json:"domainFrom,omitempty"`
	Options       []DHCPv4OptionSpec      `yaml:"options,omitempty" json:"options,omitempty"`
	Authoritative bool                    `yaml:"authoritative,omitempty" json:"authoritative,omitempty"`
	LeaseFile     string                  `yaml:"leaseFile,omitempty" json:"leaseFile,omitempty"`
	When          ResourceWhenSpec        `yaml:"when,omitempty" json:"when,omitempty"`
}

type DHCPv4ServerDNSSpec struct {
	Enabled           bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	UpstreamSource    string   `yaml:"upstreamSource,omitempty" json:"upstreamSource,omitempty" jsonschema:"enum=dhcpv4,enum=static,enum=system,enum=none"`
	UpstreamInterface string   `yaml:"upstreamInterface,omitempty" json:"upstreamInterface,omitempty"`
	UpstreamServers   []string `yaml:"upstreamServers,omitempty" json:"upstreamServers,omitempty"`
	CacheSize         int      `yaml:"cacheSize,omitempty" json:"cacheSize,omitempty" jsonschema:"minimum=0"`
}

type DHCPAddressPoolSpec struct {
	Start     string `yaml:"start,omitempty" json:"start,omitempty"`
	End       string `yaml:"end,omitempty" json:"end,omitempty"`
	LeaseTime string `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
}

type DHCPv4OptionSpec struct {
	Code  int    `yaml:"code,omitempty" json:"code,omitempty" jsonschema:"minimum=1,maximum=65535"`
	Name  string `yaml:"name,omitempty" json:"name,omitempty"`
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
}

type DHCPv4ReservationSpec struct {
	Scope      string             `yaml:"scope,omitempty" json:"scope,omitempty"`
	Server     string             `yaml:"server,omitempty" json:"server,omitempty"`
	MACAddress string             `yaml:"macAddress" json:"macAddress"`
	Hostname   string             `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	IPAddress  string             `yaml:"ipAddress" json:"ipAddress"`
	LeaseTime  string             `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	Options    []DHCPv4OptionSpec `yaml:"options,omitempty" json:"options,omitempty"`
}

type DHCPv6AddressSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	Client    string `yaml:"client,omitempty" json:"client,omitempty"`
	Required  bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type IPv6RAAddressSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	Managed   *bool  `yaml:"managed,omitempty" json:"managed,omitempty"`
	Required  bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type DHCPv6PrefixDelegationSpec struct {
	Interface string `yaml:"interface" json:"interface"`
	Client    string `yaml:"client,omitempty" json:"client,omitempty"`
	// Profile applies provider-specific defaults; NTT profiles default prefixLength to 60 and DUID type to link-layer.
	Profile      string `yaml:"profile,omitempty" json:"profile,omitempty" jsonschema:"enum=default,enum=ntt-ngn-direct-hikari-denwa,enum=ntt-hgw-lan-pd"`
	PrefixLength int    `yaml:"prefixLength,omitempty" json:"prefixLength,omitempty" jsonschema:"minimum=1,maximum=128"`
	IAID         string `yaml:"-" json:"-"`
	ClientDUID   string `yaml:"clientDUID,omitempty" json:"clientDUID,omitempty"`
	DUIDType     string `yaml:"-" json:"-"`
	Required     bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type StatusValueSourceSpec struct {
	// Resource names the source resource as Kind/name and reads its status, or a router config field for supported resources.
	Resource string `yaml:"resource" json:"resource"`
	// Field defaults to phase when omitted.
	Field    string `yaml:"field,omitempty" json:"field,omitempty"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type ResourceDependencySpec struct {
	Resource string `yaml:"resource" json:"resource"`
	// Field defaults to phase unless phase is set.
	Field string `yaml:"field,omitempty" json:"field,omitempty"`
	// Phase is shorthand for requiring the dependency phase field to equal this value.
	Phase    string `yaml:"phase,omitempty" json:"phase,omitempty"`
	Equals   string `yaml:"equals,omitempty" json:"equals,omitempty"`
	NotEmpty bool   `yaml:"notEmpty,omitempty" json:"notEmpty,omitempty"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

const (
	IPv6PDProfileDefault                 = "default"
	IPv6PDProfileNTTNGNDirectHikariDenwa = "ntt-ngn-direct-hikari-denwa"
	IPv6PDProfileNTTHGWLANPD             = "ntt-hgw-lan-pd"
)

func IsNTTIPv6PDProfile(profile string) bool {
	switch profile {
	case IPv6PDProfileNTTNGNDirectHikariDenwa, IPv6PDProfileNTTHGWLANPD:
		return true
	default:
		return false
	}
}

func EffectiveIPv6PDPrefixLength(profile string, configured int) int {
	if configured != 0 {
		return configured
	}
	if IsNTTIPv6PDProfile(profile) {
		return 60
	}
	return 0
}

func EffectiveIPv6PDDUIDType(profile, configured string) string {
	if configured != "" {
		return configured
	}
	if IsNTTIPv6PDProfile(profile) {
		return "link-layer"
	}
	return ""
}

type IPv6DelegatedAddressSpec struct {
	// PrefixDelegation references the DHCPv6PrefixDelegation resource that supplies the delegated prefix.
	PrefixDelegation string `yaml:"prefixDelegation" json:"prefixDelegation"`
	PrefixSource     string `yaml:"prefixSource,omitempty" json:"-"`
	Interface        string `yaml:"interface" json:"interface"`
	// SubnetID selects the /64 inside the delegated prefix; it defaults to 0.
	SubnetID string `yaml:"subnetID,omitempty" json:"subnetID,omitempty"`
	// AddressSuffix is ORed into the selected /64 to derive the final IPv6 address.
	AddressSuffix string                   `yaml:"addressSuffix" json:"addressSuffix"`
	SendRA        bool                     `yaml:"sendRA,omitempty" json:"sendRA,omitempty"`
	Announce      bool                     `yaml:"announce,omitempty" json:"announce,omitempty"`
	When          ResourceWhenSpec         `yaml:"when,omitempty" json:"when,omitempty"`
	DependsOn     []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen     []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type DHCPv6InformationSpec struct {
	Interface string                   `yaml:"interface" json:"interface"`
	Request   []string                 `yaml:"request,omitempty" json:"request,omitempty"`
	DependsOn []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type DNSZoneSpec struct {
	Zone         string                 `yaml:"zone" json:"zone"`
	TTL          int                    `yaml:"ttl,omitempty" json:"ttl,omitempty" jsonschema:"minimum=0"`
	DNSSEC       DNSZoneDNSSECSpec      `yaml:"dnssec,omitempty" json:"dnssec,omitempty"`
	Records      []DNSZoneRecordSpec    `yaml:"records,omitempty" json:"records,omitempty"`
	DHCPDerived  DNSZoneDHCPDerivedSpec `yaml:"dhcpDerived,omitempty" json:"dhcpDerived,omitempty"`
	ReverseZones []DNSZoneReverseSpec   `yaml:"reverseZones,omitempty" json:"reverseZones,omitempty"`
	OwnerRefs    []OwnerRef             `yaml:"ownerRefs,omitempty" json:"ownerRefs,omitempty"`
}

type DNSZoneDNSSECSpec struct {
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=validate,enum=sign"`
}

type DNSZoneRecordSpec struct {
	Hostname   string                         `yaml:"hostname" json:"hostname"`
	IPv4       string                         `yaml:"ipv4,omitempty" json:"ipv4,omitempty"`
	IPv4From   StatusValueSourceSpec          `yaml:"ipv4From,omitempty" json:"ipv4From,omitempty"`
	IPv4Source DNSZoneRecordAddressSourceSpec `yaml:"ipv4Source,omitempty" json:"-"`
	IPv6       string                         `yaml:"ipv6,omitempty" json:"ipv6,omitempty"`
	IPv6From   StatusValueSourceSpec          `yaml:"ipv6From,omitempty" json:"ipv6From,omitempty"`
	IPv6Source DNSZoneRecordAddressSourceSpec `yaml:"ipv6Source,omitempty" json:"-"`
	TTL        int                            `yaml:"ttl,omitempty" json:"ttl,omitempty" jsonschema:"minimum=0"`
}

type DNSZoneRecordAddressSourceSpec struct {
	Field    string `yaml:"field" json:"field"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type DNSZoneDHCPDerivedSpec struct {
	Sources        []string `yaml:"sources,omitempty" json:"sources,omitempty"`
	HostnameSuffix string   `yaml:"hostnameSuffix,omitempty" json:"hostnameSuffix,omitempty"`
	DDNS           bool     `yaml:"ddns,omitempty" json:"ddns,omitempty"`
	TTL            int      `yaml:"ttl,omitempty" json:"ttl,omitempty" jsonschema:"minimum=0"`
	LeaseFile      string   `yaml:"leaseFile,omitempty" json:"leaseFile,omitempty"`
}

type DNSZoneReverseSpec struct {
	Name string `yaml:"name" json:"name"`
}

type DNSResolverSpec struct {
	Listen   []DNSResolverListenSpec `yaml:"listen" json:"listen"`
	Sources  []DNSResolverSourceSpec `yaml:"-" json:"sources,omitempty" jsonschema:"-"`
	Cache    DNSResolverCacheSpec    `yaml:"cache,omitempty" json:"cache,omitempty"`
	Metrics  DNSResolverMetricsSpec  `yaml:"metrics,omitempty" json:"metrics,omitempty"`
	QueryLog DNSResolverQueryLogSpec `yaml:"queryLog,omitempty" json:"queryLog,omitempty"`
}

type DNSResolverListenSpec struct {
	Name           string                               `yaml:"name,omitempty" json:"name,omitempty"`
	Addresses      []string                             `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	AddressFrom    []StatusValueSourceSpec              `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
	AddressSources []DNSResolverListenAddressSourceSpec `yaml:"addressSources,omitempty" json:"-"`
	Port           int                                  `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
	Sources        []string                             `yaml:"sources,omitempty" json:"sources,omitempty"`
}

type DNSResolverListenAddressSourceSpec struct {
	Field    string `yaml:"field" json:"field"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type DNSResolverSourceSpec struct {
	Name              string                     `yaml:"name,omitempty" json:"name,omitempty"`
	Kind              string                     `yaml:"kind" json:"kind" jsonschema:"enum=zone,enum=forward,enum=upstream"`
	Match             []string                   `yaml:"match" json:"match"`
	ZoneRef           []string                   `yaml:"zoneRef,omitempty" json:"zoneRef,omitempty"`
	Upstreams         []string                   `yaml:"upstreams,omitempty" json:"upstreams,omitempty"`
	UpstreamFrom      []StatusValueSourceSpec    `yaml:"upstreamFrom,omitempty" json:"upstreamFrom,omitempty"`
	ViaInterface      string                     `yaml:"viaInterface,omitempty" json:"viaInterface,omitempty"`
	BootstrapResolver []string                   `yaml:"bootstrapResolver,omitempty" json:"bootstrapResolver,omitempty"`
	DNSSECValidate    bool                       `yaml:"dnssecValidate,omitempty" json:"dnssecValidate,omitempty"`
	Healthcheck       DNSResolverHealthcheckSpec `yaml:"healthcheck,omitempty" json:"healthcheck,omitempty"`
}

type DNSForwarderSpec struct {
	Resolver       string                     `yaml:"resolver" json:"resolver"`
	Match          []string                   `yaml:"match" json:"match"`
	ZoneRefs       []string                   `yaml:"zoneRefs,omitempty" json:"zoneRefs,omitempty"`
	Upstreams      []string                   `yaml:"upstreams,omitempty" json:"upstreams,omitempty"`
	DNSSECValidate bool                       `yaml:"dnssecValidate,omitempty" json:"dnssecValidate,omitempty"`
	Healthcheck    DNSResolverHealthcheckSpec `yaml:"healthcheck,omitempty" json:"healthcheck,omitempty"`
	When           ResourceWhenSpec           `yaml:"when,omitempty" json:"when,omitempty"`
}

type DNSUpstreamSpec struct {
	Protocol        string                  `yaml:"protocol" json:"protocol" jsonschema:"enum=udp,enum=tcp,enum=dot,enum=doh"`
	Address         string                  `yaml:"address,omitempty" json:"address,omitempty"`
	AddressFrom     []StatusValueSourceSpec `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
	Port            int                     `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
	Path            string                  `yaml:"path,omitempty" json:"path,omitempty"`
	TLSName         string                  `yaml:"tlsName,omitempty" json:"tlsName,omitempty"`
	Bootstrap       []string                `yaml:"bootstrap,omitempty" json:"bootstrap,omitempty"`
	SourceInterface string                  `yaml:"sourceInterface,omitempty" json:"sourceInterface,omitempty"`
	When            ResourceWhenSpec        `yaml:"when,omitempty" json:"when,omitempty"`
}

type DNSResolverCacheSpec struct {
	Enabled     bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MaxEntries  int    `yaml:"maxEntries,omitempty" json:"maxEntries,omitempty" jsonschema:"minimum=0"`
	MinTTL      string `yaml:"minTTL,omitempty" json:"minTTL,omitempty"`
	MaxTTL      string `yaml:"maxTTL,omitempty" json:"maxTTL,omitempty"`
	NegativeTTL string `yaml:"negativeTTL,omitempty" json:"negativeTTL,omitempty"`
}

type DNSResolverMetricsSpec struct {
	PerUpstream bool `yaml:"perUpstream,omitempty" json:"perUpstream,omitempty"`
}

type DNSResolverQueryLogSpec struct {
	Enabled   bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path      string `yaml:"path,omitempty" json:"path,omitempty"`
	Retention string `yaml:"retention,omitempty" json:"retention,omitempty"`
}

type TrafficFlowLogSpec struct {
	Enabled                 bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path                    string `yaml:"path,omitempty" json:"path,omitempty"`
	Source                  string `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"enum=,enum=conntrack"`
	IncludeApplicationLayer bool   `yaml:"includeApplicationLayer,omitempty" json:"includeApplicationLayer,omitempty"`
	IncludeTLSSNI           bool   `yaml:"includeTLSSNI,omitempty" json:"includeTLSSNI,omitempty"`
}

type TrafficFlowLogStatus struct {
	Phase         string `yaml:"phase,omitempty" json:"phase,omitempty"`
	Reason        string `yaml:"reason,omitempty" json:"reason,omitempty"`
	PendingReason string `yaml:"pendingReason,omitempty" json:"pendingReason,omitempty"`

	Path        string `yaml:"path,omitempty" json:"path,omitempty"`
	Source      string `yaml:"source,omitempty" json:"source,omitempty"`
	ActiveFlows int    `yaml:"activeFlows,omitempty" json:"activeFlows,omitempty"`
	Count       int    `yaml:"count,omitempty" json:"count,omitempty"`
	ObservedAt  string `yaml:"observedAt,omitempty" json:"observedAt,omitempty"`

	ApplicationLayer *TrafficFlowApplicationLayerStatus `yaml:"applicationLayer,omitempty" json:"applicationLayer,omitempty"`
}

type TrafficFlowApplicationLayerStatus struct {
	Requested bool   `yaml:"requested" json:"requested"`
	Available bool   `yaml:"available" json:"available"`
	Message   string `yaml:"message,omitempty" json:"message,omitempty"`

	Engine         string `yaml:"engine,omitempty" json:"engine,omitempty"`
	Socket         string `yaml:"socket,omitempty" json:"socket,omitempty"`
	LibNDPILoaded  bool   `yaml:"libndpiLoaded" json:"libndpiLoaded"`
	LibNDPIVersion string `yaml:"libndpiVersion,omitempty" json:"libndpiVersion,omitempty"`

	ProbeError string `yaml:"probeError,omitempty" json:"probeError,omitempty"`
	ObservedAt string `yaml:"observedAt,omitempty" json:"observedAt,omitempty"`
}

type ReadyWhenSpec struct {
	Field    string                     `yaml:"field,omitempty" json:"field,omitempty"`
	Equals   string                     `yaml:"equals,omitempty" json:"equals,omitempty"`
	NotEmpty bool                       `yaml:"not_empty,omitempty" json:"not_empty,omitempty"`
	AnyOf    [][]ReadyWhenPredicateSpec `yaml:"any_of,omitempty" json:"any_of,omitempty"`
}

type ReadyWhenPredicateSpec struct {
	Field    string `yaml:"field,omitempty" json:"field,omitempty"`
	Equals   string `yaml:"equals,omitempty" json:"equals,omitempty"`
	NotEmpty bool   `yaml:"not_empty,omitempty" json:"not_empty,omitempty"`
}

type IPv6RouterAdvertisementSpec struct {
	Interface         string                   `yaml:"interface" json:"interface"`
	Prefix            string                   `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	PrefixFrom        StatusValueSourceSpec    `yaml:"prefixFrom,omitempty" json:"prefixFrom,omitempty"`
	PrefixSource      string                   `yaml:"prefixSource,omitempty" json:"-"`
	RDNSS             []string                 `yaml:"rdnss,omitempty" json:"rdnss,omitempty"`
	RDNSSFrom         []StatusValueSourceSpec  `yaml:"rdnssFrom,omitempty" json:"rdnssFrom,omitempty"`
	DNSSL             []string                 `yaml:"dnssl,omitempty" json:"dnssl,omitempty"`
	DNSSLFrom         []StatusValueSourceSpec  `yaml:"dnsslFrom,omitempty" json:"dnsslFrom,omitempty"`
	MFlag             bool                     `yaml:"mFlag,omitempty" json:"mFlag,omitempty"`
	OFlag             bool                     `yaml:"oFlag,omitempty" json:"oFlag,omitempty"`
	MTU               int                      `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=1280,maximum=65535"`
	PRFPreference     string                   `yaml:"prfPreference,omitempty" json:"prfPreference,omitempty" jsonschema:"enum=,enum=low,enum=medium,enum=high"`
	PreferredLifetime string                   `yaml:"preferredLifetime,omitempty" json:"preferredLifetime,omitempty"`
	ValidLifetime     string                   `yaml:"validLifetime,omitempty" json:"validLifetime,omitempty"`
	ConfigPath        string                   `yaml:"configPath,omitempty" json:"configPath,omitempty"`
	PIDFile           string                   `yaml:"pidFile,omitempty" json:"pidFile,omitempty"`
	DependsOn         []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen         []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type DHCPv6ServerSpec struct {
	Server           string   `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq"`
	Managed          bool     `yaml:"managed,omitempty" json:"managed,omitempty"`
	Role             string   `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=server,enum=transit"`
	ListenInterfaces []string `yaml:"listenInterfaces,omitempty" json:"listenInterfaces,omitempty"`
	LogDHCP          bool     `yaml:"logDHCP,omitempty" json:"logDHCP,omitempty"`
	StickyHoldDays   int      `yaml:"stickyHoldDays,omitempty" json:"stickyHoldDays,omitempty" jsonschema:"minimum=0"`
	Interface        string   `yaml:"interface,omitempty" json:"interface,omitempty"`
	// DelegatedAddress references an IPv6DelegatedAddress used to derive self DNS and RA-adjacent settings.
	DelegatedAddress string `yaml:"delegatedAddress,omitempty" json:"delegatedAddress,omitempty"`
	// Mode defaults to stateless; ra-only emits router advertisements without DHCPv6 service.
	Mode         string              `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=stateless,enum=stateful,enum=both,enum=ra-only"`
	AddressPool  DHCPAddressPoolSpec `yaml:"addressPool,omitempty" json:"addressPool,omitempty"`
	DefaultRoute bool                `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	// DNSSource defaults to self; static uses dnsServers, and none omits DNS options.
	DNSSource string `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=self,enum=static,enum=none"`
	// SelfAddressPolicy references a SelfAddressPolicy resource used when dnsSource is self.
	SelfAddressPolicy string                   `yaml:"selfAddressPolicy,omitempty" json:"selfAddressPolicy,omitempty"`
	DNSServers        []string                 `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	DNSServerFrom     []StatusValueSourceSpec  `yaml:"dnsServerFrom,omitempty" json:"dnsServerFrom,omitempty"`
	SNTPServers       []string                 `yaml:"sntpServers,omitempty" json:"sntpServers,omitempty"`
	SNTPServerFrom    []StatusValueSourceSpec  `yaml:"sntpServerFrom,omitempty" json:"sntpServerFrom,omitempty"`
	DomainSearch      []string                 `yaml:"domainSearch,omitempty" json:"domainSearch,omitempty"`
	DomainSearchFrom  []StatusValueSourceSpec  `yaml:"domainSearchFrom,omitempty" json:"domainSearchFrom,omitempty"`
	LeaseTime         string                   `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	RapidCommit       bool                     `yaml:"rapidCommit,omitempty" json:"rapidCommit,omitempty"`
	ConfigPath        string                   `yaml:"configPath,omitempty" json:"configPath,omitempty"`
	PIDFile           string                   `yaml:"pidFile,omitempty" json:"pidFile,omitempty"`
	LeaseFile         string                   `yaml:"leaseFile,omitempty" json:"leaseFile,omitempty"`
	DependsOn         []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen         []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
	When              ResourceWhenSpec         `yaml:"when,omitempty" json:"when,omitempty"`
}

type DHCPLeaseSyncSpec struct {
	LeaseFile string                    `yaml:"leaseFile,omitempty" json:"leaseFile,omitempty"`
	Command   string                    `yaml:"command,omitempty" json:"command,omitempty"`
	Interval  string                    `yaml:"interval,omitempty" json:"interval,omitempty"`
	Sources   []DHCPLeaseSyncSourceSpec `yaml:"sources,omitempty" json:"sources,omitempty"`
	Targets   []DHCPLeaseSyncTargetSpec `yaml:"targets,omitempty" json:"targets,omitempty"`
	When      ResourceWhenSpec          `yaml:"when,omitempty" json:"when,omitempty"`
}

type DHCPLeaseSyncSourceSpec struct {
	Name     string `yaml:"name,omitempty" json:"name,omitempty"`
	Path     string `yaml:"path" json:"path"`
	Required *bool  `yaml:"required,omitempty" json:"required,omitempty"`
}

type DHCPLeaseSyncTargetSpec struct {
	Name       string   `yaml:"name,omitempty" json:"name,omitempty"`
	Host       string   `yaml:"host" json:"host"`
	User       string   `yaml:"user,omitempty" json:"user,omitempty"`
	Path       string   `yaml:"path,omitempty" json:"path,omitempty"`
	SSHOptions []string `yaml:"sshOptions,omitempty" json:"sshOptions,omitempty"`
	Options    []string `yaml:"options,omitempty" json:"options,omitempty"`
}

type DHCPv4RelaySpec struct {
	Interfaces []string `yaml:"interfaces" json:"interfaces"`
	Upstream   string   `yaml:"upstream" json:"upstream"`
}

type ResourceWhenSpec struct {
	// State gates a resource on status fields from other resources.
	State map[string]StateMatchSpec `yaml:"state,omitempty" json:"state,omitempty"`
	All   []ResourceWhenSpec        `yaml:"all,omitempty" json:"all,omitempty" jsonschema:"-"`
	Any   []ResourceWhenSpec        `yaml:"any,omitempty" json:"any,omitempty" jsonschema:"-"`
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
	// Source selects how the candidate address is found: delegatedAddress derives from a delegated prefix, interfaceAddress scans live interface addresses, and static uses address.
	Source           string `yaml:"source" json:"source" jsonschema:"enum=delegatedAddress,enum=interfaceAddress,enum=static"`
	Interface        string `yaml:"interface,omitempty" json:"interface,omitempty"`
	DelegatedAddress string `yaml:"delegatedAddress,omitempty" json:"delegatedAddress,omitempty"`
	Address          string `yaml:"address,omitempty" json:"address,omitempty"`
	// AddressSuffix defaults to the referenced delegated address suffix for delegatedAddress candidates.
	AddressSuffix string `yaml:"addressSuffix,omitempty" json:"addressSuffix,omitempty"`
	MatchSuffix   string `yaml:"matchSuffix,omitempty" json:"matchSuffix,omitempty"`
	// Ordinal is one-based when selecting an address from an interface.
	Ordinal int `yaml:"ordinal,omitempty" json:"ordinal,omitempty" jsonschema:"minimum=1"`
}

type DNSResolverHealthcheckSpec struct {
	Interval      string `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout       string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	FailThreshold int    `yaml:"failThreshold,omitempty" json:"failThreshold,omitempty" jsonschema:"minimum=1"`
	PassThreshold int    `yaml:"passThreshold,omitempty" json:"passThreshold,omitempty" jsonschema:"minimum=1"`
}

type DSLiteTunnelSpec struct {
	Interface  string `yaml:"interface" json:"interface"`
	TunnelName string `yaml:"tunnelName,omitempty" json:"tunnelName,omitempty"`
	// Enabled defaults to true; set enabled: false to keep the tunnel disabled.
	Enabled            *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AFTRFQDN           string   `yaml:"aftrFQDN,omitempty" json:"aftrFQDN,omitempty"`
	AFTRIPv6           string   `yaml:"aftrIPv6,omitempty" json:"aftrIPv6,omitempty"`
	AFTRDNSServers     []string `yaml:"aftrDNSServers,omitempty" json:"aftrDNSServers,omitempty"`
	AFTRAddressOrdinal int      `yaml:"aftrAddressOrdinal,omitempty" json:"aftrAddressOrdinal,omitempty" jsonschema:"minimum=1"`
	// AFTRAddressSelection controls how multiple AAAA records are selected; ordinalModulo wraps the ordinal by the answer count.
	AFTRAddressSelection string                `yaml:"aftrAddressSelection,omitempty" json:"aftrAddressSelection,omitempty" jsonschema:"enum=ordinal,enum=ordinalModulo"`
	RemoteAddress        string                `yaml:"remoteAddress,omitempty" json:"remoteAddress,omitempty"`
	LocalAddress         string                `yaml:"localAddress,omitempty" json:"localAddress,omitempty"`
	LocalAddressFrom     StatusValueSourceSpec `yaml:"localAddressFrom,omitempty" json:"localAddressFrom,omitempty"`
	LocalIPv6Source      string                `yaml:"localIPv6Source,omitempty" json:"-"`
	AFTRFrom             StatusValueSourceSpec `yaml:"aftrFrom,omitempty" json:"aftrFrom,omitempty"`
	AFTRSource           string                `yaml:"aftrSource,omitempty" json:"-"`
	// LocalAddressSource defaults to interface; delegatedAddress derives the tunnel source from localDelegatedAddress.
	LocalAddressSource    string                   `yaml:"localAddressSource,omitempty" json:"localAddressSource,omitempty" jsonschema:"enum=interface,enum=static,enum=delegatedAddress"`
	LocalDelegatedAddress string                   `yaml:"localDelegatedAddress,omitempty" json:"localDelegatedAddress,omitempty"`
	LocalAddressSuffix    string                   `yaml:"localAddressSuffix,omitempty" json:"localAddressSuffix,omitempty"`
	DefaultRoute          bool                     `yaml:"defaultRoute,omitempty" json:"defaultRoute,omitempty"`
	RouteMetric           int                      `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	MTU                   int                      `yaml:"mtu,omitempty" json:"mtu,omitempty" jsonschema:"minimum=1280,maximum=65535"`
	EncapsulationLimit    string                   `yaml:"encapsulationLimit,omitempty" json:"encapsulationLimit,omitempty"`
	When                  ResourceWhenSpec         `yaml:"when,omitempty" json:"when,omitempty"`
	DependsOn             []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen             []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type IPv4RouteSpec struct {
	Destination string                `yaml:"destination" json:"destination"`
	Type        string                `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=unicast,enum=blackhole"`
	Device      string                `yaml:"device,omitempty" json:"device,omitempty"`
	DeviceFrom  StatusValueSourceSpec `yaml:"deviceFrom,omitempty" json:"deviceFrom,omitempty"`
	Gateway     string                `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	GatewayFrom StatusValueSourceSpec `yaml:"gatewayFrom,omitempty" json:"gatewayFrom,omitempty"`
	// PreferredSource programs the Linux route preferred source (RTA_PREFSRC).
	// It is useful for host-originated traffic that must use a stable logical
	// source address while still following an explicit delivery route.
	PreferredSource string                   `yaml:"preferredSource,omitempty" json:"preferredSource,omitempty"`
	Metric          int                      `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
	DependsOn       []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen       []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type OverlayPeerSpec struct {
	Role     string          `yaml:"role" json:"role" jsonschema:"enum=onprem,enum=cloud"`
	NodeID   string          `yaml:"nodeID" json:"nodeID"`
	Underlay OverlayUnderlay `yaml:"underlay" json:"underlay"`
	Remote   OverlayRemote   `yaml:"remote,omitempty" json:"remote,omitempty"`
	PathMTU  PathMTUOptions  `yaml:"pathMTU,omitempty" json:"pathMTU,omitempty"`
}

type OverlayUnderlay struct {
	Type      string `yaml:"type" json:"type" jsonschema:"enum=wireguard,enum=tailscale,enum=ipsec,enum=route,enum=ipip,enum=gre,enum=fou,enum=gue"`
	Interface string `yaml:"interface,omitempty" json:"interface,omitempty"`
	Address   string `yaml:"address,omitempty" json:"address,omitempty"`
}

type OverlayRemote struct {
	NodeID  string `yaml:"nodeID,omitempty" json:"nodeID,omitempty"`
	Address string `yaml:"address,omitempty" json:"address,omitempty"`
}

type HybridRouteSpec struct {
	DestinationCIDRs []string           `yaml:"destinationCIDRs" json:"destinationCIDRs"`
	PeerRef          string             `yaml:"peerRef" json:"peerRef"`
	Install          HybridRouteInstall `yaml:"install,omitempty" json:"install,omitempty"`
	HealthCheckRef   string             `yaml:"healthCheckRef,omitempty" json:"healthCheckRef,omitempty"`
}

type HybridRouteInstall struct {
	Table  string `yaml:"table,omitempty" json:"table,omitempty" jsonschema:"enum=,enum=main"`
	Metric int    `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
}

type AddressMobilityDomainSpec struct {
	Prefix  string `yaml:"prefix" json:"prefix"`
	Mode    string `yaml:"mode" json:"mode" jsonschema:"enum=selective-address"`
	PeerRef string `yaml:"peerRef,omitempty" json:"peerRef,omitempty"`
}

// EventGroupSpec declares a CloudEdge Event Federation bus identity (ADR 0006).
//
// An EventGroup names a cross-node event bus that routerd nodes share. It is the
// Phase 1 anchor for Event Federation and is intentionally distinct from the
// observability event* subsystems (eventlog/eventfile) and the local EventRule
// automation primitive — those are node-local, EventGroup is cross-node.
type EventGroupSpec struct {
	// NodeName is this node's identity within the group; it is stamped as the
	// sourceNode on events emitted into this group.
	NodeName string `yaml:"nodeName" json:"nodeName"`
	// Retention bounds how many federation events and for how long the local
	// store keeps them. Empty/zero values mean unlimited.
	Retention EventGroupRetention `yaml:"retention,omitempty" json:"retention,omitempty"`
	// Auth is reserved for Phase 2 peer delivery (HMAC over the overlay). It is
	// accepted but unused in Phase 1.
	Auth EventGroupAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
	// Listen is the receiver bind for inbound peer pushes. Empty Address means
	// this node is push-only (no receiver).
	Listen EventGroupListen `yaml:"listen,omitempty" json:"listen,omitempty"`
	// ReplayWindow is a Go duration bounding the accepted message timestamp skew
	// for replay protection; empty is treated as "5m" downstream.
	ReplayWindow string `yaml:"replayWindow,omitempty" json:"replayWindow,omitempty"`
}

// EventGroupListen is the receiver bind for inbound peer pushes. Empty Address
// means this node is push-only (no receiver). Bind to the overlay address (e.g.
// the wg-hybrid address), never 0.0.0.0 implicitly.
type EventGroupListen struct {
	Address string `yaml:"address,omitempty" json:"address,omitempty"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=0,maximum=65535"`
}

// EventGroupRetention bounds local retention of federation events for a group.
type EventGroupRetention struct {
	// MaxEvents caps the number of retained events for the group; 0 means
	// unlimited.
	MaxEvents int `yaml:"maxEvents,omitempty" json:"maxEvents,omitempty" jsonschema:"minimum=0"`
	// MaxAge is a Go duration (e.g. "30m", "24h") bounding event age; "" means
	// unlimited.
	MaxAge string `yaml:"maxAge,omitempty" json:"maxAge,omitempty"`
}

// EventGroupAuth is reserved for Phase 2 peer delivery integrity (message-level
// HMAC with a shared secret). It is validated leniently and unused in Phase 1.
type EventGroupAuth struct {
	// Mode selects the integrity scheme; only "hmac" (or empty) is recognized.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=hmac"`
	// SecretRef references a secret resource carrying the shared HMAC key.
	SecretRef string `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`
	// SecretFile is a filesystem path to the shared HMAC key.
	SecretFile string `yaml:"secretFile,omitempty" json:"secretFile,omitempty"`
}

// EventPeerSpec declares a remote node a routerd node pushes federation events
// to within an EventGroup (ADR 0006, Phase 2). It is the delivery target; the
// EventGroup names the bus, the EventPeer names where to forward.
type EventPeerSpec struct {
	// GroupRef is the EventGroup this peer belongs to (required).
	GroupRef string `yaml:"groupRef" json:"groupRef"`
	// NodeName is the remote peer node identity (required).
	NodeName string `yaml:"nodeName" json:"nodeName"`
	// Endpoint is the base URL to push to, e.g. http://10.99.0.7:8787. Required
	// for push delivery.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	// Direction selects delivery direction; only "push" is supported in Phase 2.
	// Empty defaults to "push".
	Direction string `yaml:"direction,omitempty" json:"direction,omitempty" jsonschema:"enum=push"`
	// Types optionally filters delivery to these event types; empty delivers all.
	Types []string `yaml:"types,omitempty" json:"types,omitempty"`
	// SubjectPrefixes optionally filters delivery to subjects carrying one of
	// these prefixes; empty delivers all.
	SubjectPrefixes []string `yaml:"subjectPrefixes,omitempty" json:"subjectPrefixes,omitempty"`
}

// EventSubscriptionSpec declares that received federation events matching a
// predicate trigger a local Plugin (ADR 0006, Phase 3): the cloud-side rule that
// turns an observed fact into a DynamicConfigPart via a trusted local plugin.
type EventSubscriptionSpec struct {
	// GroupRef is the EventGroup whose events this subscription watches.
	GroupRef string `yaml:"groupRef" json:"groupRef"`
	// Match selects which events fire the subscription. Types is required so a
	// subscription cannot blanket-trigger a plugin on every event in the group.
	Match EventSubscriptionMatch `yaml:"match" json:"match"`
	// Trigger names the plugin to invoke and optional batching.
	Trigger EventSubscriptionTrigger `yaml:"trigger" json:"trigger"`
}

// EventSubscriptionMatch is the event predicate. Types is required (>=1);
// the rest narrow further. Empty optional slices/maps mean "any".
type EventSubscriptionMatch struct {
	// Types restricts to these event types (required, at least one), e.g.
	// routerd.client.ipv4.observed.
	Types []string `yaml:"types" json:"types"`
	// SubjectPrefixes restricts to subjects with one of these prefixes.
	SubjectPrefixes []string `yaml:"subjectPrefixes,omitempty" json:"subjectPrefixes,omitempty"`
	// Payload requires each listed key to equal the given value in the event payload.
	Payload map[string]string `yaml:"payload,omitempty" json:"payload,omitempty"`
	// SourceNodes restricts to events whose sourceNode is one of these (loop/scope guard).
	SourceNodes []string `yaml:"sourceNodes,omitempty" json:"sourceNodes,omitempty"`
}

// EventSubscriptionTrigger names the Plugin and optional coalescing windows.
type EventSubscriptionTrigger struct {
	// PluginRef names the Plugin resource invoked for matched events.
	PluginRef string `yaml:"pluginRef" json:"pluginRef"`
	// BatchWindow optionally coalesces matched events into one invocation
	// (Go duration). MVP invokes per poll tick; this is accepted forward-compat.
	BatchWindow string `yaml:"batchWindow,omitempty" json:"batchWindow,omitempty"`
	// Debounce optionally delays invocation after the last matched event
	// (Go duration). Accepted forward-compat; MVP uses poll-tick batching.
	Debounce string `yaml:"debounce,omitempty" json:"debounce,omitempty"`
}

// MobilityPoolSpec declares a selective-address mobility pool for the CloudEdge
// Mobility Control Plane. It is the ONLY operator-authored Kind in the
// mobility plane: the operator declares the /24, which routerd nodes are members
// and at which site, and the capture/authority policy ONCE. The system then
// derives BGP /32 advertisements and provider trap action plans from static
// intent and observed-client federation events.
type MobilityPoolSpec struct {
	// Prefix is the CIDR managed by this pool, e.g. 10.88.60.0/24 (required).
	Prefix string `yaml:"prefix" json:"prefix"`
	// GroupRef is the EventGroup whose observed/expired events can feed BGP
	// mobility ownership for this pool (required).
	GroupRef string `yaml:"groupRef" json:"groupRef"`
	// Values carries non-secret per-router constants referenced by profiles.
	// It is intended for provider identifiers such as NIC, subnet, region, or
	// resource group names; credentials and tokens do not belong here.
	Values map[string]string `yaml:"values,omitempty" json:"values,omitempty"`
	// Profiles defines reusable capture/discovery fragments. Members opt in
	// with profileRef and can override any concrete field locally.
	Profiles MobilityPoolProfiles `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	// Mode selects the mobility scheme. Only "selective-address" is supported in
	// the MVP; empty defaults to it.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=selective-address"`
	// Members maps routerd nodes to the site they serve within the pool
	// (required, at least one).
	Members []MobilityPoolMember `yaml:"members" json:"members"`
	// StaticHandovers declares planned static-owned address movement from an
	// on-prem member to another member. The controller releases the from member
	// first and only projects the to member after observing the release event.
	StaticHandovers []MobilityStaticHandover `yaml:"staticHandovers,omitempty" json:"staticHandovers,omitempty"`
	// CapturePolicy declares how non-owner sites capture an address that has
	// moved.
	CapturePolicy MobilityCapturePolicy `yaml:"capturePolicy,omitempty" json:"capturePolicy,omitempty"`
	// IPOwnershipPolicy retains high-level failover intent for BGP-mode
	// mobility. Ownership itself is represented by BGP best-path.
	IPOwnershipPolicy MobilityIPOwnershipPolicy `yaml:"ipOwnershipPolicy,omitempty" json:"ipOwnershipPolicy,omitempty"`
	// DeliveryPolicy selects the pool-level delivery control plane. Empty means
	// bgp; owned /32s are advertised and remote /32s are learned through BGP.
	DeliveryPolicy MobilityDeliveryPolicy `yaml:"deliveryPolicy,omitempty" json:"deliveryPolicy,omitempty"`
	// Authority declares who arbitrates ownership. The MVP supports static
	// arbitration only. Empty means every node deterministically projects the
	// shared event stream locally.
	Authority MobilityAuthority `yaml:"authority,omitempty" json:"authority,omitempty"`
}

type MobilityPoolProfiles struct {
	// CloudCaptures contains reusable cloud provider-secondary-ip capture
	// defaults. It deliberately models only cloud capture/discovery because
	// on-prem proxy-ARP authority must remain explicit and fail-closed.
	CloudCaptures map[string]MobilityCloudCaptureProfile `yaml:"cloudCaptures,omitempty" json:"cloudCaptures,omitempty"`
}

type MobilityCloudCaptureProfile struct {
	// Capture defaults for provider-secondary-ip cloud capture.
	Capture MobilityMemberCapture `yaml:"capture,omitempty" json:"capture,omitempty"`
	// OwnershipDiscovery defaults for provider private-IP observation.
	OwnershipDiscovery MobilityOwnershipDiscovery `yaml:"ownershipDiscovery,omitempty" json:"ownershipDiscovery,omitempty"`
}

// MobilityPoolMember binds a routerd node to a site within a MobilityPool. Both
// fields are required; NodeRef must be unique within the pool.
type MobilityPoolMember struct {
	// NodeRef is the routerd node identity (matches federation sourceNode).
	NodeRef string `yaml:"nodeRef" json:"nodeRef"`
	// Site is the location label the node serves within the pool.
	Site string `yaml:"site" json:"site"`
	// Role selects the SAM side semantics for this node. It is separate from Site
	// so sites can be named for topology while role remains provider-agnostic.
	Role string `yaml:"role" json:"role" jsonschema:"enum=onprem,enum=cloud"`
	// ProfileRef selects a profile from spec.profiles.cloudCaptures. It is a
	// local shorthand for the node's capture/discovery details; remote members
	// should normally remain identity-only.
	ProfileRef string `yaml:"profileRef,omitempty" json:"profileRef,omitempty"`
	// Capture declares how this member captures addresses currently owned by
	// another site.
	Capture MobilityMemberCapture `yaml:"capture,omitempty" json:"capture,omitempty"`
	// Delivery is retained for hand-authored SAM compatibility. BGP-mode
	// MobilityPool delivery does not require per-member delivery routes.
	Delivery MobilityMemberDelivery `yaml:"delivery,omitempty" json:"delivery,omitempty"`
	// DeliveryTo optionally selects delivery per owner identity. It is retained
	// for SAM compatibility; BGP-mode mobility does not lower per-lease routes.
	DeliveryTo []MobilityMemberDeliveryTarget `yaml:"deliveryTo,omitempty" json:"deliveryTo,omitempty"`
	// StaticOwnedAddresses declares IPv4 /32 addresses in the pool that this
	// member owns without relying on observed-client federation events. It is
	// intended for on-prem static-IP segments.
	StaticOwnedAddresses []string `yaml:"staticOwnedAddresses,omitempty" json:"staticOwnedAddresses,omitempty"`
	// OwnershipDiscovery optionally lets a cloud member discover locally owned
	// provider private IPs and emit them as observed federation facts.
	OwnershipDiscovery MobilityOwnershipDiscovery `yaml:"ownershipDiscovery,omitempty" json:"ownershipDiscovery,omitempty"`
	// Placement optionally places this member in an active/standby capture group.
	// When set, only the highest-priority non-drained member in the same group
	// captures provider-side addresses. Provider-secondary members that share a
	// site/providerRef with an existing placement group must declare placement.
	Placement MobilityMemberPlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
	// Maintenance carries declarative operator maintenance intent for this member.
	// Drained placement members are excluded from active capture selection.
	Maintenance MobilityMemberMaintenance `yaml:"maintenance,omitempty" json:"maintenance,omitempty"`
}

type MobilityStaticHandover struct {
	Address     string `yaml:"address" json:"address"`
	FromNodeRef string `yaml:"fromNodeRef" json:"fromNodeRef"`
	ToNodeRef   string `yaml:"toNodeRef" json:"toNodeRef"`
}

type MobilityMemberPlacement struct {
	Group string `yaml:"group,omitempty" json:"group,omitempty"`
	// Priority orders active/standby candidates. Empty/0 is auto-numbered
	// deterministically within the group as 10, 20, ... while preserving any
	// explicitly configured priorities.
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"minimum=0,maximum=1000000"`
}

type MobilityMemberMaintenance struct {
	Drain bool `yaml:"drain,omitempty" json:"drain,omitempty"`
}

type MobilityOwnershipDiscovery struct {
	// Mode selects the discovery backend. Empty/disabled does nothing.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=disabled,enum=provider-private-ip"`
	// ProviderRef defaults to the member capture.providerRef.
	ProviderRef string `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	// PluginRef optionally pins the inventory plugin. Empty resolves by
	// provider name or sole observe.providerPrivateIPs plugin.
	PluginRef string `yaml:"pluginRef,omitempty" json:"pluginRef,omitempty"`
	// SubnetRef optionally constrains the provider scan. Empty asks the plugin
	// to infer it from the self NIC.
	SubnetRef string `yaml:"subnetRef,omitempty" json:"subnetRef,omitempty"`
	// SubnetRefFrom resolves SubnetRef from spec.values when SubnetRef is not
	// set explicitly.
	SubnetRefFrom string `yaml:"subnetRefFrom,omitempty" json:"subnetRefFrom,omitempty"`
	// ScanInterval bounds provider inventory calls. Empty defaults to 60s.
	ScanInterval string `yaml:"scanInterval,omitempty" json:"scanInterval,omitempty"`
	// LeaseTTL controls the emitted observed event expiry. Empty defaults to the
	// controller default.
	LeaseTTL string `yaml:"leaseTTL,omitempty" json:"leaseTTL,omitempty"`
	// Scope narrows which provider private IPs become mobility ownership facts.
	// Empty keeps the historical broad scan behavior.
	Scope MobilityOwnershipDiscoveryScope `yaml:"scope,omitempty" json:"scope,omitempty"`
	// Selector optionally filters discovered IP records by provider tags/labels.
	Selector MobilityOwnershipDiscoverySelector `yaml:"selector,omitempty" json:"selector,omitempty"`
}

type MobilityOwnershipDiscoveryScope struct {
	// IncludePrimary controls whether provider-primary private IPs may become
	// mobility-owned addresses. Nil defaults to true for backward compatibility.
	IncludePrimary *bool `yaml:"includePrimary,omitempty" json:"includePrimary,omitempty"`
	// IncludeAddresses optionally allowlists discovered addresses by IPv4 CIDR or
	// bare IPv4 address. Empty means all pool addresses are candidates.
	IncludeAddresses []string `yaml:"includeAddresses,omitempty" json:"includeAddresses,omitempty"`
	// ExcludeAddresses optionally denylists discovered addresses by IPv4 CIDR or
	// bare IPv4 address.
	ExcludeAddresses []string `yaml:"excludeAddresses,omitempty" json:"excludeAddresses,omitempty"`
}

type MobilityOwnershipDiscoverySelector struct {
	Tags map[string]string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

type MobilityIPOwnershipPolicy struct {
	Type         string   `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=,enum=centralized"`
	PreferNodes  []string `yaml:"preferNodes,omitempty" json:"preferNodes,omitempty"`
	AutoFailover bool     `yaml:"autoFailover,omitempty" json:"autoFailover,omitempty"`
}

type MobilityMemberCapture struct {
	Type               string            `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=provider-secondary-ip,enum=proxy-arp"`
	ProviderRef        string            `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	ProviderMode       string            `yaml:"providerMode,omitempty" json:"providerMode,omitempty"`
	NICRef             string            `yaml:"nicRef,omitempty" json:"nicRef,omitempty"`
	ConfigureOSAddress bool              `yaml:"configureOSAddress,omitempty" json:"configureOSAddress,omitempty"`
	Interface          string            `yaml:"interface,omitempty" json:"interface,omitempty"`
	GratuitousARP      bool              `yaml:"gratuitousARP,omitempty" json:"gratuitousARP,omitempty"`
	ActiveWhen         CaptureActiveWhen `yaml:"activeWhen,omitempty" json:"activeWhen,omitempty"`
	// Target carries non-secret provider target hints such as region,
	// compartmentId, resourceGroup, nicName, or ipConfigName. It is copied to
	// provider ActionPlan.target; credentials and tokens do not belong here.
	Target map[string]string `yaml:"target,omitempty" json:"target,omitempty"`
	// TargetFrom resolves provider target hints from spec.values. Keys are
	// provider target keys and values are spec.values keys. Explicit target
	// entries take precedence over targetFrom.
	TargetFrom map[string]string `yaml:"targetFrom,omitempty" json:"targetFrom,omitempty"`
}

type CaptureActiveWhen struct {
	Type              string `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=,enum=single-router,enum=vrrp-master"`
	VirtualAddressRef string `yaml:"virtualAddressRef,omitempty" json:"virtualAddressRef,omitempty"`
}

type MobilityMemberDelivery struct {
	PeerRef         string `yaml:"peerRef,omitempty" json:"peerRef,omitempty"`
	Mode            string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=route"`
	TunnelInterface string `yaml:"tunnelInterface,omitempty" json:"tunnelInterface,omitempty"`
}

type MobilityMemberDeliveryTarget struct {
	NodeRef         string `yaml:"nodeRef,omitempty" json:"nodeRef,omitempty"`
	Site            string `yaml:"site,omitempty" json:"site,omitempty"`
	Role            string `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=,enum=onprem,enum=cloud"`
	PeerRef         string `yaml:"peerRef" json:"peerRef"`
	Mode            string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=route"`
	TunnelInterface string `yaml:"tunnelInterface,omitempty" json:"tunnelInterface,omitempty"`
}

// MobilityCapturePolicy declares how non-owner sites capture a moved address.
type MobilityCapturePolicy struct {
	// Mode selects the capture behavior. Only "all-non-owner-sites" is supported
	// in the MVP; empty defaults to it.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=all-non-owner-sites"`
}

// MobilityDeliveryPolicy selects the mobility delivery control plane.
type MobilityDeliveryPolicy struct {
	// Mode selects delivery. Empty means bgp delivery.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=bgp"`
}

// MobilityAuthority declares who arbitrates address ownership in the pool.
type MobilityAuthority struct {
	// Mode selects the arbitration scheme. Only "static" is supported in the
	// MVP; empty defaults to it.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=static"`
	// NodeRef optionally names the arbitrating node. When set, it must be one of
	// the member NodeRefs.
	NodeRef string `yaml:"nodeRef,omitempty" json:"nodeRef,omitempty"`
}

type CloudProviderProfileSpec struct {
	Provider       string       `yaml:"provider" json:"provider" jsonschema:"enum=azure,enum=aws,enum=oci,enum=gcp"`
	SubscriptionID string       `yaml:"subscriptionID,omitempty" json:"subscriptionID,omitempty"`
	ResourceGroup  string       `yaml:"resourceGroup,omitempty" json:"resourceGroup,omitempty"`
	Capabilities   []string     `yaml:"capabilities" json:"capabilities"`
	Auth           ProviderAuth `yaml:"auth" json:"auth"`
}

type ProviderAuth struct {
	Mode    string `yaml:"mode" json:"mode" jsonschema:"enum=external-command"`
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
}

type RemoteAddressClaimSpec struct {
	DomainRef string          `yaml:"domainRef" json:"domainRef"`
	Address   string          `yaml:"address" json:"address"`
	OwnerSide string          `yaml:"ownerSide" json:"ownerSide" jsonschema:"enum=cloud,enum=onprem"`
	Capture   AddressCapture  `yaml:"capture" json:"capture"`
	Delivery  AddressDelivery `yaml:"delivery" json:"delivery"`
}

type AddressCapture struct {
	Type               string            `yaml:"type" json:"type" jsonschema:"enum=provider-secondary-ip,enum=proxy-arp"`
	ProviderRef        string            `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	ProviderMode       string            `yaml:"providerMode,omitempty" json:"providerMode,omitempty"`
	NICRef             string            `yaml:"nicRef,omitempty" json:"nicRef,omitempty"`
	ConfigureOSAddress bool              `yaml:"configureOSAddress,omitempty" json:"configureOSAddress,omitempty"`
	Interface          string            `yaml:"interface,omitempty" json:"interface,omitempty"`
	GratuitousARP      bool              `yaml:"gratuitousARP,omitempty" json:"gratuitousARP,omitempty"`
	ActiveWhen         CaptureActiveWhen `yaml:"activeWhen,omitempty" json:"activeWhen,omitempty"`
}

type AddressDelivery struct {
	PeerRef         string `yaml:"peerRef" json:"peerRef"`
	Mode            string `yaml:"mode" json:"mode" jsonschema:"enum=route"`
	TunnelInterface string `yaml:"tunnelInterface,omitempty" json:"tunnelInterface,omitempty"`
}

// ProviderActionPolicySpec gates whether routerd may execute plugin-proposed
// provider actions (ADR 0007, Phase 5). It is EXPERIMENTAL.
//
// The default zero value is the safe, locked-down state: execution is disabled,
// only dry-run is permitted, approval is required, and no provider/action is
// allowlisted. routerd core never holds or passes provider credentials; an
// executor plugin (capability execute.providerAction) performs the mutation in
// its own process with its own cloud-native identity. This policy only decides
// whether an approved action is permitted to be handed to an executor.
type ProviderActionPolicySpec struct {
	// Enabled must be true before any execution is permitted. The bool zero
	// value (false) keeps execution disabled by default.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// DryRunOnly, when nil or true, permits only dry-run; live mutation is
	// rejected. Set explicitly to false to allow live execution (still gated by
	// Enabled, approval, and the allowlists).
	DryRunOnly *bool `yaml:"dryRunOnly,omitempty" json:"dryRunOnly,omitempty"`
	// RequireApproval, when nil or true, requires an operator approval before an
	// action is executed. Set to false only to allow policy auto-approve.
	RequireApproval *bool `yaml:"requireApproval,omitempty" json:"requireApproval,omitempty"`
	// AllowedProviders restricts which providers may be executed. Empty = none.
	AllowedProviders []string `yaml:"allowedProviders,omitempty" json:"allowedProviders,omitempty"`
	// AllowedProviderRefs optionally restricts to specific CloudProviderProfile
	// references. Empty = no provider-ref restriction.
	AllowedProviderRefs []string `yaml:"allowedProviderRefs,omitempty" json:"allowedProviderRefs,omitempty"`
	// AllowedActions restricts which canonical action verbs may be executed.
	// Empty = none.
	AllowedActions []string `yaml:"allowedActions,omitempty" json:"allowedActions,omitempty"`
	// AllowedCIDRs restricts execution to actions whose target.address falls
	// within one of these CIDRs. Empty = no CIDR restriction.
	AllowedCIDRs []string `yaml:"allowedCIDRs,omitempty" json:"allowedCIDRs,omitempty"`
	// MaxActionsPerRun caps how many actions one execution run may perform. The
	// zero value means 0 (no actions): the operator must set a positive bound.
	MaxActionsPerRun int `yaml:"maxActionsPerRun,omitempty" json:"maxActionsPerRun,omitempty"`
	// AllowUndo permits best-effort rollback. The zero value (false) disables it.
	AllowUndo bool `yaml:"allowUndo,omitempty" json:"allowUndo,omitempty"`
	// ExecutionWindow optionally restricts execution to a time window. It is
	// validated leniently (free-form, cron-ish); empty = no window restriction.
	ExecutionWindow string `yaml:"executionWindow,omitempty" json:"executionWindow,omitempty"`
}

type HealthCheckSpec struct {
	// Enabled defaults to true; set enabled: false to keep the check disabled.
	Enabled            *bool                 `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Type               string                `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=ping"`
	Daemon             string                `yaml:"-" json:"-"`
	Role               string                `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=link,enum=next-hop,enum=internet,enum=service,enum=policy"`
	AddressFamily      string                `yaml:"addressFamily,omitempty" json:"addressFamily,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
	Target             string                `yaml:"target,omitempty" json:"target,omitempty"`
	TargetSource       string                `yaml:"targetSource,omitempty" json:"targetSource,omitempty" jsonschema:"enum=auto,enum=static,enum=defaultGateway,enum=dsliteRemote"`
	Interface          string                `yaml:"interface,omitempty" json:"interface,omitempty"`
	Via                string                `yaml:"-" json:"-"`
	FwMark             int                   `yaml:"-" json:"-"`
	SourceInterface    string                `yaml:"-" json:"-"`
	SourceAddress      string                `yaml:"-" json:"-"`
	SourceAddressFrom  StatusValueSourceSpec `yaml:"-" json:"-"`
	Protocol           string                `yaml:"protocol,omitempty" json:"protocol,omitempty" jsonschema:"enum=,enum=icmp,enum=tcp,enum=dns,enum=http"`
	Port               int                   `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=0,maximum=65535"`
	Interval           string                `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout            string                `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	HealthyThreshold   int                   `yaml:"healthyThreshold,omitempty" json:"healthyThreshold,omitempty" jsonschema:"minimum=1"`
	UnhealthyThreshold int                   `yaml:"unhealthyThreshold,omitempty" json:"unhealthyThreshold,omitempty" jsonschema:"minimum=1"`
	When               ResourceWhenSpec      `yaml:"when,omitempty" json:"when,omitempty"`
}

type EgressRoutePolicySpec struct {
	Family string `yaml:"family,omitempty" json:"family,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
	// Mode selects the route policy shape: priority chooses one candidate, mark installs marked tables, and hash spreads flows across targets.
	Mode                      string                       `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=priority,enum=mark,enum=hash"`
	SourceCIDRs               []string                     `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs          []string                     `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	DestinationSetRefs        []string                     `yaml:"destinationSetRefs,omitempty" json:"destinationSetRefs,omitempty"`
	ExcludeDestinationCIDRs   []string                     `yaml:"excludeDestinationCIDRs,omitempty" json:"excludeDestinationCIDRs,omitempty"`
	ExcludeDestinationSetRefs []string                     `yaml:"excludeDestinationSetRefs,omitempty" json:"excludeDestinationSetRefs,omitempty"`
	HashFields                []string                     `yaml:"hashFields,omitempty" json:"hashFields,omitempty"`
	Selection                 string                       `yaml:"selection,omitempty" json:"selection,omitempty" jsonschema:"enum=highest-weight-ready,enum=weighted-ecmp"`
	Hysteresis                string                       `yaml:"hysteresis,omitempty" json:"hysteresis,omitempty"`
	Candidates                []EgressRoutePolicyCandidate `yaml:"candidates" json:"candidates"`
	When                      ResourceWhenSpec             `yaml:"when,omitempty" json:"when,omitempty"`
}

type EgressRoutePolicyCandidate struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Enabled defaults to true; set enabled: false to exclude the candidate from selection and rendering.
	Enabled     *bool                 `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Source      string                `yaml:"source,omitempty" json:"source,omitempty"`
	Interface   string                `yaml:"interface,omitempty" json:"interface,omitempty"`
	Device      string                `yaml:"device,omitempty" json:"device,omitempty"`
	DeviceFrom  StatusValueSourceSpec `yaml:"deviceFrom,omitempty" json:"deviceFrom,omitempty"`
	Gateway     string                `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	GatewayFrom StatusValueSourceSpec `yaml:"gatewayFrom,omitempty" json:"gatewayFrom,omitempty"`
	// GatewaySource declares whether gateway is static, learned from DHCP status, or intentionally absent.
	GatewaySource string                    `yaml:"gatewaySource,omitempty" json:"gatewaySource,omitempty" jsonschema:"enum=,enum=static,enum=dhcpv4,enum=dhcpv6,enum=none"`
	Table         int                       `yaml:"table,omitempty" json:"table,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	RouteTable    int                       `yaml:"routeTable,omitempty" json:"routeTable,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	Priority      int                       `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"minimum=0,maximum=32765"`
	Mark          int                       `yaml:"mark,omitempty" json:"mark,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	RouteMetric   int                       `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	Metric        int                       `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
	Weight        int                       `yaml:"weight,omitempty" json:"weight,omitempty" jsonschema:"minimum=0"`
	HealthCheck   string                    `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
	Targets       []EgressRoutePolicyTarget `yaml:"targets,omitempty" json:"targets,omitempty"`
	DependsOn     []ResourceDependencySpec  `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen     []ReadyWhenSpec           `yaml:"ready_when,omitempty" json:"-"`
	When          ResourceWhenSpec          `yaml:"when,omitempty" json:"when,omitempty"`
}

type EgressRoutePolicyTarget struct {
	Name              string `yaml:"name,omitempty" json:"name,omitempty"`
	Interface         string `yaml:"interface,omitempty" json:"interface,omitempty"`
	OutboundInterface string `yaml:"outboundInterface,omitempty" json:"outboundInterface,omitempty"`
	Table             int    `yaml:"table,omitempty" json:"table,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	RouteTable        int    `yaml:"routeTable,omitempty" json:"routeTable,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	Priority          int    `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"minimum=0,maximum=32765"`
	Mark              int    `yaml:"mark,omitempty" json:"mark,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	RouteMetric       int    `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	Metric            int    `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
	HealthCheck       string `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
}

func (c EgressRoutePolicyCandidate) EffectiveInterface() string {
	if c.Interface != "" {
		return c.Interface
	}
	return c.Device
}

func (c EgressRoutePolicyCandidate) EffectiveTable() int {
	if c.Table != 0 {
		return c.Table
	}
	return c.RouteTable
}

func (c EgressRoutePolicyCandidate) EffectiveMetric() int {
	if c.RouteMetric != 0 {
		return c.RouteMetric
	}
	return c.Metric
}

func (t EgressRoutePolicyTarget) EffectiveInterface() string {
	if t.OutboundInterface != "" {
		return t.OutboundInterface
	}
	return t.Interface
}

func (t EgressRoutePolicyTarget) EffectiveTable() int {
	if t.Table != 0 {
		return t.Table
	}
	return t.RouteTable
}

func (t EgressRoutePolicyTarget) EffectiveMetric() int {
	if t.RouteMetric != 0 {
		return t.RouteMetric
	}
	return t.Metric
}

type EventRuleSpec struct {
	Pattern EventRulePatternSpec `yaml:"pattern" json:"pattern"`
	Emit    EventRuleEmitSpec    `yaml:"emit" json:"emit"`
}

type EventRulePatternSpec struct {
	Operator                string   `yaml:"operator" json:"operator" jsonschema:"enum=all_of,enum=any_of,enum=sequence,enum=window,enum=absence,enum=throttle,enum=debounce,enum=count"`
	Topic                   string   `yaml:"topic,omitempty" json:"topic,omitempty"`
	Topics                  []string `yaml:"topics,omitempty" json:"topics,omitempty"`
	Trigger                 string   `yaml:"trigger,omitempty" json:"trigger,omitempty"`
	Expected                string   `yaml:"expected,omitempty" json:"expected,omitempty"`
	Duration                string   `yaml:"duration,omitempty" json:"duration,omitempty"`
	Window                  string   `yaml:"window,omitempty" json:"window,omitempty"`
	Quiet                   string   `yaml:"quiet,omitempty" json:"quiet,omitempty"`
	Interval                string   `yaml:"interval,omitempty" json:"interval,omitempty"`
	Rate                    int      `yaml:"rate,omitempty" json:"rate,omitempty" jsonschema:"minimum=0"`
	Threshold               int      `yaml:"threshold,omitempty" json:"threshold,omitempty" jsonschema:"minimum=0"`
	CorrelateBy             string   `yaml:"correlate_by,omitempty" json:"correlate_by,omitempty"`
	AllowMissingCorrelation bool     `yaml:"allow_missing_correlation,omitempty" json:"allow_missing_correlation,omitempty"`
	Strict                  bool     `yaml:"strict,omitempty" json:"strict,omitempty"`
}

type EventRuleEmitSpec struct {
	Topic      string            `yaml:"topic" json:"topic"`
	Attributes map[string]string `yaml:"attributes,omitempty" json:"attributes,omitempty"`
}

type DerivedEventSpec struct {
	Topic       string          `yaml:"topic" json:"topic"`
	Inputs      []ReadyWhenSpec `yaml:"inputs" json:"inputs"`
	EmitWhen    string          `yaml:"emitWhen,omitempty" json:"emitWhen,omitempty" jsonschema:"enum=all_true,enum=any_true"`
	RetractWhen string          `yaml:"retractWhen,omitempty" json:"retractWhen,omitempty" jsonschema:"enum=any_false,enum=all_false"`
	Hysteresis  string          `yaml:"hysteresis,omitempty" json:"hysteresis,omitempty"`
	EmitInitial bool            `yaml:"emitInitial,omitempty" json:"emitInitial,omitempty"`
}

type NAT44RuleSpec struct {
	Type            string `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=masquerade,enum=snat"`
	EgressInterface string `yaml:"egressInterface,omitempty" json:"egressInterface,omitempty"`
	// EgressPolicyRef uses the selected device from an EgressRoutePolicy when egressInterface is omitted.
	EgressPolicyRef           string   `yaml:"egressPolicyRef,omitempty" json:"egressPolicyRef,omitempty"`
	SourceRanges              []string `yaml:"sourceRanges,omitempty" json:"sourceRanges,omitempty"`
	DestinationCIDRs          []string `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	DestinationSetRefs        []string `yaml:"destinationSetRefs,omitempty" json:"destinationSetRefs,omitempty"`
	ExcludeDestinationCIDRs   []string `yaml:"excludeDestinationCIDRs,omitempty" json:"excludeDestinationCIDRs,omitempty"`
	ExcludeDestinationSetRefs []string `yaml:"excludeDestinationSetRefs,omitempty" json:"excludeDestinationSetRefs,omitempty"`
	SNATAddress               string   `yaml:"snatAddress,omitempty" json:"snatAddress,omitempty"`
	// SNATAddressFrom reads the SNAT address from another resource status or supported router resource field.
	SNATAddressFrom   StatusValueSourceSpec  `yaml:"snatAddressFrom,omitempty" json:"snatAddressFrom,omitempty"`
	OutboundInterface string                 `yaml:"outboundInterface,omitempty" json:"outboundInterface,omitempty"`
	SourceCIDRs       []string               `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	Translation       IPv4NATTranslationSpec `yaml:"translation,omitempty" json:"translation,omitempty"`
	When              ResourceWhenSpec       `yaml:"when,omitempty" json:"when,omitempty"`
}

type IngressListenSpec struct {
	Interface   string                `yaml:"interface" json:"interface"`
	Address     string                `yaml:"address,omitempty" json:"address,omitempty"`
	AddressFrom StatusValueSourceSpec `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
	Protocol    string                `yaml:"protocol" json:"protocol" jsonschema:"enum=tcp,enum=udp"`
	Port        int                   `yaml:"port" json:"port" jsonschema:"minimum=1,maximum=65535"`
}

type PortForwardSpec struct {
	Listen  IngressListenSpec  `yaml:"listen" json:"listen"`
	Target  IngressTargetSpec  `yaml:"target" json:"target"`
	Hairpin IngressHairpinSpec `yaml:"hairpin,omitempty" json:"hairpin,omitempty"`
	When    ResourceWhenSpec   `yaml:"when,omitempty" json:"when,omitempty"`
}

type IngressTargetSpec struct {
	Address     string                `yaml:"address,omitempty" json:"address,omitempty"`
	AddressFrom StatusValueSourceSpec `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
	Port        int                   `yaml:"port" json:"port" jsonschema:"minimum=1,maximum=65535"`
}

type IngressServiceSpec struct {
	Listen      IngressListenSpec        `yaml:"listen" json:"listen"`
	Hostname    string                   `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	ExternalDNS bool                     `yaml:"externalDNS,omitempty" json:"externalDNS,omitempty"`
	Backends    []IngressBackendSpec     `yaml:"backends" json:"backends"`
	Hairpin     IngressHairpinSpec       `yaml:"hairpin,omitempty" json:"hairpin,omitempty"`
	HealthCheck IngressHealthCheckSpec   `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
	Policy      IngressServicePolicySpec `yaml:"policy,omitempty" json:"policy,omitempty"`
	When        ResourceWhenSpec         `yaml:"when,omitempty" json:"when,omitempty"`
}

type IngressBackendSpec struct {
	Name        string                `yaml:"name,omitempty" json:"name,omitempty"`
	Address     string                `yaml:"address,omitempty" json:"address,omitempty"`
	AddressFrom StatusValueSourceSpec `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
	Port        int                   `yaml:"port" json:"port" jsonschema:"minimum=1,maximum=65535"`
	Weight      int                   `yaml:"weight,omitempty" json:"weight,omitempty" jsonschema:"minimum=0"`
}

type IngressHairpinSpec struct {
	Enabled    bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Mode       string   `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=auto,enum=manual,enum=off"`
	Interfaces []string `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
}

type IngressHealthCheckSpec struct {
	Protocol           string `yaml:"protocol,omitempty" json:"protocol,omitempty" jsonschema:"enum=,enum=tcp,enum=http,enum=https"`
	Interval           string `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout            string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Path               string `yaml:"path,omitempty" json:"path,omitempty"`
	Host               string `yaml:"host,omitempty" json:"host,omitempty"`
	ExpectedStatus     []int  `yaml:"expectedStatus,omitempty" json:"expectedStatus,omitempty"`
	TLSSkipVerify      bool   `yaml:"tlsSkipVerify,omitempty" json:"tlsSkipVerify,omitempty"`
	ExpectedBody       string `yaml:"expectedBody,omitempty" json:"expectedBody,omitempty"`
	HealthyThreshold   int    `yaml:"healthyThreshold,omitempty" json:"healthyThreshold,omitempty" jsonschema:"minimum=1"`
	UnhealthyThreshold int    `yaml:"unhealthyThreshold,omitempty" json:"unhealthyThreshold,omitempty" jsonschema:"minimum=1"`
}

type IngressServicePolicySpec struct {
	Selection           string `yaml:"selection,omitempty" json:"selection,omitempty" jsonschema:"enum=,enum=failover,enum=sourceHash,enum=random"`
	OnNoHealthyBackends string `yaml:"onNoHealthyBackends,omitempty" json:"onNoHealthyBackends,omitempty" jsonschema:"enum=,enum=drop,enum=reject"`
}

type IPAddressSetSpec struct {
	Addresses       []string         `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	Names           []string         `yaml:"names,omitempty" json:"names,omitempty"`
	RefreshInterval string           `yaml:"refreshInterval,omitempty" json:"refreshInterval,omitempty"`
	When            ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type LocalServiceRedirectSpec struct {
	Interface string                         `yaml:"interface" json:"interface"`
	Rules     []LocalServiceRedirectRuleSpec `yaml:"rules" json:"rules"`
	When      ResourceWhenSpec               `yaml:"when,omitempty" json:"when,omitempty"`
}

type LocalServiceRedirectRuleSpec struct {
	Name              string   `yaml:"name,omitempty" json:"name,omitempty"`
	Protocols         []string `yaml:"protocols" json:"protocols"`
	DestinationSetRef string   `yaml:"destinationSetRef" json:"destinationSetRef"`
	DestinationPort   int      `yaml:"destinationPort" json:"destinationPort" jsonschema:"minimum=1,maximum=65535"`
	RedirectPort      int      `yaml:"redirectPort" json:"redirectPort" jsonschema:"minimum=1,maximum=65535"`
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

type FirewallZoneSpec struct {
	Role       string   `yaml:"role" json:"role" jsonschema:"enum=untrust,enum=trust,enum=mgmt"`
	Interfaces []string `yaml:"interfaces" json:"interfaces"`
}

type FirewallPolicySpec struct {
	LogDeny        bool `yaml:"logDeny,omitempty" json:"logDeny,omitempty"`
	SameRoleAccept bool `yaml:"sameRoleAccept,omitempty" json:"sameRoleAccept,omitempty"`
}

type FirewallEventLogSpec struct {
	Enabled    bool                  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path       string                `yaml:"path,omitempty" json:"path,omitempty"`
	Events     []string              `yaml:"events,omitempty" json:"events,omitempty" jsonschema:"enum=deny,enum=allow,enum=rateLimit,enum=connLimit"`
	FromZones  []string              `yaml:"fromZones,omitempty" json:"fromZones,omitempty"`
	ToZones    []string              `yaml:"toZones,omitempty" json:"toZones,omitempty"`
	Rules      []string              `yaml:"rules,omitempty" json:"rules,omitempty"`
	SampleRate int                   `yaml:"sampleRate,omitempty" json:"sampleRate,omitempty" jsonschema:"minimum=0"`
	Sinks      []string              `yaml:"sinks,omitempty" json:"sinks,omitempty"`
	Retention  string                `yaml:"retention,omitempty" json:"retention,omitempty"`
	NFLogGroup int                   `yaml:"nflogGroup,omitempty" json:"nflogGroup,omitempty" jsonschema:"minimum=0,maximum=65535"`
	Log        FirewallLogPolicySpec `yaml:"log,omitempty" json:"log,omitempty"`
}

type FirewallLogSpec = FirewallEventLogSpec

type FirewallLogPolicySpec struct {
	AcceptSampleRate int  `yaml:"acceptSampleRate,omitempty" json:"acceptSampleRate,omitempty" jsonschema:"minimum=0"`
	DropSampleRate   int  `yaml:"dropSampleRate,omitempty" json:"dropSampleRate,omitempty" jsonschema:"minimum=0"`
	CopyRange        int  `yaml:"copyRange,omitempty" json:"copyRange,omitempty" jsonschema:"minimum=0"`
	Reject           bool `yaml:"reject,omitempty" json:"reject,omitempty"`
}

type ClientPolicySpec struct {
	Mode             string                    `yaml:"mode" json:"mode" jsonschema:"enum=include,enum=exclude"`
	Interfaces       []string                  `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
	MACs             []string                  `yaml:"macs,omitempty" json:"macs,omitempty"`
	Classification   []ClientPolicyClassSpec   `yaml:"classification,omitempty" json:"classification,omitempty"`
	Isolation        ClientPolicyIsolationSpec `yaml:"isolation,omitempty" json:"isolation,omitempty"`
	GuestServices    []string                  `yaml:"guestServices,omitempty" json:"guestServices,omitempty"`
	GuestEgressDeny  []string                  `yaml:"guestEgressDeny,omitempty" json:"guestEgressDeny,omitempty"`
	GuestEgressAllow []string                  `yaml:"guestEgressAllow,omitempty" json:"guestEgressAllow,omitempty"`
}

type ClientPolicyIsolationSpec struct {
	LANInternet   string `yaml:"lanInternet,omitempty" json:"lanInternet,omitempty" jsonschema:"enum=,enum=allow,enum=deny"`
	LANLAN        string `yaml:"lanLAN,omitempty" json:"lanLAN,omitempty" jsonschema:"enum=,enum=allow,enum=deny"`
	LANMgmt       string `yaml:"lanMgmt,omitempty" json:"lanMgmt,omitempty" jsonschema:"enum=,enum=allow,enum=deny"`
	MDNSBroadcast string `yaml:"mDNSBroadcast,omitempty" json:"mDNSBroadcast,omitempty" jsonschema:"enum=,enum=allow,enum=deny"`
}

type ClientPolicyClassSpec struct {
	Name            string                     `yaml:"name,omitempty" json:"name,omitempty"`
	Mode            string                     `yaml:"mode" json:"mode" jsonschema:"enum=trusted,enum=guest,enum=isolated"`
	Match           ClientPolicyClassMatchSpec `yaml:"match" json:"match"`
	IPv4Reservation string                     `yaml:"ipv4Reservation,omitempty" json:"ipv4Reservation,omitempty"`
}

type ClientPolicyClassMatchSpec struct {
	MACs             []string `yaml:"macs,omitempty" json:"macs,omitempty"`
	OUIPrefixes      []string `yaml:"ouiPrefixes,omitempty" json:"ouiPrefixes,omitempty"`
	HostnamePatterns []string `yaml:"hostnamePatterns,omitempty" json:"hostnamePatterns,omitempty"`
	DHCPFingerprints []string `yaml:"dhcpFingerprints,omitempty" json:"dhcpFingerprints,omitempty"`
}

type FirewallRuleSpec struct {
	FromZone                  string                `yaml:"fromZone" json:"fromZone"`
	ToZone                    string                `yaml:"toZone" json:"toZone"`
	SourceCIDRs               []string              `yaml:"srcCIDRs,omitempty" json:"srcCIDRs,omitempty"`
	DestinationCIDRs          []string              `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	DestinationSetRefs        []string              `yaml:"destinationSetRefs,omitempty" json:"destinationSetRefs,omitempty"`
	ExcludeDestinationSetRefs []string              `yaml:"excludeDestinationSetRefs,omitempty" json:"excludeDestinationSetRefs,omitempty"`
	Protocol                  string                `yaml:"protocol,omitempty" json:"protocol,omitempty" jsonschema:"enum=,enum=tcp,enum=udp,enum=icmp,enum=icmpv6,enum=ipv6-icmp,enum=ipip"`
	Port                      int                   `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=0,maximum=65535"`
	SourcePorts               []FirewallPort        `yaml:"sourcePorts,omitempty" json:"sourcePorts,omitempty"`
	DestinationPorts          []FirewallPort        `yaml:"destinationPorts,omitempty" json:"destinationPorts,omitempty"`
	ICMPType                  string                `yaml:"icmpType,omitempty" json:"icmpType,omitempty"`
	ICMPTypeKebab             string                `yaml:"icmp-type,omitempty" json:"-"`
	ICMPv6Type                string                `yaml:"icmpv6Type,omitempty" json:"icmpv6Type,omitempty"`
	ICMPv6TypeKebab           string                `yaml:"icmpv6-type,omitempty" json:"-"`
	Action                    string                `yaml:"action" json:"action" jsonschema:"enum=accept,enum=drop,enum=reject"`
	Log                       bool                  `yaml:"log,omitempty" json:"log,omitempty"`
	RateLimit                 FirewallRateLimitSpec `yaml:"rateLimit,omitempty" json:"rateLimit,omitempty"`
	ConnLimit                 FirewallConnLimitSpec `yaml:"connLimit,omitempty" json:"connLimit,omitempty"`
}

type FirewallPort string

func (p *FirewallPort) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!int" {
			n, err := strconv.Atoi(value.Value)
			if err != nil {
				return err
			}
			*p = FirewallPort(strconv.Itoa(n))
			return nil
		}
		*p = FirewallPort(value.Value)
		return nil
	default:
		return fmt.Errorf("port must be a scalar")
	}
}

func (p *FirewallPort) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*p = FirewallPort(strconv.Itoa(n))
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*p = FirewallPort(s)
	return nil
}

type FirewallRateLimitSpec struct {
	Rate             int    `yaml:"rate,omitempty" json:"rate,omitempty" jsonschema:"minimum=0"`
	Unit             string `yaml:"unit,omitempty" json:"unit,omitempty" jsonschema:"enum=,enum=packet,enum=byte,enum=kilobyte,enum=megabyte"`
	Per              string `yaml:"per,omitempty" json:"per,omitempty" jsonschema:"enum=,enum=second,enum=minute"`
	Log              bool   `yaml:"log,omitempty" json:"log,omitempty"`
	PacketsPerSecond int    `yaml:"packetsPerSecond,omitempty" json:"packetsPerSecond,omitempty" jsonschema:"minimum=0"`
	Burst            int    `yaml:"burst,omitempty" json:"burst,omitempty" jsonschema:"minimum=0"`
}

type FirewallConnLimitSpec struct {
	MaxPerSource int  `yaml:"maxPerSource,omitempty" json:"maxPerSource,omitempty" jsonschema:"minimum=0"`
	Log          bool `yaml:"log,omitempty" json:"log,omitempty"`
}

type HostnameSpec struct {
	Hostname string `yaml:"hostname" json:"hostname"`
	Managed  bool   `yaml:"managed,omitempty" json:"managed,omitempty"`
}

func (r Resource) SysctlSpec() (SysctlSpec, error) {
	return specAs[SysctlSpec](r)
}

func (r Resource) SysctlProfileSpec() (SysctlProfileSpec, error) {
	return specAs[SysctlProfileSpec](r)
}

func (r Resource) KernelModuleSpec() (KernelModuleSpec, error) {
	return specAs[KernelModuleSpec](r)
}

func (r Resource) PackageSpec() (PackageSpec, error) {
	return specAs[PackageSpec](r)
}

func (r Resource) NetworkAdoptionSpec() (NetworkAdoptionSpec, error) {
	return specAs[NetworkAdoptionSpec](r)
}

func (r Resource) NTPClientSpec() (NTPClientSpec, error) {
	return specAs[NTPClientSpec](r)
}

func (r Resource) NTPServerSpec() (NTPServerSpec, error) {
	return specAs[NTPServerSpec](r)
}

func (r Resource) WebConsoleSpec() (WebConsoleSpec, error) {
	return specAs[WebConsoleSpec](r)
}

func (r Resource) ManagementAccessSpec() (ManagementAccessSpec, error) {
	return specAs[ManagementAccessSpec](r)
}

func (r Resource) NixOSHostSpec() (NixOSHostSpec, error) {
	return specAs[NixOSHostSpec](r)
}

func (r Resource) InventorySpec() (InventorySpec, error) {
	return specAs[InventorySpec](r)
}

func (r Resource) InterfaceSpec() (InterfaceSpec, error) {
	return specAs[InterfaceSpec](r)
}

func (r Resource) LinkSpec() (LinkSpec, error) {
	return specAs[LinkSpec](r)
}

func (r Resource) BridgeSpec() (BridgeSpec, error) {
	return specAs[BridgeSpec](r)
}

func (r Resource) VXLANSegmentSpec() (VXLANSegmentSpec, error) {
	return specAs[VXLANSegmentSpec](r)
}

func (r Resource) WireGuardInterfaceSpec() (WireGuardInterfaceSpec, error) {
	return specAs[WireGuardInterfaceSpec](r)
}

func (r Resource) TunnelInterfaceSpec() (TunnelInterfaceSpec, error) {
	return specAs[TunnelInterfaceSpec](r)
}

func (r Resource) WireGuardPeerSpec() (WireGuardPeerSpec, error) {
	return specAs[WireGuardPeerSpec](r)
}

func (r Resource) TailscaleNodeSpec() (TailscaleNodeSpec, error) {
	return specAs[TailscaleNodeSpec](r)
}

func (r Resource) IPsecConnectionSpec() (IPsecConnectionSpec, error) {
	return specAs[IPsecConnectionSpec](r)
}

func (r Resource) VRFSpec() (VRFSpec, error) {
	return specAs[VRFSpec](r)
}

func (r Resource) VXLANTunnelSpec() (VXLANTunnelSpec, error) {
	return specAs[VXLANTunnelSpec](r)
}

func (r Resource) PPPoESessionSpec() (PPPoESessionSpec, error) {
	return specAs[PPPoESessionSpec](r)
}

func (r Resource) IPv4StaticAddressSpec() (IPv4StaticAddressSpec, error) {
	return specAs[IPv4StaticAddressSpec](r)
}

func (r Resource) VirtualAddressSpec() (VirtualAddressSpec, error) {
	return specAs[VirtualAddressSpec](r)
}

func (r Resource) BGPRouterSpec() (BGPRouterSpec, error) {
	return specAs[BGPRouterSpec](r)
}

func (r Resource) BGPPeerSpec() (BGPPeerSpec, error) {
	return specAs[BGPPeerSpec](r)
}

func (r Resource) BFDSpec() (BFDSpec, error) {
	return specAs[BFDSpec](r)
}

func (r Resource) DHCPv4ClientSpec() (DHCPv4ClientSpec, error) {
	return specAs[DHCPv4ClientSpec](r)
}

func (r Resource) IPv4StaticRouteSpec() (IPv4StaticRouteSpec, error) {
	return specAs[IPv4StaticRouteSpec](r)
}

func (r Resource) ClusterNetworkRouteSpec() (ClusterNetworkRouteSpec, error) {
	return specAs[ClusterNetworkRouteSpec](r)
}

func (r Resource) IPv6StaticRouteSpec() (IPv6StaticRouteSpec, error) {
	return specAs[IPv6StaticRouteSpec](r)
}

func (r Resource) DHCPv4ServerSpec() (DHCPv4ServerSpec, error) {
	return specAs[DHCPv4ServerSpec](r)
}

func (r Resource) DHCPv4ReservationSpec() (DHCPv4ReservationSpec, error) {
	return specAs[DHCPv4ReservationSpec](r)
}

func (r Resource) DHCPv6AddressSpec() (DHCPv6AddressSpec, error) {
	return specAs[DHCPv6AddressSpec](r)
}

func (r Resource) IPv6RAAddressSpec() (IPv6RAAddressSpec, error) {
	return specAs[IPv6RAAddressSpec](r)
}

func (r Resource) DHCPv6PrefixDelegationSpec() (DHCPv6PrefixDelegationSpec, error) {
	return specAs[DHCPv6PrefixDelegationSpec](r)
}

func (r Resource) IPv6DelegatedAddressSpec() (IPv6DelegatedAddressSpec, error) {
	return specAs[IPv6DelegatedAddressSpec](r)
}

func (r Resource) DHCPv6InformationSpec() (DHCPv6InformationSpec, error) {
	return specAs[DHCPv6InformationSpec](r)
}

func (r Resource) DNSZoneSpec() (DNSZoneSpec, error) {
	return specAs[DNSZoneSpec](r)
}

func (r Resource) IPv6RouterAdvertisementSpec() (IPv6RouterAdvertisementSpec, error) {
	return specAs[IPv6RouterAdvertisementSpec](r)
}

func (r Resource) DHCPv6ServerSpec() (DHCPv6ServerSpec, error) {
	return specAs[DHCPv6ServerSpec](r)
}

func (r Resource) DHCPLeaseSyncSpec() (DHCPLeaseSyncSpec, error) {
	return specAs[DHCPLeaseSyncSpec](r)
}

func (r Resource) DHCPv4RelaySpec() (DHCPv4RelaySpec, error) {
	return specAs[DHCPv4RelaySpec](r)
}

func (r Resource) SelfAddressPolicySpec() (SelfAddressPolicySpec, error) {
	return specAs[SelfAddressPolicySpec](r)
}

func (r Resource) DNSResolverSpec() (DNSResolverSpec, error) {
	return specAs[DNSResolverSpec](r)
}

func (r Resource) DNSForwarderSpec() (DNSForwarderSpec, error) {
	return specAs[DNSForwarderSpec](r)
}

func (r Resource) DNSUpstreamSpec() (DNSUpstreamSpec, error) {
	return specAs[DNSUpstreamSpec](r)
}

func (r Resource) LogRetentionSpec() (LogRetentionSpec, error) {
	return specAs[LogRetentionSpec](r)
}

func (r Resource) TrafficFlowLogSpec() (TrafficFlowLogSpec, error) {
	return specAs[TrafficFlowLogSpec](r)
}

func (r Resource) DSLiteTunnelSpec() (DSLiteTunnelSpec, error) {
	return specAs[DSLiteTunnelSpec](r)
}

func (r Resource) IPv4RouteSpec() (IPv4RouteSpec, error) {
	return specAs[IPv4RouteSpec](r)
}

func (r Resource) OverlayPeerSpec() (OverlayPeerSpec, error) {
	return specAs[OverlayPeerSpec](r)
}

func (r Resource) HybridRouteSpec() (HybridRouteSpec, error) {
	return specAs[HybridRouteSpec](r)
}

func (r Resource) AddressMobilityDomainSpec() (AddressMobilityDomainSpec, error) {
	return specAs[AddressMobilityDomainSpec](r)
}

func (r Resource) EventGroupSpec() (EventGroupSpec, error) {
	return specAs[EventGroupSpec](r)
}

func (r Resource) EventPeerSpec() (EventPeerSpec, error) {
	return specAs[EventPeerSpec](r)
}

func (r Resource) EventSubscriptionSpec() (EventSubscriptionSpec, error) {
	return specAs[EventSubscriptionSpec](r)
}

func (r Resource) MobilityPoolSpec() (MobilityPoolSpec, error) {
	return specAs[MobilityPoolSpec](r)
}

func (r Resource) CloudProviderProfileSpec() (CloudProviderProfileSpec, error) {
	return specAs[CloudProviderProfileSpec](r)
}

func (r Resource) RemoteAddressClaimSpec() (RemoteAddressClaimSpec, error) {
	return specAs[RemoteAddressClaimSpec](r)
}

func (r Resource) ProviderActionPolicySpec() (ProviderActionPolicySpec, error) {
	return specAs[ProviderActionPolicySpec](r)
}

func (r Resource) HealthCheckSpec() (HealthCheckSpec, error) {
	return specAs[HealthCheckSpec](r)
}

func (r Resource) EgressRoutePolicySpec() (EgressRoutePolicySpec, error) {
	return specAs[EgressRoutePolicySpec](r)
}

func (r Resource) EventRuleSpec() (EventRuleSpec, error) {
	return specAs[EventRuleSpec](r)
}

func (r Resource) DerivedEventSpec() (DerivedEventSpec, error) {
	return specAs[DerivedEventSpec](r)
}

func (r Resource) NAT44RuleSpec() (NAT44RuleSpec, error) {
	return specAs[NAT44RuleSpec](r)
}

func (r Resource) PortForwardSpec() (PortForwardSpec, error) {
	return specAs[PortForwardSpec](r)
}

func (r Resource) IngressServiceSpec() (IngressServiceSpec, error) {
	return specAs[IngressServiceSpec](r)
}

func (r Resource) IPAddressSetSpec() (IPAddressSetSpec, error) {
	return specAs[IPAddressSetSpec](r)
}

func (r Resource) LocalServiceRedirectSpec() (LocalServiceRedirectSpec, error) {
	return specAs[LocalServiceRedirectSpec](r)
}

func (r Resource) FirewallZoneSpec() (FirewallZoneSpec, error) {
	return specAs[FirewallZoneSpec](r)
}

func (r Resource) FirewallPolicySpec() (FirewallPolicySpec, error) {
	return specAs[FirewallPolicySpec](r)
}

func (r Resource) ClientPolicySpec() (ClientPolicySpec, error) {
	return specAs[ClientPolicySpec](r)
}

func (r Resource) FirewallRuleSpec() (FirewallRuleSpec, error) {
	return specAs[FirewallRuleSpec](r)
}

func (r Resource) FirewallLogSpec() (FirewallLogSpec, error) {
	return specAs[FirewallLogSpec](r)
}

func (r Resource) FirewallEventLogSpec() (FirewallEventLogSpec, error) {
	return specAs[FirewallEventLogSpec](r)
}

func (r Resource) LogSinkSpec() (LogSinkSpec, error) {
	return specAs[LogSinkSpec](r)
}

func (r Resource) TelemetrySpec() (TelemetrySpec, error) {
	return specAs[TelemetrySpec](r)
}

func (r Resource) ObservabilityPipelineSpec() (ObservabilityPipelineSpec, error) {
	return specAs[ObservabilityPipelineSpec](r)
}

func (r Resource) RouterdClusterSpec() (RouterdClusterSpec, error) {
	return specAs[RouterdClusterSpec](r)
}

func (r Resource) HostnameSpec() (HostnameSpec, error) {
	return specAs[HostnameSpec](r)
}

func (r Resource) PluginSpec() (PluginSpec, error) {
	return specAs[PluginSpec](r)
}

func (r Resource) DynamicConfigSourceSpec() (DynamicConfigSourceSpec, error) {
	return specAs[DynamicConfigSourceSpec](r)
}

func (r Resource) DynamicOverridePolicySpec() (DynamicOverridePolicySpec, error) {
	return specAs[DynamicOverridePolicySpec](r)
}

func specAs[T any](r Resource) (T, error) {
	spec, ok := r.Spec.(T)
	if !ok {
		if ptr, ok := r.Spec.(*T); ok && ptr != nil {
			return *ptr, nil
		}
		var zero T
		if r.Spec == nil {
			return zero, fmt.Errorf("%s has unexpected spec type <nil>", r.ID())
		}
		data, err := json.Marshal(r.Spec)
		if err != nil {
			return zero, fmt.Errorf("%s has unexpected spec type %T", r.ID(), r.Spec)
		}
		if err := json.Unmarshal(data, &zero); err != nil {
			return zero, fmt.Errorf("%s has unexpected spec type %T: %w", r.ID(), r.Spec, err)
		}
		return zero, nil
	}
	return spec, nil
}

func BoolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
