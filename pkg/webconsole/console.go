// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"routerd/internal/hostcmd"
	"routerd/pkg/api"
	"routerd/pkg/apply"
	"routerd/pkg/bus"
	"routerd/pkg/conntracktuning"
	"routerd/pkg/controlapi"
	"routerd/pkg/dhcpfingerprint"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
	"routerd/pkg/platform"
	routerstate "routerd/pkg/state"
	"routerd/pkg/tailscale"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Options struct {
	Router                 *api.Router
	Store                  routerstate.Store
	Result                 func() *apply.Result
	Connections            func(limit int) (*observe.ConnectionTable, error)
	VPNStatus              func() (VPNStatus, error)
	Title                  string
	BasePath               string
	ConnectionsLimit       int
	DNSQueryLogPath        string
	TrafficFlowLogPath     string
	FirewallLogPath        string
	DHCPFingerprintLogPath string
	DHCPStickyLogPath      string
	DHCPLeasePaths         []string
	ConfigPath             string
	ControllerModes        []controlapi.ControllerStatus
	ControllerStatuses     func() []controlapi.ControllerStatus
	Bus                    *bus.Bus
	ReverseLookup          func(ctx context.Context, address string) ([]string, error)
}

type Handler struct {
	opts        Options
	reverseDNS  *reverseDNSCache
	systemUsage *systemUsageSampler
}

type Snapshot struct {
	GeneratedAt      time.Time                     `json:"generatedAt"`
	Status           controlapi.Status             `json:"status"`
	Controllers      []controlapi.ControllerStatus `json:"controllers,omitempty"`
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
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
	MAC         string    `json:"mac"`
	IP          string    `json:"ip"`
	Hostname    string    `json:"hostname,omitempty"`
	ClientID    string    `json:"clientId,omitempty"`
	Vendor      string    `json:"vendor,omitempty"`
	Family      string    `json:"family,omitempty"`
	Source      string    `json:"source,omitempty"`
	StickyUntil time.Time `json:"stickyUntil,omitempty"`
	StickyState string    `json:"stickyState,omitempty"`
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

//go:embed static
var staticFiles embed.FS

func New(opts Options) Handler {
	if opts.Title == "" {
		opts.Title = "routerd"
	}
	if opts.BasePath == "" {
		opts.BasePath = "/"
	}
	if opts.ConnectionsLimit == 0 {
		opts.ConnectionsLimit = 200
	}
	if opts.Connections == nil {
		opts.Connections = observe.Connections
	}
	if len(opts.DHCPLeasePaths) == 0 {
		opts.DHCPLeasePaths = []string{"/run/routerd/dnsmasq.leases", "/var/lib/misc/dnsmasq.leases"}
	}
	if opts.DHCPFingerprintLogPath == "" {
		defaults, _ := platform.Current()
		opts.DHCPFingerprintLogPath = strings.TrimRight(defaults.StateDir, "/") + "/dhcp-fingerprints.db"
	}
	if opts.DHCPStickyLogPath == "" {
		defaults, _ := platform.Current()
		opts.DHCPStickyLogPath = strings.TrimRight(defaults.StateDir, "/") + "/dhcp-sticky.db"
	}
	if opts.ReverseLookup == nil {
		opts.ReverseLookup = net.DefaultResolver.LookupAddr
	}
	return Handler{opts: opts, reverseDNS: newReverseDNSCache(time.Hour), systemUsage: &systemUsageSampler{}}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "read-only console", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, h.basePath())
	path = strings.TrimPrefix(path, "/")
	if path == "" || path == "index.html" {
		h.index(w)
		return
	}
	switch path {
	case "api/v1/summary":
		h.summary(w, r)
	case "api/v1/resources":
		h.resources(w)
	case "api/v1/controllers":
		h.controllers(w)
	case "api/v1/events":
		h.events(w, r)
	case "api/v1/events/stream", "api/events/stream", "v1/events/stream":
		h.eventStream(w, r)
	case "api/v1/connections":
		h.connections(w, r)
	case "api/v1/dns-queries":
		h.dnsQueries(w, r)
	case "api/v1/traffic-flows":
		h.trafficFlows(w, r)
	case "api/v1/firewall-logs":
		h.firewallLogs(w, r)
	case "api/v1/firewall/deny-timeline":
		h.firewallDenyTimeline(w, r)
	case "api/v1/clients":
		h.clients(w)
	case "api/v1/vpn":
		h.vpn(w)
	case "api/v1/routes":
		h.routes(w)
	case "api/v1/bgp":
		h.operationalStatus(w, "bgp")
	case "api/v1/vrrp":
		h.operationalStatus(w, "vrrp")
	case "api/v1/ingress":
		h.operationalStatus(w, "ingress")
	case "api/v1/config":
		h.config(w)
	case "api/v1/generations":
		h.generations(w, r)
	case "bgp":
		h.operationalPage(w, "bgp")
	case "vrrp":
		h.operationalPage(w, "vrrp")
	case "ingress":
		h.operationalPage(w, "ingress")
	case "routes":
		h.routesPage(w)
	default:
		if strings.HasPrefix(path, "api/v1/generations/") {
			h.generationDetail(w, r, strings.TrimPrefix(path, "api/v1/generations/"))
			return
		}
		if strings.HasPrefix(path, "api/") {
			http.NotFound(w, r)
			return
		}
		h.asset(w, r, path)
	}
}

func (h Handler) Snapshot(opts SnapshotOptions) Snapshot {
	if opts.EventLimit == 0 {
		opts.EventLimit = 50
	}
	if opts.FirewallLimit == 0 {
		opts.FirewallLimit = 50
	}
	if opts.DNSQueryLimit == 0 {
		opts.DNSQueryLimit = 50
	}
	if opts.TrafficFlowLimit == 0 {
		opts.TrafficFlowLimit = -1
	}
	if opts.FingerprintQueryLimit <= 0 {
		opts.FingerprintQueryLimit = opts.DNSQueryLimit
	}
	if opts.DHCPFingerprintLimit <= 0 {
		opts.DHCPFingerprintLimit = 200
	}
	now := time.Now().UTC()
	clientSince := now.Add(-clientObservationWindow)
	var errors []string
	var err error
	var resources []routerstate.ObjectStatus
	if !opts.SkipResources {
		resources, err = h.resourceStatuses()
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	var events []routerstate.StoredEvent
	if opts.EventLimit >= 0 {
		events, err = h.eventList(opts.EventLimit)
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	var connections *observe.ConnectionTable
	if h.opts.Connections != nil && opts.ConnectionsLimit >= 0 {
		connections, err = h.opts.Connections(opts.ConnectionsLimit)
		if err != nil {
			errors = append(errors, err.Error())
		} else if opts.IncludeDPIEnrichment {
			if err := h.enrichConnectionsWithDPI(connections, now, clientObservationWindow); err != nil {
				errors = append(errors, err.Error())
			}
		} else {
			applyConnectionTablePortFallback(connections)
		}
		h.enrichConnectionsWithLocalRedirect(connections)
		if err := h.enrichConnectionsWithRemoteIdentity(context.Background(), connections); err != nil {
			errors = append(errors, err.Error())
		}
	}
	var dnsQueries []logstore.DNSQuery
	if opts.DNSQueryLimit >= 0 {
		dnsQueries, err = h.queryLogList(logstore.DNSQueryFilter{Since: clientSince, Limit: opts.DNSQueryLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	fingerprintDNSQueries := dnsQueries
	if opts.IncludeClients && opts.FingerprintQueryLimit > opts.DNSQueryLimit {
		if queries, err := h.queryLogList(logstore.DNSQueryFilter{Since: clientSince, Limit: opts.FingerprintQueryLimit}); err == nil {
			fingerprintDNSQueries = queries
		} else {
			errors = append(errors, err.Error())
		}
	}
	var trafficFlows []logstore.TrafficFlow
	if opts.TrafficFlowLimit >= 0 {
		trafficFlows, err = h.trafficFlowList(logstore.TrafficFlowFilter{Since: clientSince, Limit: opts.TrafficFlowLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
		trafficFlows = enrichTrafficFlowsWithDNS(trafficFlows, dnsQueries)
		if opts.IncludeDPIEnrichment {
			if enriched, err := h.enrichTrafficFlowsWithDPI(trafficFlows, now, clientObservationWindow); err == nil {
				trafficFlows = enriched
			} else {
				errors = append(errors, err.Error())
			}
		} else {
			applyTrafficFlowListPortFallback(trafficFlows)
		}
	}
	var firewallLogs []logstore.FirewallLogEntry
	if opts.FirewallLimit >= 0 {
		firewallSince := now.Add(-24 * time.Hour)
		if opts.IncludeClients {
			firewallSince = clientSince
		}
		firewallLogs, err = h.firewallLogList(logstore.FirewallLogFilter{Since: firewallSince, Action: "drop", Limit: opts.FirewallLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
		if err := h.enrichFirewallLogsWithRemoteIdentity(context.Background(), firewallLogs); err != nil {
			errors = append(errors, err.Error())
		}
		h.enrichFirewallLogsWithAddressSets(firewallLogs)
	}
	var conntrackTuning *conntracktuning.Summary
	if opts.IncludeConntrackTuning {
		tuning, err := h.conntrackTuningSummary(time.Now().UTC(), 24*time.Hour, h.opts.Router != nil && h.opts.Router.Spec.Apply.AutoTuneConntrack)
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			conntrackTuning = &tuning
		}
	}
	var dhcpLeases []DHCPLease
	var stickyLeases []logstore.DHCPStickyLease
	if !opts.SkipDHCPLeases || opts.IncludeClients {
		dhcpLeases, err = h.dhcpLeaseList()
		if err != nil {
			errors = append(errors, err.Error())
		}
		stickyLeases, err = h.dhcpStickyLeaseList(logstore.DHCPStickyFilter{HeldOnly: true, Now: now, Limit: 10000})
		if err != nil {
			errors = append(errors, err.Error())
		}
		dhcpLeases = annotateDHCPLeasesWithSticky(dhcpLeases, stickyLeases, now)
	}
	var dhcpFingerprints []logstore.DHCPFingerprint
	var neighbors []NeighborEntry
	var clientFirewallLogs []logstore.FirewallLogEntry
	var clients []ClientEntry
	if opts.IncludeClients {
		dhcpFingerprints, err = h.dhcpFingerprintList(logstore.DHCPFingerprintFilter{Since: clientSince, Limit: opts.DHCPFingerprintLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
		if opts.FirewallLimit < 0 {
			clientFirewallLogs, err = h.firewallLogList(logstore.FirewallLogFilter{Since: clientSince, Action: "drop", Limit: 1000})
			if err != nil {
				errors = append(errors, err.Error())
			}
		} else {
			clientFirewallLogs = firewallLogs
		}
		neighbors, err = neighborList()
		if err != nil {
			errors = append(errors, err.Error())
		}
		clients = h.annotateClientsWithPolicy(correlateClients(dhcpLeases, neighbors, trafficFlows, fingerprintDNSQueries, clientFirewallLogs, dhcpFingerprints))
	}
	var vpn VPNStatus
	if opts.IncludeVPN {
		vpn, err = h.vpnStatus()
		if err != nil {
			errors = append(errors, err.Error())
		}
		errors = append(errors, vpn.Errors...)
	}
	result := (*apply.Result)(nil)
	if h.opts.Result != nil {
		result = h.opts.Result()
	}
	dpiStatus := h.dpiStatus(context.Background())
	systemUsage := h.readSystemUsage()
	result = resultWithLatestGeneration(result, h.opts.Store)
	controllers := h.controllerStatuses()
	recordConsoleMetrics(context.Background(), resources, controllers, dhcpLeases, clients, stickyLeases, now)
	return Snapshot{
		GeneratedAt:      now,
		Status:           statusWithControllers(result, controllers),
		Controllers:      controllers,
		Phases:           phaseCounts(resources),
		Resources:        resources,
		Interfaces:       h.interfaceSummaries(resources),
		Events:           events,
		Connections:      connections,
		DNSQueries:       dnsQueries,
		TrafficFlows:     trafficFlows,
		FirewallLogs:     firewallLogs,
		ConntrackTuning:  conntrackTuning,
		DHCPFingerprints: dhcpFingerprints,
		DHCPLeases:       dhcpLeases,
		Neighbors:        neighbors,
		Clients:          clients,
		VPN:              vpn,
		DPI:              dpiStatus,
		SystemUsage:      systemUsage,
		Errors:           errors,
	}
}

func statusWithControllers(result *apply.Result, controllers []controlapi.ControllerStatus) controlapi.Status {
	status := controlapi.NewStatus(result)
	status.Status.Controllers = controllers
	return status
}

func resultWithLatestGeneration(result *apply.Result, store routerstate.Store) *apply.Result {
	if store == nil {
		return result
	}
	reader, ok := store.(routerstate.LatestGenerationReader)
	if !ok {
		return result
	}
	generation := reader.LatestGeneration()
	if generation == 0 {
		return result
	}
	if result == nil {
		return &apply.Result{Generation: generation}
	}
	next := *result
	next.Generation = generation
	return &next
}

func (h Handler) index(w http.ResponseWriter) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	page := string(data)
	page = strings.ReplaceAll(page, "__ROUTERD_TITLE_TEXT__", html.EscapeString(h.opts.Title))
	page = strings.ReplaceAll(page, "__ROUTERD_TITLE_JS__", template.JSEscapeString(h.opts.Title))
	page = strings.ReplaceAll(page, "__ROUTERD_BASE_PATH__", template.JSEscapeString(h.basePath()))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (h Handler) asset(w http.ResponseWriter, r *http.Request, path string) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request := new(http.Request)
	*request = *r
	urlCopy := *r.URL
	request.URL = &urlCopy
	request.URL.Path = "/" + strings.TrimPrefix(path, "/")
	http.FileServer(http.FS(sub)).ServeHTTP(w, request)
}

func (h Handler) summary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.Snapshot(SnapshotOptions{
		EventLimit:             signedIntQuery(r, "events", 50),
		ConnectionsLimit:       signedIntQuery(r, "connections", h.opts.ConnectionsLimit),
		FirewallLimit:          signedIntQuery(r, "firewallLogs", 50),
		DNSQueryLimit:          signedIntQuery(r, "dnsQueries", 50),
		TrafficFlowLimit:       signedIntQuery(r, "trafficFlows", 50),
		FingerprintQueryLimit:  intQuery(r, "fingerprintQueries", 1000),
		DHCPFingerprintLimit:   intQuery(r, "dhcpFingerprints", 1000),
		IncludeDPIEnrichment:   boolQuery(r, "dpi", false),
		IncludeClients:         boolQuery(r, "clients", false),
		IncludeConntrackTuning: boolQuery(r, "tuning", false),
		IncludeVPN:             boolQuery(r, "vpn", true),
		SkipResources:          !boolQuery(r, "resources", true),
		SkipDHCPLeases:         !boolQuery(r, "dhcpLeases", true),
	}))
}

func (h Handler) resources(w http.ResponseWriter) {
	resources, err := h.resourceStatuses()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, resources)
}

type OperationalStatus struct {
	GeneratedAt time.Time                  `json:"generatedAt"`
	Kind        string                     `json:"kind"`
	Resources   []routerstate.ObjectStatus `json:"resources"`
}

type RoutesStatus struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Routes      []RouteEntry   `json:"routes"`
	BGPPeers    []RouteBGPPeer `json:"bgpPeers,omitempty"`
	Errors      []string       `json:"errors,omitempty"`
}

type RouteEntry struct {
	Source      string `json:"source"`
	Resource    string `json:"resource,omitempty"`
	Family      string `json:"family,omitempty"`
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Device      string `json:"device,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Table       string `json:"table,omitempty"`
	Metric      string `json:"metric,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Type        string `json:"type,omitempty"`
	Peer        string `json:"peer,omitempty"`
	Phase       string `json:"phase,omitempty"`
	ObservedAt  string `json:"observedAt,omitempty"`
}

type RouteBGPPeer struct {
	Router           string `json:"router"`
	Peer             string `json:"peer"`
	ASN              string `json:"asn,omitempty"`
	State            string `json:"state,omitempty"`
	Established      bool   `json:"established,omitempty"`
	PrefixesReceived string `json:"prefixesReceived,omitempty"`
	Messages         string `json:"messages,omitempty"`
	LastEstablished  string `json:"lastEstablishedAt,omitempty"`
	LastError        string `json:"lastErrorReason,omitempty"`
}

func (h Handler) operationalStatus(w http.ResponseWriter, kind string) {
	resources, err := h.operationalResources(kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, OperationalStatus{GeneratedAt: time.Now().UTC(), Kind: kind, Resources: resources})
}

func (h Handler) routes(w http.ResponseWriter) {
	status := h.routesStatus()
	writeJSON(w, status)
}

func (h Handler) routesPage(w http.ResponseWriter) {
	status := h.routesStatus()
	page := routesHTMLPage(h.opts.Title, status)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (h Handler) operationalPage(w http.ResponseWriter, kind string) {
	resources, err := h.operationalResources(kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	page := operationalHTMLPage(h.opts.Title, kind, resources)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (h Handler) operationalResources(kind string) ([]routerstate.ObjectStatus, error) {
	resources, err := h.resourceStatuses()
	if err != nil {
		return nil, err
	}
	var out []routerstate.ObjectStatus
	for _, resource := range resources {
		switch kind {
		case "bgp":
			if resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer" {
				out = append(out, resource)
			}
		case "vrrp":
			if resource.Kind == "VirtualIPv4Address" {
				out = append(out, resource)
			}
		case "ingress":
			if resource.Kind == "IngressService" {
				out = append(out, resource)
			}
		}
	}
	return out, nil
}

func (h Handler) routesStatus() RoutesStatus {
	status := RoutesStatus{GeneratedAt: time.Now().UTC()}
	resources, err := h.resourceStatuses()
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
	}
	status.Routes = append(status.Routes, h.configuredRouteEntries(resources)...)
	status.Routes = append(status.Routes, bgpRouteEntries(resources)...)
	status.BGPPeers = bgpRoutePeers(resources)
	live, errors := liveKernelRouteEntries()
	status.Routes = append(status.Routes, live...)
	status.Errors = append(status.Errors, errors...)
	sortRouteEntries(status.Routes)
	sort.Slice(status.BGPPeers, func(i, j int) bool {
		if status.BGPPeers[i].Router != status.BGPPeers[j].Router {
			return status.BGPPeers[i].Router < status.BGPPeers[j].Router
		}
		return status.BGPPeers[i].Peer < status.BGPPeers[j].Peer
	})
	return status
}

func (h Handler) configuredRouteEntries(resources []routerstate.ObjectStatus) []RouteEntry {
	statuses := map[string]map[string]any{}
	for _, resource := range resources {
		statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Name] = resource.Status
	}
	var out []RouteEntry
	if h.opts.Router == nil {
		return out
	}
	expanded := api.ExpandClusterNetworkRoutes(h.opts.Router)
	for _, resource := range expanded.Spec.Resources {
		status := statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name]
		switch resource.Kind {
		case "IPv4StaticRoute":
			spec, err := resource.IPv4StaticRouteSpec()
			if err != nil {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "static",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv4",
				Destination: firstNonEmpty(stringFromMap(status, "destination"), spec.Destination),
				Gateway:     firstNonEmpty(stringFromMap(status, "gateway"), spec.Via),
				Device:      firstNonEmpty(stringFromMap(status, "device"), spec.Interface),
				Metric:      routeMetricText(firstNonEmpty(stringFromMap(status, "metric"), strconv.Itoa(spec.Metric))),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  firstNonEmpty(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "IPv6StaticRoute":
			spec, err := resource.IPv6StaticRouteSpec()
			if err != nil {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "static",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv6",
				Destination: firstNonEmpty(stringFromMap(status, "destination"), spec.Destination),
				Gateway:     firstNonEmpty(stringFromMap(status, "gateway"), spec.Via),
				Device:      firstNonEmpty(stringFromMap(status, "device"), spec.Interface),
				Metric:      routeMetricText(firstNonEmpty(stringFromMap(status, "metric"), strconv.Itoa(spec.Metric))),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  firstNonEmpty(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "IPv4Route":
			spec, err := resource.IPv4RouteSpec()
			if err != nil {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "static",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv4",
				Destination: firstNonEmpty(stringFromMap(status, "destination"), spec.Destination),
				Gateway:     firstNonEmpty(stringFromMap(status, "gateway"), spec.Gateway),
				Device:      firstNonEmpty(stringFromMap(status, "device"), spec.Device),
				Metric:      routeMetricText(firstNonEmpty(stringFromMap(status, "metric"), strconv.Itoa(spec.Metric))),
				Type:        firstNonEmpty(stringFromMap(status, "type"), spec.Type),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  firstNonEmpty(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "DHCPv4Lease":
			spec, err := resource.DHCPv4LeaseSpec()
			if err != nil {
				continue
			}
			gateway := firstNonEmpty(stringFromMap(status, "appliedDefaultGateway"), stringFromMap(status, "defaultGateway"), stringFromMap(status, "gateway"))
			if gateway == "" {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "dhcpv4",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv4",
				Destination: "default",
				Gateway:     gateway,
				Device:      firstNonEmpty(stringFromMap(status, "interface"), spec.Interface),
				Protocol:    "dhcp",
				Metric:      routeMetricText(firstNonEmpty(stringFromMap(status, "routeMetric"), strconv.Itoa(spec.RouteMetric))),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  firstNonEmpty(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "IPv4DefaultRoutePolicy":
			spec, err := resource.IPv4DefaultRoutePolicySpec()
			if err != nil {
				continue
			}
			for _, candidate := range spec.Candidates {
				if candidate.RouteSet != "" {
					continue
				}
				out = append(out, RouteEntry{
					Source:      "policy",
					Resource:    resource.Kind + "/" + resource.Metadata.Name,
					Family:      "ipv4",
					Destination: "default",
					Gateway:     candidate.Gateway,
					Device:      candidate.Interface,
					Table:       routeMetricText(strconv.Itoa(candidate.Table)),
					Metric:      routeMetricText(strconv.Itoa(candidate.RouteMetric)),
					Phase:       stringFromMap(status, "phase"),
					ObservedAt:  firstNonEmpty(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
				})
			}
		}
	}
	return out
}

func bgpRouteEntries(resources []routerstate.ObjectStatus) []RouteEntry {
	var out []RouteEntry
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		for _, prefix := range statusList(resource.Status["prefixes"]) {
			destination := firstNonEmpty(statusAnyText(prefix["prefix"]), statusAnyText(prefix["network"]))
			if destination == "" {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "bgp",
				Resource:    resource.Kind + "/" + resource.Name,
				Family:      routeFamily(destination),
				Destination: destination,
				Protocol:    "bgp",
				Peer:        firstNonEmpty(statusAnyText(prefix["peer"]), statusAnyText(prefix["nextHop"]), statusAnyText(prefix["nexthop"])),
				Phase:       statusText(resource.Status, "phase"),
				ObservedAt:  statusText(resource.Status, "observedAt"),
			})
		}
	}
	return out
}

func bgpRoutePeers(resources []routerstate.ObjectStatus) []RouteBGPPeer {
	var out []RouteBGPPeer
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		for _, peer := range statusList(resource.Status["peers"]) {
			messages := ""
			if statusAnyText(peer["messagesReceived"]) != "" || statusAnyText(peer["messagesSent"]) != "" {
				messages = fmt.Sprintf("%d/%d", statusIntValue(peer["messagesReceived"]), statusIntValue(peer["messagesSent"]))
			}
			established, _ := statusBoolValue(peer["established"])
			out = append(out, RouteBGPPeer{
				Router:           resource.Name,
				Peer:             statusAnyText(peer["address"]),
				ASN:              statusAnyText(peer["asn"]),
				State:            statusAnyText(peer["state"]),
				Established:      established,
				PrefixesReceived: statusAnyText(peer["prefixesReceived"]),
				Messages:         messages,
				LastEstablished:  statusAnyText(peer["lastEstablishedAt"]),
				LastError:        statusAnyText(peer["lastErrorReason"]),
			})
		}
	}
	return out
}

type linuxRouteJSON struct {
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway"`
	Dev      string `json:"dev"`
	Protocol string `json:"protocol"`
	Proto    string `json:"proto"`
	Table    any    `json:"table"`
	Metric   any    `json:"metric"`
	Scope    string `json:"scope"`
	Type     string `json:"type"`
	Prefsrc  string `json:"prefsrc"`
}

func liveKernelRouteEntries() ([]RouteEntry, []string) {
	var entries []RouteEntry
	var errors []string
	for _, family := range []struct {
		Name string
		Flag string
	}{
		{Name: "ipv4", Flag: "-4"},
		{Name: "ipv6", Flag: "-6"},
	} {
		out, err := commandOutputTimeout(2*time.Second, "ip", "-j", family.Flag, "route", "show", "table", "all")
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		routes, err := parseLinuxRoutesJSON(out, family.Name)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		entries = append(entries, routes...)
	}
	return entries, errors
}

func parseLinuxRoutesJSON(data []byte, family string) ([]RouteEntry, error) {
	var raw []linuxRouteJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse ip route %s json: %w", family, err)
	}
	out := make([]RouteEntry, 0, len(raw))
	for _, item := range raw {
		destination := firstNonEmpty(item.Dst, "default")
		protocol := firstNonEmpty(item.Protocol, item.Proto)
		out = append(out, RouteEntry{
			Source:      "kernel",
			Family:      family,
			Destination: destination,
			Gateway:     item.Gateway,
			Device:      item.Dev,
			Protocol:    protocol,
			Table:       routeAnyText(item.Table),
			Metric:      routeAnyText(item.Metric),
			Scope:       item.Scope,
			Type:        item.Type,
			Phase:       "installed",
		})
	}
	return out, nil
}

func sortRouteEntries(entries []RouteEntry) {
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.Family != right.Family {
			return left.Family < right.Family
		}
		if left.Destination != right.Destination {
			return left.Destination < right.Destination
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Resource != right.Resource {
			return left.Resource < right.Resource
		}
		return left.Device < right.Device
	})
}

func routeAnyText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func routeMetricText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return ""
	}
	return value
}

func routeFamily(destination string) string {
	destination = strings.TrimSpace(destination)
	if destination == "" || destination == "default" {
		return ""
	}
	prefix, err := netip.ParsePrefix(destination)
	if err == nil {
		if prefix.Addr().Is6() {
			return "ipv6"
		}
		return "ipv4"
	}
	addr, err := netip.ParseAddr(destination)
	if err == nil && addr.Is6() {
		return "ipv6"
	}
	if err == nil {
		return "ipv4"
	}
	if strings.Contains(destination, ":") {
		return "ipv6"
	}
	return "ipv4"
}

func (h Handler) controllers(w http.ResponseWriter) {
	controllers := controlapi.NewControllers(h.controllerStatuses())
	writeJSON(w, controllers)
}

func (h Handler) events(w http.ResponseWriter, r *http.Request) {
	events, err := h.eventListQuery(routerstate.EventQuery{
		Limit:    intQuery(r, "limit", 100),
		SinceID:  int64(intQuery(r, "sinceID", 0)),
		Topic:    strings.TrimSpace(r.URL.Query().Get("topic")),
		Kind:     strings.TrimSpace(r.URL.Query().Get("kind")),
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		Resource: strings.TrimSpace(r.URL.Query().Get("resource")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, filterStoredEvents(events, storedEventFilter{
		ResourceKind: strings.TrimSpace(r.URL.Query().Get("resourceKind")),
		ResourceName: strings.TrimSpace(r.URL.Query().Get("resourceName")),
		Severity:     strings.TrimSpace(r.URL.Query().Get("severity")),
		Query:        strings.TrimSpace(r.URL.Query().Get("q")),
	}))
}

func (h Handler) eventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx := r.Context()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	var events <-chan bus.Event
	var cancel func()
	if h.opts.Bus != nil {
		events, cancel = h.opts.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 64)
		defer cancel()
	}

	_ = writeSSE(w, "connected", map[string]string{"status": "connected", "generatedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	flusher.Flush()

	if h.opts.Bus == nil {
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				_, _ = fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, "routerd-event", event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, eventName string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func (h Handler) connections(w http.ResponseWriter, r *http.Request) {
	if h.opts.Connections == nil {
		writeError(w, http.StatusNotImplemented, "connections observer is unavailable")
		return
	}
	table, err := h.opts.Connections(intQuery(r, "limit", h.opts.ConnectionsLimit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.enrichConnectionsWithDPI(table, time.Now().UTC(), time.Hour); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.enrichConnectionsWithLocalRedirect(table)
	if err := h.enrichConnectionsWithRemoteIdentity(r.Context(), table); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, table)
}

func (h Handler) dnsQueries(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-time.Hour)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			since = time.Now().Add(-duration)
		}
	}
	rows, err := h.queryLogList(logstore.DNSQueryFilter{
		Since:  since,
		Client: r.URL.Query().Get("client"),
		QName:  r.URL.Query().Get("qname"),
		Limit:  intQuery(r, "limit", 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (h Handler) trafficFlows(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-time.Hour)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			since = time.Now().Add(-duration)
		}
	}
	rows, err := h.trafficFlowList(logstore.TrafficFlowFilter{
		Since:  since,
		Client: r.URL.Query().Get("client"),
		Peer:   r.URL.Query().Get("peer"),
		Limit:  intQuery(r, "limit", 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queries, err := h.queryLogList(logstore.DNSQueryFilter{Since: since, Limit: 1000})
	if err == nil {
		rows = enrichTrafficFlowsWithDNS(rows, queries)
	}
	if enriched, err := h.enrichTrafficFlowsWithDPI(rows, time.Now().UTC(), time.Hour); err == nil {
		rows = enriched
	}
	writeJSON(w, rows)
}

func (h Handler) firewallLogs(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			since = time.Now().Add(-duration)
		}
	}
	rows, err := h.firewallLogList(logstore.FirewallLogFilter{
		Since:  since,
		Action: r.URL.Query().Get("action"),
		Src:    r.URL.Query().Get("src"),
		Limit:  intQuery(r, "limit", 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.enrichFirewallLogsWithRemoteIdentity(r.Context(), rows); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.enrichFirewallLogsWithAddressSets(rows)
	writeJSON(w, rows)
}

func (h Handler) firewallDenyTimeline(w http.ResponseWriter, r *http.Request) {
	window := 24 * time.Hour
	if raw := strings.TrimSpace(r.URL.Query().Get("range")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			window = duration
		}
	}
	if window < time.Minute {
		window = time.Minute
	}
	if window > 7*24*time.Hour {
		window = 7 * 24 * time.Hour
	}
	bucket := 5 * time.Minute
	if raw := strings.TrimSpace(r.URL.Query().Get("bucket")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			bucket = duration
		}
	}
	if bucket < time.Minute {
		bucket = time.Minute
	}
	if bucket > time.Hour {
		bucket = time.Hour
	}
	now := time.Now().UTC()
	rows, err := h.firewallDenyTimelineList(now.Add(-window), now, bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []logstore.FirewallDenyTimelineBucket{}
	}
	writeJSON(w, rows)
}

func (h Handler) clients(w http.ResponseWriter) {
	now := time.Now().UTC()
	clientSince := now.Add(-clientObservationWindow)
	leases, err := h.dhcpLeaseList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stickyLeases, err := h.dhcpStickyLeaseList(logstore.DHCPStickyFilter{HeldOnly: true, Now: now, Limit: 10000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	leases = annotateDHCPLeasesWithSticky(leases, stickyLeases, now)
	neighbors, err := neighborList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	flows, err := h.trafficFlowList(logstore.TrafficFlowFilter{Since: clientSince, Limit: 200})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queries, err := h.queryLogList(logstore.DNSQueryFilter{Since: clientSince, Limit: 1000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	flows = enrichTrafficFlowsWithDNS(flows, queries)
	if enriched, err := h.enrichTrafficFlowsWithDPI(flows, now, clientObservationWindow); err == nil {
		flows = enriched
	}
	firewallLogs, err := h.firewallLogList(logstore.FirewallLogFilter{Since: clientSince, Action: "drop", Limit: 1000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dhcpFingerprints, err := h.dhcpFingerprintList(logstore.DHCPFingerprintFilter{Since: clientSince, Limit: 1000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, h.annotateClientsWithPolicy(correlateClients(leases, neighbors, flows, queries, firewallLogs, dhcpFingerprints)))
}

func (h Handler) vpn(w http.ResponseWriter) {
	status, err := h.vpnStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, status)
}

func (h Handler) vpnStatus() (VPNStatus, error) {
	if h.opts.VPNStatus != nil {
		return h.opts.VPNStatus()
	}
	return hostVPNStatus()
}

func hostVPNStatus() (VPNStatus, error) {
	var status VPNStatus
	if out, err := commandOutputTimeout(2*time.Second, "wg", "show", "all", "dump"); err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else {
		interfaces, err := parseWireGuardAllDump(out)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
		} else {
			status.WireGuard = interfaces
		}
	}
	if out, err := commandOutputTimeout(2*time.Second, "tailscale", "status", "--json"); err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else {
		tailscale, err := parseTailscaleStatusJSON(out)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
		} else {
			status.Tailscale = tailscale
		}
	}
	return status, nil
}

func commandOutputTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	commandName := hostCommandPath(name)
	out, err := exec.CommandContext(ctx, commandName, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s %s timed out", name, strings.Join(args, " "))
	}
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func hostCommandPath(name string) string {
	return hostcmd.Resolve(name)
}

func parseWireGuardAllDump(data []byte) ([]WireGuardInterfaceStatus, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, nil
	}
	interfaces := map[string]*WireGuardInterfaceStatus{}
	ensure := func(name string) *WireGuardInterfaceStatus {
		item := interfaces[name]
		if item == nil {
			item = &WireGuardInterfaceStatus{Name: name}
			interfaces[name] = item
		}
		return item
	}
	for lineNo, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		switch {
		case len(fields) == 5:
			item := ensure(fields[0])
			item.PublicKey = wireGuardValue(fields[2])
			item.ListenPort = parseWireGuardInt(fields[3])
			item.FwMark = wireGuardValue(fields[4])
		case len(fields) >= 9:
			item := ensure(fields[0])
			peer := WireGuardPeerStatus{
				PublicKey:              wireGuardValue(fields[1]),
				Endpoint:               wireGuardValue(fields[3]),
				AllowedIPs:             splitWireGuardList(fields[4]),
				LatestHandshake:        parseWireGuardHandshake(fields[5]),
				TransferRxBytes:        parseWireGuardInt64(fields[6]),
				TransferTxBytes:        parseWireGuardInt64(fields[7]),
				PersistentKeepaliveSec: parseWireGuardInt(fields[8]),
			}
			item.Peers = append(item.Peers, peer)
		default:
			return nil, fmt.Errorf("wg dump line %d has %d fields", lineNo+1, len(fields))
		}
	}
	out := make([]WireGuardInterfaceStatus, 0, len(interfaces))
	for _, item := range interfaces {
		sort.Slice(item.Peers, func(i, j int) bool {
			return item.Peers[i].PublicKey < item.Peers[j].PublicKey
		})
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func wireGuardValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(none)" || value == "off" {
		return ""
	}
	return value
}

func splitWireGuardList(value string) []string {
	value = wireGuardValue(value)
	if value == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseWireGuardInt(value string) int {
	value = wireGuardValue(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func parseWireGuardInt64(value string) int64 {
	value = wireGuardValue(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func parseWireGuardHandshake(value string) time.Time {
	seconds := parseWireGuardInt64(value)
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

func parseTailscaleStatusJSON(data []byte) (*TailscaleStatus, error) {
	status, err := tailscale.ParseStatusJSON(data)
	if err != nil {
		return nil, err
	}
	if status.BackendState == "" && status.DNSName == "" && len(status.Peers) == 0 {
		return nil, nil
	}
	out := &TailscaleStatus{
		BackendState:    status.BackendState,
		TailnetName:     status.TailnetName,
		MagicDNSSuffix:  status.MagicDNSSuffix,
		MagicDNSEnabled: status.MagicDNSEnabled,
		CertDomains:     status.CertDomains,
		HostName:        status.HostName,
		DNSName:         status.DNSName,
		TailscaleIPs:    status.TailscaleIPs,
		AllowedIPs:      status.AllowedIPs,
		Online:          status.Online,
		Active:          status.Active,
		ExitNode:        status.ExitNode,
		ExitNodeOption:  status.ExitNodeOption,
	}
	for _, peer := range status.Peers {
		out.Peers = append(out.Peers, TailscalePeerStatus{
			ID:             peer.ID,
			HostName:       peer.HostName,
			DNSName:        peer.DNSName,
			TailscaleIPs:   peer.TailscaleIPs,
			AllowedIPs:     peer.AllowedIPs,
			Online:         peer.Online,
			Active:         peer.Active,
			ExitNode:       peer.ExitNode,
			ExitNodeOption: peer.ExitNodeOption,
			Relay:          peer.Relay,
			LastSeen:       peer.LastSeen,
			RxBytes:        peer.RxBytes,
			TxBytes:        peer.TxBytes,
		})
	}
	return out, nil
}

func (h Handler) config(w http.ResponseWriter) {
	path := strings.TrimSpace(h.opts.ConfigPath)
	if path == "" {
		writeError(w, http.StatusNotFound, "config path is unavailable")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, ConfigSnapshot{Path: path, Text: string(data)})
}

func (h Handler) generations(w http.ResponseWriter, r *http.Request) {
	reader, ok := h.opts.Store.(routerstate.GenerationHistoryReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "generation history is unavailable")
		return
	}
	rows, err := reader.ListGenerations(intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (h Handler) generationDetail(w http.ResponseWriter, r *http.Request, suffix string) {
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 2 && parts[1] == "config" {
		h.generationConfig(w, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "diff" {
		h.generationDiff(w, r, parts[0], parts[2])
		return
	}
	http.NotFound(w, r)
}

func (h Handler) generationConfig(w http.ResponseWriter, idText string) {
	reader, ok := h.opts.Store.(routerstate.GenerationHistoryReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "generation history is unavailable")
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid generation id")
		return
	}
	configYAML, found, err := reader.GenerationConfig(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusConflict, "this generation has no stored YAML; diff is available for newly applied generations")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(configYAML))
}

func (h Handler) generationDiff(w http.ResponseWriter, r *http.Request, fromText, toText string) {
	reader, ok := h.opts.Store.(routerstate.GenerationHistoryReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "generation history is unavailable")
		return
	}
	from, err := strconv.ParseInt(fromText, 10, 64)
	if err != nil || from <= 0 {
		writeError(w, http.StatusBadRequest, "invalid from generation id")
		return
	}
	to, err := strconv.ParseInt(toText, 10, 64)
	if err != nil || to <= 0 {
		writeError(w, http.StatusBadRequest, "invalid to generation id")
		return
	}
	fromYAML, found, err := reader.GenerationConfig(from)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusConflict, fmt.Sprintf("generation %d has no stored YAML", from))
		return
	}
	toYAML, found, err := reader.GenerationConfig(to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusConflict, fmt.Sprintf("generation %d has no stored YAML", to))
		return
	}
	diff := unifiedDiff(fmt.Sprintf("generation-%d.yaml", from), fmt.Sprintf("generation-%d.yaml", to), fromYAML, toYAML)
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, GenerationDiff{From: from, To: to, Diff: diff})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(diff))
}

func (h Handler) resourceStatuses() ([]routerstate.ObjectStatus, error) {
	if lister, ok := h.opts.Store.(routerstate.ObjectStatusLister); ok {
		resources, err := lister.ListObjectStatuses()
		if err != nil {
			return nil, err
		}
		resources = h.filterStaleObjectStatuses(resources)
		return annotateResourceOwnership(resources, h.controllerStatuses()), nil
	}
	return nil, nil
}

func (h Handler) controllerStatuses() []controlapi.ControllerStatus {
	if h.opts.ControllerStatuses != nil {
		return h.opts.ControllerStatuses()
	}
	return h.opts.ControllerModes
}

func operationalHTMLPage(title, kind string, resources []routerstate.ObjectStatus) string {
	displayTitle := map[string]string{"bgp": "BGP", "vrrp": "VRRP", "ingress": "IngressService"}[kind]
	var body strings.Builder
	switch kind {
	case "bgp":
		writeBGPHTML(&body, resources)
	case "vrrp":
		writeVRRPHTML(&body, resources)
	case "ingress":
		writeIngressHTML(&body, resources)
	default:
		body.WriteString(`<p>Unknown view.</p>`)
	}
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title+" "+displayTitle) + `</title>
<style>
:root{color-scheme:light dark;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
body{margin:0;background:#f7f8fa;color:#171b22}
main{max-width:1180px;margin:0 auto;padding:24px}
nav{display:flex;gap:12px;margin:0 0 20px}
a{color:#0f5f9f;text-decoration:none}
h1{font-size:24px;margin:0 0 18px}
section{margin:0 0 28px}
table{width:100%;border-collapse:collapse;background:#fff;border:1px solid #dde2ea}
th,td{padding:9px 10px;border-bottom:1px solid #e7ebf0;text-align:left;font-size:14px;vertical-align:top}
th{background:#eef2f6;font-weight:650}
code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.muted{color:#5d6673}
.toolbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 16px}
.badge{display:inline-flex;align-items:center;gap:6px;border:1px solid #bfd3e6;border-radius:999px;padding:4px 9px;font-size:13px;background:#fff}
.controls{display:flex;gap:8px;flex-wrap:wrap;margin:10px 0}
.controls input,.controls select{font:inherit;font-size:14px;padding:6px 8px;border:1px solid #cad3df;border-radius:6px;background:#fff;color:inherit}
.metric-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px;margin:0 0 18px}
.metric-card{border:1px solid #dde2ea;background:#fff;padding:10px;border-radius:6px}
.metric-value{display:block;font-size:22px;font-weight:700;margin-top:4px}
.chart{width:100%;height:128px;display:block;border:1px solid #dde2ea;background:#fff}
.event-message{max-width:42rem;overflow-wrap:anywhere}
@media (prefers-color-scheme:dark){body{background:#11151b;color:#eef2f6}table{background:#171c23;border-color:#303744}th,td{border-color:#2a313c}th{background:#202733}a{color:#8bc7ff}.muted{color:#a7b0bd}}
@media (prefers-color-scheme:dark){.badge,.metric-card,.chart{background:#171c23;border-color:#303744}.controls input,.controls select{background:#171c23;border-color:#303744}}
</style>
</head>
<body>
<main>
<nav><a href="./">Summary</a><a href="routes">Routes</a><a href="bgp">BGP</a><a href="vrrp">VRRP</a><a href="ingress">Ingress</a></nav>
<h1>` + html.EscapeString(displayTitle) + `</h1>
<div class="toolbar"><span id="live-state" class="badge">Connecting</span><span id="last-updated" class="muted"></span></div>
<section>
<div id="metrics" class="metric-grid"></div>
<div class="controls"><label>Range <select id="metric-range"><option value="5">5 min</option><option value="15" selected>15 min</option><option value="60">60 min</option></select></label></div>
<svg id="metric-chart" class="chart" viewBox="0 0 300 128" role="img" aria-label="Operational metrics"></svg>
</section>
<div id="operational-content">` + body.String() + `</div>
<section>
<h2>Event log</h2>
<div class="controls">
<label>Kind <input id="event-kind" placeholder="resource kind"></label>
<label>Resource <input id="event-resource" placeholder="resource name"></label>
<label>Search <input id="event-search" placeholder="message, reason, attribute"></label>
</div>
<table><thead><tr><th>Time</th><th>Kind</th><th>Resource</th><th>Severity</th><th>Message</th></tr></thead><tbody id="event-log"></tbody></table>
</section>
<script>` + operationalPageScript(kind) + `</script>
</main>
</body>
</html>`
}

func routesHTMLPage(title string, status RoutesStatus) string {
	var body strings.Builder
	body.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title+" Routes") + `</title>
<style>
:root{color-scheme:light dark;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
body{margin:0;background:#f7f8fa;color:#171b22}
main{max-width:1240px;margin:0 auto;padding:24px}
nav{display:flex;gap:12px;margin:0 0 20px;flex-wrap:wrap}
a{color:#0f5f9f;text-decoration:none}
h1{font-size:24px;margin:0 0 18px}
section{margin:0 0 28px}
table{width:100%;border-collapse:collapse;background:#fff;border:1px solid #dde2ea}
th,td{padding:9px 10px;border-bottom:1px solid #e7ebf0;text-align:left;font-size:14px;vertical-align:top}
th{background:#eef2f6;font-weight:650}
.muted{color:#5d6673}
.toolbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 16px}
.badge{display:inline-flex;align-items:center;gap:6px;border:1px solid #bfd3e6;border-radius:999px;padding:4px 9px;font-size:13px;background:#fff}
.controls{display:flex;gap:8px;flex-wrap:wrap;margin:10px 0}
.controls input,.controls select{font:inherit;font-size:14px;padding:6px 8px;border:1px solid #cad3df;border-radius:6px;background:#fff;color:inherit}
@media (prefers-color-scheme:dark){body{background:#11151b;color:#eef2f6}table{background:#171c23;border-color:#303744}th,td{border-color:#2a313c}th{background:#202733}a{color:#8bc7ff}.muted{color:#a7b0bd}.badge{background:#171c23;border-color:#303744}.controls input,.controls select{background:#171c23;border-color:#303744}}
</style>
</head>
<body>
<main>
<nav><a href="./">Summary</a><a href="routes">Routes</a><a href="bgp">BGP</a><a href="vrrp">VRRP</a><a href="ingress">Ingress</a></nav>
<h1>Routes</h1>
<div class="toolbar"><span id="live-state" class="badge">Polling</span><span id="last-updated" class="muted">Updated ` + html.EscapeString(status.GeneratedAt.Format(time.RFC3339)) + `</span></div>
<section><div class="controls"><label>Source <select id="route-source"><option value="">All</option><option>kernel</option><option>bgp</option><option>static</option><option>dhcpv4</option><option>policy</option></select></label><label>Search <input id="route-search" placeholder="prefix, gateway, device, peer"></label></div><table><thead><tr><th>Source</th><th>Resource</th><th>Family</th><th>Destination</th><th>Gateway</th><th>Device</th><th>Protocol</th><th>Table</th><th>Metric</th><th>Peer</th><th>Phase</th></tr></thead><tbody id="routes-body">`)
	writeRouteRowsHTML(&body, status.Routes)
	body.WriteString(`</tbody></table></section><section><h2>BGP Peers</h2><table><thead><tr><th>Router</th><th>Peer</th><th>ASN</th><th>State</th><th>Prefixes</th><th>Messages</th><th>Last Established</th><th>Last Error</th></tr></thead><tbody id="bgp-peers-body">`)
	writeRoutePeerRowsHTML(&body, status.BGPPeers)
	body.WriteString(`</tbody></table></section>`)
	if len(status.Errors) > 0 {
		body.WriteString(`<section><h2>Collection errors</h2><table><tbody>`)
		for _, errText := range status.Errors {
			writeHTMLRow(&body, []string{errText})
		}
		body.WriteString(`</tbody></table></section>`)
	}
	body.WriteString(`<script>
(function(){
const routesBody=document.getElementById("routes-body"),peersBody=document.getElementById("bgp-peers-body"),state=document.getElementById("live-state"),updated=document.getElementById("last-updated"),source=document.getElementById("route-source"),search=document.getElementById("route-search");
let routes=[],peers=[];
function esc(v){return String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));}
function row(cells){return "<tr>"+cells.map(v=>"<td>"+esc(v||"-")+"</td>").join("")+"</tr>";}
function routeText(r){return [r.source,r.resource,r.family,r.destination,r.gateway,r.device,r.protocol,r.table,r.metric,r.peer,r.phase].join(" ").toLowerCase();}
function render(){const s=(search.value||"").toLowerCase(),src=source.value;routesBody.innerHTML=routes.filter(r=>(!src||r.source===src)&&(!s||routeText(r).includes(s))).map(r=>row([r.source,r.resource,r.family,r.destination,r.gateway,r.device,r.protocol,r.table,r.metric,r.peer,r.phase])).join("")||'<tr><td colspan="11" class="muted">No matching routes</td></tr>';peersBody.innerHTML=peers.map(p=>row([p.router,p.peer,p.asn,p.state,p.prefixesReceived,p.messages,p.lastEstablishedAt,p.lastErrorReason])).join("")||'<tr><td colspan="8" class="muted">No BGP peers observed</td></tr>';}
async function refresh(){try{const r=await fetch("./api/v1/routes",{cache:"no-store"});if(!r.ok)throw new Error(String(r.status));const data=await r.json();routes=data.routes||[];peers=data.bgpPeers||[];updated.textContent=data.generatedAt?"Updated "+new Date(data.generatedAt).toLocaleString():"";state.textContent="Live";render();}catch(e){state.textContent="Polling failed";}}
[source,search].forEach(el=>el.addEventListener("input",render));setInterval(refresh,30000);refresh();
})();
</script></main></body></html>`)
	return body.String()
}

func writeRouteRowsHTML(body *strings.Builder, routes []RouteEntry) {
	for _, route := range routes {
		writeHTMLRow(body, []string{route.Source, route.Resource, route.Family, route.Destination, route.Gateway, route.Device, route.Protocol, route.Table, route.Metric, route.Peer, route.Phase})
	}
}

func writeRoutePeerRowsHTML(body *strings.Builder, peers []RouteBGPPeer) {
	for _, peer := range peers {
		writeHTMLRow(body, []string{peer.Router, peer.Peer, peer.ASN, peer.State, peer.PrefixesReceived, peer.Messages, peer.LastEstablished, peer.LastError})
	}
}

func operationalPageScript(kind string) string {
	return `(function(){
const view=` + strconv.Quote(kind) + `;
const apiBase="./api/v1/";
const streamURL="./api/events/stream";
const resourceKinds={bgp:["BGPRouter","BGPPeer"],vrrp:["VirtualIPv4Address"],ingress:["IngressService"]}[view]||[];
const state=document.getElementById("live-state");
const updated=document.getElementById("last-updated");
const content=document.getElementById("operational-content");
const metrics=document.getElementById("metrics");
const chart=document.getElementById("metric-chart");
const range=document.getElementById("metric-range");
const eventLog=document.getElementById("event-log");
const eventKind=document.getElementById("event-kind");
const eventResource=document.getElementById("event-resource");
const eventSearch=document.getElementById("event-search");
let events=[];
let refreshTimer=0;
function esc(value){return String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));}
function status(value,key){return value&&value.status?value.status[key]:undefined;}
function list(value){return Array.isArray(value)?value:[];}
async function json(path){const r=await fetch(path,{cache:"no-store"});if(!r.ok)throw new Error(path+": "+r.status);return r.json();}
function setLive(text){if(state)state.textContent=text;}
function schedule(delay){clearTimeout(refreshTimer);refreshTimer=setTimeout(refreshAll,delay);}
async function refreshAll(){try{const [status,log]=await Promise.all([json(apiBase+view),json(apiBase+"events?limit=200")]);renderStatus(status);events=log||[];renderEvents();setLive("Live updates");}catch(e){setLive("Polling fallback");}}
function renderStatus(payload){const resources=payload.resources||[];if(updated)updated.textContent=payload.generatedAt?"Updated "+new Date(payload.generatedAt).toLocaleString():"";content.innerHTML=renderTables(resources);appendSample(resources);renderMetrics();}
function renderTables(resources){if(view==="bgp")return renderBGP(resources);if(view==="vrrp")return renderVRRP(resources);if(view==="ingress")return renderIngress(resources);return "<p>Unknown view.</p>";}
function rows(cells){return "<tr>"+cells.map(v=>"<td>"+esc(v||"-")+"</td>").join("")+"</tr>";}
function renderBGP(resources){let routers=resources.filter(r=>r.kind==="BGPRouter"),out="<section><table><thead><tr><th>Router</th><th>Phase</th><th>Peers</th><th>Prefixes</th><th>Observed</th></tr></thead><tbody>";for(const r of routers){const peers=list(status(r,"peers"));out+=rows([r.name,status(r,"phase"),Number(status(r,"establishedPeers")||0)+"/"+peers.length,status(r,"acceptedPrefixes"),status(r,"observedAt")]);}out+="</tbody></table></section><section><table><thead><tr><th>Peer</th><th>ASN</th><th>State</th><th>Messages</th><th>Prefixes</th><th>Last Error</th></tr></thead><tbody>";for(const r of routers){for(const p of list(status(r,"peers"))){out+=rows([p.address,p.asn,p.state,Number(p.messagesReceived||0)+"/"+Number(p.messagesSent||0),p.prefixesReceived,p.lastErrorReason]);}}return out+"</tbody></table></section>";}
function renderVRRP(resources){let out="<section><table><thead><tr><th>VIP</th><th>Hostname</th><th>Role</th><th>Priority</th><th>Interface</th><th>VRID</th><th>Last Transition</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="VirtualIPv4Address")){out+=rows([status(r,"address"),status(r,"hostname"),status(r,"role")||"unknown",Number(status(r,"priority")||0)+"/"+Number(status(r,"basePriority")||0),status(r,"interface"),status(r,"virtualRouterID"),status(r,"lastRoleTransitionAt")]);}out+="</tbody></table></section><section><table><thead><tr><th>VIP</th><th>Track</th><th>State</th><th>Penalty</th><th>Unhealthy</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="VirtualIPv4Address")){for(const t of list(status(r,"track"))){out+=rows([r.name,t.resource,t.state,t.penalty,t.unhealthyConsecutive]);}}return out+"</tbody></table></section>";}
function backendAddress(b){return b.port?(b.resolvedAddress||b.address||"")+":"+b.port:(b.resolvedAddress||b.address||"");}
function renderIngress(resources){let out="<section><table><thead><tr><th>Service</th><th>Hostname</th><th>Phase</th><th>Active Backend</th><th>Health</th><th>Selection</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="IngressService")){const a=status(r,"activeBackend")||{};out+=rows([r.name,status(r,"hostname"),status(r,"phase"),(a.name||"-")+" / "+backendAddress(a),Number(status(r,"healthyBackends")||0)+"/"+Number(status(r,"totalBackends")||0),status(r,"selection")]);}out+="</tbody></table></section><section><table><thead><tr><th>Service</th><th>Backend</th><th>Address</th><th>State</th><th>Counts</th><th>Last Healthy</th><th>Last Unhealthy</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="IngressService")){for(const b of list(status(r,"backends"))){out+=rows([r.name,b.name,backendAddress(b),b.healthy?"Healthy":"Unhealthy",Number(b.healthyCount||0)+"/"+Number(b.unhealthyCount||0),b.lastHealthyAt,b.lastUnhealthyAt]);}}return out+"</tbody></table></section>";}
function sample(resources){const now=new Date().toISOString();if(view==="bgp"){let established=0,peers=0,prefixes=0;for(const r of resources.filter(r=>r.kind==="BGPRouter")){established+=Number(status(r,"establishedPeers")||0);peers+=list(status(r,"peers")).length;prefixes+=Number(status(r,"acceptedPrefixes")||0);}return {time:now,a:established,b:peers,c:prefixes,labels:["established peers","total peers","accepted prefixes"]};}if(view==="vrrp"){let master=0,backup=0,unknown=0;for(const r of resources.filter(r=>r.kind==="VirtualIPv4Address")){const role=String(status(r,"role")||"").toLowerCase();if(role==="master")master++;else if(role==="backup")backup++;else unknown++;}return {time:now,a:master,b:backup,c:unknown,labels:["master","backup","unknown"]};}let healthy=0,total=0,active=0;for(const r of resources.filter(r=>r.kind==="IngressService")){healthy+=Number(status(r,"healthyBackends")||0);total+=Number(status(r,"totalBackends")||0);if(status(r,"activeBackend"))active++;}return {time:now,a:healthy,b:total,c:active,labels:["healthy backends","total backends","active services"]};}
function metricKey(){return "routerd:operational-metrics:"+view;}
function loadSamples(){try{return JSON.parse(localStorage.getItem(metricKey())||"[]");}catch{return [];}}
function saveSamples(samples){try{localStorage.setItem(metricKey(),JSON.stringify(samples.slice(-720)));}catch{}}
function appendSample(resources){const samples=loadSamples();const next=sample(resources);const last=samples[samples.length-1];if(!last||Date.parse(next.time)-Date.parse(last.time)>=9000||last.a!==next.a||last.b!==next.b||last.c!==next.c){samples.push(next);saveSamples(samples);}}
function renderMetrics(){const samples=trimSamples(loadSamples());const last=samples[samples.length-1]||{a:0,b:0,c:0,labels:["a","b","c"]};metrics.innerHTML=last.labels.map((l,i)=>'<div class="metric-card"><span class="muted">'+esc(l)+'</span><span class="metric-value">'+esc(last[["a","b","c"][i]])+'</span></div>').join("");drawChart(samples,last.labels);}
function trimSamples(samples){const minutes=Number(range.value||15);const cutoff=Date.now()-minutes*60000;return samples.filter(s=>Date.parse(s.time)>=cutoff);}
function points(samples,key,max){return samples.map((s,i)=>{const x=samples.length<2?150:(i/(samples.length-1))*280+10;const y=112-(Number(s[key]||0)/Math.max(1,max))*96;return x.toFixed(1)+","+y.toFixed(1);}).join(" ");}
function drawChart(samples,labels){const max=Math.max(1,...samples.flatMap(s=>[s.a||0,s.b||0,s.c||0]));const series=[["a","#60cdff"],["b","#54b054"],["c","#f7b955"]];chart.innerHTML='<line x1="10" y1="112" x2="290" y2="112" stroke="#667" stroke-width="1"/>'+series.map(([k,c])=>'<polyline fill="none" stroke="'+c+'" stroke-width="2.4" points="'+points(samples,k,max)+'"/>').join("")+labels.map((l,i)=>'<text x="'+(12+i*92)+'" y="14" fill="'+series[i][1]+'" font-size="10">'+esc(l)+'</text>').join("");}
function eventMatches(e){const kind=(e.resourceKind||e.kind||"");const name=(e.resourceName||e.name||"");if(resourceKinds.length&&resourceKinds.indexOf(kind)<0)return false;if(eventKind.value&&kind.toLowerCase().indexOf(eventKind.value.toLowerCase())<0)return false;if(eventResource.value&&name.toLowerCase().indexOf(eventResource.value.toLowerCase())<0)return false;if(eventSearch.value){const text=JSON.stringify(e).toLowerCase();if(text.indexOf(eventSearch.value.toLowerCase())<0)return false;}return true;}
function renderEvents(){eventLog.innerHTML=events.filter(eventMatches).slice(0,200).map(e=>rows([e.createdAt?new Date(e.createdAt).toLocaleString():"",e.resourceKind||e.kind,e.resourceName||e.name,e.severity,e.message||e.reason||e.type])).join("")||'<tr><td colspan="5" class="muted">No matching events</td></tr>';}
[eventKind,eventResource,eventSearch,range].forEach(el=>el&&el.addEventListener("input",()=>{renderEvents();renderMetrics();}));
if(window.EventSource){const source=new EventSource(streamURL);source.addEventListener("connected",()=>setLive("Live updates"));source.addEventListener("routerd-event",event=>{try{const e=JSON.parse(event.data);if(e.resource&&resourceKinds.indexOf(e.resource.kind)>=0)schedule(150);events.unshift({createdAt:e.time,type:e.type,reason:e.reason,message:e.message,severity:e.severity,resourceKind:e.resource.kind,resourceName:e.resource.name,attributes:e.attributes});events=events.slice(0,200);renderEvents();}catch{schedule(500);}});source.onerror=()=>setLive("Polling fallback");}else{setLive("Polling fallback");}
setInterval(refreshAll,30000);
refreshAll();
})();`
}

func writeBGPHTML(body *strings.Builder, resources []routerstate.ObjectStatus) {
	body.WriteString(`<section><table><thead><tr><th>Router</th><th>Phase</th><th>Peers</th><th>Prefixes</th><th>Observed</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		peers := statusList(resource.Status["peers"])
		writeHTMLRow(body, []string{
			resource.Name,
			statusText(resource.Status, "phase"),
			fmt.Sprintf("%d/%d", statusIntValue(resource.Status["establishedPeers"]), len(peers)),
			statusText(resource.Status, "acceptedPrefixes"),
			statusText(resource.Status, "observedAt"),
		})
	}
	body.WriteString(`</tbody></table></section>`)
	body.WriteString(`<section><table><thead><tr><th>Peer</th><th>ASN</th><th>State</th><th>Messages</th><th>Prefixes</th><th>Last Error</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		for _, peer := range statusList(resource.Status["peers"]) {
			writeHTMLRow(body, []string{
				statusAnyText(peer["address"]),
				statusAnyText(peer["asn"]),
				statusAnyText(peer["state"]),
				fmt.Sprintf("%d/%d", statusIntValue(peer["messagesReceived"]), statusIntValue(peer["messagesSent"])),
				statusAnyText(peer["prefixesReceived"]),
				defaultConsoleText(statusAnyText(peer["lastErrorReason"]), "-"),
			})
		}
	}
	body.WriteString(`</tbody></table></section>`)
}

func writeVRRPHTML(body *strings.Builder, resources []routerstate.ObjectStatus) {
	body.WriteString(`<section><table><thead><tr><th>VIP</th><th>Hostname</th><th>Role</th><th>Priority</th><th>Interface</th><th>VRID</th><th>Last Transition</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "VirtualIPv4Address" {
			continue
		}
		writeHTMLRow(body, []string{
			statusText(resource.Status, "address"),
			defaultConsoleText(statusText(resource.Status, "hostname"), "-"),
			defaultConsoleText(statusText(resource.Status, "role"), "unknown"),
			fmt.Sprintf("%d/%d", statusIntValue(resource.Status["priority"]), statusIntValue(resource.Status["basePriority"])),
			statusText(resource.Status, "interface"),
			statusText(resource.Status, "virtualRouterID"),
			statusText(resource.Status, "lastRoleTransitionAt"),
		})
	}
	body.WriteString(`</tbody></table></section>`)
	body.WriteString(`<section><table><thead><tr><th>VIP</th><th>Track</th><th>State</th><th>Penalty</th><th>Unhealthy</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "VirtualIPv4Address" {
			continue
		}
		for _, track := range statusList(resource.Status["track"]) {
			writeHTMLRow(body, []string{
				resource.Name,
				statusAnyText(track["resource"]),
				statusAnyText(track["state"]),
				statusAnyText(track["penalty"]),
				statusAnyText(track["unhealthyConsecutive"]),
			})
		}
	}
	body.WriteString(`</tbody></table></section>`)
}

func writeIngressHTML(body *strings.Builder, resources []routerstate.ObjectStatus) {
	body.WriteString(`<section><table><thead><tr><th>Service</th><th>Hostname</th><th>Phase</th><th>Active Backend</th><th>Health</th><th>Selection</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "IngressService" {
			continue
		}
		active := statusObject(resource.Status["activeBackend"])
		writeHTMLRow(body, []string{
			resource.Name,
			defaultConsoleText(statusText(resource.Status, "hostname"), "-"),
			statusText(resource.Status, "phase"),
			activeBackendHTMLText(active),
			fmt.Sprintf("%d/%d", statusIntValue(resource.Status["healthyBackends"]), statusIntValue(resource.Status["totalBackends"])),
			statusText(resource.Status, "selection"),
		})
	}
	body.WriteString(`</tbody></table></section>`)
	body.WriteString(`<section><table><thead><tr><th>Service</th><th>Backend</th><th>Address</th><th>State</th><th>Counts</th><th>Last Healthy</th><th>Last Unhealthy</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "IngressService" {
			continue
		}
		for _, backend := range statusList(resource.Status["backends"]) {
			state := "Unhealthy"
			if healthy, ok := statusBoolValue(backend["healthy"]); ok && healthy {
				state = "Healthy"
			}
			writeHTMLRow(body, []string{
				resource.Name,
				statusAnyText(backend["name"]),
				backendHTMLAddress(backend),
				state,
				fmt.Sprintf("%d/%d", statusIntValue(backend["healthyCount"]), statusIntValue(backend["unhealthyCount"])),
				statusAnyText(backend["lastHealthyAt"]),
				statusAnyText(backend["lastUnhealthyAt"]),
			})
		}
	}
	body.WriteString(`</tbody></table></section>`)
}

func writeHTMLRow(body *strings.Builder, cells []string) {
	body.WriteString("<tr>")
	for _, cell := range cells {
		if strings.TrimSpace(cell) == "" {
			cell = "-"
		}
		body.WriteString("<td>")
		body.WriteString(html.EscapeString(cell))
		body.WriteString("</td>")
	}
	body.WriteString("</tr>")
}

func activeBackendHTMLText(active map[string]any) string {
	name := statusAnyText(active["name"])
	address := statusAnyText(active["address"])
	port := statusIntValue(active["port"])
	if name == "" && address == "" {
		return "-"
	}
	if port > 0 {
		return fmt.Sprintf("%s / %s:%d", defaultConsoleText(name, "-"), address, port)
	}
	return fmt.Sprintf("%s / %s", defaultConsoleText(name, "-"), address)
}

func backendHTMLAddress(backend map[string]any) string {
	address := statusAnyText(backend["address"])
	resolved := statusAnyText(backend["resolvedAddress"])
	port := statusIntValue(backend["port"])
	if resolved != "" && resolved != address {
		return fmt.Sprintf("%s -> %s:%d", address, resolved, port)
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", defaultConsoleText(resolved, address), port)
	}
	return defaultConsoleText(resolved, address)
}

func statusList(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, object)
			}
		}
		return out
	default:
		return nil
	}
}

func statusObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func statusAnyText(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func defaultConsoleText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (h Handler) filterStaleObjectStatuses(resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	if h.opts.Router == nil {
		return resources
	}
	declared := map[string]struct{}{}
	for _, resource := range h.opts.Router.Spec.Resources {
		declared[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name] = struct{}{}
	}
	out := resources[:0]
	for _, resource := range resources {
		if resource.Kind == "WireGuardPeer" {
			key := resource.APIVersion + "/" + resource.Kind + "/" + resource.Name
			if _, ok := declared[key]; !ok {
				continue
			}
		}
		out = append(out, resource)
	}
	return out
}

func annotateResourceOwnership(resources []routerstate.ObjectStatus, controllers []controlapi.ControllerStatus) []routerstate.ObjectStatus {
	ownerByKind := map[string]string{}
	for _, controller := range controllers {
		for _, kind := range controller.ResourceKinds {
			if _, exists := ownerByKind[kind]; !exists {
				ownerByKind[kind] = controller.Name
			}
		}
	}
	for i := range resources {
		status := resources[i].Status
		if status == nil {
			status = map[string]any{}
			resources[i].Status = status
		}
		if resources[i].Owner == "" {
			resources[i].Owner = statusText(status, "owner")
		}
		if resources[i].Owner == "" {
			resources[i].Owner = ownerByKind[resources[i].Kind]
		}
		if resources[i].Owner == "" {
			resources[i].Owner = defaultResourceOwnerController(resources[i].Kind)
		}
		if resources[i].Owner != "" {
			status["owner"] = resources[i].Owner
		}
		if resources[i].ManagedBy == "" {
			resources[i].ManagedBy = statusText(status, "managedBy")
		}
		if resources[i].ManagedBy == "" {
			if managed, ok := statusBoolValue(status["managed"]); ok && !managed {
				resources[i].ManagedBy = "external"
			} else {
				resources[i].ManagedBy = "routerd"
			}
		}
		status["managedBy"] = resources[i].ManagedBy
		if resources[i].Management == "" {
			resources[i].Management = statusText(status, "management")
		}
		if resources[i].Management == "" {
			if managed, ok := statusBoolValue(status["managed"]); ok && !managed {
				resources[i].Management = "adopted"
			} else if strings.EqualFold(resources[i].ManagedBy, "external") {
				resources[i].Management = "adopted"
			} else {
				resources[i].Management = "managed"
			}
		}
		status["management"] = resources[i].Management
	}
	return resources
}

func defaultResourceOwnerController(kind string) string {
	switch kind {
	case "IPv4StaticAddress", "IPv6DelegatedAddress", "IPv6RAAddress", "Interface", "Link":
		return "address"
	case "DHCPv4Lease":
		return "dhcpv4lease"
	case "DHCPv4Server", "DHCPv6Server", "DHCPv6Scope", "DHCPv6Information", "IPv6RouterAdvertisement":
		return "dhcpv6"
	case "DNSResolver", "DNSZone":
		return "dns-resolver"
	case "DSLiteTunnel":
		return "dslite"
	case "FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy":
		return "firewall"
	case "NAT44Rule", "IPv4SourceNAT":
		return "nat"
	case "NetworkAdoption":
		return "network-adoption"
	case "Package", "KernelModule":
		return "package"
	case "PPPoEInterface", "PPPoESession":
		return "pppoesession"
	case "IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "IPv4PolicyRoute", "IPv4PolicyRouteSet", "EgressRoutePolicy", "PathMTUPolicy":
		return "route"
	case "SystemdUnit", "TailscaleNode", "HealthCheck", "NTPClient", "NTPServer", "SysctlProfile", "Sysctl", "LogRetention", "Hostname", "ConntrackTuning":
		return "systemd-unit"
	case "ConntrackObserver", "TrafficFlowLog":
		return "conntrack"
	default:
		return ""
	}
}

func statusText(status map[string]any, key string) string {
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusBoolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	}
	return false, false
}

func (h Handler) eventList(limit int) ([]routerstate.StoredEvent, error) {
	return h.eventListQuery(routerstate.EventQuery{Limit: limit})
}

func (h Handler) eventListQuery(query routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	if lister, ok := h.opts.Store.(routerstate.EventLister); ok {
		return lister.ListEvents(query)
	}
	return nil, nil
}

type storedEventFilter struct {
	ResourceKind string
	ResourceName string
	Severity     string
	Query        string
}

func filterStoredEvents(events []routerstate.StoredEvent, filter storedEventFilter) []routerstate.StoredEvent {
	if filter.ResourceKind == "" && filter.ResourceName == "" && filter.Severity == "" && filter.Query == "" {
		return events
	}
	query := strings.ToLower(filter.Query)
	var out []routerstate.StoredEvent
	for _, event := range events {
		if filter.ResourceKind != "" && event.ResourceKind != filter.ResourceKind && event.Kind != filter.ResourceKind {
			continue
		}
		if filter.ResourceName != "" && event.ResourceName != filter.ResourceName && event.Name != filter.ResourceName {
			continue
		}
		if filter.Severity != "" && !strings.EqualFold(event.Severity, filter.Severity) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(storedEventSearchText(event)), query) {
			continue
		}
		out = append(out, event)
	}
	return out
}

func storedEventSearchText(event routerstate.StoredEvent) string {
	return strings.Join([]string{
		event.Topic,
		event.Type,
		event.Reason,
		event.Message,
		event.Kind,
		event.Name,
		event.ResourceKind,
		event.ResourceName,
		event.Severity,
		fmt.Sprint(event.Attributes),
	}, " ")
}

func (h Handler) queryLogList(filter logstore.DNSQueryFilter) ([]logstore.DNSQuery, error) {
	if strings.TrimSpace(h.opts.DNSQueryLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenDNSQueryLogReadOnly(h.opts.DNSQueryLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) trafficFlowList(filter logstore.TrafficFlowFilter) ([]logstore.TrafficFlow, error) {
	if strings.TrimSpace(h.opts.TrafficFlowLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenTrafficFlowLogReadOnly(h.opts.TrafficFlowLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) firewallLogList(filter logstore.FirewallLogFilter) ([]logstore.FirewallLogEntry, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) firewallDenyTimelineList(since time.Time, until time.Time, bucket time.Duration) ([]logstore.FirewallDenyTimelineBucket, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.DenyTimeline(context.Background(), since, until, bucket)
}

func (h Handler) conntrackTuningSummary(now time.Time, window time.Duration, autoApply bool) (conntracktuning.Summary, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return conntracktuning.Analyze(conntracktuning.Inputs{Now: now, Window: window, AutoApply: autoApply}), nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	defer store.Close()
	since := now.Add(-window)
	firewallLogs, err := store.List(context.Background(), logstore.FirewallLogFilter{Since: since, Limit: 1000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	dpiFlows, err := store.ListDPIFlows(context.Background(), logstore.DPIFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	expiredFlows, err := store.ListExpiredFlows(context.Background(), logstore.ExpiredFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	return conntracktuning.Analyze(conntracktuning.Inputs{
		DPIFlows:     dpiFlows,
		FirewallLogs: firewallLogs,
		ExpiredFlows: expiredFlows,
		Now:          now,
		Window:       window,
		AutoApply:    autoApply,
	}), nil
}

func (h Handler) dpiStatus(ctx context.Context) *DPIStatus {
	classifier := probeDPIService(ctx, "/run/routerd/dpi-classifier/default.sock", "/v1/status")
	agent := probeDPIService(ctx, "/run/routerd/ndpi-agent/default.sock", "/v1/status")
	if classifier == nil && agent == nil {
		return nil
	}
	return &DPIStatus{Classifier: classifier, Agent: agent}
}

type systemUsageSampler struct {
	mu      sync.Mutex
	prevCPU cpuTimes
}

type cpuTimes struct {
	total uint64
	idle  uint64
}

func (h Handler) readSystemUsage() SystemUsage {
	if h.systemUsage == nil {
		return SystemUsage{}
	}
	usage := h.systemUsage.sample()
	if disk, ok := diskUsage("/"); ok {
		usage.Disks = append(usage.Disks, disk)
	}
	return usage
}

func (s *systemUsageSampler) sample() SystemUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	usage := SystemUsage{}
	if load, ok := readLoad1(); ok {
		usage.Load1 = &load
	}
	if total, available, ok := readMemoryInfo(); ok && total > 0 {
		used := total - available
		usage.MemoryTotalBytes = total
		usage.MemoryUsedBytes = used
		percent := float64(used) / float64(total)
		usage.MemoryUsedPercent = &percent
	}
	if current, ok := readCPUTimes(); ok {
		if s.prevCPU.total > 0 && current.total > s.prevCPU.total {
			totalDelta := current.total - s.prevCPU.total
			idleDelta := current.idle - s.prevCPU.idle
			if totalDelta > 0 && idleDelta <= totalDelta {
				percent := float64(totalDelta-idleDelta) / float64(totalDelta)
				usage.CPUPercent = &percent
			}
		}
		s.prevCPU = current
	}
	return usage
}

func readCPUTimes() (cpuTimes, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		values := make([]uint64, 0, len(fields)-1)
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuTimes{}, false
			}
			values = append(values, value)
		}
		total := uint64(0)
		for _, value := range values {
			total += value
		}
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return cpuTimes{total: total, idle: idle}, true
	}
	return cpuTimes{}, false
}

func readLoad1() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func readMemoryInfo() (total uint64, available uint64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	total = values["MemTotal"]
	available = values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	return total, available, total > 0
}

func diskUsage(path string) (DiskUsage, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskUsage{}, false
	}
	if stat.Bsize <= 0 || stat.Blocks <= 0 || stat.Bavail < 0 {
		return DiskUsage{}, false
	}
	blockSize := uint64(stat.Bsize)
	blocks := uint64(stat.Blocks)
	available := uint64(stat.Bavail)
	maxUint := ^uint64(0)
	if blocks > maxUint/blockSize || available > maxUint/blockSize {
		return DiskUsage{}, false
	}
	total := blocks * blockSize
	free := available * blockSize
	if total == 0 || free > total {
		return DiskUsage{}, false
	}
	used := total - free
	percent := float64(used) / float64(total)
	return DiskUsage{Path: path, UsedBytes: used, TotalBytes: total, UsedPercent: &percent}, true
}

func probeDPIService(ctx context.Context, socket, path string) *DPIServiceStatus {
	if _, err := os.Stat(socket); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	client := &http.Client{
		Timeout: 150 * time.Millisecond,
		Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return &DPIServiceStatus{Socket: socket, Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return &DPIServiceStatus{Socket: socket, Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return &DPIServiceStatus{Socket: socket, Error: resp.Status}
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &DPIServiceStatus{Socket: socket, Error: err.Error()}
	}
	return dpiServiceStatusFromMap(socket, raw)
}

func dpiServiceStatusFromMap(socket string, raw map[string]any) *DPIServiceStatus {
	status := &DPIServiceStatus{
		Available:      true,
		Socket:         socket,
		Engine:         stringMapValue(raw, "engine"),
		ActiveEngine:   stringMapValue(raw, "activeEngine"),
		LibNDPILoaded:  boolMapValue(raw, "libndpiLoaded"),
		LibNDPIVersion: stringMapValue(raw, "libndpiVersion"),
		Reason:         stringMapValue(raw, "reason"),
	}
	if stats, ok := raw["stats"].(map[string]any); ok {
		status.Stats = stats
	}
	if agent, ok := raw["agent"].(map[string]any); ok {
		if status.ActiveEngine == "" && boolMapValue(agent, "available") {
			status.ActiveEngine = "ndpi-agent"
		}
		if !boolMapValue(agent, "available") && status.Reason == "" {
			status.Reason = stringMapValue(agent, "error")
		}
	}
	return status
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func boolMapValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func (h Handler) dhcpFingerprintList(filter logstore.DHCPFingerprintFilter) ([]logstore.DHCPFingerprint, error) {
	if strings.TrimSpace(h.opts.DHCPFingerprintLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenDHCPFingerprintLog(h.opts.DHCPFingerprintLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func (h Handler) dhcpStickyLeaseList(filter logstore.DHCPStickyFilter) ([]logstore.DHCPStickyLease, error) {
	if strings.TrimSpace(h.opts.DHCPStickyLogPath) == "" {
		return nil, nil
	}
	if _, err := os.Stat(h.opts.DHCPStickyLogPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	store, err := logstore.OpenDHCPStickyLogReadOnly(h.opts.DHCPStickyLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func annotateDHCPLeasesWithSticky(leases []DHCPLease, sticky []logstore.DHCPStickyLease, now time.Time) []DHCPLease {
	if len(sticky) == 0 {
		return leases
	}
	byIP := map[string]logstore.DHCPStickyLease{}
	byMAC := map[string]logstore.DHCPStickyLease{}
	for _, row := range sticky {
		if row.StickyUntil.IsZero() || !row.StickyUntil.After(now) {
			continue
		}
		if row.IP != "" {
			byIP[row.IP] = row
		}
		if row.MAC != "" {
			byMAC[strings.ToLower(row.MAC)] = row
		}
	}
	seen := map[string]bool{}
	for i := range leases {
		seen[leases[i].IP+"|"+strings.ToLower(leases[i].MAC)] = true
		row, ok := byIP[leases[i].IP]
		if !ok {
			row, ok = byMAC[strings.ToLower(leases[i].MAC)]
		}
		if !ok {
			continue
		}
		leases[i].StickyUntil = row.StickyUntil
		leases[i].StickyState = "held"
	}
	for _, row := range byIP {
		key := row.IP + "|" + strings.ToLower(row.MAC)
		if seen[key] {
			continue
		}
		leases = append(leases, DHCPLease{
			MAC:         row.MAC,
			IP:          row.IP,
			Hostname:    row.Hostname,
			Family:      row.Family,
			Source:      "sticky-history",
			StickyUntil: row.StickyUntil,
			StickyState: "held",
		})
	}
	return leases
}

func recordConsoleMetrics(ctx context.Context, resources []routerstate.ObjectStatus, controllers []controlapi.ControllerStatus, leases []DHCPLease, clients []ClientEntry, sticky []logstore.DHCPStickyLease, now time.Time) {
	meter := otel.Meter("routerd")
	dryRunGauge, _ := meter.Int64Gauge("routerd.controller.dry_run.count")
	controllerErrorGauge, _ := meter.Int64Gauge("routerd.controller.reconcile.errors")
	controllerLastDurationGauge, _ := meter.Float64Gauge("routerd.controller.reconcile.last_duration_ms")
	phaseGauge, _ := meter.Int64Gauge("routerd.resource.phase.count")
	leaseGauge, _ := meter.Int64Gauge("routerd.dhcp.lease.active")
	stickyGauge, _ := meter.Int64Gauge("routerd.dhcp.sticky.held")
	clientGauge, _ := meter.Int64Gauge("routerd.client.active.count")
	var dryRun int64
	for _, controller := range controllers {
		if strings.EqualFold(strings.TrimSpace(controller.Mode), "dry-run") {
			dryRun++
		}
		attrs := metric.WithAttributes(attribute.String("routerd.controller.name", controller.Name))
		controllerErrorGauge.Record(ctx, controller.ReconcileErrorCount, attrs)
		if controller.LastDurationMillis > 0 {
			controllerLastDurationGauge.Record(ctx, controller.LastDurationMillis, attrs)
		}
	}
	dryRunGauge.Record(ctx, dryRun)
	phaseCounts := map[string]int64{}
	for _, resource := range resources {
		phase := "Unknown"
		if resource.Status != nil {
			if value := strings.TrimSpace(fmt.Sprint(resource.Status["phase"])); value != "" && value != "<nil>" {
				phase = value
			}
		}
		phaseCounts[phase]++
	}
	for phase, count := range phaseCounts {
		phaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("routerd.resource.phase", phase)))
	}
	activeLeases := map[string]int64{}
	for _, lease := range leases {
		if lease.Source == "sticky-history" {
			continue
		}
		family := strings.ToLower(strings.TrimSpace(lease.Family))
		if family == "" {
			family = "ipv4"
			if strings.Contains(lease.IP, ":") {
				family = "ipv6"
			}
		}
		activeLeases[family]++
	}
	for family, count := range activeLeases {
		leaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
	}
	stickyHeld := map[string]int64{}
	for _, lease := range sticky {
		if lease.StickyUntil.IsZero() || !lease.StickyUntil.After(now) {
			continue
		}
		family := strings.ToLower(strings.TrimSpace(lease.Family))
		if family == "" {
			family = "ipv4"
			if strings.Contains(lease.IP, ":") {
				family = "ipv6"
			}
		}
		stickyHeld[family]++
	}
	for family, count := range stickyHeld {
		stickyGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
	}
	if clients != nil {
		clientGauge.Record(ctx, int64(len(clients)))
	}
}

func (h Handler) enrichTrafficFlowsWithDPI(flows []logstore.TrafficFlow, now time.Time, ttl time.Duration) ([]logstore.TrafficFlow, error) {
	if len(flows) == 0 {
		return flows, nil
	}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		for i := range flows {
			applyTrafficFlowPortFallback(&flows[i])
		}
		return flows, nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return flows, err
	}
	defer store.Close()
	for i := range flows {
		entry := logstore.FirewallLogEntry{
			Protocol:   flows[i].Protocol,
			SrcAddress: flows[i].ClientAddress,
			SrcPort:    flows[i].ClientPort,
			DstAddress: flows[i].PeerAddress,
			DstPort:    flows[i].PeerPort,
		}
		dpiFlow, ok, err := store.FindDPIFlowForFirewallEntry(context.Background(), entry, now, ttl)
		if err != nil {
			return flows, err
		}
		if !ok {
			applyTrafficFlowPortFallback(&flows[i])
			continue
		}
		if flows[i].AppName == "" {
			flows[i].AppName = dpiFlow.AppName
		}
		if flows[i].AppCategory == "" {
			flows[i].AppCategory = dpiFlow.AppCategory
		}
		if flows[i].AppConfidence == 0 {
			flows[i].AppConfidence = dpiFlow.AppConfidence
		}
		if flows[i].DetectedProtocol == "" {
			flows[i].DetectedProtocol = dpiFlow.DetectedProtocol
		}
		if flows[i].MasterProtocol == "" {
			flows[i].MasterProtocol = dpiFlow.MasterProtocol
		}
		if flows[i].ApplicationProtocol == "" {
			flows[i].ApplicationProtocol = dpiFlow.ApplicationProtocol
		}
		if flows[i].Category == "" {
			flows[i].Category = dpiFlow.Category
		}
		if len(flows[i].Risk) == 0 {
			flows[i].Risk = append([]string(nil), dpiFlow.Risk...)
		}
		if flows[i].Confidence == 0 {
			flows[i].Confidence = dpiFlow.Confidence
		}
		if len(flows[i].Metadata) == 0 && len(dpiFlow.Metadata) > 0 {
			flows[i].Metadata = map[string]string{}
			for key, value := range dpiFlow.Metadata {
				flows[i].Metadata[key] = value
			}
		}
		if flows[i].Engine == "" {
			flows[i].Engine = dpiFlow.Engine
		}
		if flows[i].Source == "" {
			flows[i].Source = dpiFlow.Source
		}
		if flows[i].TLSSNI == "" {
			flows[i].TLSSNI = dpiFlow.TLSSNI
		}
		if flows[i].HTTPHost == "" {
			flows[i].HTTPHost = dpiFlow.HTTPHost
		}
		if flows[i].DNSQuery == "" {
			flows[i].DNSQuery = dpiFlow.DNSQuery
		}
		if flows[i].ResolvedHostname == "" {
			flows[i].ResolvedHostname = firstNonEmpty(dpiFlow.TLSSNI, dpiFlow.HTTPHost, dpiFlow.DNSQuery)
		}
		applyTrafficFlowPortFallback(&flows[i])
	}
	return flows, nil
}

func (h Handler) enrichConnectionsWithDPI(table *observe.ConnectionTable, now time.Time, ttl time.Duration) error {
	if table == nil || len(table.Entries) == 0 {
		return nil
	}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		for i := range table.Entries {
			applyConnectionPortFallback(&table.Entries[i])
		}
		return nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return err
	}
	defer store.Close()
	for i := range table.Entries {
		entry := &table.Entries[i]
		flow, ok, err := store.FindDPIFlowForFirewallEntry(context.Background(), logstore.FirewallLogEntry{
			Protocol:   entry.Protocol,
			SrcAddress: entry.Original.Source,
			SrcPort:    atoiDefault(entry.Original.SourcePort, 0),
			DstAddress: entry.Original.Destination,
			DstPort:    atoiDefault(entry.Original.DestinationPort, 0),
		}, now, ttl)
		if err != nil {
			return err
		}
		if !ok {
			applyConnectionPortFallback(entry)
			continue
		}
		entry.AppName = flow.AppName
		entry.AppCategory = flow.AppCategory
		entry.AppConfidence = flow.AppConfidence
		entry.TLSSNI = flow.TLSSNI
		entry.HTTPHost = flow.HTTPHost
		entry.DNSQuery = flow.DNSQuery
		applyConnectionPortFallback(entry)
	}
	return nil
}

func (h Handler) enrichConnectionsWithRemoteIdentity(ctx context.Context, table *observe.ConnectionTable) error {
	if table == nil || len(table.Entries) == 0 {
		return nil
	}
	addresses := make([]string, 0, len(table.Entries))
	seen := map[string]bool{}
	for i := range table.Entries {
		entry := &table.Entries[i]
		annotateTupleServices(&entry.Original, entry.Protocol)
		annotateTupleServices(&entry.Reply, entry.Protocol)
		for _, address := range []string{entry.Original.Source, entry.Original.Destination, entry.Reply.Source, entry.Reply.Destination} {
			if !shouldReverseLookup(address) || seen[address] {
				continue
			}
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 || h.reverseDNS == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	labels := h.reverseDNS.lookupMany(ctx, addresses, h.opts.ReverseLookup)
	for i := range table.Entries {
		entry := &table.Entries[i]
		annotateTupleHostnames(&entry.Original, labels)
		annotateTupleHostnames(&entry.Reply, labels)
		applyConnectionPortFallback(entry)
	}
	return nil
}

func (h Handler) enrichFirewallLogsWithRemoteIdentity(ctx context.Context, logs []logstore.FirewallLogEntry) error {
	if len(logs) == 0 {
		return nil
	}
	addresses := make([]string, 0, len(logs)*2)
	seen := map[string]bool{}
	for i := range logs {
		entry := &logs[i]
		if entry.SrcService == "" {
			entry.SrcService = serviceNameForPort(entry.Protocol, entry.SrcPort)
		}
		if entry.DstService == "" {
			entry.DstService = serviceNameForPort(entry.Protocol, entry.DstPort)
		}
		for _, address := range []string{entry.SrcAddress, entry.DstAddress} {
			if !shouldReverseLookup(address) || seen[address] {
				continue
			}
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 || h.reverseDNS == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	labels := h.reverseDNS.lookupMany(ctx, addresses, h.opts.ReverseLookup)
	for i := range logs {
		if logs[i].SrcHostname == "" {
			logs[i].SrcHostname = labels[logs[i].SrcAddress]
		}
		if logs[i].DstHostname == "" {
			logs[i].DstHostname = labels[logs[i].DstAddress]
		}
	}
	return nil
}

type consoleAddressSet struct {
	Name      string
	Addresses map[netip.Addr]struct{}
}

type localRedirectDisplayRule struct {
	ResourceName      string
	RuleName          string
	DestinationSetRef string
	DestinationPort   int
	RedirectPort      int
	Protocols         map[string]struct{}
}

func (h Handler) enrichConnectionsWithLocalRedirect(table *observe.ConnectionTable) {
	if table == nil || len(table.Entries) == 0 || h.opts.Router == nil {
		return
	}
	sets := h.consoleAddressSets()
	rules := h.localRedirectDisplayRules()
	if len(rules) == 0 {
		return
	}
	for i := range table.Entries {
		if match := matchConnectionLocalRedirect(table.Entries[i], rules, sets); match != nil {
			table.Entries[i].LocalRedirect = match
		}
	}
}

func (h Handler) enrichFirewallLogsWithAddressSets(logs []logstore.FirewallLogEntry) {
	if len(logs) == 0 || h.opts.Router == nil {
		return
	}
	sets := h.consoleAddressSets()
	if len(sets) == 0 {
		return
	}
	ruleRefs := h.firewallRuleDestinationSetRefs()
	for i := range logs {
		log := &logs[i]
		seen := map[string]int{}
		seenSets := map[*consoleAddressSet]int{}
		for _, ref := range ruleRefs[strings.TrimSpace(log.RuleName)] {
			set, ok := sets[ref]
			if !ok {
				continue
			}
			current := set.contains(log.DstAddress)
			log.DestinationSets = append(log.DestinationSets, logstore.AddressSetMatch{
				ResourceName: set.Name,
				SetName:      ref,
				Source:       "firewall-rule",
				Current:      current,
			})
			seen[ref] = len(log.DestinationSets) - 1
			seenSets[set] = len(log.DestinationSets) - 1
		}
		for _, ref := range sortedConsoleAddressSetRefs(sets) {
			set := sets[ref]
			if !set.contains(log.DstAddress) {
				continue
			}
			if index, ok := seenSets[set]; ok {
				log.DestinationSets[index].Current = true
				continue
			}
			if index, ok := seen[ref]; ok {
				log.DestinationSets[index].Current = true
				continue
			}
			log.DestinationSets = append(log.DestinationSets, logstore.AddressSetMatch{
				ResourceName: set.Name,
				SetName:      ref,
				Source:       "current-destination",
				Current:      true,
			})
		}
	}
}

func matchConnectionLocalRedirect(entry observe.ConnectionEntry, rules []localRedirectDisplayRule, sets map[string]*consoleAddressSet) *observe.LocalRedirect {
	protocol := strings.ToLower(strings.TrimSpace(entry.Protocol))
	dstPort := atoiDefault(entry.Original.DestinationPort, 0)
	replySourcePort := atoiDefault(entry.Reply.SourcePort, 0)
	originalAddress := normalizedIPAddrString(entry.Original.Destination)
	replyAddress := normalizedIPAddrString(entry.Reply.Source)
	if protocol == "" || dstPort == 0 || originalAddress == "" || replyAddress == "" || originalAddress == replyAddress {
		return nil
	}
	var tupleCandidates []localRedirectDisplayRule
	for _, rule := range rules {
		if _, ok := rule.Protocols[protocol]; !ok {
			continue
		}
		if rule.DestinationPort != dstPort || rule.RedirectPort != replySourcePort {
			continue
		}
		tupleCandidates = append(tupleCandidates, rule)
		if set := sets[rule.DestinationSetRef]; set != nil && set.contains(originalAddress) {
			return &observe.LocalRedirect{
				ResourceName:      rule.ResourceName,
				RuleName:          rule.RuleName,
				DestinationSetRef: rule.DestinationSetRef,
				OriginalAddress:   originalAddress,
				RedirectAddress:   replyAddress,
				RedirectPort:      rule.RedirectPort,
				Match:             "destination-set",
			}
		}
	}
	if len(tupleCandidates) == 1 {
		rule := tupleCandidates[0]
		return &observe.LocalRedirect{
			ResourceName:      rule.ResourceName,
			RuleName:          rule.RuleName,
			DestinationSetRef: rule.DestinationSetRef,
			OriginalAddress:   originalAddress,
			RedirectAddress:   replyAddress,
			RedirectPort:      rule.RedirectPort,
			Match:             "tuple",
		}
	}
	return nil
}

func (h Handler) localRedirectDisplayRules() []localRedirectDisplayRule {
	if h.opts.Router == nil {
		return nil
	}
	var out []localRedirectDisplayRule
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "LocalServiceRedirect" {
			continue
		}
		spec, err := resource.LocalServiceRedirectSpec()
		if err != nil {
			continue
		}
		for i, rule := range spec.Rules {
			protocols := map[string]struct{}{}
			for _, protocol := range rule.Protocols {
				protocol = strings.ToLower(strings.TrimSpace(protocol))
				if protocol != "" {
					protocols[protocol] = struct{}{}
				}
			}
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				name = strconv.Itoa(i)
			}
			out = append(out, localRedirectDisplayRule{
				ResourceName:      resource.Metadata.Name,
				RuleName:          name,
				DestinationSetRef: strings.TrimSpace(rule.DestinationSetRef),
				DestinationPort:   rule.DestinationPort,
				RedirectPort:      rule.RedirectPort,
				Protocols:         protocols,
			})
		}
	}
	return out
}

func (h Handler) firewallRuleDestinationSetRefs() map[string][]string {
	out := map[string][]string{}
	if h.opts.Router == nil {
		return out
	}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallRule" {
			continue
		}
		spec, err := resource.FirewallRuleSpec()
		if err != nil {
			continue
		}
		for _, ref := range spec.DestinationSetRefs {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				out[resource.Metadata.Name] = appendUnique(out[resource.Metadata.Name], ref)
			}
		}
	}
	return out
}

func (h Handler) consoleAddressSets() map[string]*consoleAddressSet {
	if h.opts.Router == nil {
		return nil
	}
	statuses := h.objectStatusMap()
	out := map[string]*consoleAddressSet{}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPAddressSet" {
			continue
		}
		spec, err := resource.IPAddressSetSpec()
		if err != nil {
			continue
		}
		set := &consoleAddressSet{
			Name:      resource.Metadata.Name,
			Addresses: map[netip.Addr]struct{}{},
		}
		for _, address := range spec.Addresses {
			set.addAddress(address)
		}
		status := statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name]
		for _, key := range []string{"addresses", "ipv4Addresses", "ipv6Addresses"} {
			for _, address := range stringSliceFromMap(status, key) {
				set.addAddress(address)
			}
		}
		out[resource.Metadata.Name] = set
		out["IPAddressSet/"+resource.Metadata.Name] = set
	}
	return out
}

func (h Handler) objectStatusMap() map[string]map[string]any {
	out := map[string]map[string]any{}
	lister, ok := h.opts.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return out
	}
	resources, err := lister.ListObjectStatuses()
	if err != nil {
		return out
	}
	for _, resource := range h.filterStaleObjectStatuses(resources) {
		out[resource.APIVersion+"/"+resource.Kind+"/"+resource.Name] = resource.Status
	}
	return out
}

func (s *consoleAddressSet) addAddress(value string) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return
	}
	s.Addresses[addr.Unmap()] = struct{}{}
}

func (s *consoleAddressSet) contains(value string) bool {
	if s == nil {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	_, ok := s.Addresses[addr.Unmap()]
	return ok
}

func sortedConsoleAddressSetRefs(sets map[string]*consoleAddressSet) []string {
	seen := map[*consoleAddressSet]struct{}{}
	var out []string
	for ref, set := range sets {
		if strings.Contains(ref, "/") {
			continue
		}
		if _, ok := seen[set]; ok {
			continue
		}
		seen[set] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func annotateTupleServices(tuple *observe.ConntrackTuple, protocol string) {
	if tuple == nil {
		return
	}
	if tuple.SourceService == "" {
		tuple.SourceService = serviceNameForPort(protocol, atoiDefault(tuple.SourcePort, 0))
	}
	if tuple.DestinationService == "" {
		tuple.DestinationService = serviceNameForPort(protocol, atoiDefault(tuple.DestinationPort, 0))
	}
}

func annotateTupleHostnames(tuple *observe.ConntrackTuple, labels map[string]string) {
	if tuple == nil {
		return
	}
	if tuple.SourceHostname == "" {
		tuple.SourceHostname = labels[tuple.Source]
	}
	if tuple.DestinationHostname == "" {
		tuple.DestinationHostname = labels[tuple.Destination]
	}
}

func shouldReverseLookup(address string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	return addr.IsValid() && !addr.IsUnspecified() && !addr.IsMulticast()
}

type reverseDNSCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]reverseDNSEntry
}

type reverseDNSEntry struct {
	name    string
	expires time.Time
}

func newReverseDNSCache(ttl time.Duration) *reverseDNSCache {
	return &reverseDNSCache{ttl: ttl, entries: map[string]reverseDNSEntry{}}
}

func (c *reverseDNSCache) lookupMany(ctx context.Context, addresses []string, lookup func(context.Context, string) ([]string, error)) map[string]string {
	now := time.Now()
	out := map[string]string{}
	var pending []string
	c.mu.Lock()
	for _, address := range addresses {
		if entry, ok := c.entries[address]; ok && now.Before(entry.expires) {
			if entry.name != "" {
				out[address] = entry.name
			}
			continue
		}
		pending = append(pending, address)
	}
	c.mu.Unlock()
	if len(pending) == 0 || lookup == nil {
		return out
	}
	type result struct {
		address string
		name    string
	}
	sem := make(chan struct{}, 8)
	results := make(chan result, len(pending))
	var wg sync.WaitGroup
	for _, address := range pending {
		address := address
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			names, err := lookup(ctx, address)
			if err != nil {
				results <- result{address: address}
				return
			}
			results <- result{address: address, name: normalizeReverseDNSName(names)}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for item := range results {
		c.store(item.address, item.name, now.Add(c.ttl))
		if item.name != "" {
			out[item.address] = item.name
		}
	}
	return out
}

func (c *reverseDNSCache) store(address string, name string, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[address] = reverseDNSEntry{name: name, expires: expires}
}

func normalizeReverseDNSName(names []string) string {
	for _, name := range names {
		name = strings.TrimSuffix(strings.TrimSpace(name), ".")
		if name != "" {
			return name
		}
	}
	return ""
}

type portProtocolFallback struct {
	app        string
	category   string
	confidence int
}

func applyTrafficFlowPortFallback(flow *logstore.TrafficFlow) {
	if flow == nil {
		return
	}
	flow.AppName = canonicalProtocolAppName(flow.AppName)
	if fallback, ok := portProtocolFallbackFor(flow.Protocol, flow.PeerPort, flow.ClientPort, flow.ResolvedHostname, ""); ok {
		override := knownAppName(flow.AppName) && preferPortFallbackOverApp(flow.AppName, fallback.app)
		if knownAppName(flow.AppName) && !override && !preferMoreSpecificPortFallback(flow.AppCategory, flow.AppName, flow.AppConfidence, fallback) {
			return
		}
		flow.AppName = fallback.app
		flow.AppCategory = fallback.category
		flow.AppConfidence = fallback.confidence
		flow.Source = "port-fallback"
		if override {
			flow.ResolvedHostname = ""
		}
	}
}

func applyTrafficFlowListPortFallback(flows []logstore.TrafficFlow) {
	for i := range flows {
		applyTrafficFlowPortFallback(&flows[i])
	}
}

func applyConnectionPortFallback(entry *observe.ConnectionEntry) {
	if entry == nil {
		return
	}
	entry.AppName = canonicalProtocolAppName(entry.AppName)
	if fallback, ok := portProtocolFallbackFor(entry.Protocol, atoiDefault(entry.Original.DestinationPort, 0), atoiDefault(entry.Original.SourcePort, 0), entry.Original.DestinationHostname, entry.Original.SourceHostname); ok {
		override := knownAppName(entry.AppName) && preferPortFallbackOverApp(entry.AppName, fallback.app)
		if knownAppName(entry.AppName) && !override && !preferMoreSpecificPortFallback(entry.AppCategory, entry.AppName, entry.AppConfidence, fallback) {
			return
		}
		entry.AppName = fallback.app
		entry.AppCategory = fallback.category
		entry.AppConfidence = fallback.confidence
		if override {
			entry.DNSQuery = ""
		}
	}
}

func preferMoreSpecificPortFallback(category, current string, confidence int, fallback portProtocolFallback) bool {
	if !strings.EqualFold(strings.TrimSpace(category), "port-fallback") {
		return false
	}
	current = strings.ToLower(strings.TrimSpace(current))
	if current == "" || current == "unknown" || current == "unidentified" {
		return true
	}
	if fallback.confidence > confidence {
		return true
	}
	return current == "stun" && fallback.app == "tailscale"
}

func applyConnectionTablePortFallback(table *observe.ConnectionTable) {
	if table == nil {
		return
	}
	for i := range table.Entries {
		applyConnectionPortFallback(&table.Entries[i])
	}
}

func knownAppName(value string) bool {
	value = canonicalProtocolAppName(value)
	return value != "" && value != "unknown" && value != "unidentified"
}

func canonicalProtocolAppName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "aws-https", "google-https", "microsoft-https", "apple-https", "cloudflare-https":
		return "tls"
	default:
		return value
	}
}

func preferPortFallbackOverApp(current, fallback string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if current != "dns" {
		return false
	}
	switch fallback {
	case "tailscale", "stun", "wireguard", "quic":
		return true
	default:
		return false
	}
}

func portProtocolFallbackFor(protocol string, primaryPort, secondaryPort int, primaryHost, secondaryHost string) (portProtocolFallback, bool) {
	transport := strings.ToLower(strings.TrimSpace(protocol))
	for _, item := range []struct {
		port int
		host string
	}{{primaryPort, primaryHost}, {secondaryPort, secondaryHost}} {
		if fallback, ok := portProtocolFallbackByPort(transport, item.port, item.host); ok {
			return fallback, true
		}
	}
	return portProtocolFallback{}, false
}

func portProtocolFallbackByPort(protocol string, port int, host string) (portProtocolFallback, bool) {
	if port <= 0 {
		return portProtocolFallback{}, false
	}
	confidence := 40
	category := "port-fallback"
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if (port == 443 || port == 8443) && protocol == "tcp" {
		if tailscaleHostLabel(host) {
			return portProtocolFallback{app: "tailscale", category: category, confidence: 60}, true
		}
	}
	if protocol == "udp" && tailscaleHostLabel(host) {
		switch port {
		case 3478, 5349, 41641:
			return portProtocolFallback{app: "tailscale", category: category, confidence: 60}, true
		}
	}
	switch port {
	case 20, 21:
		return portProtocolFallback{app: "ftp", category: category, confidence: confidence}, true
	case 22:
		return portProtocolFallback{app: "ssh", category: category, confidence: confidence}, true
	case 25, 465, 587:
		return portProtocolFallback{app: "smtp", category: category, confidence: confidence}, true
	case 53:
		return portProtocolFallback{app: "dns", category: category, confidence: confidence}, true
	case 67, 68:
		if protocol == "udp" {
			return portProtocolFallback{app: "dhcp", category: category, confidence: confidence}, true
		}
	case 80, 8080, 8000, 8888:
		return portProtocolFallback{app: "http", category: category, confidence: confidence}, true
	case 110, 995:
		return portProtocolFallback{app: "pop3", category: category, confidence: confidence}, true
	case 123:
		if protocol == "udp" {
			return portProtocolFallback{app: "ntp", category: category, confidence: confidence}, true
		}
	case 137, 138:
		if protocol == "udp" {
			return portProtocolFallback{app: "netbios", category: category, confidence: confidence}, true
		}
	case 139, 445:
		return portProtocolFallback{app: "smb", category: category, confidence: confidence}, true
	case 143, 993:
		return portProtocolFallback{app: "imap", category: category, confidence: confidence}, true
	case 443, 8443:
		if protocol == "udp" {
			return portProtocolFallback{app: "quic", category: category, confidence: 35}, true
		}
		return portProtocolFallback{app: "tls", category: category, confidence: confidence}, true
	case 500, 4500:
		if protocol == "udp" {
			return portProtocolFallback{app: "ipsec", category: category, confidence: confidence}, true
		}
	case 853:
		return portProtocolFallback{app: "dns", category: category, confidence: confidence}, true
	case 1900:
		if protocol == "udp" {
			return portProtocolFallback{app: "ssdp", category: category, confidence: confidence}, true
		}
	case 3306:
		return portProtocolFallback{app: "mysql", category: category, confidence: confidence}, true
	case 3389:
		return portProtocolFallback{app: "rdp", category: category, confidence: confidence}, true
	case 4317:
		if protocol == "tcp" {
			return portProtocolFallback{app: "otlp", category: category, confidence: confidence}, true
		}
	case 3478, 5349:
		if protocol == "udp" {
			return portProtocolFallback{app: "stun", category: category, confidence: confidence}, true
		}
	case 4318:
		if protocol == "tcp" {
			return portProtocolFallback{app: "otlp-http", category: category, confidence: confidence}, true
		}
	case 5353:
		if protocol == "udp" {
			return portProtocolFallback{app: "mdns", category: category, confidence: confidence}, true
		}
	case 5355:
		if protocol == "udp" {
			return portProtocolFallback{app: "llmnr", category: category, confidence: confidence}, true
		}
	case 5432:
		return portProtocolFallback{app: "postgresql", category: category, confidence: confidence}, true
	case 51820:
		if protocol == "udp" {
			return portProtocolFallback{app: "wireguard", category: category, confidence: confidence}, true
		}
	case 41641:
		if protocol == "udp" {
			return portProtocolFallback{app: "tailscale", category: category, confidence: 55}, true
		}
	}
	return portProtocolFallback{}, false
}

func tailscaleHostLabel(host string) bool {
	if host == "" {
		return false
	}
	return host == "stun.l.google.com" ||
		host == "login.tailscale.com" ||
		host == "controlplane.tailscale.com" ||
		strings.HasSuffix(host, ".tailscale.com") ||
		strings.HasSuffix(host, ".ts.net")
}

func serviceNameForPort(protocol string, port int) string {
	if port <= 0 {
		return ""
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if name, ok := ianaServiceNames[port]; ok {
		if protocol == "udp" {
			if udp, ok := ianaUDPServiceNames[port]; ok {
				return udp
			}
		}
		return name
	}
	return ""
}

var ianaServiceNames = map[int]string{
	20:    "ftp-data",
	21:    "ftp",
	22:    "ssh",
	25:    "smtp",
	53:    "dns",
	67:    "dhcp-server",
	68:    "dhcp-client",
	80:    "http",
	110:   "pop3",
	123:   "ntp",
	137:   "netbios-ns",
	138:   "netbios-dgm",
	139:   "netbios-ssn",
	143:   "imap",
	443:   "https",
	445:   "microsoft-ds",
	465:   "submissions",
	500:   "isakmp",
	587:   "submission",
	853:   "domain-s",
	993:   "imaps",
	995:   "pop3s",
	1900:  "ssdp",
	3306:  "mysql",
	3389:  "ms-wbt-server",
	3478:  "stun",
	4317:  "otlp",
	4318:  "otlp-http",
	4500:  "ipsec-nat-t",
	5432:  "postgresql",
	5353:  "mdns",
	5355:  "llmnr",
	51820: "wireguard",
	41641: "tailscale",
}

var ianaUDPServiceNames = map[int]string{
	53:    "dns",
	67:    "dhcp-server",
	68:    "dhcp-client",
	123:   "ntp",
	137:   "netbios-ns",
	138:   "netbios-dgm",
	443:   "quic",
	500:   "isakmp",
	853:   "domain-s",
	1900:  "ssdp",
	3478:  "stun",
	4500:  "ipsec-nat-t",
	5353:  "mdns",
	5355:  "llmnr",
	51820: "wireguard",
	41641: "tailscale",
}

func (h Handler) dhcpLeaseList() ([]DHCPLease, error) {
	seen := map[string]DHCPLease{}
	now := time.Now().UTC()
	pathPriority := map[string]int{}
	for priority, path := range h.opts.DHCPLeasePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := pathPriority[path]; !ok {
			pathPriority[path] = priority
		}
		leases, err := readDnsmasqLeases(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, lease := range leases {
			if leaseExpired(lease, now) {
				continue
			}
			key := lease.IP
			if key == "" {
				key = lease.MAC
			}
			if key == "" {
				continue
			}
			if existing, ok := seen[key]; !ok || preferDHCPLease(lease, existing, pathPriority[path], pathPriorityValue(pathPriority, existing.Source)) {
				seen[key] = lease
			}
		}
	}
	out := make([]DHCPLease, 0, len(seen))
	for _, lease := range seen {
		out = append(out, lease)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].IP < out[j].IP
	})
	return out, nil
}

func leaseExpired(lease DHCPLease, now time.Time) bool {
	return !lease.ExpiresAt.IsZero() && !lease.ExpiresAt.After(now)
}

func preferDHCPLease(candidate, existing DHCPLease, candidatePriority, existingPriority int) bool {
	if !candidate.ExpiresAt.IsZero() && !existing.ExpiresAt.IsZero() && !candidate.ExpiresAt.Equal(existing.ExpiresAt) {
		return candidate.ExpiresAt.After(existing.ExpiresAt)
	}
	return candidatePriority < existingPriority
}

func pathPriorityValue(priorities map[string]int, path string) int {
	if priority, ok := priorities[path]; ok {
		return priority
	}
	return len(priorities)
}

func readDnsmasqLeases(path string) ([]DHCPLease, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []DHCPLease
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		expiresUnix, _ := strconv.ParseInt(fields[0], 10, 64)
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		lease := DHCPLease{
			MAC:      strings.ToLower(fields[1]),
			IP:       fields[2],
			Hostname: hostname,
			Family:   leaseAddressFamily(fields[2]),
			Source:   path,
		}
		if expiresUnix > 0 {
			lease.ExpiresAt = time.Unix(expiresUnix, 0).UTC()
		}
		if len(fields) >= 5 && fields[4] != "*" {
			lease.ClientID = fields[4]
		}
		lease.Vendor = macVendor(lease.MAC)
		out = append(out, lease)
	}
	return out, nil
}

func leaseAddressFamily(address string) string {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return ""
	}
	if parsed.Is4() {
		return "ipv4"
	}
	if parsed.Is6() {
		return "ipv6"
	}
	return ""
}

func neighborList() ([]NeighborEntry, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return freeBSDNeighborList()
	}
	out, err := exec.Command("ip", "-j", "neigh", "show").Output()
	if err != nil {
		return nil, err
	}
	return parseIPNeighborJSON(out)
}

func freeBSDNeighborList() ([]NeighborEntry, error) {
	var combined []NeighborEntry
	var errs []string
	if out, err := exec.Command("arp", "-an").Output(); err == nil {
		combined = append(combined, parseFreeBSDARP(out)...)
	} else {
		errs = append(errs, err.Error())
	}
	if out, err := exec.Command("ndp", "-an").Output(); err == nil {
		combined = append(combined, parseFreeBSDNDP(out)...)
	} else {
		errs = append(errs, err.Error())
	}
	if len(combined) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].IfName != combined[j].IfName {
			return combined[i].IfName < combined[j].IfName
		}
		return combined[i].IP < combined[j].IP
	})
	return combined, nil
}

func parseFreeBSDARP(data []byte) []NeighborEntry {
	var out []NeighborEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] == "?" && !strings.HasPrefix(fields[1], "(") {
			continue
		}
		ip := strings.Trim(fields[1], "()")
		mac := strings.ToLower(fields[3])
		if ip == "" || mac == "" || mac == "(incomplete)" {
			continue
		}
		ifname := ""
		for i, field := range fields {
			if field == "on" && i+1 < len(fields) {
				ifname = fields[i+1]
				break
			}
		}
		out = append(out, NeighborEntry{IP: ip, IfName: ifname, MAC: mac, State: "REACHABLE", Source: "arp", Vendor: macVendor(mac)})
	}
	return out
}

func parseFreeBSDNDP(data []byte) []NeighborEntry {
	var out []NeighborEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.EqualFold(fields[0], "Neighbor") {
			continue
		}
		ip := strings.TrimSuffix(fields[0], "%"+fields[2])
		mac := strings.ToLower(fields[1])
		if ip == "" || mac == "" || mac == "(incomplete)" || strings.EqualFold(mac, "Linklayer") {
			continue
		}
		state := ""
		if len(fields) >= 5 {
			state = fields[4]
		}
		out = append(out, NeighborEntry{IP: ip, IfName: fields[2], MAC: mac, State: state, Source: "ndp", Vendor: macVendor(mac)})
	}
	return out
}

func parseIPNeighborJSON(data []byte) ([]NeighborEntry, error) {
	var raw []struct {
		Dst    string          `json:"dst"`
		Dev    string          `json:"dev"`
		LLAddr string          `json:"lladdr"`
		State  json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	seen := map[string]NeighborEntry{}
	for _, item := range raw {
		ip := strings.TrimSpace(item.Dst)
		if ip == "" {
			continue
		}
		mac := strings.ToLower(strings.TrimSpace(item.LLAddr))
		entry := NeighborEntry{
			IP:     ip,
			IfName: strings.TrimSpace(item.Dev),
			MAC:    mac,
			State:  parseNeighborState(item.State),
			Source: "ip-neigh",
			Vendor: macVendor(mac),
		}
		seen[ip+"|"+entry.IfName] = entry
	}
	out := make([]NeighborEntry, 0, len(seen))
	for _, entry := range seen {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IfName != out[j].IfName {
			return out[i].IfName < out[j].IfName
		}
		return out[i].IP < out[j].IP
	})
	return out, nil
}

func parseNeighborState(raw json.RawMessage) string {
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return strings.Join(values, ",")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}

func correlateClients(leases []DHCPLease, neighbors []NeighborEntry, flows []logstore.TrafficFlow, queries []logstore.DNSQuery, firewallLogs []logstore.FirewallLogEntry, dhcpFingerprints ...[]logstore.DHCPFingerprint) []ClientEntry {
	rows := map[string]*clientMutableEntry{}
	ipToKey := map[string]string{}
	passive := buildPassiveFingerprints(leases, flows, queries, firewallLogs)
	dhcpFingerprintByMAC := latestDHCPFingerprintByMAC(dhcpFingerprints...)
	upsert := func(key, address string) *clientMutableEntry {
		key = strings.TrimSpace(key)
		if key == "" {
			key = strings.TrimSpace(address)
		}
		if key == "" {
			key = "-"
		}
		row := rows[key]
		if row == nil {
			row = &clientMutableEntry{
				ClientEntry: ClientEntry{ID: key},
				addresses:   map[string]bool{},
				sources:     map[string]bool{},
				peers:       map[string]bool{},
				activity:    map[string]*clientActivityStat{},
			}
			rows[key] = row
		}
		if address = strings.TrimSpace(address); address != "" {
			row.addresses[address] = true
			ipToKey[address] = key
		}
		return row
	}
	for _, lease := range leases {
		if strings.TrimSpace(lease.IP) == "" {
			continue
		}
		key := clientCorrelationKey(lease.MAC, lease.IP)
		row := upsert(key, lease.IP)
		if row.Hostname == "" {
			row.Hostname = lease.Hostname
		}
		if row.MAC == "" {
			row.MAC = normalizeClientMAC(lease.MAC)
		}
		if row.Vendor == "" {
			row.Vendor = lease.Vendor
		}
		if !lease.StickyUntil.IsZero() {
			row.StickyUntil = lease.StickyUntil.Format(time.RFC3339Nano)
			row.StickyState = firstNonEmptyString(lease.StickyState, "held")
		}
		row.sources["dhcpv4"] = true
	}
	for _, neighbor := range neighbors {
		if strings.TrimSpace(neighbor.IP) == "" {
			continue
		}
		if neighborStateFailed(neighbor.State) {
			continue
		}
		key := clientCorrelationKey(neighbor.MAC, neighbor.IP)
		row := upsert(key, neighbor.IP)
		if row.MAC == "" {
			row.MAC = normalizeClientMAC(neighbor.MAC)
		}
		if row.Vendor == "" {
			row.Vendor = neighbor.Vendor
		}
		if row.State == "" {
			row.State = neighbor.State
		}
		source := strings.TrimSpace(neighbor.Source)
		if source == "" {
			source = "neighbor"
		}
		row.sources[source] = true
	}
	for ip, fingerprint := range passive {
		if ip == "" || ipToKey[ip] != "" {
			continue
		}
		if key := matchFingerprintToClient(rows, ip, fingerprint); key != "" {
			ipToKey[ip] = key
			rows[key].addresses[ip] = true
		}
	}
	for _, query := range queries {
		ip := strings.TrimSpace(query.ClientAddress)
		if ip == "" {
			continue
		}
		key := ipToKey[ip]
		if key == "" {
			key = passiveCorrelationKey(passive[ip], ip)
		}
		row := upsert(key, ip)
		if query.QuestionName != "" {
			row.peers[strings.TrimSuffix(query.QuestionName, ".")] = true
		}
		row.sources["dns"] = true
	}
	for _, flow := range flows {
		ip := strings.TrimSpace(flow.ClientAddress)
		if ip == "" {
			continue
		}
		key := ipToKey[ip]
		if key == "" {
			key = passiveCorrelationKey(passive[ip], ip)
		}
		row := upsert(key, ip)
		if flow.Accounting {
			row.BytesOut += flow.BytesOut
			row.BytesIn += flow.BytesIn
		}
		peer := firstNonEmptyString(flow.TLSSNI, flow.HTTPHost, flow.DNSQuery, flow.ResolvedHostname, flow.PeerAddress)
		if peer != "" {
			row.peers[peer] = true
		}
		row.recordActivity(flowActivityName(flow), flowActivityDetail(flow), flow.BytesOut+flow.BytesIn, flow.EndedAt)
		row.sources["traffic"] = true
	}
	out := make([]ClientEntry, 0, len(rows))
	for _, row := range rows {
		row.Addresses = sortedClientAddresses(row.addresses)
		row.Sources = sortedSet(row.sources)
		row.Peers = sortedSet(row.peers)
		row.applyActivitySummary()
		fingerprint := inferClientFingerprint(row.ClientEntry, passive, dhcpFingerprintByMAC[normalizeClientMAC(row.MAC)])
		row.InferredOSFamily = fingerprint.OSFamily
		row.InferredDeviceClass = fingerprint.DeviceClass
		row.FingerprintConfidence = fingerprint.Confidence
		row.FingerprintSignals = fingerprint.Signals
		out = append(out, row.ClientEntry)
	}
	sort.Slice(out, func(i, j int) bool {
		trafficI := out[i].BytesOut + out[i].BytesIn
		trafficJ := out[j].BytesOut + out[j].BytesIn
		if trafficI != trafficJ {
			return trafficI > trafficJ
		}
		return out[i].ID < out[j].ID
	})
	return out
}

type clientPolicyAssignment struct {
	Name      string
	Mode      string
	Isolation []string
}

func (h Handler) annotateClientsWithPolicy(clients []ClientEntry) []ClientEntry {
	if h.opts.Router == nil || len(clients) == 0 {
		return clients
	}
	byMAC := map[string]clientPolicyAssignment{}
	for _, res := range h.opts.Router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "ClientPolicy" {
			continue
		}
		spec, err := res.ClientPolicySpec()
		if err != nil {
			continue
		}
		assignment := clientPolicyAssignment{Name: res.Metadata.Name, Mode: spec.Mode, Isolation: clientPolicyIsolationLabels(spec)}
		for _, mac := range spec.MACs {
			if normalized := normalizeClientMAC(mac); normalized != "" {
				byMAC[normalized] = assignment
			}
		}
		for _, entry := range spec.Classification {
			if normalized := normalizeClientMAC(entry.MACAddress); normalized != "" {
				switch spec.Mode {
				case "include":
					if entry.As == "" || entry.As == "guest" {
						byMAC[normalized] = assignment
					}
				case "exclude":
					if entry.As == "" || entry.As == "trusted" {
						byMAC[normalized] = clientPolicyAssignment{Name: res.Metadata.Name, Mode: "trusted", Isolation: []string{"trusted exception"}}
					}
				}
			}
		}
	}
	for i := range clients {
		assignment, ok := byMAC[normalizeClientMAC(clients[i].MAC)]
		if !ok {
			continue
		}
		clients[i].ClientPolicy = assignment.Name
		clients[i].ClientPolicyMode = assignment.Mode
		clients[i].IsolationPolicy = assignment.Isolation
	}
	return clients
}

func clientPolicyIsolationLabels(spec api.ClientPolicySpec) []string {
	var labels []string
	if spec.Isolation.LANInternet != "" {
		labels = append(labels, "internet "+spec.Isolation.LANInternet)
	}
	if spec.Isolation.LANLAN != "" {
		labels = append(labels, "LAN "+spec.Isolation.LANLAN)
	}
	if spec.Isolation.LANMgmt != "" {
		labels = append(labels, "mgmt "+spec.Isolation.LANMgmt)
	}
	if spec.Isolation.MDNSBroadcast != "" {
		labels = append(labels, "discovery "+spec.Isolation.MDNSBroadcast)
	}
	if len(labels) == 0 {
		labels = append(labels, "private LAN deny")
	}
	return labels
}

func neighborStateFailed(state string) bool {
	for _, part := range strings.Split(strings.ToUpper(state), ",") {
		if strings.TrimSpace(part) == "FAILED" {
			return true
		}
	}
	return false
}

func clientCorrelationKey(mac, ip string) string {
	if normalized := normalizeClientMAC(mac); normalized != "" {
		return normalized
	}
	return strings.TrimSpace(ip)
}

func normalizeClientMAC(mac string) string {
	return strings.ToLower(strings.TrimSpace(mac))
}

func (row *clientMutableEntry) recordActivity(protocol, detail string, bytes int64, seen time.Time) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "unknown" {
		protocol = "unidentified"
	}
	if row.activity == nil {
		row.activity = map[string]*clientActivityStat{}
	}
	stat := row.activity[protocol]
	if stat == nil {
		stat = &clientActivityStat{Protocol: protocol}
		row.activity[protocol] = stat
	}
	stat.Count++
	if bytes > 0 {
		stat.Bytes += bytes
	}
	if strings.TrimSpace(detail) != "" {
		stat.Detail = detail
	}
	if seen.IsZero() {
		seen = time.Now().UTC()
	}
	if stat.LastSeen.IsZero() || seen.After(stat.LastSeen) {
		stat.LastSeen = seen
	}
}

func (row *clientMutableEntry) applyActivitySummary() {
	if len(row.activity) == 0 {
		return
	}
	stats := make([]*clientActivityStat, 0, len(row.activity))
	for _, stat := range row.activity {
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Bytes != stats[j].Bytes {
			return stats[i].Bytes > stats[j].Bytes
		}
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Protocol < stats[j].Protocol
	})
	row.ProtocolMix = make([]string, 0, min(len(stats), 3))
	for _, stat := range stats {
		if len(row.ProtocolMix) >= 3 {
			break
		}
		row.ProtocolMix = append(row.ProtocolMix, stat.Protocol)
	}
	row.PrimaryActivity = classifyClientActivity(stats)
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].LastSeen.After(stats[j].LastSeen)
	})
	row.LastProtocol = stats[0].Protocol
	row.LastProtocolDetail = stats[0].Detail
}

func flowActivityName(flow logstore.TrafficFlow) string {
	return flowActivityProtocol(flow)
}

func flowActivityDetail(flow logstore.TrafficFlow) string {
	app := flowActivityProtocol(flow)
	switch {
	case strings.TrimSpace(flow.TLSSNI) != "":
		return "TLS-SNI=" + strings.TrimSpace(flow.TLSSNI)
	case strings.TrimSpace(flow.HTTPHost) != "":
		return "HTTP-Host=" + strings.TrimSpace(flow.HTTPHost)
	case strings.TrimSpace(flow.DNSQuery) != "":
		if app == "netbios" {
			return "NBNS-query=" + strings.TrimSpace(flow.DNSQuery)
		}
		return "DNS-query=" + strings.TrimSpace(flow.DNSQuery)
	case strings.TrimSpace(flow.ResolvedHostname) != "":
		if app == "netbios" {
			return "NBNS-query=" + strings.TrimSpace(flow.ResolvedHostname)
		}
		if app == "dns" {
			return "DNS-query=" + strings.TrimSpace(flow.ResolvedHostname)
		}
		if app == "http" {
			return "HTTP-Host=" + strings.TrimSpace(flow.ResolvedHostname)
		}
		return "Host=" + strings.TrimSpace(flow.ResolvedHostname)
	case strings.TrimSpace(flow.PeerAddress) != "":
		return strings.TrimSpace(flow.PeerAddress)
	default:
		return ""
	}
}

func flowActivityProtocol(flow logstore.TrafficFlow) string {
	if name := canonicalProtocolAppName(flow.AppName); name != "" && name != "unknown" {
		if protocol := providerActivityProtocol(name, flow); protocol != "" {
			return protocol
		}
		return name
	}
	if strings.TrimSpace(flow.TLSSNI) != "" {
		return "tls"
	}
	switch flow.PeerPort {
	case 53:
		return "dns"
	case 80:
		return "http"
	case 3478, 5349:
		return "stun"
	case 41641:
		return "tailscale"
	case 51820:
		return "wireguard"
	case 443:
		if strings.EqualFold(flow.Protocol, "udp") {
			return "quic"
		}
		return "tls"
	}
	return strings.ToLower(strings.TrimSpace(flow.Protocol))
}

func providerActivityProtocol(app string, flow logstore.TrafficFlow) string {
	switch strings.ToLower(strings.TrimSpace(app)) {
	case "google", "googleservices", "amazonaws", "microsoft", "microsoft365", "azure", "apple", "appleicloud", "applepush", "cloudflare", "nintendo":
		if strings.EqualFold(flow.Protocol, "udp") && flow.PeerPort == 443 {
			return "quic"
		}
		return "tls"
	default:
		return ""
	}
}

func classifyClientActivity(stats []*clientActivityStat) string {
	totalBytes := int64(0)
	totalCount := 0
	seen := map[string]bool{}
	for _, stat := range stats {
		totalBytes += stat.Bytes
		totalCount += stat.Count
		seen[stat.Protocol] = true
	}
	if len(seen) >= 4 {
		return "mixed"
	}
	if seen["netbios"] || seen["mdns"] || seen["ssdp"] {
		return "iot-telemetry"
	}
	if seen["dns"] && len(seen) == 1 {
		return "resolver-only"
	}
	for _, stat := range stats {
		if (stat.Protocol == "tls" || stat.Protocol == "http") && (totalBytes == 0 || stat.Bytes*100 >= totalBytes*60 || stat.Count*100 >= totalCount*60) {
			return "web-heavy"
		}
	}
	return "mixed"
}

type clientMutableEntry struct {
	ClientEntry
	addresses map[string]bool
	sources   map[string]bool
	peers     map[string]bool
	activity  map[string]*clientActivityStat
}

type clientActivityStat struct {
	Protocol string
	Detail   string
	Bytes    int64
	Count    int
	LastSeen time.Time
}

type clientFingerprint struct {
	OSFamily    string
	DeviceClass string
	Confidence  int
	Signals     []string
	Hostname    string
	Vendor      string
}

type fingerprintAccumulator struct {
	osScores      map[string]int
	classScores   map[string]int
	osClassScores map[string]int
	signals       map[string]bool
	signalScores  map[string]fingerprintSignalScore
	hostname      string
	vendor        string
	hasMulticast  bool
}

type fingerprintSignalScore struct {
	OSFamily    string
	DeviceClass string
	Score       int
}

func buildPassiveFingerprints(_ []DHCPLease, flows []logstore.TrafficFlow, queries []logstore.DNSQuery, firewallLogs []logstore.FirewallLogEntry) map[string]*fingerprintAccumulator {
	out := map[string]*fingerprintAccumulator{}
	acc := func(ip string) *fingerprintAccumulator {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			return nil
		}
		item := out[ip]
		if item == nil {
			item = &fingerprintAccumulator{osScores: map[string]int{}, classScores: map[string]int{}, osClassScores: map[string]int{}, signals: map[string]bool{}, signalScores: map[string]fingerprintSignalScore{}}
			out[ip] = item
		}
		return item
	}
	for _, query := range queries {
		item := acc(query.ClientAddress)
		if item == nil {
			continue
		}
		applyDomainFingerprint(item, query.QuestionName)
	}
	for _, flow := range flows {
		item := acc(flow.ClientAddress)
		if item == nil {
			continue
		}
		applyDomainFingerprint(item, flow.ResolvedHostname)
		applyDomainFingerprint(item, flow.TLSSNI)
		applyDomainFingerprint(item, flow.HTTPHost)
		applyDomainFingerprint(item, flow.DNSQuery)
		applyTransportFingerprint(item, flow.Protocol, flow.PeerAddress, flow.PeerPort)
		applyAppFingerprint(item, flow.AppName, flow.AppCategory, flow.AppConfidence)
	}
	for _, entry := range firewallLogs {
		ip := firewallClientAddress(entry)
		item := acc(ip)
		if item == nil {
			continue
		}
		applyDomainFingerprint(item, entry.DPITLSSNI)
		applyDomainFingerprint(item, entry.DPIHTTPHost)
		applyDomainFingerprint(item, entry.DPIDNSQuery)
		applyTransportFingerprint(item, entry.Protocol, entry.DstAddress, entry.DstPort)
		applyAppFingerprint(item, entry.DPIApp, entry.DPICategory, entry.DPIConfidence)
	}
	return out
}

func applyHostVendorFingerprint(item *fingerprintAccumulator, hostname, vendor, clientID string) {
	hostText := strings.ToLower(strings.Join([]string{hostname, clientID}, " "))
	switch {
	case containsAny(hostText, "doorbell", "nest-cam", "nest cam", "nest-doorbell"):
		addFingerprintSignal(item, "iot", "camera", 150, "hostname/camera")
	case containsAny(hostText, "bravia", "android-tv", "androidtv", "google-tv"):
		addFingerprintSignal(item, "iot", "smart-tv", 150, "hostname/smart-tv")
	case containsAny(hostText, "echo", "alexa"):
		addFingerprintSignal(item, "iot", "smart-speaker", 130, "hostname/amazon-echo")
	case containsAny(hostText, "google-nest", "google home", "google-home", "nest-mini", "nest hub"):
		addFingerprintSignal(item, "iot", "smart-speaker", 130, "hostname/google-nest")
	case strings.Contains(hostText, "chromecast"):
		addFingerprintSignal(item, "iot", "smart-tv", 130, "hostname/chromecast")
	case strings.Contains(hostText, "roku"):
		addFingerprintSignal(item, "iot", "smart-tv", 130, "hostname/roku")
	case strings.Contains(hostText, "firetv") || strings.Contains(hostText, "fire-tv"):
		addFingerprintSignal(item, "iot", "smart-tv", 130, "hostname/fire-tv")
	case strings.Contains(hostText, "switchbot"):
		addFingerprintSignal(item, "iot", "iot", 130, "hostname/switchbot")
	case containsAny(hostText, "hue", "philips-hue"):
		addFingerprintSignal(item, "iot", "lighting", 130, "hostname/hue")
	case strings.Contains(hostText, "ring"):
		addFingerprintSignal(item, "iot", "camera", 130, "hostname/ring")
	case containsAny(hostText, "eufy", "wyze"):
		addFingerprintSignal(item, "iot", "camera", 130, "hostname/camera")
	case containsAny(hostText, "roomba", "irobot", "roborock"):
		addFingerprintSignal(item, "iot", "vacuum", 130, "hostname/vacuum")
	case strings.Contains(hostText, "sonos"):
		addFingerprintSignal(item, "iot", "smart-speaker", 130, "hostname/sonos")
	case containsAny(hostText, "kasa", "tapo", "tp-link", "tplink", "aqara", "tuya", "smartlife", "shelly", "nature-remo", "broadlink", "aiseg", "ecoflow", "atom", "espressif"):
		addFingerprintSignal(item, "iot", "iot", 125, "hostname/iot")
	case strings.Contains(hostText, "synology"):
		addFingerprintSignal(item, "nas", "nas", 140, "hostname/synology")
	case strings.Contains(hostText, "qnap"):
		addFingerprintSignal(item, "nas", "nas", 140, "hostname/qnap")
	case containsAny(hostText, "hp-printer", "officejet", "laserjet", "deskjet"):
		addFingerprintSignal(item, "printer", "printer", 140, "hostname/hp-printer")
	case strings.Contains(hostText, "canon"):
		addFingerprintSignal(item, "printer", "printer", 130, "hostname/canon")
	case strings.Contains(hostText, "epson"):
		addFingerprintSignal(item, "printer", "printer", 130, "hostname/epson")
	case strings.Contains(hostText, "brother"):
		addFingerprintSignal(item, "printer", "printer", 130, "hostname/brother")
	case containsAny(hostText, "yealink", "polycom"):
		addFingerprintSignal(item, "voip", "voip", 130, "hostname/voip")
	case strings.Contains(hostText, "tesla"):
		addFingerprintSignal(item, "iot", "ev", 140, "hostname/tesla")
	case strings.Contains(hostText, "nintendo"):
		addFingerprintSignal(item, "nintendo", "gaming-console", 140, "hostname/nintendo")
	case strings.Contains(hostText, "playstation") || strings.Contains(hostText, "ps5") || strings.Contains(hostText, "ps4"):
		addFingerprintSignal(item, "playstation", "gaming-console", 140, "hostname/playstation")
	case strings.Contains(hostText, "xbox"):
		addFingerprintSignal(item, "xbox", "gaming-console", 140, "hostname/xbox")
	case strings.Contains(hostText, "steamdeck") || strings.Contains(hostText, "steam deck"):
		addFingerprintSignal(item, "steam-os", "gaming-console", 140, "hostname/steamdeck")
	case strings.Contains(hostText, "iphone"):
		addFingerprintSignal(item, "Apple", "phone", 120, "hostname=iphone")
	case strings.Contains(hostText, "ipad"):
		addFingerprintSignal(item, "Apple", "tablet", 120, "hostname=ipad")
	case strings.Contains(hostText, "macbook") || strings.Contains(hostText, "imac") || strings.Contains(hostText, "mac mini"):
		addFingerprintSignal(item, "Apple", "computer", 100, "hostname/mac")
	case strings.Contains(hostText, "windows") || strings.HasPrefix(strings.TrimSpace(hostText), "win-") || strings.Contains(hostText, "microsoft"):
		addFingerprintSignal(item, "Windows", "computer", 90, "hostname/windows")
	case strings.Contains(hostText, "samsung"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/samsung")
	case strings.Contains(hostText, "xiaomi"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/xiaomi")
	case strings.Contains(hostText, "huawei"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/huawei")
	case strings.Contains(hostText, "oppo"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/oppo")
	case strings.Contains(hostText, "android") || strings.Contains(hostText, "pixel") || strings.Contains(hostText, "oneplus") || strings.Contains(hostText, "motorola"):
		addFingerprintSignal(item, "Android", "phone", 100, "hostname/android")
	}
	vendorText := strings.ToLower(strings.TrimSpace(vendor))
	switch {
	case containsAny(vendorText, "nintendo"):
		addFingerprintSignal(item, "nintendo", "gaming-console", 80, "vendor/nintendo")
	case containsAny(vendorText, "playstation", "sony computer entertainment", "sce"):
		addFingerprintSignal(item, "playstation", "gaming-console", 80, "vendor/playstation")
	case containsAny(vendorText, "xbox", "microsoft"):
		addFingerprintSignal(item, "xbox", "gaming-console", 70, "vendor/xbox")
	case containsAny(vendorText, "bravia", "sony visual", "sony tv"):
		addFingerprintSignal(item, "iot", "smart-tv", 80, "vendor/bravia")
	case containsAny(vendorText, "synology"):
		addFingerprintSignal(item, "nas", "nas", 70, "vendor/synology")
	case containsAny(vendorText, "qnap"):
		addFingerprintSignal(item, "nas", "nas", 70, "vendor/qnap")
	case containsAny(vendorText, "hewlett", "hp inc", "canon", "epson", "brother", "ricoh", "konica"):
		addFingerprintSignal(item, "printer", "printer", 70, "vendor/printer")
	case containsAny(vendorText, "yealink", "polycom"):
		addFingerprintSignal(item, "voip", "voip", 70, "vendor/voip")
	case containsAny(vendorText, "amazon"):
		addFingerprintSignal(item, "iot", "smart-speaker", 55, "vendor/amazon")
	case containsAny(vendorText, "google"):
		addFingerprintSignal(item, "Android", "", 20, "vendor/google")
	case containsAny(vendorText, "roku"):
		addFingerprintSignal(item, "iot", "smart-tv", 55, "vendor/roku")
	case containsAny(vendorText, "ring", "irobot", "roborock", "sonos", "philips", "eufy", "wyze", "tuya", "shelly", "aqara", "tp-link", "tplink", "panasonic", "espressif", "ecoflow", "atom tech"):
		addFingerprintSignal(item, "iot", "iot", 55, "vendor/iot")
	case strings.Contains(vendorText, "apple") && !strings.Contains(vendorText, "private"):
		addFingerprintSignal(item, "Apple", "", 35, "vendor/apple")
	case containsAny(vendorText, "samsung", "xiaomi", "huawei", "oppo"):
		addFingerprintSignal(item, "Android", "phone", 55, "vendor/android-oem")
	}
}

func applyDomainFingerprint(item *fingerprintAccumulator, name string) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" {
		return
	}
	switch {
	case domainMatchesAny(name, "amazonalexa.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-speaker", 110, "dns/amazon-echo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "dms.amazon.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-speaker", 70, "dns/amazon-device:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "googlecast.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-tv", 110, "dns/googlecast:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "gvt1.com", "clients3.google.com", "l.google.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-tv", 45, "dns/google-media:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "roku.com", "rokulabs.net"):
		addUniqueFingerprintSignal(item, "iot", "smart-tv", 120, "dns/roku:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "switchbot.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/switchbot:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "kasa-smart.com", "tplinkcloud.com", "tapo.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/tplink-iot:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "aqara.com", "lumiunited.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/aqara:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "tuyaus.com", "tuyaeu.com", "tuya.com", "smartlife.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/tuya:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "shelly.cloud", "shelly.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/shelly:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "nature.global", "nature.global.edgekey.net"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/nature-remo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "broadlink.com.cn"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/broadlink:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "ecoflow.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/ecoflow:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "meethue.com"):
		addUniqueFingerprintSignal(item, "iot", "lighting", 120, "dns/hue:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "ring.com"):
		addUniqueFingerprintSignal(item, "iot", "camera", 120, "dns/ring:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "eufylife.com", "eufy.com", "anker.com", "wyze.com"):
		addUniqueFingerprintSignal(item, "iot", "camera", 120, "dns/eufy:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "irobotapi.com", "iadc.irobot.com", "roborock.com"):
		addUniqueFingerprintSignal(item, "iot", "vacuum", 120, "dns/vacuum:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "sonos.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-speaker", 120, "dns/sonos:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "synology.com", "quickconnect.to"):
		addUniqueFingerprintSignal(item, "nas", "nas", 120, "dns/synology:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "qnap.com"):
		addUniqueFingerprintSignal(item, "nas", "nas", 120, "dns/qnap:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "hpconnected.com"):
		addUniqueFingerprintSignal(item, "printer", "printer", 120, "dns/hp-printer:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "cps.canon.jp", "epsonconnect.com"):
		addUniqueFingerprintSignal(item, "printer", "printer", 120, "dns/printer:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "hp.com", "canon.com", "epson.com", "epson.jp", "brother.com", "ricoh.com", "konicaminolta.com"):
		addUniqueFingerprintSignal(item, "printer", "printer", 65, "dns/printer:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "yealink.com", "polycom.com"):
		addUniqueFingerprintSignal(item, "voip", "voip", 120, "dns/voip:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "zoom.us", "zoomgov.com", "webex.com"):
		addUniqueFingerprintSignal(item, "voip", "voip", 25, "dns/conference:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "teams.microsoft.com", "skype.com"):
		addUniqueFingerprintSignal(item, "voip", "voip", 20, "dns/conference:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "zerotier.com"):
		addUniqueFingerprintSignal(item, "linux", "", 25, "dns/zerotier:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "samsung.com", "samsungcloud.com", "samsungelectronics.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/samsung:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "xiaomi.com", "mi.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/xiaomi:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "huawei.com", "hicloud.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/huawei:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "oppo.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/oppo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "tesla.com", "teslamotors.com"):
		addUniqueFingerprintSignal(item, "iot", "ev", 120, "dns/tesla:"+shortFingerprintSignal(name))
	case containsAny(name, "bravia.dtv") || strings.Contains(name, "_androidtvremote."):
		addFingerprintSignal(item, "iot", "smart-tv", 100, "mdns/smart-tv")
	case domainMatchesAny(name, "nintendo.net", "npln.jp", "ndas.srv.nintendo.net", "gs.nintendo.net", "accounts.nintendo.com"):
		addUniqueFingerprintSignal(item, "nintendo", "gaming-console", 120, "dns/nintendo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "playstation.net", "sonyentertainmentnetwork.com", "scea.com"):
		addUniqueFingerprintSignal(item, "playstation", "gaming-console", 120, "dns/playstation:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "xboxlive.com", "xbox.com"):
		addUniqueFingerprintSignal(item, "xbox", "gaming-console", 120, "dns/xbox:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "steampowered.com", "steamcontent.com"):
		addUniqueFingerprintSignal(item, "steam-os", "gaming-console", 120, "dns/steam:"+shortFingerprintSignal(name))
	case strings.Contains(name, "icloud.com") || strings.Contains(name, "apple.com") || strings.Contains(name, "mzstatic.com") || strings.Contains(name, "push.apple.com") || strings.Contains(name, "captive.apple.com"):
		addFingerprintSignal(item, "Apple", "", 35, "dns/apple:"+shortFingerprintSignal(name))
	case strings.Contains(name, "windowsupdate.com") || strings.Contains(name, "msftconnecttest.com") || strings.Contains(name, "microsoft.com") || strings.Contains(name, "office365.com") || strings.Contains(name, "live.com"):
		addFingerprintSignal(item, "Windows", "computer", 35, "dns/windows:"+shortFingerprintSignal(name))
	case strings.Contains(name, "connectivitycheck.gstatic.com") || strings.Contains(name, "android.clients.google.com") || strings.Contains(name, "gms.") || strings.Contains(name, "googleapis.com"):
		addUniqueFingerprintSignal(item, "Android", "", 20, "dns/android:"+shortFingerprintSignal(name))
	case strings.Contains(name, "_airplay.") || strings.Contains(name, "_raop.") || strings.Contains(name, "_companion-link.") || strings.Contains(name, "_homekit."):
		addFingerprintSignal(item, "Apple", "", 80, "mdns/apple")
	case strings.Contains(name, "_googlecast.") || strings.Contains(name, "_androidtvremote."):
		addFingerprintSignal(item, "iot", "smart-tv", 80, "mdns/googlecast")
	case strings.Contains(name, "_smb.") || strings.Contains(name, "_workstation.") || strings.Contains(name, "wpad."):
		addFingerprintSignal(item, "Windows", "computer", 35, "dns/windows-service")
	case domainMatchesAny(name, "amazonaws.com"):
		addUniqueFingerprintSignal(item, "iot", "", 15, "dns/aws-device:"+shortFingerprintSignal(name))
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func domainMatchesAny(name string, suffixes ...string) bool {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	for _, suffix := range suffixes {
		suffix = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(suffix)), ".")
		if suffix == "" {
			continue
		}
		if name == suffix || strings.HasSuffix(name, "."+suffix) {
			return true
		}
	}
	return false
}

func applyTransportFingerprint(item *fingerprintAccumulator, proto, peer string, port int) {
	proto = strings.ToLower(strings.TrimSpace(proto))
	peer = strings.ToLower(strings.TrimSpace(peer))
	if proto != "udp" {
		return
	}
	switch {
	case port == 5353 || peer == "224.0.0.251" || peer == "ff02::fb":
		item.hasMulticast = true
		addFingerprintSignal(item, "", "", 20, "multicast/mdns")
	case port == 1900 || peer == "239.255.255.250" || peer == "ff02::c":
		item.hasMulticast = true
		addFingerprintSignal(item, "iot", "iot", 55, "multicast/ssdp")
	case port == 137 || port == 138 || port == 139:
		item.hasMulticast = true
		addFingerprintSignal(item, "Windows", "computer", 60, "multicast/netbios")
	}
}

func applyAppFingerprint(item *fingerprintAccumulator, app, category string, confidence int) {
	text := strings.ToLower(strings.Join([]string{app, category}, " "))
	if text == "" {
		return
	}
	weight := 35
	if confidence >= 80 {
		weight = 70
	}
	switch {
	case strings.Contains(text, "mdns"):
		addFingerprintSignal(item, "", "", maxInt(weight, 80), "dpi/mdns")
	case strings.Contains(text, "ssdp"):
		addFingerprintSignal(item, "iot", "iot", maxInt(weight, 55), "dpi/ssdp")
	case strings.Contains(text, "netbios") || strings.Contains(text, "smb"):
		addFingerprintSignal(item, "Windows", "computer", maxInt(weight, 60), "dpi/netbios")
	}
}

func addFingerprintSignal(item *fingerprintAccumulator, osFamily, deviceClass string, score int, signal string) {
	if item == nil {
		return
	}
	if item.osScores == nil {
		item.osScores = map[string]int{}
	}
	if item.classScores == nil {
		item.classScores = map[string]int{}
	}
	if item.osClassScores == nil {
		item.osClassScores = map[string]int{}
	}
	if item.signals == nil {
		item.signals = map[string]bool{}
	}
	if item.signalScores == nil {
		item.signalScores = map[string]fingerprintSignalScore{}
	}
	if signal != "" {
		item.signals[signal] = true
		contribution := item.signalScores[signal]
		if contribution.OSFamily == "" {
			contribution.OSFamily = osFamily
		}
		if contribution.DeviceClass == "" {
			contribution.DeviceClass = deviceClass
		}
		contribution.Score += score
		item.signalScores[signal] = contribution
	}
	if osFamily != "" {
		item.osScores[osFamily] += score
	}
	if deviceClass != "" {
		item.classScores[deviceClass] += score
	}
	if osFamily != "" && deviceClass != "" {
		item.osClassScores[osClassScoreKey(osFamily, deviceClass)] += score
	}
}

func addUniqueFingerprintSignal(item *fingerprintAccumulator, osFamily, deviceClass string, score int, signal string) {
	if item != nil && signal != "" && item.signals != nil && item.signals[signal] {
		return
	}
	addFingerprintSignal(item, osFamily, deviceClass, score, signal)
}

func applyDHCPFingerprint(item *fingerprintAccumulator, fp *logstore.DHCPFingerprint) {
	if item == nil || fp == nil {
		return
	}
	applyHostVendorFingerprint(item, fp.Hostname, fp.VendorClass, "")
	if fp.Hostname != "" {
		item.hostname = fp.Hostname
	}
	if fp.VendorClass != "" {
		item.vendor = fp.VendorClass
	}
	osFamily := fp.OSFamily
	deviceClass := fp.DeviceClass
	confidence := fp.Confidence
	signal := fp.Signal
	if osFamily == "" && deviceClass == "" && len(fp.RequestedOptions) > 0 {
		match := dhcpfingerprint.Infer(dhcpfingerprint.Fingerprint{
			MAC:              fp.MAC,
			Hostname:         fp.Hostname,
			VendorClass:      fp.VendorClass,
			RequestedOptions: fp.RequestedOptions,
			ObservedAt:       fp.ObservedAt,
			Source:           fp.Source,
		})
		osFamily = match.OSFamily
		deviceClass = match.DeviceClass
		confidence = match.Confidence
		signal = match.Signal
	}
	if osFamily == "" && deviceClass == "" {
		return
	}
	if confidence <= 0 {
		confidence = 75
	}
	if signal == "" {
		signal = "dhcp-fingerprint"
	}
	if fp.DeviceName != "" {
		signal += ":" + strings.ToLower(strings.ReplaceAll(fp.DeviceName, " ", "-"))
	}
	addFingerprintSignal(item, osFamily, deviceClass, confidence, signal)
}

func latestDHCPFingerprintByMAC(groups ...[]logstore.DHCPFingerprint) map[string]*logstore.DHCPFingerprint {
	out := map[string]*logstore.DHCPFingerprint{}
	for _, group := range groups {
		for _, fp := range group {
			mac := normalizeClientMAC(fp.MAC)
			if mac == "" {
				continue
			}
			current := out[mac]
			next := fp
			if current == nil || next.ObservedAt.After(current.ObservedAt) {
				out[mac] = &next
			}
		}
	}
	return out
}

func matchFingerprintToClient(rows map[string]*clientMutableEntry, ip string, fingerprint *fingerprintAccumulator) string {
	if fingerprint == nil {
		return ""
	}
	fp := fingerprint.result()
	if fp.Confidence < 60 || fp.OSFamily == "" {
		return ""
	}
	var matched string
	var samePrefixMatched string
	for key, row := range rows {
		if row.MAC == "" {
			continue
		}
		rowFP := inferClientFingerprint(row.ClientEntry, nil, nil)
		if rowFP.OSFamily != fp.OSFamily {
			continue
		}
		if fp.DeviceClass != "" && rowFP.DeviceClass != "" && fp.DeviceClass != rowFP.DeviceClass {
			continue
		}
		if clientHasSameIPv6Prefix(row.addresses, ip, 64) {
			if samePrefixMatched != "" {
				return ""
			}
			samePrefixMatched = key
			continue
		}
		if matched != "" {
			return ""
		}
		matched = key
	}
	if samePrefixMatched != "" {
		return samePrefixMatched
	}
	return matched
}

func clientHasSameIPv6Prefix(addresses map[string]bool, ip string, bits int) bool {
	addrText, _, _ := strings.Cut(strings.TrimSpace(ip), "/")
	addr, err := netip.ParseAddr(addrText)
	if err != nil || !addr.Is6() || addr.Is4In6() {
		return false
	}
	prefix := netip.PrefixFrom(addr, bits).Masked()
	for candidate := range addresses {
		candidateText, _, _ := strings.Cut(strings.TrimSpace(candidate), "/")
		other, err := netip.ParseAddr(candidateText)
		if err == nil && other.Is6() && !other.Is4In6() && prefix.Contains(other) {
			return true
		}
	}
	return false
}

func passiveCorrelationKey(fingerprint *fingerprintAccumulator, ip string) string {
	if fingerprint == nil {
		return ip
	}
	fp := fingerprint.result()
	if fp.Confidence >= 60 && fingerprint.hostname != "" {
		return "host:" + strings.ToLower(fingerprint.hostname)
	}
	return ip
}

func inferClientFingerprint(entry ClientEntry, passive map[string]*fingerprintAccumulator, dhcpFingerprint *logstore.DHCPFingerprint) clientFingerprint {
	acc := &fingerprintAccumulator{osScores: map[string]int{}, classScores: map[string]int{}, osClassScores: map[string]int{}, signals: map[string]bool{}, signalScores: map[string]fingerprintSignalScore{}}
	applyHostVendorFingerprint(acc, entry.Hostname, entry.Vendor, "")
	applyDHCPFingerprint(acc, dhcpFingerprint)
	for _, peer := range entry.Peers {
		applyDomainFingerprint(acc, peer)
	}
	for _, address := range entry.Addresses {
		if passive != nil {
			acc.merge(passive[address])
		}
	}
	return acc.result()
}

func (f *fingerprintAccumulator) merge(other *fingerprintAccumulator) {
	if f == nil || other == nil {
		return
	}
	if f.osScores == nil {
		f.osScores = map[string]int{}
	}
	if f.classScores == nil {
		f.classScores = map[string]int{}
	}
	if f.osClassScores == nil {
		f.osClassScores = map[string]int{}
	}
	if f.signals == nil {
		f.signals = map[string]bool{}
	}
	if f.signalScores == nil {
		f.signalScores = map[string]fingerprintSignalScore{}
	}
	if len(other.signalScores) > 0 {
		for signal, contribution := range other.signalScores {
			if f.signals[signal] {
				continue
			}
			addFingerprintSignal(f, contribution.OSFamily, contribution.DeviceClass, contribution.Score, signal)
		}
	} else {
		for key, value := range other.osScores {
			f.osScores[key] += value
		}
		for key, value := range other.classScores {
			f.classScores[key] += value
		}
		for key, value := range other.osClassScores {
			f.osClassScores[key] += value
		}
		for key := range other.signals {
			f.signals[key] = true
		}
	}
	f.hostname = firstNonEmptyString(f.hostname, other.hostname)
	f.vendor = firstNonEmptyString(f.vendor, other.vendor)
	f.hasMulticast = f.hasMulticast || other.hasMulticast
}

func (f *fingerprintAccumulator) result() clientFingerprint {
	if f == nil {
		return clientFingerprint{}
	}
	osFamily, osScore := bestFingerprintScore(f.osScores)
	deviceClass, classScore := f.bestDeviceClassForOS(osFamily)
	confidence := osScore
	if classScore > 0 {
		confidence += classScore / 3
	}
	if len(f.signals) > 1 {
		confidence += 10
	}
	if confidence > 100 {
		confidence = 100
	}
	if confidence < 25 {
		return clientFingerprint{}
	}
	return clientFingerprint{
		OSFamily:    osFamily,
		DeviceClass: deviceClass,
		Confidence:  confidence,
		Signals:     sortedSet(f.signals),
		Hostname:    f.hostname,
		Vendor:      f.vendor,
	}
}

func (f *fingerprintAccumulator) bestDeviceClassForOS(osFamily string) (string, int) {
	if osFamily != "" {
		filtered := map[string]int{}
		prefix := osFamily + "|"
		for key, value := range f.osClassScores {
			if strings.HasPrefix(key, prefix) {
				filtered[strings.TrimPrefix(key, prefix)] += value
			}
		}
		if len(filtered) > 0 {
			return bestFingerprintScore(filtered)
		}
	}
	return bestFingerprintScore(f.classScores)
}

func osClassScoreKey(osFamily, deviceClass string) string {
	return osFamily + "|" + deviceClass
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func bestFingerprintScore(scores map[string]int) (string, int) {
	var best string
	var bestScore int
	for key, score := range scores {
		if score > bestScore || (score == bestScore && key < best) {
			best = key
			bestScore = score
		}
	}
	return best, bestScore
}

func firewallClientAddress(entry logstore.FirewallLogEntry) string {
	if isLikelyClientAddress(entry.SrcAddress) {
		return entry.SrcAddress
	}
	if isLikelyClientAddress(entry.DstAddress) {
		return entry.DstAddress
	}
	return ""
}

func isLikelyClientAddress(address string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if addr.Is4() {
		return addr.IsPrivate()
	}
	return addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

func shortFingerprintSignal(name string) string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	parts := strings.Split(name, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return name
}

func sortedClientAddresses(values map[string]bool) []string {
	out := sortedSet(values)
	sort.SliceStable(out, func(i, j int) bool {
		return compareClientAddress(out[i], out[j]) < 0
	})
	return out
}

func compareClientAddress(a, b string) int {
	pa, erra := netip.ParseAddr(a)
	pb, errb := netip.ParseAddr(b)
	if erra != nil || errb != nil {
		return strings.Compare(a, b)
	}
	if pa.Is4() != pb.Is4() {
		if pa.Is4() {
			return -1
		}
		return 1
	}
	if pa.Is6() && pa.IsLinkLocalUnicast() != pb.IsLinkLocalUnicast() {
		if pa.IsLinkLocalUnicast() {
			return 1
		}
		return -1
	}
	return pa.Compare(pb)
}

func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func macVendor(mac string) string {
	oui := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(mac), "-", ":"))
	parts := strings.Split(oui, ":")
	if len(parts) < 3 {
		return ""
	}
	oui = strings.Join(parts[:3], ":")
	vendors := map[string]string{
		"00:01:4A": "Sony",
		"00:13:A9": "Sony",
		"00:16:B8": "Sony",
		"00:19:C5": "Sony",
		"00:1A:80": "Sony",
		"00:1D:0D": "Sony",
		"00:24:BE": "Sony",
		"00:50:F2": "Microsoft Xbox",
		"00:F6:20": "Google",
		"04:03:D6": "Nintendo",
		"04:5D:4B": "Sony",
		"0C:56:5C": "Nintendo",
		"18:2A:7B": "Nintendo",
		"18:74:2E": "Amazon",
		"18:EC:E7": "Panasonic",
		"20:16:D8": "Microsoft Xbox",
		"28:18:78": "Microsoft Xbox",
		"30:24:32": "Nintendo",
		"30:59:B7": "Microsoft Xbox",
		"30:75:12": "Sony",
		"34:AF:2C": "Nintendo",
		"3C:A9:AB": "Apple",
		"40:F4:07": "Nintendo",
		"44:65:0D": "Amazon",
		"48:A5:E7": "Nintendo",
		"48:D6:D5": "Google",
		"4E:20:15": "Apple private address",
		"50:1A:C5": "Microsoft Xbox",
		"50:F5:DA": "Amazon",
		"54:42:49": "Sony",
		"58:BD:A3": "Nintendo",
		"5C:BA:37": "Microsoft Xbox",
		"60:45:BD": "Microsoft Xbox",
		"60:6B:BD": "Sony",
		"64:E8:33": "EcoFlow",
		"68:54:FD": "Amazon",
		"70:48:F7": "Nintendo",
		"70:77:81": "Sony",
		"74:C2:46": "Amazon",
		"7C:1E:52": "Microsoft Xbox",
		"7C:BB:8A": "Nintendo",
		"7C:DD:E9": "ATOM tech Inc.",
		"80:81:9F": "Nintendo",
		"84:C7:EA": "Sony",
		"84:D6:D0": "Amazon",
		"88:71:E5": "Amazon",
		"8C:CD:E8": "Nintendo",
		"98:41:5C": "Nintendo",
		"98:5F:D3": "Microsoft Xbox",
		"A4:C0:E1": "Nintendo",
		"AC:63:BE": "Amazon",
		"AC:9B:0A": "Sony",
		"B4:52:7D": "Sony",
		"B8:68:70": "Apple",
		"B8:78:2E": "Nintendo",
		"CC:9E:00": "Nintendo",
		"D8:10:68": "Amazon",
		"D8:9D:67": "Microsoft Xbox",
		"E0:E7:51": "Nintendo",
		"E8:4E:CE": "Nintendo",
		"EC:FA:BC": "Espressif",
		"F0:BF:97": "Sony",
		"FC:A1:83": "Amazon",
	}
	if vendor, ok := vendors[oui]; ok {
		return vendor
	}
	return "OUI " + oui
}

func (h Handler) interfaceSummaries(resources []routerstate.ObjectStatus) []InterfaceSummary {
	if h.opts.Router == nil {
		return nil
	}
	statuses := map[string]map[string]any{}
	for _, resource := range resources {
		statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Name] = resource.Status
	}
	type zoneInfo struct {
		role string
		zone string
	}
	zones := map[string]zoneInfo{}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallZone" {
			continue
		}
		spec, err := resource.FirewallZoneSpec()
		if err != nil {
			continue
		}
		for _, ref := range spec.Interfaces {
			kind, name := splitResourceRef(ref)
			if kind == "" || name == "" {
				continue
			}
			zones[kind+"/"+name] = zoneInfo{role: spec.Role, zone: resource.Metadata.Name}
		}
	}
	addresses := interfaceConfiguredAddresses(h.opts.Router, statuses)
	var out []InterfaceSummary
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			continue
		}
		status := statuses[api.NetAPIVersion+"/Interface/"+resource.Metadata.Name]
		item := InterfaceSummary{
			Name:    resource.Metadata.Name,
			IfName:  spec.IfName,
			Phase:   stringFromMap(status, "phase"),
			Managed: spec.Managed,
			Owner:   spec.Owner,
		}
		if zone, ok := zones["Interface/"+resource.Metadata.Name]; ok {
			item.Role = zone.role
			item.Zone = zone.zone
		}
		if ifi, err := net.InterfaceByName(spec.IfName); err == nil {
			item.MTU = ifi.MTU
			item.Flags = ifi.Flags.String()
			item.HardwareAddress = ifi.HardwareAddr.String()
			if item.Phase == "" {
				if ifi.Flags&net.FlagUp != 0 {
					item.Phase = "Up"
				} else {
					item.Phase = "Down"
				}
			}
			if addrs, err := ifi.Addrs(); err == nil {
				for _, addr := range addrs {
					item.Addresses = appendUnique(item.Addresses, addr.String())
				}
			}
		}
		for _, addr := range addresses[resource.Metadata.Name] {
			item.Addresses = appendUnique(item.Addresses, addr)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		roleOrder := map[string]int{"untrust": 0, "trust": 1, "mgmt": 2}
		if roleOrder[out[i].Role] != roleOrder[out[j].Role] {
			return roleOrder[out[i].Role] < roleOrder[out[j].Role]
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func interfaceConfiguredAddresses(router *api.Router, statuses map[string]map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "IPv4StaticAddress":
			spec, err := resource.IPv4StaticAddressSpec()
			if err != nil {
				continue
			}
			addr := firstNonEmpty(stringFromMap(statuses[api.NetAPIVersion+"/IPv4StaticAddress/"+resource.Metadata.Name], "address"), spec.Address)
			if addr != "" {
				out[spec.Interface] = appendUnique(out[spec.Interface], addr)
			}
		case "IPv6DelegatedAddress":
			spec, err := resource.IPv6DelegatedAddressSpec()
			if err != nil {
				continue
			}
			addr := stringFromMap(statuses[api.NetAPIVersion+"/IPv6DelegatedAddress/"+resource.Metadata.Name], "address")
			if addr != "" {
				out[spec.Interface] = appendUnique(out[spec.Interface], addr)
			}
		case "DHCPv4Lease":
			iface, addr := addressStatusForInterface(resource, statuses)
			if iface != "" && addr != "" {
				out[iface] = appendUnique(out[iface], addr)
			}
		}
	}
	return out
}

func addressStatusForInterface(resource api.Resource, statuses map[string]map[string]any) (string, string) {
	status := statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name]
	iface := stringFromMap(status, "interface")
	addr := firstNonEmpty(stringFromMap(status, "address"), stringFromMap(status, "ip"))
	if iface != "" {
		return iface, addr
	}
	switch resource.Kind {
	case "DHCPv4Lease":
		spec, err := resource.DHCPv4LeaseSpec()
		if err == nil {
			return spec.Interface, addr
		}
	}
	return "", addr
}

func splitResourceRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	parts := strings.Split(ref, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSliceFromMap(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	switch value := values[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func normalizedIPAddrString(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (h Handler) basePath() string {
	base := h.opts.BasePath
	if base == "" {
		base = "/"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	if base != "/" {
		base = strings.TrimRight(base, "/") + "/"
	}
	return base
}

func phaseCounts(resources []routerstate.ObjectStatus) map[string]int {
	out := map[string]int{}
	for _, resource := range resources {
		phase := fmt.Sprint(resource.Status["phase"])
		if strings.TrimSpace(phase) == "" || phase == "<nil>" {
			phase = "Unknown"
		}
		out[phase]++
	}
	return out
}

func intQuery(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func signedIntQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < -1 {
		return fallback
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func boolQuery(r *http.Request, key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func atoiDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func parseConsoleDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func enrichTrafficFlowsWithDNS(flows []logstore.TrafficFlow, queries []logstore.DNSQuery) []logstore.TrafficFlow {
	if len(flows) == 0 || len(queries) == 0 {
		return flows
	}
	labels := map[string]string{}
	for _, query := range queries {
		name := strings.TrimSuffix(query.QuestionName, ".")
		if name == "" {
			continue
		}
		for _, answer := range query.Answers {
			answer = strings.TrimSpace(answer)
			if answer == "" {
				continue
			}
			if _, exists := labels[answer]; !exists {
				labels[answer] = name
			}
		}
	}
	for i := range flows {
		if strings.TrimSpace(flows[i].ResolvedHostname) == "" {
			flows[i].ResolvedHostname = labels[flows[i].PeerAddress]
		}
	}
	return flows
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func unifiedDiff(fromName, toName, fromText, toText string) string {
	if fromText == toText {
		return fmt.Sprintf("--- %s\n+++ %s\n", fromName, toName)
	}
	fromLines := splitDiffLines(fromText)
	toLines := splitDiffLines(toText)
	ops := diffLineOps(fromLines, toLines)
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", fromName)
	fmt.Fprintf(&b, "+++ %s\n", toName)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(fromLines), len(toLines))
	for _, op := range ops {
		b.WriteByte(op.prefix)
		b.WriteString(op.line)
		if !strings.HasSuffix(op.line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type diffLineOp struct {
	prefix byte
	line   string
}

func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func diffLineOps(a, b []string) []diffLineOp {
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffLineOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffLineOp{prefix: ' ', line: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffLineOp{prefix: '-', line: a[i]})
			i++
		default:
			ops = append(ops, diffLineOp{prefix: '+', line: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffLineOp{prefix: '-', line: a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffLineOp{prefix: '+', line: b[j]})
	}
	return ops
}

func SortResources(resources []routerstate.ObjectStatus) {
	sort.Slice(resources, func(i, j int) bool {
		a := resources[i].Kind + "/" + resources[i].Name
		b := resources[j].Kind + "/" + resources[j].Name
		return a < b
	})
}
