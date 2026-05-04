package webconsole

import (
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
	"routerd/pkg/observe"
	routerstate "routerd/pkg/state"
)

type Options struct {
	Router    *api.Router
	Store     routerstate.Store
	Result    func() *apply.Result
	NAPT      func(limit int) (*observe.NAPTTable, error)
	Title     string
	BasePath  string
	NAPTLimit int
}

type Handler struct {
	opts Options
}

type Snapshot struct {
	GeneratedAt time.Time                  `json:"generatedAt"`
	Status      controlapi.Status          `json:"status"`
	Phases      map[string]int             `json:"phases"`
	Resources   []routerstate.ObjectStatus `json:"resources"`
	Events      []routerstate.StoredEvent  `json:"events"`
	NAPT        *observe.NAPTTable         `json:"napt,omitempty"`
	Errors      []string                   `json:"errors,omitempty"`
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
	result := (*apply.Result)(nil)
	if h.opts.Result != nil {
		result = h.opts.Result()
	}
	status := controlapi.NewStatus(result)
	return Snapshot{
		GeneratedAt: time.Now().UTC(),
		Status:      status,
		Phases:      phaseCounts(resources),
		Resources:   resources,
		Events:      events,
		NAPT:        napt,
		Errors:      errors,
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
    main{padding:16px;display:grid;gap:16px}
    section{border:1px solid #303030;border-radius:8px;background:#181818;padding:14px;min-width:0;overflow:hidden}
    h2{font-size:15px;margin:0 0 10px}
    .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px}
    .metric{border:1px solid #2d2d2d;border-radius:6px;padding:10px;background:#202020}
    .metric b{display:block;font-size:20px;margin-top:4px}
    table{width:100%;border-collapse:collapse;display:block;overflow-x:auto;max-width:100%;-webkit-overflow-scrolling:touch}
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
  <section><h2>Resources</h2><div id="resources"></div></section>
  <section><h2>Events</h2><div id="events"></div></section>
</main>
<script>
const base = {{.BasePath}};
function cls(phase){return /Healthy|Applied|Active|Bound|Installed|Up/.test(phase) ? "ok" : /Pending|Drifted|Unknown/.test(phase) ? "warn" : "bad"}
function esc(v){return String(v ?? "").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function kv(label,value){return '<div class="metric"><span class="muted">'+esc(label)+'</span><b>'+esc(value)+'</b></div>'}
function table(headers, rows){return '<table><thead><tr>'+headers.map(h=>'<th>'+esc(h)+'</th>').join("")+'</tr></thead><tbody>'+rows.join("")+'</tbody></table>'}
async function refresh(){
  const res = await fetch(base + "api/summary?events=80&napt=30", {cache:"no-store"});
  const s = await res.json();
  const status = s.status?.status || {};
  const napt = s.napt || {};
  document.getElementById("overview").innerHTML = [
    kv("phase", status.phase || "Unknown"),
    kv("generation", status.generation || "-"),
    kv("resources", status.resourceCount || (s.resources||[]).length),
    kv("conntrack", napt.max ? String(napt.count)+"/"+String(napt.max) : (napt.count ?? "-"))
  ].join("");
  document.getElementById("traffic").innerHTML = table(["proto","state","src","dst","timeout"], (napt.entries||[]).slice(0,30).map(e =>
    '<tr><td>'+esc(e.protocol)+'</td><td>'+esc(e.state)+'</td><td><code>'+esc(e.original?.source)+':'+esc(e.original?.sourcePort)+'</code></td><td><code>'+esc(e.original?.destination)+':'+esc(e.original?.destinationPort)+'</code></td><td>'+esc(e.timeout)+'</td></tr>'));
  const important = (s.resources||[]).filter(r => /EgressRoutePolicy|HealthCheck|DNSResolver|DHCP|DSLiteTunnel|NAT44Rule|IPv4Route|Firewall|WireGuard|VXLAN/.test(r.kind));
  document.getElementById("resources").innerHTML = table(["kind","name","phase","detail"], important.slice(0,80).map(r => {
    const st = r.status || {};
    const detail = ["selectedCandidate","selectedDevice","activeEgressInterface","target","address","currentPrefix"].map(k => st[k] ? k+"="+st[k] : "").filter(Boolean).join(" ");
    return '<tr><td>'+esc(r.kind)+'</td><td>'+esc(r.name)+'</td><td class="'+cls(st.phase||"Unknown")+'">'+esc(st.phase||"Unknown")+'</td><td><code>'+esc(detail)+'</code></td></tr>';
  }));
  document.getElementById("events").innerHTML = table(["time","severity","topic","resource","message"], (s.events||[]).slice(0,80).map(e =>
    '<tr><td>'+esc(e.createdAt)+'</td><td>'+esc(e.severity||"")+'</td><td><code>'+esc(e.topic||e.type)+'</code></td><td>'+esc((e.resourceKind||e.kind||"") + "/" + (e.resourceName||e.name||""))+'</td><td>'+esc(e.reason||e.message||"")+'</td></tr>'));
}
refresh(); setInterval(refresh, 5000);
</script>
</body>
</html>`))
