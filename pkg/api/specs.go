// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

type LogSinkSpec struct {
	Type     string            `yaml:"type" json:"type" jsonschema:"enum=syslog,enum=plugin"`
	Enabled  *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinLevel string            `yaml:"minLevel,omitempty" json:"minLevel,omitempty" jsonschema:"enum=debug,enum=info,enum=warning,enum=error"`
	Syslog   LogSinkSyslogSpec `yaml:"syslog,omitempty" json:"syslog,omitempty"`
	Plugin   LogSinkPluginSpec `yaml:"plugin,omitempty" json:"plugin,omitempty"`
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
	Schedule          string                   `yaml:"schedule,omitempty" json:"schedule,omitempty" jsonschema:"enum=,enum=daily"`
	IncrementalVacuum bool                     `yaml:"incrementalVacuum,omitempty" json:"incrementalVacuum,omitempty"`
	Targets           []LogRetentionTargetSpec `yaml:"targets" json:"targets"`
}

type LogRetentionTargetSpec struct {
	File      string `yaml:"file" json:"file"`
	Retention string `yaml:"retention" json:"retention"`
}

type ApplyPolicySpec struct {
	Mode                string   `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=strict,enum=progressive"`
	ProtectedInterfaces []string `yaml:"protectedInterfaces,omitempty" json:"protectedInterfaces,omitempty"`
	ProtectedZones      []string `yaml:"protectedZones,omitempty" json:"protectedZones,omitempty"`
	AutoTuneConntrack   bool     `yaml:"autoTuneConntrack,omitempty" json:"autoTuneConntrack,omitempty"`
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
	FwMark         int    `yaml:"fwmark,omitempty" json:"fwmark,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	Table          int    `yaml:"table,omitempty" json:"table,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
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
	Operator          string   `yaml:"operator,omitempty" json:"operator,omitempty"`
	ShieldsUp         *bool    `yaml:"shieldsUp,omitempty" json:"shieldsUp,omitempty"`
	BinaryPath        string   `yaml:"binaryPath,omitempty" json:"binaryPath,omitempty"`
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
	Interface       string `yaml:"interface" json:"interface"`
	IfName          string `yaml:"ifname,omitempty" json:"ifname,omitempty"`
	Enabled         *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Disabled        bool   `yaml:"disabled,omitempty" json:"disabled,omitempty"`
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
	Interface          string `yaml:"interface" json:"interface"`
	Address            string `yaml:"address" json:"address"`
	Exclusive          bool   `yaml:"exclusive,omitempty" json:"exclusive,omitempty"`
	AllowOverlap       bool   `yaml:"allowOverlap,omitempty" json:"allowOverlap,omitempty"`
	AllowOverlapReason string `yaml:"allowOverlapReason,omitempty" json:"allowOverlapReason,omitempty"`
}

type VirtualIPv4AddressSpec struct {
	Interface   string                `yaml:"interface" json:"interface"`
	Address     string                `yaml:"address" json:"address"`
	Hostname    string                `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	ExternalDNS bool                  `yaml:"externalDNS,omitempty" json:"externalDNS,omitempty"`
	Mode        string                `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=static,enum=vrrp"`
	VRRP        VirtualIPv4VRRPSpec   `yaml:"vrrp,omitempty" json:"vrrp,omitempty"`
	Track       []ResourceTrackSpec   `yaml:"track,omitempty" json:"track,omitempty"`
	When        ResourceWhenSpec      `yaml:"when,omitempty" json:"when,omitempty"`
	AddressFrom StatusValueSourceSpec `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
}

type VirtualIPv6AddressSpec struct {
	Interface   string                `yaml:"interface" json:"interface"`
	Address     string                `yaml:"address" json:"address"`
	Hostname    string                `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	ExternalDNS bool                  `yaml:"externalDNS,omitempty" json:"externalDNS,omitempty"`
	Mode        string                `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=,enum=static,enum=vrrp"`
	VRRP        VirtualIPv6VRRPSpec   `yaml:"vrrp,omitempty" json:"vrrp,omitempty"`
	Track       []ResourceTrackSpec   `yaml:"track,omitempty" json:"track,omitempty"`
	When        ResourceWhenSpec      `yaml:"when,omitempty" json:"when,omitempty"`
	AddressFrom StatusValueSourceSpec `yaml:"addressFrom,omitempty" json:"addressFrom,omitempty"`
}

type VirtualIPv4VRRPSpec struct {
	VirtualRouterID    int                   `yaml:"virtualRouterID" json:"virtualRouterID" jsonschema:"minimum=1,maximum=255"`
	Priority           int                   `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"minimum=1,maximum=254"`
	Preempt            *bool                 `yaml:"preempt,omitempty" json:"preempt,omitempty"`
	PreemptDelay       string                `yaml:"preemptDelay,omitempty" json:"preemptDelay,omitempty"`
	Peers              []string              `yaml:"peers,omitempty" json:"peers,omitempty"`
	AdvertInterval     string                `yaml:"advertInterval,omitempty" json:"advertInterval,omitempty"`
	Authentication     string                `yaml:"authentication,omitempty" json:"authentication,omitempty"`
	AuthenticationFrom SecretValueSourceSpec `yaml:"authenticationFrom,omitempty" json:"authenticationFrom,omitempty"`
}

type VirtualIPv6VRRPSpec struct {
	VirtualRouterID    int                   `yaml:"virtualRouterID" json:"virtualRouterID" jsonschema:"minimum=1,maximum=255"`
	Priority           int                   `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"minimum=1,maximum=254"`
	Preempt            *bool                 `yaml:"preempt,omitempty" json:"preempt,omitempty"`
	PreemptDelay       string                `yaml:"preemptDelay,omitempty" json:"preemptDelay,omitempty"`
	Peers              []string              `yaml:"peers,omitempty" json:"peers,omitempty"`
	AdvertInterval     string                `yaml:"advertInterval,omitempty" json:"advertInterval,omitempty"`
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
	ASN             uint32                 `yaml:"asn" json:"asn" jsonschema:"minimum=1"`
	RouterID        string                 `yaml:"routerID" json:"routerID"`
	VRF             string                 `yaml:"vrf,omitempty" json:"vrf,omitempty"`
	Listen          BGPListenSpec          `yaml:"listen,omitempty" json:"listen,omitempty"`
	ImportPolicy    BGPImportPolicySpec    `yaml:"importPolicy,omitempty" json:"importPolicy,omitempty"`
	ExportPolicy    BGPExportPolicySpec    `yaml:"exportPolicy,omitempty" json:"exportPolicy,omitempty"`
	Redistribute    BGPRedistributeSpec    `yaml:"redistribute,omitempty" json:"redistribute,omitempty"`
	Communities     BGPCommunitiesSpec     `yaml:"communities,omitempty" json:"communities,omitempty"`
	Timers          BGPTimersSpec          `yaml:"timers,omitempty" json:"timers,omitempty"`
	GracefulRestart BGPGracefulRestartSpec `yaml:"gracefulRestart,omitempty" json:"gracefulRestart,omitempty"`
	Watcher         BGPWatcherSpec         `yaml:"watcher,omitempty" json:"watcher,omitempty"`
	Backend         string                 `yaml:"backend,omitempty" json:"backend,omitempty" jsonschema:"enum=,enum=frr"`
	When            ResourceWhenSpec       `yaml:"when,omitempty" json:"when,omitempty"`
}

type BGPListenSpec struct {
	Address string `yaml:"address,omitempty" json:"address,omitempty"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
}

type BGPImportPolicySpec struct {
	AllowedPrefixes []string `yaml:"allowedPrefixes,omitempty" json:"allowedPrefixes,omitempty"`
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
	Keepalive    string `yaml:"keepalive,omitempty" json:"keepalive,omitempty"`
	HoldTime     string `yaml:"holdTime,omitempty" json:"holdTime,omitempty"`
	ConnectRetry string `yaml:"connectRetry,omitempty" json:"connectRetry,omitempty"`
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
	RouterRef    string                `yaml:"routerRef" json:"routerRef"`
	PeerASN      uint32                `yaml:"peerASN" json:"peerASN" jsonschema:"minimum=1"`
	Peers        []string              `yaml:"peers" json:"peers"`
	Password     string                `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFrom SecretValueSourceSpec `yaml:"passwordFrom,omitempty" json:"passwordFrom,omitempty"`
	ExportPolicy BGPExportPolicySpec   `yaml:"exportPolicy,omitempty" json:"exportPolicy,omitempty"`
	Timers       BGPTimersSpec         `yaml:"timers,omitempty" json:"timers,omitempty"`
	Communities  BGPCommunitiesSpec    `yaml:"communities,omitempty" json:"communities,omitempty"`
	BFD          BGPBFDSpec            `yaml:"bfd,omitempty" json:"bfd,omitempty"`
	When         ResourceWhenSpec      `yaml:"when,omitempty" json:"when,omitempty"`
}

type BGPBFDSpec struct {
	Enabled          *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinRxInterval    string `yaml:"minRxInterval,omitempty" json:"minRxInterval,omitempty"`
	MinTxInterval    string `yaml:"minTxInterval,omitempty" json:"minTxInterval,omitempty"`
	DetectMultiplier int    `yaml:"detectMultiplier,omitempty" json:"detectMultiplier,omitempty" jsonschema:"minimum=1,maximum=50"`
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
	Server           string                  `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq,enum=kea,enum=dhcpd"`
	Managed          bool                    `yaml:"managed,omitempty" json:"managed,omitempty"`
	Role             string                  `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=server,enum=transit"`
	ListenInterfaces []string                `yaml:"listenInterfaces,omitempty" json:"listenInterfaces,omitempty"`
	LogDHCP          bool                    `yaml:"logDHCP,omitempty" json:"logDHCP,omitempty"`
	StickyHoldDays   int                     `yaml:"stickyHoldDays,omitempty" json:"stickyHoldDays,omitempty" jsonschema:"minimum=0"`
	DNS              DHCPv4ServerDNSSpec     `yaml:"dns,omitempty" json:"dns,omitempty"`
	Interface        string                  `yaml:"interface,omitempty" json:"interface,omitempty"`
	AddressPool      DHCPAddressPoolSpec     `yaml:"addressPool,omitempty" json:"addressPool,omitempty"`
	Gateway          string                  `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	GatewayFrom      StatusValueSourceSpec   `yaml:"gatewayFrom,omitempty" json:"gatewayFrom,omitempty"`
	DNSServers       []string                `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	DNSServerFrom    []StatusValueSourceSpec `yaml:"dnsServerFrom,omitempty" json:"dnsServerFrom,omitempty"`
	NTPServers       []string                `yaml:"ntpServers,omitempty" json:"ntpServers,omitempty"`
	NTPServerFrom    []StatusValueSourceSpec `yaml:"ntpServerFrom,omitempty" json:"ntpServerFrom,omitempty"`
	Domain           string                  `yaml:"domain,omitempty" json:"domain,omitempty"`
	DomainFrom       StatusValueSourceSpec   `yaml:"domainFrom,omitempty" json:"domainFrom,omitempty"`
	Options          []DHCPv4OptionSpec      `yaml:"options,omitempty" json:"options,omitempty"`
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

type DHCPv4ScopeSpec struct {
	Server        string           `yaml:"server" json:"server"`
	Interface     string           `yaml:"interface" json:"interface"`
	RangeStart    string           `yaml:"rangeStart" json:"rangeStart"`
	RangeEnd      string           `yaml:"rangeEnd" json:"rangeEnd"`
	LeaseTime     string           `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	RouterSource  string           `yaml:"routerSource,omitempty" json:"routerSource,omitempty" jsonschema:"enum=interfaceAddress,enum=static,enum=none"`
	Router        string           `yaml:"router,omitempty" json:"router,omitempty"`
	DNSSource     string           `yaml:"dnsSource,omitempty" json:"dnsSource,omitempty" jsonschema:"enum=dhcpv4,enum=static,enum=self,enum=none"`
	DNSInterface  string           `yaml:"dnsInterface,omitempty" json:"dnsInterface,omitempty"`
	DNSServers    []string         `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	Authoritative bool             `yaml:"authoritative,omitempty" json:"authoritative,omitempty"`
	When          ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
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
	Interface    string `yaml:"interface" json:"interface"`
	Client       string `yaml:"client,omitempty" json:"client,omitempty"`
	Profile      string `yaml:"profile,omitempty" json:"profile,omitempty" jsonschema:"enum=default,enum=ntt-ngn-direct-hikari-denwa,enum=ntt-hgw-lan-pd"`
	PrefixLength int    `yaml:"prefixLength,omitempty" json:"prefixLength,omitempty" jsonschema:"minimum=1,maximum=128"`
	IAID         string `yaml:"iaid,omitempty" json:"iaid,omitempty"`
	DUIDType     string `yaml:"duidType,omitempty" json:"duidType,omitempty" jsonschema:"enum=,enum=vendor,enum=uuid,enum=link-layer-time,enum=link-layer"`
	Required     bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type StatusValueSourceSpec struct {
	Resource string `yaml:"resource" json:"resource"`
	Field    string `yaml:"field,omitempty" json:"field,omitempty"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type ResourceDependencySpec struct {
	Resource string `yaml:"resource" json:"resource"`
	Field    string `yaml:"field,omitempty" json:"field,omitempty"`
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
	PrefixDelegation string                   `yaml:"prefixDelegation" json:"prefixDelegation"`
	PrefixSource     string                   `yaml:"prefixSource,omitempty" json:"-"`
	Interface        string                   `yaml:"interface" json:"interface"`
	SubnetID         string                   `yaml:"subnetID,omitempty" json:"subnetID,omitempty"`
	AddressSuffix    string                   `yaml:"addressSuffix" json:"addressSuffix"`
	SendRA           bool                     `yaml:"sendRA,omitempty" json:"sendRA,omitempty"`
	Announce         bool                     `yaml:"announce,omitempty" json:"announce,omitempty"`
	When             ResourceWhenSpec         `yaml:"when,omitempty" json:"when,omitempty"`
	DependsOn        []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen        []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
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
	Sources  []DNSResolverSourceSpec `yaml:"sources" json:"sources"`
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
	Enabled       bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path          string `yaml:"path,omitempty" json:"path,omitempty"`
	Retention     string `yaml:"retention,omitempty" json:"retention,omitempty"`
	Source        string `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"enum=,enum=conntrack"`
	IncludeNDPI   bool   `yaml:"includeNDPI,omitempty" json:"includeNDPI,omitempty"`
	IncludeTLSSNI bool   `yaml:"includeTLSSNI,omitempty" json:"includeTLSSNI,omitempty"`
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
	Server           string                   `yaml:"server,omitempty" json:"server,omitempty" jsonschema:"enum=dnsmasq"`
	Managed          bool                     `yaml:"managed,omitempty" json:"managed,omitempty"`
	Role             string                   `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=server,enum=transit"`
	ListenInterfaces []string                 `yaml:"listenInterfaces,omitempty" json:"listenInterfaces,omitempty"`
	LogDHCP          bool                     `yaml:"logDHCP,omitempty" json:"logDHCP,omitempty"`
	StickyHoldDays   int                      `yaml:"stickyHoldDays,omitempty" json:"stickyHoldDays,omitempty" jsonschema:"minimum=0"`
	Interface        string                   `yaml:"interface,omitempty" json:"interface,omitempty"`
	Mode             string                   `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=stateless,enum=stateful,enum=both"`
	AddressPool      DHCPAddressPoolSpec      `yaml:"addressPool,omitempty" json:"addressPool,omitempty"`
	DNSServers       []string                 `yaml:"dnsServers,omitempty" json:"dnsServers,omitempty"`
	DNSServerFrom    []StatusValueSourceSpec  `yaml:"dnsServerFrom,omitempty" json:"dnsServerFrom,omitempty"`
	SNTPServers      []string                 `yaml:"sntpServers,omitempty" json:"sntpServers,omitempty"`
	SNTPServerFrom   []StatusValueSourceSpec  `yaml:"sntpServerFrom,omitempty" json:"sntpServerFrom,omitempty"`
	DomainSearch     []string                 `yaml:"domainSearch,omitempty" json:"domainSearch,omitempty"`
	DomainSearchFrom []StatusValueSourceSpec  `yaml:"domainSearchFrom,omitempty" json:"domainSearchFrom,omitempty"`
	LeaseTime        string                   `yaml:"leaseTime,omitempty" json:"leaseTime,omitempty"`
	RapidCommit      bool                     `yaml:"rapidCommit,omitempty" json:"rapidCommit,omitempty"`
	ConfigPath       string                   `yaml:"configPath,omitempty" json:"configPath,omitempty"`
	PIDFile          string                   `yaml:"pidFile,omitempty" json:"pidFile,omitempty"`
	DependsOn        []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen        []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type DHCPv4RelaySpec struct {
	Interfaces []string `yaml:"interfaces" json:"interfaces"`
	Upstream   string   `yaml:"upstream" json:"upstream"`
}

type DHCPv6ScopeSpec struct {
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
	Source           string `yaml:"source" json:"source" jsonschema:"enum=delegatedAddress,enum=interfaceAddress,enum=static"`
	Interface        string `yaml:"interface,omitempty" json:"interface,omitempty"`
	DelegatedAddress string `yaml:"delegatedAddress,omitempty" json:"delegatedAddress,omitempty"`
	Address          string `yaml:"address,omitempty" json:"address,omitempty"`
	AddressSuffix    string `yaml:"addressSuffix,omitempty" json:"addressSuffix,omitempty"`
	MatchSuffix      string `yaml:"matchSuffix,omitempty" json:"matchSuffix,omitempty"`
	Ordinal          int    `yaml:"ordinal,omitempty" json:"ordinal,omitempty" jsonschema:"minimum=1"`
}

type DNSResolverHealthcheckSpec struct {
	Interval      string `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout       string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	FailThreshold int    `yaml:"failThreshold,omitempty" json:"failThreshold,omitempty" jsonschema:"minimum=1"`
	PassThreshold int    `yaml:"passThreshold,omitempty" json:"passThreshold,omitempty" jsonschema:"minimum=1"`
}

type DSLiteTunnelSpec struct {
	Interface             string                   `yaml:"interface" json:"interface"`
	TunnelName            string                   `yaml:"tunnelName,omitempty" json:"tunnelName,omitempty"`
	AFTRFQDN              string                   `yaml:"aftrFQDN,omitempty" json:"aftrFQDN,omitempty"`
	AFTRIPv6              string                   `yaml:"aftrIPv6,omitempty" json:"aftrIPv6,omitempty"`
	AFTRDNSServers        []string                 `yaml:"aftrDNSServers,omitempty" json:"aftrDNSServers,omitempty"`
	AFTRAddressOrdinal    int                      `yaml:"aftrAddressOrdinal,omitempty" json:"aftrAddressOrdinal,omitempty" jsonschema:"minimum=1"`
	AFTRAddressSelection  string                   `yaml:"aftrAddressSelection,omitempty" json:"aftrAddressSelection,omitempty" jsonschema:"enum=ordinal,enum=ordinalModulo"`
	RemoteAddress         string                   `yaml:"remoteAddress,omitempty" json:"remoteAddress,omitempty"`
	LocalAddress          string                   `yaml:"localAddress,omitempty" json:"localAddress,omitempty"`
	LocalAddressFrom      StatusValueSourceSpec    `yaml:"localAddressFrom,omitempty" json:"localAddressFrom,omitempty"`
	LocalIPv6Source       string                   `yaml:"localIPv6Source,omitempty" json:"-"`
	AFTRFrom              StatusValueSourceSpec    `yaml:"aftrFrom,omitempty" json:"aftrFrom,omitempty"`
	AFTRSource            string                   `yaml:"aftrSource,omitempty" json:"-"`
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
	Destination string                   `yaml:"destination" json:"destination"`
	Type        string                   `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=unicast,enum=blackhole"`
	Device      string                   `yaml:"device,omitempty" json:"device,omitempty"`
	DeviceFrom  StatusValueSourceSpec    `yaml:"deviceFrom,omitempty" json:"deviceFrom,omitempty"`
	Gateway     string                   `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	GatewayFrom StatusValueSourceSpec    `yaml:"gatewayFrom,omitempty" json:"gatewayFrom,omitempty"`
	Metric      int                      `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
	DependsOn   []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen   []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
}

type HealthCheckSpec struct {
	Enabled            *bool                 `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Disabled           bool                  `yaml:"disabled,omitempty" json:"disabled,omitempty"`
	Type               string                `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=ping"`
	Daemon             string                `yaml:"daemon,omitempty" json:"daemon,omitempty" jsonschema:"enum=,enum=routerd-healthcheck"`
	Role               string                `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"enum=link,enum=next-hop,enum=internet,enum=service,enum=policy"`
	AddressFamily      string                `yaml:"addressFamily,omitempty" json:"addressFamily,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
	Target             string                `yaml:"target,omitempty" json:"target,omitempty"`
	TargetSource       string                `yaml:"targetSource,omitempty" json:"targetSource,omitempty" jsonschema:"enum=auto,enum=static,enum=defaultGateway,enum=dsliteRemote"`
	Interface          string                `yaml:"interface,omitempty" json:"interface,omitempty"`
	Via                string                `yaml:"via,omitempty" json:"via,omitempty"`
	FwMark             int                   `yaml:"fwmark,omitempty" json:"fwmark,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	SourceInterface    string                `yaml:"sourceInterface,omitempty" json:"sourceInterface,omitempty"`
	SourceAddress      string                `yaml:"sourceAddress,omitempty" json:"sourceAddress,omitempty"`
	SourceAddressFrom  StatusValueSourceSpec `yaml:"sourceAddressFrom,omitempty" json:"sourceAddressFrom,omitempty"`
	Protocol           string                `yaml:"protocol,omitempty" json:"protocol,omitempty" jsonschema:"enum=,enum=icmp,enum=tcp,enum=dns,enum=http"`
	Port               int                   `yaml:"port,omitempty" json:"port,omitempty" jsonschema:"minimum=0,maximum=65535"`
	Interval           string                `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout            string                `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	HealthyThreshold   int                   `yaml:"healthyThreshold,omitempty" json:"healthyThreshold,omitempty" jsonschema:"minimum=1"`
	UnhealthyThreshold int                   `yaml:"unhealthyThreshold,omitempty" json:"unhealthyThreshold,omitempty" jsonschema:"minimum=1"`
	When               ResourceWhenSpec      `yaml:"when,omitempty" json:"when,omitempty"`
}

type EgressRoutePolicySpec struct {
	Family           string                       `yaml:"family,omitempty" json:"family,omitempty" jsonschema:"enum=ipv4,enum=ipv6"`
	DestinationCIDRs []string                     `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	Selection        string                       `yaml:"selection,omitempty" json:"selection,omitempty" jsonschema:"enum=highest-weight-ready,enum=weighted-ecmp"`
	Hysteresis       string                       `yaml:"hysteresis,omitempty" json:"hysteresis,omitempty"`
	Candidates       []EgressRoutePolicyCandidate `yaml:"candidates" json:"candidates"`
}

type EgressRoutePolicyCandidate struct {
	Name          string                   `yaml:"name,omitempty" json:"name,omitempty"`
	Disabled      bool                     `yaml:"disabled,omitempty" json:"disabled,omitempty"`
	Source        string                   `yaml:"source,omitempty" json:"source,omitempty"`
	Device        string                   `yaml:"device,omitempty" json:"device,omitempty"`
	DeviceFrom    StatusValueSourceSpec    `yaml:"deviceFrom,omitempty" json:"deviceFrom,omitempty"`
	Gateway       string                   `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	GatewayFrom   StatusValueSourceSpec    `yaml:"gatewayFrom,omitempty" json:"gatewayFrom,omitempty"`
	GatewaySource string                   `yaml:"gatewaySource,omitempty" json:"gatewaySource,omitempty" jsonschema:"enum=,enum=static,enum=dhcpv4,enum=dhcpv6,enum=none"`
	RouteTable    int                      `yaml:"routeTable,omitempty" json:"routeTable,omitempty" jsonschema:"minimum=0,maximum=4294967295"`
	Metric        int                      `yaml:"metric,omitempty" json:"metric,omitempty" jsonschema:"minimum=0"`
	Weight        int                      `yaml:"weight,omitempty" json:"weight,omitempty" jsonschema:"minimum=0"`
	HealthCheck   string                   `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
	DependsOn     []ResourceDependencySpec `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	ReadyWhen     []ReadyWhenSpec          `yaml:"ready_when,omitempty" json:"-"`
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

type IPv4DefaultRoutePolicySpec struct {
	Mode                    string                            `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=priority"`
	SourceCIDRs             []string                          `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs        []string                          `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	ExcludeDestinationCIDRs []string                          `yaml:"excludeDestinationCIDRs,omitempty" json:"excludeDestinationCIDRs,omitempty"`
	Candidates              []IPv4DefaultRoutePolicyCandidate `yaml:"candidates" json:"candidates"`
}

type IPv4DefaultRoutePolicyCandidate struct {
	Name          string           `yaml:"name,omitempty" json:"name,omitempty"`
	Interface     string           `yaml:"interface,omitempty" json:"interface,omitempty"`
	RouteSet      string           `yaml:"routeSet,omitempty" json:"routeSet,omitempty"`
	GatewaySource string           `yaml:"gatewaySource,omitempty" json:"gatewaySource,omitempty" jsonschema:"enum=none,enum=dhcpv4,enum=static"`
	Gateway       string           `yaml:"gateway,omitempty" json:"gateway,omitempty"`
	Priority      int              `yaml:"priority" json:"priority" jsonschema:"minimum=1"`
	Table         int              `yaml:"table,omitempty" json:"table,omitempty" jsonschema:"minimum=1,maximum=4294967295"`
	Mark          int              `yaml:"mark,omitempty" json:"mark,omitempty" jsonschema:"minimum=1,maximum=4294967295"`
	RouteMetric   int              `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	HealthCheck   string           `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
	When          ResourceWhenSpec `yaml:"when,omitempty" json:"when,omitempty"`
}

type NAT44RuleSpec struct {
	Type                      string                 `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=masquerade,enum=snat"`
	EgressInterface           string                 `yaml:"egressInterface,omitempty" json:"egressInterface,omitempty"`
	EgressPolicyRef           string                 `yaml:"egressPolicyRef,omitempty" json:"egressPolicyRef,omitempty"`
	SourceRanges              []string               `yaml:"sourceRanges,omitempty" json:"sourceRanges,omitempty"`
	DestinationCIDRs          []string               `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	DestinationSetRefs        []string               `yaml:"destinationSetRefs,omitempty" json:"destinationSetRefs,omitempty"`
	ExcludeDestinationCIDRs   []string               `yaml:"excludeDestinationCIDRs,omitempty" json:"excludeDestinationCIDRs,omitempty"`
	ExcludeDestinationSetRefs []string               `yaml:"excludeDestinationSetRefs,omitempty" json:"excludeDestinationSetRefs,omitempty"`
	SNATAddress               string                 `yaml:"snatAddress,omitempty" json:"snatAddress,omitempty"`
	SNATAddressFrom           StatusValueSourceSpec  `yaml:"snatAddressFrom,omitempty" json:"snatAddressFrom,omitempty"`
	OutboundInterface         string                 `yaml:"outboundInterface,omitempty" json:"outboundInterface,omitempty"`
	SourceCIDRs               []string               `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	Translation               IPv4NATTranslationSpec `yaml:"translation,omitempty" json:"translation,omitempty"`
	When                      ResourceWhenSpec       `yaml:"when,omitempty" json:"when,omitempty"`
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

type IPv4PolicyRouteSpec struct {
	OutboundInterface         string   `yaml:"outboundInterface" json:"outboundInterface"`
	Table                     int      `yaml:"table" json:"table" jsonschema:"minimum=1,maximum=4294967295"`
	Priority                  int      `yaml:"priority" json:"priority" jsonschema:"minimum=1,maximum=32765"`
	Mark                      int      `yaml:"mark" json:"mark" jsonschema:"minimum=1,maximum=4294967295"`
	SourceCIDRs               []string `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs          []string `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	DestinationSetRefs        []string `yaml:"destinationSetRefs,omitempty" json:"destinationSetRefs,omitempty"`
	ExcludeDestinationCIDRs   []string `yaml:"excludeDestinationCIDRs,omitempty" json:"excludeDestinationCIDRs,omitempty"`
	ExcludeDestinationSetRefs []string `yaml:"excludeDestinationSetRefs,omitempty" json:"excludeDestinationSetRefs,omitempty"`
	RouteMetric               int      `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	AllowLocalSourceNAT       bool     `yaml:"allowLocalSourceNAT,omitempty" json:"allowLocalSourceNAT,omitempty"`
}

type IPv4PolicyRouteSetSpec struct {
	Mode                      string                  `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=hash"`
	HashFields                []string                `yaml:"hashFields,omitempty" json:"hashFields,omitempty"`
	SourceCIDRs               []string                `yaml:"sourceCIDRs,omitempty" json:"sourceCIDRs,omitempty"`
	DestinationCIDRs          []string                `yaml:"destinationCIDRs,omitempty" json:"destinationCIDRs,omitempty"`
	DestinationSetRefs        []string                `yaml:"destinationSetRefs,omitempty" json:"destinationSetRefs,omitempty"`
	ExcludeDestinationCIDRs   []string                `yaml:"excludeDestinationCIDRs,omitempty" json:"excludeDestinationCIDRs,omitempty"`
	ExcludeDestinationSetRefs []string                `yaml:"excludeDestinationSetRefs,omitempty" json:"excludeDestinationSetRefs,omitempty"`
	Targets                   []IPv4PolicyRouteTarget `yaml:"targets" json:"targets"`
	When                      ResourceWhenSpec        `yaml:"when,omitempty" json:"when,omitempty"`
}

type IPv4PolicyRouteTarget struct {
	Name              string `yaml:"name,omitempty" json:"name,omitempty"`
	OutboundInterface string `yaml:"outboundInterface" json:"outboundInterface"`
	Table             int    `yaml:"table" json:"table" jsonschema:"minimum=1,maximum=4294967295"`
	Priority          int    `yaml:"priority" json:"priority" jsonschema:"minimum=1,maximum=32765"`
	Mark              int    `yaml:"mark" json:"mark" jsonschema:"minimum=1,maximum=4294967295"`
	RouteMetric       int    `yaml:"routeMetric,omitempty" json:"routeMetric,omitempty" jsonschema:"minimum=0"`
	HealthCheck       string `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
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

type FirewallLogSpec struct {
	Enabled    bool                  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Path       string                `yaml:"path,omitempty" json:"path,omitempty"`
	Retention  string                `yaml:"retention,omitempty" json:"retention,omitempty"`
	NFLogGroup int                   `yaml:"nflogGroup,omitempty" json:"nflogGroup,omitempty" jsonschema:"minimum=0,maximum=65535"`
	Log        FirewallLogPolicySpec `yaml:"log,omitempty" json:"log,omitempty"`
}

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
	MACAddress      string `yaml:"macAddress" json:"macAddress"`
	As              string `yaml:"as,omitempty" json:"as,omitempty" jsonschema:"enum=,enum=guest,enum=trusted"`
	Name            string `yaml:"name,omitempty" json:"name,omitempty"`
	IPv4Reservation string `yaml:"ipv4Reservation,omitempty" json:"ipv4Reservation,omitempty"`
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

func (r Resource) VirtualIPv4AddressSpec() (VirtualIPv4AddressSpec, error) {
	return specAs[VirtualIPv4AddressSpec](r)
}

func (r Resource) VirtualIPv6AddressSpec() (VirtualIPv6AddressSpec, error) {
	return specAs[VirtualIPv6AddressSpec](r)
}

func (r Resource) BGPRouterSpec() (BGPRouterSpec, error) {
	return specAs[BGPRouterSpec](r)
}

func (r Resource) BGPPeerSpec() (BGPPeerSpec, error) {
	return specAs[BGPPeerSpec](r)
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

func (r Resource) DHCPv4ScopeSpec() (DHCPv4ScopeSpec, error) {
	return specAs[DHCPv4ScopeSpec](r)
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

func (r Resource) DHCPv4RelaySpec() (DHCPv4RelaySpec, error) {
	return specAs[DHCPv4RelaySpec](r)
}

func (r Resource) DHCPv6ScopeSpec() (DHCPv6ScopeSpec, error) {
	return specAs[DHCPv6ScopeSpec](r)
}

func (r Resource) SelfAddressPolicySpec() (SelfAddressPolicySpec, error) {
	return specAs[SelfAddressPolicySpec](r)
}

func (r Resource) DNSResolverSpec() (DNSResolverSpec, error) {
	return specAs[DNSResolverSpec](r)
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

func (r Resource) IPv4DefaultRoutePolicySpec() (IPv4DefaultRoutePolicySpec, error) {
	return specAs[IPv4DefaultRoutePolicySpec](r)
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

func (r Resource) IPv4PolicyRouteSpec() (IPv4PolicyRouteSpec, error) {
	return specAs[IPv4PolicyRouteSpec](r)
}

func (r Resource) IPv4PolicyRouteSetSpec() (IPv4PolicyRouteSetSpec, error) {
	return specAs[IPv4PolicyRouteSetSpec](r)
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
