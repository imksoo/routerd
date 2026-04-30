package controlapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const Prefix = "/api/control.routerd.net/v1alpha1"

type Handler struct {
	Status     func(*http.Request) (*Status, error)
	NAPT       func(*http.Request, NAPTRequest) (*NAPTTable, error)
	Apply      func(*http.Request, ApplyRequest) (*ApplyResult, error)
	DHCP6Event func(*http.Request, DHCP6EventRequest) (*DHCP6EventResult, error)
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
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/apply":
		h.handleApply(w, r)
	case r.Method == http.MethodPost && r.URL.Path == Prefix+"/dhcp6-event":
		h.handleDHCP6Event(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
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

func (h Handler) handleDHCP6Event(w http.ResponseWriter, r *http.Request) {
	if h.DHCP6Event == nil {
		writeError(w, http.StatusNotImplemented, "dhcp6-event handler is not configured")
		return
	}
	defer r.Body.Close()
	var req DHCP6EventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIVersion != "" && req.APIVersion != APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if req.Kind != "" && req.Kind != "DHCP6Event" {
		writeError(w, http.StatusBadRequest, "unsupported kind")
		return
	}
	result, err := h.DHCP6Event(r, req)
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
