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
	"time"

	"routerd/pkg/api"
	"routerd/pkg/apply"
	"routerd/pkg/controlapi"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
	"routerd/pkg/platform"
	routerstate "routerd/pkg/state"
)

type Options struct {
	Router             *api.Router
	Store              routerstate.Store
	Result             func() *apply.Result
	Connections        func(limit int) (*observe.ConnectionTable, error)
	VPNStatus          func() (VPNStatus, error)
	Title              string
	BasePath           string
	ConnectionsLimit   int
	DNSQueryLogPath    string
	TrafficFlowLogPath string
	FirewallLogPath    string
	DHCPLeasePaths     []string
	ConfigPath         string
}

type Handler struct {
	opts Options
}

type Snapshot struct {
	GeneratedAt  time.Time                   `json:"generatedAt"`
	Status       controlapi.Status           `json:"status"`
	Phases       map[string]int              `json:"phases"`
	Resources    []routerstate.ObjectStatus  `json:"resources"`
	Interfaces   []InterfaceSummary          `json:"interfaces,omitempty"`
	Events       []routerstate.StoredEvent   `json:"events"`
	Connections  *observe.ConnectionTable    `json:"connections,omitempty"`
	DNSQueries   []logstore.DNSQuery         `json:"dnsQueries,omitempty"`
	TrafficFlows []logstore.TrafficFlow      `json:"trafficFlows,omitempty"`
	FirewallLogs []logstore.FirewallLogEntry `json:"firewallLogs,omitempty"`
	DHCPLeases   []DHCPLease                 `json:"dhcpLeases,omitempty"`
	Neighbors    []NeighborEntry             `json:"neighbors,omitempty"`
	Clients      []ClientEntry               `json:"clients,omitempty"`
	VPN          VPNStatus                   `json:"vpn,omitempty"`
	Errors       []string                    `json:"errors,omitempty"`
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
	ID        string   `json:"id"`
	Hostname  string   `json:"hostname,omitempty"`
	MAC       string   `json:"mac,omitempty"`
	Vendor    string   `json:"vendor,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	State     string   `json:"state,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	Peers     []string `json:"peers,omitempty"`
	BytesOut  int64    `json:"bytesOut,omitempty"`
	BytesIn   int64    `json:"bytesIn,omitempty"`
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
	BackendState   string                `json:"backendState,omitempty"`
	HostName       string                `json:"hostName,omitempty"`
	DNSName        string                `json:"dnsName,omitempty"`
	TailscaleIPs   []string              `json:"tailscaleIPs,omitempty"`
	AllowedIPs     []string              `json:"allowedIPs,omitempty"`
	Online         bool                  `json:"online,omitempty"`
	Active         bool                  `json:"active,omitempty"`
	ExitNode       bool                  `json:"exitNode,omitempty"`
	ExitNodeOption bool                  `json:"exitNodeOption,omitempty"`
	Peers          []TailscalePeerStatus `json:"peers,omitempty"`
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
	return Handler{opts: opts}
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
	case "api/v1/events":
		h.events(w, r)
	case "api/v1/connections":
		h.connections(w, r)
	case "api/v1/dns-queries":
		h.dnsQueries(w, r)
	case "api/v1/traffic-flows":
		h.trafficFlows(w, r)
	case "api/v1/firewall-logs":
		h.firewallLogs(w, r)
	case "api/v1/clients":
		h.clients(w)
	case "api/v1/vpn":
		h.vpn(w)
	case "api/v1/config":
		h.config(w)
	case "api/v1/generations":
		h.generations(w, r)
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

func (h Handler) Snapshot(limit int, connectionsLimit int) Snapshot {
	var errors []string
	resources, err := h.resourceStatuses()
	if err != nil {
		errors = append(errors, err.Error())
	}
	events, err := h.eventList(limit)
	if err != nil {
		errors = append(errors, err.Error())
	}
	var connections *observe.ConnectionTable
	if h.opts.Connections != nil && connectionsLimit >= 0 {
		connections, err = h.opts.Connections(connectionsLimit)
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	dnsQueries, err := h.queryLogList(logstore.DNSQueryFilter{Since: time.Now().Add(-time.Hour), Limit: 200})
	if err != nil {
		errors = append(errors, err.Error())
	}
	trafficFlows, err := h.trafficFlowList(logstore.TrafficFlowFilter{Since: time.Now().Add(-time.Hour), Limit: 200})
	if err != nil {
		errors = append(errors, err.Error())
	}
	trafficFlows = enrichTrafficFlowsWithDNS(trafficFlows, dnsQueries)
	firewallLogs, err := h.firewallLogList(logstore.FirewallLogFilter{Since: time.Now().Add(-24 * time.Hour), Action: "drop", Limit: 200})
	if err != nil {
		errors = append(errors, err.Error())
	}
	dhcpLeases, err := h.dhcpLeaseList()
	if err != nil {
		errors = append(errors, err.Error())
	}
	neighbors, err := neighborList()
	if err != nil {
		errors = append(errors, err.Error())
	}
	vpn, err := h.vpnStatus()
	if err != nil {
		errors = append(errors, err.Error())
	}
	errors = append(errors, vpn.Errors...)
	result := (*apply.Result)(nil)
	if h.opts.Result != nil {
		result = h.opts.Result()
	}
	result = resultWithLatestGeneration(result, h.opts.Store)
	return Snapshot{
		GeneratedAt:  time.Now().UTC(),
		Status:       controlapi.NewStatus(result),
		Phases:       phaseCounts(resources),
		Resources:    resources,
		Interfaces:   h.interfaceSummaries(resources),
		Events:       events,
		Connections:  connections,
		DNSQueries:   dnsQueries,
		TrafficFlows: trafficFlows,
		FirewallLogs: firewallLogs,
		DHCPLeases:   dhcpLeases,
		Neighbors:    neighbors,
		Clients:      correlateClients(dhcpLeases, neighbors, trafficFlows),
		VPN:          vpn,
		Errors:       errors,
	}
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
	writeJSON(w, h.Snapshot(intQuery(r, "events", 25), intQuery(r, "connections", h.opts.ConnectionsLimit)))
}

func (h Handler) resources(w http.ResponseWriter) {
	resources, err := h.resourceStatuses()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, resources)
}

func (h Handler) events(w http.ResponseWriter, r *http.Request) {
	events, err := h.eventList(intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, events)
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
	writeJSON(w, rows)
}

func (h Handler) clients(w http.ResponseWriter) {
	leases, err := h.dhcpLeaseList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	neighbors, err := neighborList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	flows, err := h.trafficFlowList(logstore.TrafficFlowFilter{Since: time.Now().Add(-time.Hour), Limit: 200})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, correlateClients(leases, neighbors, flows))
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
	if strings.Contains(name, "/") {
		return name
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, dir := range []string{"/usr/local/bin", "/usr/local/sbin", "/usr/bin", "/usr/sbin", "/bin", "/sbin"} {
		candidate := dir + "/" + name
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return name
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
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	var raw struct {
		BackendState string                             `json:"BackendState"`
		Self         tailscalePeerStatusJSON            `json:"Self"`
		Peer         map[string]tailscalePeerStatusJSON `json:"Peer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	status := &TailscaleStatus{
		BackendState:   raw.BackendState,
		HostName:       raw.Self.HostName,
		DNSName:        raw.Self.DNSName,
		TailscaleIPs:   raw.Self.TailscaleIPs,
		AllowedIPs:     raw.Self.AllowedIPs,
		Online:         raw.Self.Online,
		Active:         raw.Self.Active,
		ExitNode:       raw.Self.ExitNode,
		ExitNodeOption: raw.Self.ExitNodeOption,
	}
	for id, peer := range raw.Peer {
		status.Peers = append(status.Peers, TailscalePeerStatus{
			ID:             id,
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
	sort.Slice(status.Peers, func(i, j int) bool {
		left, right := status.Peers[i], status.Peers[j]
		if left.Online != right.Online {
			return left.Online
		}
		if left.Active != right.Active {
			return left.Active
		}
		if lastSeenAfter(left.LastSeen, right.LastSeen) {
			return true
		}
		if lastSeenAfter(right.LastSeen, left.LastSeen) {
			return false
		}
		return strings.ToLower(left.HostName) < strings.ToLower(right.HostName)
	})
	return status, nil
}

type tailscalePeerStatusJSON struct {
	HostName       string   `json:"HostName"`
	DNSName        string   `json:"DNSName"`
	TailscaleIPs   []string `json:"TailscaleIPs"`
	AllowedIPs     []string `json:"AllowedIPs"`
	Online         bool     `json:"Online"`
	Active         bool     `json:"Active"`
	ExitNode       bool     `json:"ExitNode"`
	ExitNodeOption bool     `json:"ExitNodeOption"`
	Relay          string   `json:"Relay"`
	LastSeen       string   `json:"LastSeen"`
	RxBytes        int64    `json:"RxBytes"`
	TxBytes        int64    `json:"TxBytes"`
}

func lastSeenAfter(left, right string) bool {
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil {
		return left != "" && right == ""
	}
	return leftTime.After(rightTime)
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
		return lister.ListObjectStatuses()
	}
	return nil, nil
}

func (h Handler) eventList(limit int) ([]routerstate.StoredEvent, error) {
	if lister, ok := h.opts.Store.(routerstate.EventLister); ok {
		return lister.ListEvents(routerstate.EventQuery{Limit: limit})
	}
	return nil, nil
}

func (h Handler) queryLogList(filter logstore.DNSQueryFilter) ([]logstore.DNSQuery, error) {
	if strings.TrimSpace(h.opts.DNSQueryLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenDNSQueryLog(h.opts.DNSQueryLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func (h Handler) trafficFlowList(filter logstore.TrafficFlowFilter) ([]logstore.TrafficFlow, error) {
	if strings.TrimSpace(h.opts.TrafficFlowLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenTrafficFlowLog(h.opts.TrafficFlowLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func (h Handler) firewallLogList(filter logstore.FirewallLogFilter) ([]logstore.FirewallLogEntry, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func (h Handler) dhcpLeaseList() ([]DHCPLease, error) {
	seen := map[string]DHCPLease{}
	for _, path := range h.opts.DHCPLeasePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		leases, err := readDnsmasqLeases(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, lease := range leases {
			key := lease.IP
			if key == "" {
				key = lease.MAC
			}
			if key == "" {
				continue
			}
			seen[key] = lease
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

func correlateClients(leases []DHCPLease, neighbors []NeighborEntry, flows []logstore.TrafficFlow) []ClientEntry {
	type mutableClient struct {
		ClientEntry
		addresses map[string]bool
		sources   map[string]bool
		peers     map[string]bool
	}
	rows := map[string]*mutableClient{}
	ipToKey := map[string]string{}
	upsert := func(key, address string) *mutableClient {
		key = strings.TrimSpace(key)
		if key == "" {
			key = strings.TrimSpace(address)
		}
		if key == "" {
			key = "-"
		}
		row := rows[key]
		if row == nil {
			row = &mutableClient{
				ClientEntry: ClientEntry{ID: key},
				addresses:   map[string]bool{},
				sources:     map[string]bool{},
				peers:       map[string]bool{},
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
	for _, flow := range flows {
		ip := strings.TrimSpace(flow.ClientAddress)
		if ip == "" {
			continue
		}
		key := ipToKey[ip]
		if key == "" {
			key = ip
		}
		row := upsert(key, ip)
		if flow.Accounting {
			row.BytesOut += flow.BytesOut
			row.BytesIn += flow.BytesIn
		}
		peer := firstNonEmptyString(flow.ResolvedHostname, flow.TLSSNI, flow.PeerAddress)
		if peer != "" {
			row.peers[peer] = true
		}
		row.sources["traffic"] = true
	}
	out := make([]ClientEntry, 0, len(rows))
	for _, row := range rows {
		row.Addresses = sortedClientAddresses(row.addresses)
		row.Sources = sortedSet(row.sources)
		row.Peers = sortedSet(row.peers)
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
		"00:F6:20": "Google",
		"18:EC:E7": "Panasonic",
		"3C:A9:AB": "Apple",
		"48:D6:D5": "Google",
		"4E:20:15": "Apple private address",
		"64:E8:33": "EcoFlow",
		"7C:DD:E9": "ATOM tech Inc.",
		"B8:68:70": "Apple",
		"D8:10:68": "Amazon",
		"EC:FA:BC": "Espressif",
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
		case "DHCPv4Address", "DHCPv4Lease":
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
	case "DHCPv4Address":
		spec, err := resource.DHCPv4AddressSpec()
		if err == nil {
			return spec.Interface, addr
		}
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
