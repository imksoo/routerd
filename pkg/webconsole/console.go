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
	routerstate "routerd/pkg/state"
)

type Options struct {
	Router             *api.Router
	Store              routerstate.Store
	Result             func() *apply.Result
	Connections        func(limit int) (*observe.ConnectionTable, error)
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
	Errors       []string                    `json:"errors,omitempty"`
}

type ConfigSnapshot struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type DHCPLease struct {
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	MAC       string    `json:"mac"`
	IP        string    `json:"ip"`
	Hostname  string    `json:"hostname,omitempty"`
	ClientID  string    `json:"clientId,omitempty"`
	Vendor    string    `json:"vendor,omitempty"`
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
	case "api/v1/config":
		h.config(w)
	default:
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
	result := (*apply.Result)(nil)
	if h.opts.Result != nil {
		result = h.opts.Result()
	}
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
		Errors:       errors,
	}
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

func neighborList() ([]NeighborEntry, error) {
	out, err := exec.Command("ip", "-j", "neigh", "show").Output()
	if err != nil {
		return nil, err
	}
	return parseIPNeighborJSON(out)
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
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func SortResources(resources []routerstate.ObjectStatus) {
	sort.Slice(resources, func(i, j int) bool {
		a := resources[i].Kind + "/" + resources[i].Name
		b := resources[j].Kind + "/" + resources[j].Name
		return a < b
	})
}
