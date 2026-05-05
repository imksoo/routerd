package webconsole

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"net/http"
	"os"
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
	Events       []routerstate.StoredEvent   `json:"events"`
	Connections  *observe.ConnectionTable    `json:"connections,omitempty"`
	DNSQueries   []logstore.DNSQuery         `json:"dnsQueries,omitempty"`
	TrafficFlows []logstore.TrafficFlow      `json:"trafficFlows,omitempty"`
	FirewallLogs []logstore.FirewallLogEntry `json:"firewallLogs,omitempty"`
	Errors       []string                    `json:"errors,omitempty"`
}

type ConfigSnapshot struct {
	Path string `json:"path"`
	Text string `json:"text"`
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
	result := (*apply.Result)(nil)
	if h.opts.Result != nil {
		result = h.opts.Result()
	}
	return Snapshot{
		GeneratedAt:  time.Now().UTC(),
		Status:       controlapi.NewStatus(result),
		Phases:       phaseCounts(resources),
		Resources:    resources,
		Events:       events,
		Connections:  connections,
		DNSQueries:   dnsQueries,
		TrafficFlows: trafficFlows,
		FirewallLogs: firewallLogs,
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
