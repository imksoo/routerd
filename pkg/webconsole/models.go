// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"time"

	"github.com/imksoo/routerd/pkg/conntracktuning"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type Snapshot struct {
	GeneratedAt      time.Time                     `json:"generatedAt"`
	ConsoleLinks     []ConsoleLink                 `json:"consoleLinks,omitempty"`
	Status           controlapi.Status             `json:"status"`
	Controllers      []controlapi.ControllerStatus `json:"controllers,omitempty"`
	GatewayHealth    GatewayHealth                 `json:"gatewayHealth"`
	Phases           map[string]int                `json:"phases,omitempty"`
	Resources        []routerstate.ObjectStatus    `json:"resources,omitempty"`
	Interfaces       []InterfaceSummary            `json:"interfaces,omitempty"`
	Events           []routerstate.StoredEvent     `json:"events,omitempty"`
	Connections      *observe.ConnectionTable      `json:"connections,omitempty"`
	DNSQueries       []logstore.DNSQuery           `json:"dnsQueries,omitempty"`
	TrafficFlows     []logstore.TrafficFlow        `json:"trafficFlows,omitempty"`
	FirewallLogs     []logstore.FirewallLogEntry   `json:"firewallLogs,omitempty"`
	ConntrackTuning  *conntracktuning.Summary      `json:"conntrackTuning,omitempty"`
	DHCPFingerprints []logstore.DHCPFingerprint    `json:"dhcpFingerprints,omitempty"`
	DHCPLeases       []DHCPLease                   `json:"dhcpLeases,omitempty"`
	Neighbors        []NeighborEntry               `json:"neighbors,omitempty"`
	Clients          []ClientEntry                 `json:"clients,omitempty"`
	VPN              VPNStatus                     `json:"vpn,omitempty"`
	DPI              *DPIStatus                    `json:"dpi,omitempty"`
	SystemUsage      SystemUsage                   `json:"systemUsage,omitempty"`
	Errors           []string                      `json:"errors,omitempty"`
}

type ConsoleLink struct {
	Label       string `json:"label"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type SystemUsage struct {
	CPUPercent        *float64    `json:"cpuPercent,omitempty"`
	Load1             *float64    `json:"load1,omitempty"`
	MemoryUsedBytes   uint64      `json:"memoryUsedBytes,omitempty"`
	MemoryTotalBytes  uint64      `json:"memoryTotalBytes,omitempty"`
	MemoryUsedPercent *float64    `json:"memoryUsedPercent,omitempty"`
	Disks             []DiskUsage `json:"disks,omitempty"`
}

type DiskUsage struct {
	Path        string   `json:"path"`
	UsedBytes   uint64   `json:"usedBytes"`
	TotalBytes  uint64   `json:"totalBytes"`
	UsedPercent *float64 `json:"usedPercent,omitempty"`
}

type SnapshotOptions struct {
	EventLimit             int
	ConnectionsLimit       int
	FirewallLimit          int
	DNSQueryLimit          int
	TrafficFlowLimit       int
	FingerprintQueryLimit  int
	DHCPFingerprintLimit   int
	IncludeDPIEnrichment   bool
	IncludeClients         bool
	IncludeConntrackTuning bool
	IncludeVPN             bool
	SkipResources          bool
	SkipDHCPLeases         bool
}

const clientObservationWindow = time.Hour

type GatewayHealth struct {
	Overall    string                   `json:"overall"`
	Components []GatewayHealthComponent `json:"components,omitempty"`
}

type GatewayHealthComponent struct {
	Kind               string              `json:"kind"`
	Name               string              `json:"name"`
	Status             string              `json:"status"`
	Phase              string              `json:"phase,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	Detail             string              `json:"detail,omitempty"`
	SelectedCandidate  string              `json:"selectedCandidate,omitempty"`
	PreferredCandidate string              `json:"preferredCandidate,omitempty"`
	SelectedPath       string              `json:"selectedPath,omitempty"`
	PreferredPath      string              `json:"preferredPath,omitempty"`
	FallbackReason     string              `json:"fallbackReason,omitempty"`
	FailedProbes       []string            `json:"failedProbes,omitempty"`
	LastTransition     string              `json:"lastTransition,omitempty"`
	Waiting            []map[string]string `json:"waiting,omitempty"`
}

type DPIStatus struct {
	Classifier *DPIServiceStatus `json:"classifier,omitempty"`
	Agent      *DPIServiceStatus `json:"agent,omitempty"`
}

type DPIServiceStatus struct {
	Available      bool           `json:"available"`
	Socket         string         `json:"socket,omitempty"`
	Engine         string         `json:"engine,omitempty"`
	ActiveEngine   string         `json:"activeEngine,omitempty"`
	LibNDPILoaded  bool           `json:"libndpiLoaded,omitempty"`
	LibNDPIVersion string         `json:"libndpiVersion,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Error          string         `json:"error,omitempty"`
	Stats          map[string]any `json:"stats,omitempty"`
}

type ConfigSnapshot struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type GenerationDiff struct {
	From int64  `json:"from"`
	To   int64  `json:"to"`
	Diff string `json:"diff"`
}

type DHCPLease struct {
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	MAC       string    `json:"mac"`
	IP        string    `json:"ip"`
	Hostname  string    `json:"hostname,omitempty"`
	ClientID  string    `json:"clientId,omitempty"`
	Vendor    string    `json:"vendor,omitempty"`
	Family    string    `json:"family,omitempty"`
	Source    string    `json:"source,omitempty"`
	// StickyUntil is nil unless the lease is actively held. A time.Time value
	// cannot be omitted by encoding/json, even with omitempty, which exposed
	// Go's zero time to API consumers as 0001-01-01T00:00:00Z.
	StickyUntil *time.Time `json:"stickyUntil,omitempty"`
	StickyState string     `json:"stickyState,omitempty"`
}

type NeighborEntry struct {
	IP     string `json:"ip"`
	IfName string `json:"ifname,omitempty"`
	MAC    string `json:"mac,omitempty"`
	State  string `json:"state,omitempty"`
	Source string `json:"source,omitempty"`
	Vendor string `json:"vendor,omitempty"`
}

type ClientEntry struct {
	ID                    string   `json:"id"`
	Hostname              string   `json:"hostname,omitempty"`
	MAC                   string   `json:"mac,omitempty"`
	Vendor                string   `json:"vendor,omitempty"`
	Addresses             []string `json:"addresses,omitempty"`
	State                 string   `json:"state,omitempty"`
	Sources               []string `json:"sources,omitempty"`
	Peers                 []string `json:"peers,omitempty"`
	BytesOut              int64    `json:"bytesOut,omitempty"`
	BytesIn               int64    `json:"bytesIn,omitempty"`
	PrimaryActivity       string   `json:"primaryActivity,omitempty"`
	LastProtocol          string   `json:"lastProtocol,omitempty"`
	LastProtocolDetail    string   `json:"lastProtocolDetail,omitempty"`
	ProtocolMix           []string `json:"protocolMix,omitempty"`
	InferredOSFamily      string   `json:"inferredOSFamily,omitempty"`
	InferredDeviceClass   string   `json:"inferredDeviceClass,omitempty"`
	FingerprintConfidence int      `json:"fingerprintConfidence,omitempty"`
	FingerprintSignals    []string `json:"fingerprintSignals,omitempty"`
	StickyUntil           string   `json:"stickyUntil,omitempty"`
	StickyState           string   `json:"stickyState,omitempty"`
	ClientPolicy          string   `json:"clientPolicy,omitempty"`
	ClientPolicyMode      string   `json:"clientPolicyMode,omitempty"`
	IsolationPolicy       []string `json:"isolationPolicy,omitempty"`
}

type InterfaceSummary struct {
	Name            string   `json:"name"`
	IfName          string   `json:"ifname"`
	Phase           string   `json:"phase,omitempty"`
	Role            string   `json:"role,omitempty"`
	Zone            string   `json:"zone,omitempty"`
	Managed         bool     `json:"managed,omitempty"`
	Owner           string   `json:"owner,omitempty"`
	MTU             int      `json:"mtu,omitempty"`
	HardwareAddress string   `json:"hardwareAddress,omitempty"`
	Flags           string   `json:"flags,omitempty"`
	Addresses       []string `json:"addresses,omitempty"`
}

type VPNStatus struct {
	WireGuard []WireGuardInterfaceStatus `json:"wireGuard,omitempty"`
	Tailscale *TailscaleStatus           `json:"tailscale,omitempty"`
	Errors    []string                   `json:"errors,omitempty"`
}

type WireGuardInterfaceStatus struct {
	Name       string                `json:"name"`
	PublicKey  string                `json:"publicKey,omitempty"`
	ListenPort int                   `json:"listenPort,omitempty"`
	FwMark     string                `json:"fwmark,omitempty"`
	Peers      []WireGuardPeerStatus `json:"peers,omitempty"`
}

type WireGuardPeerStatus struct {
	PublicKey              string    `json:"publicKey"`
	Endpoint               string    `json:"endpoint,omitempty"`
	AllowedIPs             []string  `json:"allowedIPs,omitempty"`
	LatestHandshake        time.Time `json:"latestHandshake,omitempty"`
	TransferRxBytes        int64     `json:"transferRxBytes,omitempty"`
	TransferTxBytes        int64     `json:"transferTxBytes,omitempty"`
	PersistentKeepaliveSec int       `json:"persistentKeepaliveSec,omitempty"`
}

type TailscaleStatus struct {
	BackendState    string                `json:"backendState,omitempty"`
	TailnetName     string                `json:"tailnetName,omitempty"`
	MagicDNSSuffix  string                `json:"magicDNSSuffix,omitempty"`
	MagicDNSEnabled bool                  `json:"magicDNSEnabled,omitempty"`
	CertDomains     []string              `json:"certDomains,omitempty"`
	HostName        string                `json:"hostName,omitempty"`
	DNSName         string                `json:"dnsName,omitempty"`
	TailscaleIPs    []string              `json:"tailscaleIPs,omitempty"`
	AllowedIPs      []string              `json:"allowedIPs,omitempty"`
	Online          bool                  `json:"online,omitempty"`
	Active          bool                  `json:"active,omitempty"`
	ExitNode        bool                  `json:"exitNode,omitempty"`
	ExitNodeOption  bool                  `json:"exitNodeOption,omitempty"`
	Peers           []TailscalePeerStatus `json:"peers,omitempty"`
}

type TailscalePeerStatus struct {
	ID             string   `json:"id,omitempty"`
	HostName       string   `json:"hostName,omitempty"`
	DNSName        string   `json:"dnsName,omitempty"`
	TailscaleIPs   []string `json:"tailscaleIPs,omitempty"`
	AllowedIPs     []string `json:"allowedIPs,omitempty"`
	Online         bool     `json:"online,omitempty"`
	Active         bool     `json:"active,omitempty"`
	ExitNode       bool     `json:"exitNode,omitempty"`
	ExitNodeOption bool     `json:"exitNodeOption,omitempty"`
	Relay          string   `json:"relay,omitempty"`
	LastSeen       string   `json:"lastSeen,omitempty"`
	RxBytes        int64    `json:"rxBytes,omitempty"`
	TxBytes        int64    `json:"txBytes,omitempty"`
}
