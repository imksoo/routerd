package webconsole

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
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
	if path == "" || path == "/" || path == "index.html" {
		h.index(w, r)
		return
	}
	switch strings.TrimPrefix(path, "/") {
	case "api/summary":
		h.summary(w, r)
	case "api/resources":
		h.resources(w, r)
	case "api/events":
		h.events(w, r)
	case "api/connections":
		h.connections(w, r)
	case "api/dns-queries":
		h.dnsQueries(w, r)
	case "api/traffic-flows":
		h.trafficFlows(w, r)
	case "api/firewall-logs":
		h.firewallLogs(w, r)
	default:
		http.NotFound(w, r)
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
	status := controlapi.NewStatus(result)
	return Snapshot{
		GeneratedAt:  time.Now().UTC(),
		Status:       status,
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

func (h Handler) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTemplate.Execute(w, map[string]string{
		"Title":    h.opts.Title,
		"BasePath": h.basePath(),
	})
}

func (h Handler) summary(w http.ResponseWriter, r *http.Request) {
	limit := intQuery(r, "events", 25)
	connectionsLimit := intQuery(r, "connections", h.opts.ConnectionsLimit)
	writeJSON(w, h.Snapshot(limit, connectionsLimit))
}

func (h Handler) resources(w http.ResponseWriter, r *http.Request) {
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

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root{color-scheme:dark;background:#111;color:#e8e8e8;font:14px/1.45 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    *{box-sizing:border-box}
    body{margin:0;background:#111;color:#e8e8e8;overflow-x:hidden}
    header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 18px;border-bottom:1px solid #303030;background:#181818;position:sticky;top:0;z-index:1;max-width:100vw}
    h1{font-size:18px;margin:0;font-weight:650;min-width:0;max-width:calc(100vw - 120px);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    main{padding:16px;display:grid;gap:16px;max-width:1280px;margin:0 auto}
    section{border:1px solid #303030;border-radius:8px;background:#181818;padding:14px;min-width:0;overflow:hidden}
    h2{font-size:15px;margin:0 0 10px}
    .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px}
    .metric{border:1px solid #2d2d2d;border-radius:6px;padding:10px;background:#202020}
    .metric b{display:block;font-size:20px;margin-top:4px}
    .table-wrap{width:100%;overflow-x:auto;-webkit-overflow-scrolling:touch}
    .connection-groups{display:grid;gap:10px}
    .connection-group{border:1px solid #2b2b2b;border-radius:6px;background:#151515;overflow:hidden}
    .connection-group summary{display:flex;align-items:center;gap:8px;padding:9px 10px;cursor:pointer;list-style:none}
    .connection-group summary::-webkit-details-marker{display:none}
    .connection-group summary:before{content:"+";display:inline-grid;place-items:center;width:16px;height:16px;border:1px solid #4a4a4a;border-radius:50%;font-size:12px;color:#c9c9c9;flex:0 0 auto}
    .connection-group[open] summary:before{content:"-"}
    .connection-group .table-wrap{border-top:1px solid #2b2b2b}
    .group-title{font-weight:650;white-space:nowrap}
    .group-count{margin-left:auto;color:#9a9a9a;font-size:12px;white-space:nowrap}
    .flow-cell{display:grid;gap:5px;min-width:0}
    .flow-summary{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;align-items:center;list-style:none;min-width:0}
    .flow-summary::-webkit-details-marker{display:none}
    .return-detail{display:grid;gap:3px;margin-top:4px}
    .return-detail div{display:grid;grid-template-columns:74px minmax(0,1fr);gap:6px;align-items:baseline}
    .return-detail span{font-size:11px;color:#9a9a9a;text-transform:uppercase}
    .return-toggle summary{cursor:pointer}
    .return-button{color:#9a9a9a;font-size:12px;display:inline-flex;align-items:center;gap:4px;white-space:nowrap}
    .return-button:before{content:"+";display:inline-grid;place-items:center;width:14px;height:14px;border:1px solid #4a4a4a;border-radius:50%;font-size:11px;color:#c9c9c9}
    .return-toggle[open] .return-button:before{content:"-"}
    .addr{white-space:nowrap;word-break:normal}
    .dst-label{display:block;color:#9a9a9a;font-size:12px;margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .pill{display:inline-flex;align-items:center;border-radius:999px;padding:2px 8px;font-size:12px;font-weight:650;border:1px solid #3a3a3a;background:#282828;color:#ddd;white-space:nowrap}
    .family-ipv4{border-color:#3977d4;background:#152846;color:#8ab4ff}.family-ipv6{border-color:#9b6fd3;background:#2c2142;color:#d2a8ff}
    .proto-tcp{border-color:#3977d4;background:#152846;color:#8ab4ff}.proto-udp{border-color:#3b8b65;background:#102d22;color:#7ee787}.proto-icmp,.proto-icmpv6,.proto-ipv6_icmp{border-color:#9b6fd3;background:#2c2142;color:#d2a8ff}
    .state-established,.state-assured{border-color:#3b8b65;background:#102d22;color:#7ee787}.state-syn_sent,.state-unreplied{border-color:#997b2f;background:#342a12;color:#f2cc60}.state-time_wait,.state-close{border-color:#7b7b7b;background:#242424;color:#c9c9c9}
    tr.flash, .metric.flash{animation:flash 1.6s ease-out}
    @keyframes flash{0%{background:#3a320f;box-shadow:inset 3px 0 #f2cc60}100%{background:transparent;box-shadow:inset 0 0 transparent}}
    table{width:100%;border-collapse:collapse;min-width:440px}
    th,td{text-align:left;border-bottom:1px solid #2b2b2b;padding:7px 6px;vertical-align:top}
    th{font-size:12px;color:#aaa;font-weight:600}
    code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;word-break:break-word}
    .ok{color:#7ee787}.warn{color:#f2cc60}.bad{color:#ff7b72}.muted{color:#9a9a9a;flex:0 0 auto}
    @media (max-width:640px){
      header{padding:10px 12px}
      h1{font-size:16px;max-width:calc(100vw - 96px)}
      main{padding:10px;gap:10px}
      section{padding:10px}
      .grid{grid-template-columns:repeat(auto-fit,minmax(130px,1fr))}
      .metric b{font-size:18px}
      th,td{padding:6px 5px}
    }
  </style>
</head>
<body>
<header><h1>{{.Title}}</h1><span class="muted">read-only</span></header>
<main>
  <section><h2>Overview</h2><div id="overview"></div></section>
  <section><h2>Connections</h2><div id="traffic"></div></section>
  <section><h2>Client Traffic</h2><div id="client-traffic"></div></section>
  <section><h2>Recent Deny</h2><div id="recent-deny"></div></section>
  <section><h2>Resources</h2><div id="resources"></div></section>
  <section><h2>Events</h2><div id="events"></div></section>
</main>
<script>
const base = {{.BasePath}};
const seen = {traffic:new Map(), resources:new Map(), events:new Map()};
const connectionGroupOpen = new Map();
let firstPaint = true;
let refreshSeq = 0;
function cls(phase){return /Healthy|Applied|Active|Bound|Installed|Up/.test(phase) ? "ok" : /Pending|Drifted|Unknown/.test(phase) ? "warn" : "bad"}
function text(v){return String(v ?? "")}
function clear(el){while(el.firstChild) el.removeChild(el.firstChild)}
function el(tag, attrs, children){
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value === null || value === undefined || value === false) continue;
    if (key === "class") node.className = value;
    else if (key === "text") node.textContent = text(value);
    else if (value === true) node.setAttribute(key, "");
    else node.setAttribute(key, value);
  }
  for (const child of children || []) node.append(child instanceof Node ? child : document.createTextNode(text(child)));
  return node;
}
function renderInto(id, node){const target=document.getElementById(id); clear(target); target.append(node)}
function kvNode(label,value){return el("div",{class:"metric"},[el("span",{class:"muted",text:label}),el("b",{text:value})])}
function tableNode(headers, rows){
  const thead = el("thead",{},[el("tr",{},headers.map(h=>el("th",{text:h})))]);
  const tbody = el("tbody",{},rows);
  return el("div",{class:"table-wrap"},[el("table",{},[thead,tbody])]);
}
function token(v){return String(v || "unknown").toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, "") || "unknown"}
function pill(value, prefix){return el("span",{class:"pill "+prefix+"-"+token(value),text:value || "-"})}
function dstLabel(tuple, labels){
  return labels?.[tuple?.destination] || "";
}
function hostPort(tuple, side){
  const host = tuple?.[side] || "";
  const port = side === "source" ? tuple?.sourcePort : tuple?.destinationPort;
  return host ? host + (port ? ":" + port : "") : "";
}
function endpoint(tuple){
  return el("code",{class:"addr",text:hostPort(tuple,"source")+" -> "+hostPort(tuple,"destination")});
}
function dnsLabelMap(rows){
  const labels = {};
  for (const row of rows || []) {
    for (const answer of row.answers || []) if (!labels[answer]) labels[answer] = row.questionName;
  }
  return labels;
}
function clientTrafficRows(flows){
  const totals = new Map();
  for (const flow of flows || []) {
    const key = flow.clientAddress || "-";
    const current = totals.get(key) || {client:key, bytesOut:0, bytesIn:0, peers:new Set()};
    current.bytesOut += Number(flow.bytesOut || 0);
    current.bytesIn += Number(flow.bytesIn || 0);
    const peer = flow.resolvedHostname || flow.tlsSNI || flow.peerAddress;
    if (peer) current.peers.add(peer);
    totals.set(key, current);
  }
  return Array.from(totals.values()).sort((a,b)=>a.client.localeCompare(b.client)).slice(0,10);
}
function bytes(v){return v ? String(v) : "-"}
function denyRows(logs){
  const totals = new Map();
  for (const row of logs || []) {
    const key = (row.srcAddress || "-") + ">" + (row.dstAddress || "-");
    const current = totals.get(key) || {src:row.srcAddress || "-", dst:row.dstAddress || "-", count:0, proto:row.protocol || "-", rule:row.ruleName || "-"};
    current.count++;
    totals.set(key, current);
  }
  return Array.from(totals.values()).sort((a,b)=>b.count-a.count || a.src.localeCompare(b.src)).slice(0,10);
}
function connectionFamilyCounts(connections){
  const counts = {ipv4:0, ipv6:0, other:0};
  const byFamily = connections?.byFamily || {};
  if (Object.keys(byFamily).length) {
    for (const [familyKey, value] of Object.entries(byFamily)) {
      const family = String(familyKey || "").toLowerCase();
      if (family === "ipv4") counts.ipv4 += Number(value || 0);
      else if (family === "ipv6") counts.ipv6 += Number(value || 0);
      else counts.other += Number(value || 0);
    }
  } else {
    for (const entry of connections?.entries || []) {
      const family = String(entry.family || "").toLowerCase();
      if (family === "ipv4") counts.ipv4++;
      else if (family === "ipv6") counts.ipv6++;
      else counts.other++;
    }
  }
  const parts = ["v4 "+counts.ipv4, "v6 "+counts.ipv6];
  if (counts.other) parts.push("other "+counts.other);
  return parts.join(" / ");
}
function connectionGroupKey(e){
  const family = String(e.family || "other").toLowerCase();
  const proto = String(e.protocol || "other").toLowerCase().replace(/[^a-z0-9]+/g, "_") || "other";
  return family + "/" + proto;
}
function connectionGroupLabel(key){
  const [family, proto] = key.split("/");
  const fam = family === "ipv4" ? "IPv4" : family === "ipv6" ? "IPv6" : "Other";
  return {family:fam, proto:proto || "other"};
}
function connectionGroups(entries){
  const groups = new Map();
  for (const entry of entries || []) {
    const key = connectionGroupKey(entry);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(entry);
  }
  const order = {ipv4:0, ipv6:1, other:2, tcp:0, udp:1, icmp:2, icmpv6:3, ipv6_icmp:3, gre:4, esp:5, other:9};
  return Array.from(groups.entries()).sort((a,b)=>{
    const [af,ap] = a[0].split("/");
    const [bf,bp] = b[0].split("/");
    return (order[af] ?? 9) - (order[bf] ?? 9) || (order[ap] ?? 9) - (order[bp] ?? 9) || a[0].localeCompare(b[0]);
  }).map(([key, rows])=>({key, rows}));
}
function remember(bucket, key, value){
  const previous = bucket.get(key);
  bucket.set(key, value);
  return !firstPaint && previous !== value;
}
function rowClass(changed){return changed ? ' class="flash"' : ''}
function flowKey(e){return [e.family,e.protocol,e.state,e.original?.source,e.original?.sourcePort,e.original?.destination,e.original?.destinationPort,e.reply?.source,e.reply?.sourcePort,e.reply?.destination,e.reply?.destinationPort,e.mark].join("|")}
function flowSig(e){return JSON.stringify([e.state,e.assured,e.original,e.reply,e.mark])}
function sameReverse(original, reply){
  return reply?.source === original?.destination && reply?.destination === original?.source &&
    String(reply?.sourcePort || "") === String(original?.destinationPort || "") &&
    String(reply?.destinationPort || "") === String(original?.sourcePort || "");
}
function hasTuple(tuple){return !!(tuple?.source || tuple?.destination || tuple?.sourcePort || tuple?.destinationPort)}
function natDelta(e){
  if (!hasTuple(e.reply) || sameReverse(e.original, e.reply)) return "";
  const out = [];
  const replyDst = hostPort(e.reply, "destination");
  const originalSrc = hostPort(e.original, "source");
  if (replyDst && replyDst !== originalSrc) out.push("reply dst " + replyDst);
  const replySrc = hostPort(e.reply, "source");
  const originalDst = hostPort(e.original, "destination");
  if (replySrc && replySrc !== originalDst) out.push("reply src " + replySrc);
  return out.join(" / ");
}
function returnDetails(e){
  if (!hasTuple(e.reply)) return null;
  const delta = natDelta(e);
  const detailRows = [el("div",{},[el("span",{text:"reply"}),endpoint(e.reply)])];
  if (delta) detailRows.push(el("div",{},[el("span",{text:"nat"}),el("code",{class:"addr",text:delta})]));
  return el("details",{class:"flow-cell return-toggle"},[
    el("summary",{class:"flow-summary"},[endpoint(e.original),el("span",{class:"return-button",text:"return"})]),
    el("div",{class:"return-detail"},detailRows),
  ]);
}
function flowCell(e){
  return returnDetails(e) || el("div",{class:"flow-cell"},[endpoint(e.original)]);
}
function connectionGroupNode(group, dnsLabels){
  const label = connectionGroupLabel(group.key);
  const title = label.family + "/" + String(label.proto || "other").toUpperCase() + " " + String(group.rows.length);
  const rows = group.rows.map(e => {
    const state = e.state || (e.assured ? "ASSURED" : "stateless");
    const changed = remember(seen.traffic, flowKey(e), flowSig(e));
    const dst = dstLabel(e.original, dnsLabels);
    return el("tr", changed ? {class:"flash"} : {}, [
      el("td",{},[pill(state, "state")]),
      el("td",{},[flowCell(e)]),
      el("td",{},[el("span",{class:"dst-label",text:dst || "-"})]),
      el("td",{text:String(e.timeout || 0)+"s"}),
    ]);
  });
  const open = connectionGroupOpen.has(group.key) ? connectionGroupOpen.get(group.key) : false;
  const node = el("details",{class:"connection-group",open:open},[
    el("summary",{},[el("span",{class:"group-title",text:title}),pill(label.family, "family"),pill(label.proto, "proto")]),
    tableNode(["state","flow","dst label","timeout"], rows),
  ]);
  node.addEventListener("toggle", () => connectionGroupOpen.set(group.key, node.open));
  return node;
}
async function refresh(){
  const seq = ++refreshSeq;
  const res = await fetch(base + "api/summary?events=15&connections=200", {cache:"no-store"});
  const s = await res.json();
  if (seq !== refreshSeq) return;
  const status = s.status?.status || {};
  const connections = s.connections || {};
  const dnsLabels = dnsLabelMap(s.dnsQueries || []);
  renderInto("overview", el("div",{class:"grid"},[
    kvNode("phase", status.phase || "Unknown"),
    kvNode("generation", status.generation || "-"),
    kvNode("resources", status.resourceCount || (s.resources||[]).length),
    kvNode("conntrack", connections.max ? String(connections.count)+"/"+String(connections.max) : (connections.count ?? "-")),
    kvNode("families", connectionFamilyCounts(connections)),
  ]));
  const groups = connectionGroups(connections.entries || []);
  renderInto("traffic", groups.length ? el("div",{class:"connection-groups"},groups.map(group=>connectionGroupNode(group,dnsLabels))) : el("div",{class:"muted",text:"No active connections"}));
  renderInto("client-traffic", tableNode(["client","bytes out","bytes in","recent peers"], clientTrafficRows(s.trafficFlows || []).map(row =>
    el("tr",{},[el("td",{},[el("code",{text:row.client})]),el("td",{text:bytes(row.bytesOut)}),el("td",{text:bytes(row.bytesIn)}),el("td",{},[el("code",{text:Array.from(row.peers).sort().slice(0,4).join(", ")})])]))));
  renderInto("recent-deny", tableNode(["count","source","destination","proto","rule"], denyRows(s.firewallLogs || []).map(row =>
    el("tr",{},[el("td",{text:row.count}),el("td",{},[el("code",{text:row.src})]),el("td",{},[el("code",{text:row.dst})]),el("td",{},[pill(row.proto, "proto")]),el("td",{text:row.rule})]))));
  const important = (s.resources||[]).filter(r => /EgressRoutePolicy|HealthCheck|DNSResolver|DHCP|DSLiteTunnel|NAT44Rule|IPv4Route|Firewall|WireGuard|VXLAN/.test(r.kind));
  renderInto("resources", tableNode(["kind","name","phase","detail"], important.slice(0,80).map(r => {
    const st = r.status || {};
    const detail = ["selectedCandidate","selectedDevice","activeEgressInterface","target","address","currentPrefix"].map(k => st[k] ? k+"="+st[k] : "").filter(Boolean).join(" ");
    const key = r.apiVersion + "/" + r.kind + "/" + r.name;
    const changed = remember(seen.resources, key, JSON.stringify(st));
    return el("tr", changed ? {class:"flash"} : {}, [el("td",{text:r.kind}),el("td",{text:r.name}),el("td",{class:cls(st.phase||"Unknown"),text:st.phase||"Unknown"}),el("td",{},[el("code",{text:detail})])]);
  })));
  renderInto("events", tableNode(["time","severity","topic","resource","message"], (s.events||[]).slice(0,15).map(e =>
    el("tr", remember(seen.events, String(e.id || e.createdAt || e.topic), JSON.stringify(e)) ? {class:"flash"} : {}, [
      el("td",{text:e.createdAt}),
      el("td",{text:e.severity||""}),
      el("td",{},[el("code",{text:e.topic||e.type})]),
      el("td",{text:(e.resourceKind||e.kind||"") + "/" + (e.resourceName||e.name||"")}),
      el("td",{text:e.reason||e.message||""}),
    ]))));
  firstPaint = false;
}
refresh(); setInterval(refresh, 5000);
</script>
</body>
</html>`))
