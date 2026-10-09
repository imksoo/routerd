// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"embed"
	"html"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/observe"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type Options struct {
	Router                 *api.Router
	Store                  routerstate.Store
	Result                 func() *apply.Result
	Connections            func(limit int) (*observe.ConnectionTable, error)
	VPNStatus              func() (VPNStatus, error)
	Title                  string
	BasePath               string
	ConsoleLinks           []ConsoleLink
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
	// ReverseLookup resolves an address to names. Implementations MUST honor
	// ctx cancellation / deadline: lookupMany runs these in a bounded worker
	// pool and waits for every dispatched lookup to return, so a lookup that
	// ignores ctx can stall the pool's drain.
	ReverseLookup func(ctx context.Context, address string) ([]string, error)
}

type Handler struct {
	opts        Options
	reverseDNS  *reverseDNSCache
	systemUsage *systemUsageSampler
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
		defaults, features := platform.Current()
		opts.DHCPLeasePaths = platform.DnsmasqLeaseCandidates(defaults, features)
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
	case "api/v1/dns-queries/aggregate":
		h.dnsQueriesAggregate(w, r)
	case "api/v1/traffic-flows":
		h.trafficFlows(w, r)
	case "api/v1/traffic-flows/aggregate":
		h.trafficFlowsAggregate(w, r)
	case "api/v1/firewall-logs":
		h.firewallLogs(w, r)
	case "api/v1/firewall/deny-timeline":
		h.firewallDenyTimeline(w, r)
	case "api/v1/clients":
		h.clients(w, r)
	case "api/v1/vpn":
		h.vpn(w)
	case "api/v1/routes":
		h.routes(w)
	case "api/v1/sam":
		h.sam(w)
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

func cleanConsoleLinks(links []ConsoleLink) []ConsoleLink {
	cleaned := make([]ConsoleLink, 0, len(links))
	for _, link := range links {
		label := strings.TrimSpace(link.Label)
		url := strings.TrimSpace(link.URL)
		if label == "" || url == "" {
			continue
		}
		cleaned = append(cleaned, ConsoleLink{
			Label:       label,
			URL:         url,
			Description: strings.TrimSpace(link.Description),
		})
	}
	return cleaned
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
