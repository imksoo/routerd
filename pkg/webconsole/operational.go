// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h Handler) operationalStatus(w http.ResponseWriter, kind string) {
	resources, err := h.operationalResources(kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, OperationalStatus{GeneratedAt: time.Now().UTC(), Kind: kind, Resources: h.resourceRows(resources)})
}

func (h Handler) routes(w http.ResponseWriter, r *http.Request) {
	status := h.routesStatus(r.Context())
	writeJSON(w, status)
}

func (h Handler) operationalPage(w http.ResponseWriter, kind string) {
	resources, err := h.operationalResources(kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	page := operationalHTMLPage(h.opts.Title, kind, h.resourceRows(resources))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func operationalHTMLPage(title, kind string, resources []ResourceStatus) string {
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
<nav><a href="./">Summary</a><a href="bgp">BGP</a><a href="vrrp">VRRP</a><a href="ingress">Ingress</a></nav>
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

func operationalPageScript(kind string) string {
	return `(function(){
const view=` + strconv.Quote(kind) + `;
const apiBase="./api/v1/";
const streamURL="./api/events/stream";
const resourceKinds={bgp:["BGPRouter","BGPPeer"],vrrp:["VirtualAddress"],ingress:["IngressService"]}[view]||[];
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
let refreshRequest;
function esc(value){return String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));}
function status(value,key){return value&&value.status?value.status[key]:undefined;}
function list(value){return Array.isArray(value)?value:[];}
async function json(path,signal){const r=await fetch(path,{cache:"no-store",signal});if(!r.ok)throw new Error(path+": "+r.status);return r.json();}
function setLive(text){if(state)state.textContent=text;}
function schedule(delay){clearTimeout(refreshTimer);refreshTimer=setTimeout(refreshAll,delay);}
async function refreshAll(){const owner=new AbortController();if(refreshRequest)refreshRequest.abort();refreshRequest=owner;try{const [status,log]=await Promise.all([json(apiBase+view,owner.signal),json(apiBase+"events?limit=200",owner.signal)]);if(refreshRequest!==owner)return;renderStatus(status);events=log||[];renderEvents();setLive("Live updates");}catch(e){if(!owner.signal.aborted&&refreshRequest===owner)setLive("Polling fallback");}finally{if(refreshRequest===owner)refreshRequest=undefined;}}
function renderStatus(payload){const resources=payload.resources||[];if(updated)updated.textContent=payload.generatedAt?"Updated "+new Date(payload.generatedAt).toLocaleString():"";content.innerHTML=renderTables(resources);appendSample(resources);renderMetrics();}
function renderTables(resources){if(view==="bgp")return renderBGP(resources);if(view==="vrrp")return renderVRRP(resources);if(view==="ingress")return renderIngress(resources);return "<p>Unknown view.</p>";}
function resourceLabel(row,text){return row.declared===false?(text||"-")+" (retained status; not declared)":text;}
function count(value){return typeof value==="number"&&Number.isFinite(value)?value:null;}
function pair(a,b){return (count(a)??"-")+"/"+(count(b)??"-");}
function sum(values){return values.some(v=>v===null)?null:values.reduce((a,b)=>a+b,0);}
function rows(cells){return "<tr>"+cells.map(v=>"<td>"+esc(v===undefined||v===null||v===""?"-":v)+"</td>").join("")+"</tr>";}
function renderBGP(resources){let routers=resources.filter(r=>r.kind==="BGPRouter"),out="<section><table><thead><tr><th>Router</th><th>Phase</th><th>Peers</th><th>Prefixes</th><th>Observed</th></tr></thead><tbody>";for(const r of routers){const peers=list(status(r,"peers"));out+=rows([resourceLabel(r,r.name),status(r,"phase"),pair(status(r,"establishedPeers"),Array.isArray(status(r,"peers"))?peers.length:null),status(r,"acceptedPrefixes"),status(r,"observedAt")]);}out+="</tbody></table></section><section><table><thead><tr><th>Peer</th><th>ASN</th><th>State</th><th>Messages</th><th>Prefixes</th><th>Last Error</th></tr></thead><tbody>";for(const r of routers){for(const p of list(status(r,"peers"))){out+=rows([p.address,p.asn,p.state,pair(p.messagesReceived,p.messagesSent),p.prefixesReceived,p.lastErrorReason]);}}return out+"</tbody></table></section>";}
function renderVRRP(resources){let out="<section><table><thead><tr><th>VIP</th><th>Hostname</th><th>Role</th><th>Priority</th><th>Interface</th><th>VRID</th><th>Last Transition</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="VirtualAddress")){out+=rows([resourceLabel(r,status(r,"address")),status(r,"hostname"),status(r,"role")||"unknown",pair(status(r,"priority"),status(r,"basePriority")),status(r,"interface"),status(r,"virtualRouterID"),status(r,"lastRoleTransitionAt")]);}out+="</tbody></table></section><section><table><thead><tr><th>VIP</th><th>Track</th><th>State</th><th>Penalty</th><th>Unhealthy</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="VirtualAddress")){for(const t of list(status(r,"track"))){out+=rows([r.name,t.resource,t.state,t.penalty,t.unhealthyConsecutive]);}}return out+"</tbody></table></section>";}
function backendAddress(b){return b.port?(b.resolvedAddress||b.address||"")+":"+b.port:(b.resolvedAddress||b.address||"");}
function renderIngress(resources){let out="<section><table><thead><tr><th>Service</th><th>Hostname</th><th>Phase</th><th>Active Backend</th><th>Health</th><th>Selection</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="IngressService")){const a=status(r,"activeBackend")||{};out+=rows([resourceLabel(r,r.name),status(r,"hostname"),status(r,"phase"),(a.name||"-")+" / "+backendAddress(a),pair(status(r,"healthyBackends"),status(r,"totalBackends")),status(r,"selection")]);}out+="</tbody></table></section><section><table><thead><tr><th>Service</th><th>Backend</th><th>Address</th><th>State</th><th>Counts</th><th>Last Healthy</th><th>Last Unhealthy</th></tr></thead><tbody>";for(const r of resources.filter(r=>r.kind==="IngressService")){for(const b of list(status(r,"backends"))){out+=rows([r.name,b.name,backendAddress(b),b.healthy===true?"Healthy":b.healthy===false?"Unhealthy":"Unknown",pair(b.healthyCount,b.unhealthyCount),b.lastHealthyAt,b.lastUnhealthyAt]);}}return out+"</tbody></table></section>";}
function sample(resources){const time=new Date().toISOString();if(view==="bgp"){const routers=resources.filter(r=>r.kind==="BGPRouter");return {time,a:sum(routers.map(r=>count(status(r,"establishedPeers")))),b:sum(routers.map(r=>Array.isArray(status(r,"peers"))?status(r,"peers").length:null)),c:sum(routers.map(r=>count(status(r,"acceptedPrefixes")))),labels:["established peers","total peers","accepted prefixes"]};}if(view==="vrrp"){let master=0,backup=0,unknown=0;for(const r of resources.filter(r=>r.kind==="VirtualAddress")){const role=String(status(r,"role")||"").toLowerCase();if(role==="master")master++;else if(role==="backup")backup++;else unknown++;}return {time,a:master,b:backup,c:unknown,labels:["master","backup","unknown"]};}const services=resources.filter(r=>r.kind==="IngressService");return {time,a:sum(services.map(r=>count(status(r,"healthyBackends")))),b:sum(services.map(r=>count(status(r,"totalBackends")))),c:sum(services.map(r=>{const backend=status(r,"activeBackend");return backend&&backend.name?1:count(status(r,"totalBackends"))!==null?0:null;})),labels:["healthy backends","total backends","active services"]};}
function metricKey(){return "routerd:operational-metrics:"+view;}
function loadSamples(){try{const rows=JSON.parse(localStorage.getItem(metricKey())||"[]");return Array.isArray(rows)?rows.filter(s=>s&&typeof s.time==="string"&&Array.isArray(s.labels)&&s.labels.length===3).slice(-720):[];}catch{return [];}}
function saveSamples(samples){try{localStorage.setItem(metricKey(),JSON.stringify(samples.slice(-720)));}catch{}}
function appendSample(resources){const samples=loadSamples();const next=sample(resources);const last=samples[samples.length-1];if(!last||Date.parse(next.time)-Date.parse(last.time)>=9000||last.a!==next.a||last.b!==next.b||last.c!==next.c){samples.push(next);saveSamples(samples);}}
function renderMetrics(){const samples=trimSamples(loadSamples());const last=samples[samples.length-1]||{a:null,b:null,c:null,labels:["a","b","c"]};metrics.innerHTML=last.labels.map((l,i)=>'<div class="metric-card"><span class="muted">'+esc(l)+'</span><span class="metric-value">'+esc(count(last[["a","b","c"][i]])??"not observed")+'</span></div>').join("");drawChart(samples,last.labels);}
function trimSamples(samples){const minutes=Number(range.value||15);const cutoff=Date.now()-minutes*60000;return samples.filter(s=>Date.parse(s.time)>=cutoff);}
function points(samples,key,max){let drawing=false;return samples.map((s,i)=>{const value=count(s[key]);if(value===null){drawing=false;return "";}const x=samples.length<2?150:(i/(samples.length-1))*280+10;const y=112-(value/Math.max(1,max))*96;const point=(drawing?"L":"M")+x.toFixed(1)+","+y.toFixed(1);drawing=true;return point;}).join(" ");}
function drawChart(samples,labels){const max=Math.max(1,...samples.flatMap(s=>[s.a||0,s.b||0,s.c||0]));const series=[["a","#60cdff"],["b","#54b054"],["c","#f7b955"]];chart.innerHTML='<line x1="10" y1="112" x2="290" y2="112" stroke="#667" stroke-width="1"/>'+series.map(([k,c])=>'<path fill="none" stroke="'+c+'" stroke-width="2.4" d="'+points(samples,k,max)+'"/>').join("")+labels.map((l,i)=>'<text x="'+(12+i*92)+'" y="14" fill="'+series[i][1]+'" font-size="10">'+esc(l)+'</text>').join("");}
function eventMatches(e){const kind=(e.resourceKind||e.kind||"");const name=(e.resourceName||e.name||"");if(resourceKinds.length&&resourceKinds.indexOf(kind)<0)return false;if(eventKind.value&&kind.toLowerCase().indexOf(eventKind.value.toLowerCase())<0)return false;if(eventResource.value&&name.toLowerCase().indexOf(eventResource.value.toLowerCase())<0)return false;if(eventSearch.value){const text=JSON.stringify(e).toLowerCase();if(text.indexOf(eventSearch.value.toLowerCase())<0)return false;}return true;}
function renderEvents(){eventLog.innerHTML=events.filter(eventMatches).slice(0,200).map(e=>rows([e.createdAt?new Date(e.createdAt).toLocaleString():"",e.resourceKind||e.kind,e.resourceName||e.name,e.severity,e.message||e.reason||e.type])).join("")||'<tr><td colspan="5" class="muted">No matching events</td></tr>';}
[eventKind,eventResource,eventSearch,range].forEach(el=>el&&el.addEventListener("input",()=>{renderEvents();renderMetrics();}));
if(window.EventSource){const source=new EventSource(streamURL);source.addEventListener("connected",()=>setLive("Live updates"));source.addEventListener("routerd-event",event=>{try{const e=JSON.parse(event.data);const resource=e.resource||{};if(resourceKinds.indexOf(resource.kind)>=0)schedule(150);events.unshift({createdAt:e.time,type:e.type,reason:e.reason,message:e.message,severity:e.severity,resourceKind:resource.kind,resourceName:resource.name,attributes:e.attributes});events=events.slice(0,200);renderEvents();}catch{schedule(500);}});source.onerror=()=>setLive("Polling fallback");}else{setLive("Polling fallback");}
setInterval(refreshAll,30000);
refreshAll();
})();`
}

func writeBGPHTML(body *strings.Builder, resources []ResourceStatus) {
	body.WriteString(`<section><table><thead><tr><th>Router</th><th>Phase</th><th>Peers</th><th>Prefixes</th><th>Observed</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		peers := statusList(resource.Status["peers"])
		peerCount := "-"
		if resource.Status["peers"] != nil {
			peerCount = strconv.Itoa(len(peers))
		}
		writeHTMLRow(body, []string{
			operationalResourceLabel(resource, resource.Name),
			statusText(resource.Status, "phase"),
			statusCountPair(resource.Status["establishedPeers"], peerCount),
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
				statusCountPair(peer["messagesReceived"], peer["messagesSent"]),
				statusAnyText(peer["prefixesReceived"]),
				defaultConsoleText(statusAnyText(peer["lastErrorReason"]), "-"),
			})
		}
	}
	body.WriteString(`</tbody></table></section>`)
}

func writeVRRPHTML(body *strings.Builder, resources []ResourceStatus) {
	body.WriteString(`<section><table><thead><tr><th>VIP</th><th>Hostname</th><th>Role</th><th>Priority</th><th>Interface</th><th>VRID</th><th>Last Transition</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "VirtualAddress" {
			continue
		}
		writeHTMLRow(body, []string{
			operationalResourceLabel(resource, statusText(resource.Status, "address")),
			defaultConsoleText(statusText(resource.Status, "hostname"), "-"),
			defaultConsoleText(statusText(resource.Status, "role"), "unknown"),
			statusCountPair(resource.Status["priority"], resource.Status["basePriority"]),
			statusText(resource.Status, "interface"),
			statusText(resource.Status, "virtualRouterID"),
			statusText(resource.Status, "lastRoleTransitionAt"),
		})
	}
	body.WriteString(`</tbody></table></section>`)
	body.WriteString(`<section><table><thead><tr><th>VIP</th><th>Track</th><th>State</th><th>Penalty</th><th>Unhealthy</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "VirtualAddress" {
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

func writeIngressHTML(body *strings.Builder, resources []ResourceStatus) {
	body.WriteString(`<section><table><thead><tr><th>Service</th><th>Hostname</th><th>Phase</th><th>Active Backend</th><th>Health</th><th>Selection</th></tr></thead><tbody>`)
	for _, resource := range resources {
		if resource.Kind != "IngressService" {
			continue
		}
		active := statusObject(resource.Status["activeBackend"])
		writeHTMLRow(body, []string{
			operationalResourceLabel(resource, resource.Name),
			defaultConsoleText(statusText(resource.Status, "hostname"), "-"),
			statusText(resource.Status, "phase"),
			activeBackendHTMLText(active),
			statusCountPair(resource.Status["healthyBackends"], resource.Status["totalBackends"]),
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
			state := "Unknown"
			if healthy, ok := statusBoolValue(backend["healthy"]); ok {
				if healthy {
					state = "Healthy"
				} else {
					state = "Unhealthy"
				}
			}
			writeHTMLRow(body, []string{
				resource.Name,
				statusAnyText(backend["name"]),
				backendHTMLAddress(backend),
				state,
				statusCountPair(backend["healthyCount"], backend["unhealthyCount"]),
				statusAnyText(backend["lastHealthyAt"]),
				statusAnyText(backend["lastUnhealthyAt"]),
			})
		}
	}
	body.WriteString(`</tbody></table></section>`)
}

func operationalResourceLabel(resource ResourceStatus, label string) string {
	if resource.Declared != nil && !*resource.Declared {
		return defaultConsoleText(label, "-") + " (retained status; not declared)"
	}
	return label
}

func statusCountPair(first, second any) string {
	return defaultConsoleText(statusAnyText(first), "-") + "/" + defaultConsoleText(statusAnyText(second), "-")
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
