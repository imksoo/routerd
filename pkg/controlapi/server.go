// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	Prefix              = "/api/control.routerd.net/v1alpha1"
	maxRequestBodyBytes = 1 << 20
	maxConnectionRows   = 5000
)

type Handler struct {
	Status                   func(*http.Request) (*Status, error)
	Controllers              func(*http.Request) (*Controllers, error)
	Runtime                  func(*http.Request) (*RuntimeStats, error)
	Connections              func(*http.Request, ConnectionsRequest) (*ConnectionTable, error)
	DNSQueries               func(*http.Request, DNSQueriesRequest) (*DNSQueries, error)
	DNSQueriesAggregate      func(*http.Request, DNSQueriesRequest) (*DNSQueriesAggregate, error)
	TrafficFlows             func(*http.Request, TrafficFlowsRequest) (*TrafficFlows, error)
	TrafficFlowsAggregate    func(*http.Request, TrafficFlowsRequest) (*TrafficFlowsAggregate, error)
	FirewallLogs             func(*http.Request, FirewallLogsRequest) (*FirewallLogs, error)
	Get                      func(*http.Request, GetRequest) (*GetResult, error)
	Describe                 func(*http.Request, DescribeRequest) (*DescribeResult, error)
	Probe                    func(*http.Request, ProbeRequest) (*ProbeResult, error)
	Apply                    func(*http.Request, ApplyRequest) (*ApplyResult, error)
	Plan                     func(*http.Request, PlanRequest) (*PlanResult, error)
	Delete                   func(*http.Request, DeleteRequest) (*DeleteResult, error)
	Validate                 func(*http.Request, ValidateRequest) (*ValidateResult, error)
	SubmitSAMEnrollmentClaim func(*http.Request, SAMEnrollmentClaimSubmitRequest) (*SAMEnrollmentClaimSubmitResult, error)
	SetLogLevel              func(*http.Request, LogLevelRequest) (*LogLevelResult, error)
	DHCPv6Event              func(*http.Request, DHCPv6EventRequest) (*DHCPv6EventResult, error)
	DHCPLeaseEvent           func(*http.Request, DHCPLeaseEventRequest) (*DHCPLeaseEventResult, error)
}

type ConnectionsRequest struct {
	Limit int
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/status":
		h.handleStatus(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/controllers":
		h.handleControllers(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/runtime":
		h.handleRuntime(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/connections":
		h.handleConnections(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/dns-queries":
		h.handleDNSQueries(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/dns-queries/aggregate":
		h.handleDNSQueriesAggregate(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/traffic-flows":
		h.handleTrafficFlows(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/traffic-flows/aggregate":
		h.handleTrafficFlowsAggregate(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/firewall-logs":
		h.handleFirewallLogs(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/get":
		h.handleGet(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/describe":
		h.handleDescribe(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/probe":
		h.handleProbe(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/apply":
		h.handleApply(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/plan":
		h.handlePlan(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/delete":
		h.handleDelete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/validate":
		h.handleValidate(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/sam-enrollment-claims":
		h.handleSubmitSAMEnrollmentClaim(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/log-level":
		h.handleSetLogLevel(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/dhcpv6-event":
		h.handleDHCPv6Event(w, r)
	case r.Method == http.MethodPost && (r.URL.Path == Prefix+"/dhcp-lease-event" || r.URL.Path == "/v1/events/dhcp"):
		h.handleDHCPLeaseEvent(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	if h.Get == nil {
		writeError(w, http.StatusNotImplemented, "get handler is not configured")
		return
	}
	q := r.URL.Query()
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		limit = parsed
	}
	eventsLimit := 10
	if raw := q.Get("events-limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "events-limit must be a non-negative integer")
			return
		}
		eventsLimit = parsed
	}
	var sinceID int64
	if raw := q.Get("since-id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "since-id must be a non-negative integer")
			return
		}
		sinceID = parsed
	}
	result, err := h.Get(r, GetRequest{
		Subject:     q.Get("subject"),
		EventsLimit: eventsLimit,
		Limit:       limit,
		SinceID:     sinceID,
		Topic:       q.Get("topic"),
		Resource:    q.Get("resource"),
		KindFilter:  q.Get("kind"),
		NameFilter:  q.Get("name"),
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleDescribe(w http.ResponseWriter, r *http.Request) {
	if h.Describe == nil {
		writeError(w, http.StatusNotImplemented, "describe handler is not configured")
		return
	}
	eventsLimit := 10
	if raw := r.URL.Query().Get("events-limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "events-limit must be a non-negative integer")
			return
		}
		eventsLimit = parsed
	}
	result, err := h.Describe(r, DescribeRequest{Target: r.URL.Query().Get("target"), EventsLimit: eventsLimit})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleProbe(w http.ResponseWriter, r *http.Request) {
	if h.Probe == nil {
		writeError(w, http.StatusNotImplemented, "probe handler is not configured")
		return
	}
	result, err := h.Probe(r, ProbeRequest{Subject: r.URL.Query().Get("subject"), Target: r.URL.Query().Get("target")})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleDHCPLeaseEvent(w http.ResponseWriter, r *http.Request) {
	if h.DHCPLeaseEvent == nil {
		writeError(w, http.StatusNotImplemented, "dhcp lease event handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DHCPLeaseEventRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "DHCPLeaseEvent" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.DHCPLeaseEvent(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if h.Status == nil {
		writeError(w, http.StatusNotImplemented, "status handler is not configured")
		return
	}
	status, err := h.Status(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h Handler) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if h.Runtime == nil {
		writeError(w, http.StatusNotImplemented, "runtime stats handler is not configured")
		return
	}
	stats, err := h.Runtime(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h Handler) handleControllers(w http.ResponseWriter, r *http.Request) {
	if h.Controllers == nil {
		writeError(w, http.StatusNotImplemented, "controllers handler is not configured")
		return
	}
	controllers, err := h.Controllers(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, controllers)
}

func (h Handler) handleConnections(w http.ResponseWriter, r *http.Request) {
	if h.Connections == nil {
		writeError(w, http.StatusNotImplemented, "connections handler is not configured")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		if parsed > maxConnectionRows {
			parsed = maxConnectionRows
		}
		limit = parsed
	}
	table, err := h.Connections(r, ConnectionsRequest{Limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, table)
}

func (h Handler) handleDNSQueries(w http.ResponseWriter, r *http.Request) {
	if h.DNSQueries == nil {
		writeError(w, http.StatusNotImplemented, "dns query log handler is not configured")
		return
	}
	req, ok := buildDNSQueriesRequest(w, r)
	if !ok {
		return
	}
	// Allow ?agg=1 to route through the aggregate handler if configured.
	if r.URL.Query().Get("agg") == "1" && h.DNSQueriesAggregate != nil {
		agg, err := h.DNSQueriesAggregate(r, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, agg)
		return
	}
	rows, err := h.DNSQueries(r, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h Handler) handleDNSQueriesAggregate(w http.ResponseWriter, r *http.Request) {
	if h.DNSQueriesAggregate == nil {
		writeError(w, http.StatusNotImplemented, "dns query aggregate handler is not configured")
		return
	}
	req, ok := buildDNSQueriesRequest(w, r)
	if !ok {
		return
	}
	agg, err := h.DNSQueriesAggregate(r, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, agg)
}

func (h Handler) handleTrafficFlows(w http.ResponseWriter, r *http.Request) {
	if h.TrafficFlows == nil {
		writeError(w, http.StatusNotImplemented, "traffic flow log handler is not configured")
		return
	}
	req, ok := buildTrafficFlowsRequest(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("agg") == "1" && h.TrafficFlowsAggregate != nil {
		agg, err := h.TrafficFlowsAggregate(r, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, agg)
		return
	}
	rows, err := h.TrafficFlows(r, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h Handler) handleTrafficFlowsAggregate(w http.ResponseWriter, r *http.Request) {
	if h.TrafficFlowsAggregate == nil {
		writeError(w, http.StatusNotImplemented, "traffic flow aggregate handler is not configured")
		return
	}
	req, ok := buildTrafficFlowsRequest(w, r)
	if !ok {
		return
	}
	agg, err := h.TrafficFlowsAggregate(r, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, agg)
}

func buildDNSQueriesRequest(w http.ResponseWriter, r *http.Request) (DNSQueriesRequest, bool) {
	base, ok := logQueryRequest(w, r)
	if !ok {
		return DNSQueriesRequest{}, false
	}
	q := r.URL.Query()
	var durMinUS int64
	if raw := strings.TrimSpace(q.Get("duration-min-us")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "duration-min-us must be a non-negative integer")
			return DNSQueriesRequest{}, false
		}
		durMinUS = parsed
	}
	return DNSQueriesRequest{
		Since:         base.since,
		From:          q.Get("from"),
		To:            q.Get("to"),
		Client:        q.Get("client"),
		QName:         q.Get("qname"),
		QNameSuffix:   q.Get("qname-suffix"),
		ResponseCode:  q.Get("rcode"),
		Upstream:      q.Get("upstream"),
		DurationMinUS: durMinUS,
		Limit:         base.limit,
	}, true
}

func buildTrafficFlowsRequest(w http.ResponseWriter, r *http.Request) (TrafficFlowsRequest, bool) {
	base, ok := logQueryRequest(w, r)
	if !ok {
		return TrafficFlowsRequest{}, false
	}
	q := r.URL.Query()
	return TrafficFlowsRequest{
		Since:      base.since,
		From:       q.Get("from"),
		To:         q.Get("to"),
		Client:     q.Get("client"),
		Peer:       q.Get("peer"),
		PeerSuffix: q.Get("peer-suffix"),
		Protocol:   q.Get("protocol"),
		Asymmetric: q.Get("asymmetric") == "1" || strings.EqualFold(q.Get("asymmetric"), "true"),
		Limit:      base.limit,
	}, true
}

func (h Handler) handleFirewallLogs(w http.ResponseWriter, r *http.Request) {
	if h.FirewallLogs == nil {
		writeError(w, http.StatusNotImplemented, "firewall log handler is not configured")
		return
	}
	req, ok := logQueryRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.FirewallLogs(r, FirewallLogsRequest{Since: req.since, Action: r.URL.Query().Get("action"), Src: r.URL.Query().Get("src"), Limit: req.limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

type parsedLogQuery struct {
	since string
	limit int
}

func logQueryRequest(w http.ResponseWriter, r *http.Request) (parsedLogQuery, bool) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return parsedLogQuery{}, false
		}
		// Issue #36: raise hard cap from 1000 to 10000 to support
		// longer-range investigations through the HTTP API.
		if parsed > 10000 {
			parsed = 10000
		}
		limit = parsed
	}
	since := r.URL.Query().Get("since")
	if since == "" {
		since = "1h"
	}
	return parsedLogQuery{since: since, limit: limit}, true
}

func (h Handler) handleApply(w http.ResponseWriter, r *http.Request) {
	if h.Apply == nil {
		writeError(w, http.StatusNotImplemented, "apply handler is not configured")
		return
	}
	defer r.Body.Close()
	var req ApplyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "ApplyRequest" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.Apply(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handlePlan(w http.ResponseWriter, r *http.Request) {
	if h.Plan == nil {
		writeError(w, http.StatusNotImplemented, "plan handler is not configured")
		return
	}
	defer r.Body.Close()
	var req PlanRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "PlanRequest" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.Plan(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if h.Delete == nil {
		writeError(w, http.StatusNotImplemented, "delete handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DeleteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "DeleteRequest" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.Delete(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleValidate(w http.ResponseWriter, r *http.Request) {
	if h.Validate == nil {
		writeError(w, http.StatusNotImplemented, "validate handler is not configured")
		return
	}
	defer r.Body.Close()
	var req ValidateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "ValidateRequest" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.Validate(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleSubmitSAMEnrollmentClaim(w http.ResponseWriter, r *http.Request) {
	if h.SubmitSAMEnrollmentClaim == nil {
		writeError(w, http.StatusNotImplemented, "sam enrollment claim submit handler is not configured")
		return
	}
	defer r.Body.Close()
	var req SAMEnrollmentClaimSubmitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "SAMEnrollmentClaimSubmitRequest" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.SubmitSAMEnrollmentClaim(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	if h.SetLogLevel == nil {
		writeError(w, http.StatusNotImplemented, "log level handler is not configured")
		return
	}
	defer r.Body.Close()
	var req LogLevelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "LogLevelRequest" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.SetLogLevel(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h Handler) handleDHCPv6Event(w http.ResponseWriter, r *http.Request) {
	if h.DHCPv6Event == nil {
		writeError(w, http.StatusNotImplemented, "dhcpv6-event handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DHCPv6EventRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "DHCPv6Event" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.DHCPv6Event(r, req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

var ErrBadRequest = errors.New("bad request")

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, NewError(message))
}
