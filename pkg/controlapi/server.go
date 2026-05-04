package controlapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const Prefix = "/api/control.routerd.net/v1alpha1"

type Handler struct {
	Status         func(*http.Request) (*Status, error)
	NAPT           func(*http.Request, NAPTRequest) (*NAPTTable, error)
	DNSQueries     func(*http.Request, DNSQueriesRequest) (*DNSQueries, error)
	TrafficFlows   func(*http.Request, TrafficFlowsRequest) (*TrafficFlows, error)
	FirewallLogs   func(*http.Request, FirewallLogsRequest) (*FirewallLogs, error)
	Apply          func(*http.Request, ApplyRequest) (*ApplyResult, error)
	Delete         func(*http.Request, DeleteRequest) (*DeleteResult, error)
	DHCPv6Event    func(*http.Request, DHCPv6EventRequest) (*DHCPv6EventResult, error)
	DHCPLeaseEvent func(*http.Request, DHCPLeaseEventRequest) (*DHCPLeaseEventResult, error)
}

type NAPTRequest struct {
	Limit int
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/status":
		h.handleStatus(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/napt":
		h.handleNAPT(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/dns-queries":
		h.handleDNSQueries(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/traffic-flows":
		h.handleTrafficFlows(w, r)
	case r.Method == http.MethodGet && r.URL.Path == Prefix+"/firewall-logs":
		h.handleFirewallLogs(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/apply":
		h.handleApply(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/delete":
		h.handleDelete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/dhcpv6-event":
		h.handleDHCPv6Event(w, r)
	case r.Method == http.MethodPost && (r.URL.Path == Prefix+"/dhcp-lease-event" || r.URL.Path == "/v1/events/dhcp"):
		h.handleDHCPLeaseEvent(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h Handler) handleDHCPLeaseEvent(w http.ResponseWriter, r *http.Request) {
	if h.DHCPLeaseEvent == nil {
		writeError(w, http.StatusNotImplemented, "dhcp lease event handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DHCPLeaseEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

func (h Handler) handleNAPT(w http.ResponseWriter, r *http.Request) {
	if h.NAPT == nil {
		writeError(w, http.StatusNotImplemented, "napt handler is not configured")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		limit = parsed
	}
	table, err := h.NAPT(r, NAPTRequest{Limit: limit})
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
	req, ok := logQueryRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.DNSQueries(r, DNSQueriesRequest{Since: req.since, Client: r.URL.Query().Get("client"), QName: r.URL.Query().Get("qname"), Limit: req.limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h Handler) handleTrafficFlows(w http.ResponseWriter, r *http.Request) {
	if h.TrafficFlows == nil {
		writeError(w, http.StatusNotImplemented, "traffic flow log handler is not configured")
		return
	}
	req, ok := logQueryRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.TrafficFlows(r, TrafficFlowsRequest{Since: req.since, Client: r.URL.Query().Get("client"), Peer: r.URL.Query().Get("peer"), Limit: req.limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
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
		if parsed > 1000 {
			parsed = 1000
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

func (h Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if h.Delete == nil {
		writeError(w, http.StatusNotImplemented, "delete handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

func (h Handler) handleDHCPv6Event(w http.ResponseWriter, r *http.Request) {
	if h.DHCPv6Event == nil {
		writeError(w, http.StatusNotImplemented, "dhcpv6-event handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DHCPv6EventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
