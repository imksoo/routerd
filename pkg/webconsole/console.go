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
	NAPT               func(limit int) (*observe.NAPTTable, error)
	Title              string
	BasePath           string
	NAPTLimit          int
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
	NAPT         *observe.NAPTTable          `json:"napt,omitempty"`
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
	if opts.NAPTLimit == 0 {
		opts.NAPTLimit = 40
	}
	if opts.NAPT == nil {
		opts.NAPT = observe.NAPT
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
	case "api/napt":
		h.napt(w, r)
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

func (h Handler) Snapshot(limit int, naptLimit int) Snapshot {
	var errors []string
	resources, err := h.resourceStatuses()
	if err != nil {
		errors = append(errors, err.Error())
	}
	events, err := h.eventList(limit)
	if err != nil {
		errors = append(errors, err.Error())
	}
	var napt *observe.NAPTTable
	if h.opts.NAPT != nil && naptLimit >= 0 {
		napt, err = h.opts.NAPT(naptLimit)
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
		NAPT:         napt,
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
	naptLimit := intQuery(r, "napt", h.opts.NAPTLimit)
	writeJSON(w, h.Snapshot(limit, naptLimit))
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

func (h Handler) napt(w http.ResponseWriter, r *http.Request) {
	if h.opts.NAPT == nil {
		writeError(w, http.StatusNotImplemented, "napt observer is unavailable")
		return
	}
	table, err := h.opts.NAPT(intQuery(r, "limit", h.opts.NAPTLimit))
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
    .proto-tcp{border-color:#3977d4;background:#152846;color:#8ab4ff}.proto-udp{border-color:#3b8b65;background:#102d22;color:#7ee787}.proto-icmp{border-color:#9b6fd3;background:#2c2142;color:#d2a8ff}
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
  <section><h2>Overview</h2><div class="grid" id="overview"></div></section>
  <section><h2>Traffic</h2><div id="traffic"></div></section>
  <section><h2>Client Traffic</h2><div id="client-traffic"></div></section>
  <section><h2>Recent Deny</h2><div id="recent-deny"></div></section>
  <section><h2>Resources</h2><div id="resources"></div></section>
  <section><h2>Events</h2><div id="events"></div></section>
</main>
<script>
const base = {{.BasePath}};
const seen = {traffic:new Map(), resources:new Map(), events:new Map()};
let firstPaint = true;
function cls(phase){return /Healthy|Applied|Active|Bound|Installed|Up/.test(phase) ? "ok" : /Pending|Drifted|Unknown/.test(phase) ? "warn" : "bad"}
function esc(v){return String(v ?? "").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function kv(label,value){return '<div class="metric"><span class="muted">'+esc(label)+'</span><b>'+esc(value)+'</b></div>'}
function table(headers, rows){return '<div class="table-wrap"><table><thead><tr>'+headers.map(h=>'<th>'+esc(h)+'</th>').join("")+'</tr></thead><tbody>'+rows.join("")+'</tbody></table></div>'}
function token(v){return String(v || "unknown").toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, "") || "unknown"}
function pill(value, prefix){return '<span class="pill '+prefix+'-'+token(value)+'">'+esc(value || "-")+'</span>'}
function dstLabel(tuple, labels){
  return labels?.[tuple?.destination] || "";
}
function endpoint(tuple){
  return '<code class="addr">'+esc(tuple?.source)+':'+esc(tuple?.sourcePort)+' → '+esc(tuple?.destination)+':'+esc(tuple?.destinationPort)+'</code>';
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
function hostPort(tuple, side){
  const host = tuple?.[side] || "";
  const port = side === "source" ? tuple?.sourcePort : tuple?.destinationPort;
  return host ? host + (port ? ":" + port : "") : "";
}
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
  if (!hasTuple(e.reply)) return "";
  const delta = natDelta(e);
  const rows = [
    '<div><span>reply</span>'+endpoint(e.reply)+'</div>',
    delta ? '<div><span>nat</span><code class="addr">'+esc(delta)+'</code></div>' : ''
  ].join("");
  return '<details class="flow-cell return-toggle"><summary class="flow-summary">'+endpoint(e.original)+'<span class="return-button">return</span></summary><div class="return-detail">'+rows+'</div></details>';
}
function flowCell(e){
  return returnDetails(e) || '<div class="flow-cell">'+endpoint(e.original)+'</div>';
}
async function refresh(){
  const res = await fetch(base + "api/summary?events=15&napt=30", {cache:"no-store"});
  const s = await res.json();
  const status = s.status?.status || {};
  const napt = s.napt || {};
  const dnsLabels = dnsLabelMap(s.dnsQueries || []);
  document.getElementById("overview").innerHTML = [
    kv("phase", status.phase || "Unknown"),
    kv("generation", status.generation || "-"),
    kv("resources", status.resourceCount || (s.resources||[]).length),
    kv("conntrack", napt.max ? String(napt.count)+"/"+String(napt.max) : (napt.count ?? "-"))
  ].join("");
  document.getElementById("traffic").innerHTML = table(["proto","state","flow","dst label","timeout"], (napt.entries||[]).slice(0,30).map(e => {
    const state = e.state || (e.assured ? "ASSURED" : "stateless");
    const changed = remember(seen.traffic, flowKey(e), flowSig(e));
    const label = dstLabel(e.original, dnsLabels);
    return '<tr'+rowClass(changed)+'><td>'+pill(e.protocol, "proto")+'</td><td>'+pill(state, "state")+'</td><td>'+flowCell(e)+'</td><td><span class="dst-label">'+esc(label || "-")+'</span></td><td>'+esc(e.timeout)+'s</td></tr>';
  }));
  document.getElementById("client-traffic").innerHTML = table(["client","bytes out","bytes in","recent peers"], clientTrafficRows(s.trafficFlows || []).map(row =>
    '<tr><td><code>'+esc(row.client)+'</code></td><td>'+bytes(row.bytesOut)+'</td><td>'+bytes(row.bytesIn)+'</td><td><code>'+esc(Array.from(row.peers).sort().slice(0,4).join(", "))+'</code></td></tr>'));
  document.getElementById("recent-deny").innerHTML = table(["count","source","destination","proto","rule"], denyRows(s.firewallLogs || []).map(row =>
    '<tr><td>'+esc(row.count)+'</td><td><code>'+esc(row.src)+'</code></td><td><code>'+esc(row.dst)+'</code></td><td>'+pill(row.proto, "proto")+'</td><td>'+esc(row.rule)+'</td></tr>'));
  const important = (s.resources||[]).filter(r => /EgressRoutePolicy|HealthCheck|DNSResolver|DHCP|DSLiteTunnel|NAT44Rule|IPv4Route|Firewall|WireGuard|VXLAN/.test(r.kind));
  document.getElementById("resources").innerHTML = table(["kind","name","phase","detail"], important.slice(0,80).map(r => {
    const st = r.status || {};
    const detail = ["selectedCandidate","selectedDevice","activeEgressInterface","target","address","currentPrefix"].map(k => st[k] ? k+"="+st[k] : "").filter(Boolean).join(" ");
    const key = r.apiVersion + "/" + r.kind + "/" + r.name;
    const changed = remember(seen.resources, key, JSON.stringify(st));
    return '<tr'+rowClass(changed)+'><td>'+esc(r.kind)+'</td><td>'+esc(r.name)+'</td><td class="'+cls(st.phase||"Unknown")+'">'+esc(st.phase||"Unknown")+'</td><td><code>'+esc(detail)+'</code></td></tr>';
  }));
  document.getElementById("events").innerHTML = table(["time","severity","topic","resource","message"], (s.events||[]).slice(0,15).map(e =>
    '<tr'+rowClass(remember(seen.events, String(e.id || e.createdAt || e.topic), JSON.stringify(e)))+'><td>'+esc(e.createdAt)+'</td><td>'+esc(e.severity||"")+'</td><td><code>'+esc(e.topic||e.type)+'</code></td><td>'+esc((e.resourceKind||e.kind||"") + "/" + (e.resourceName||e.name||""))+'</td><td>'+esc(e.reason||e.message||"")+'</td></tr>'));
  firstPaint = false;
}
refresh(); setInterval(refresh, 5000);
</script>
</body>
</html>`))
