// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestManagementWebConsoleAdmissionEnforcesConfiguredSourceCIDRs(t *testing.T) {
	router := testManagementAccessRouter([]string{"192.168.123.0/24"})
	calls := 0
	handler := managementWebConsoleAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), router, func() *api.Router { return router })

	allowed := httptest.NewRequest(http.MethodGet, "http://routerd.test/", nil)
	allowed.RemoteAddr = "192.168.123.130:43210"
	if got := serveHTTPStatus(handler, allowed); got != http.StatusNoContent {
		t.Fatalf("allowed status = %d, want %d", got, http.StatusNoContent)
	}

	denied := httptest.NewRequest(http.MethodGet, "http://routerd.test/", nil)
	denied.RemoteAddr = "172.18.0.50:43210"
	denied.Header.Set("X-Forwarded-For", "192.168.123.130")
	if got := serveHTTPStatus(handler, denied); got != http.StatusForbidden {
		t.Fatalf("denied status = %d, want %d", got, http.StatusForbidden)
	}
	if calls != 1 {
		t.Fatalf("admitted calls = %d, want 1", calls)
	}
}

func TestManagementWebConsoleAdmissionDoesNotRestrictWithoutSourceCIDRs(t *testing.T) {
	router := testManagementAccessRouter(nil)
	handler := managementWebConsoleAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), router, func() *api.Router { return router })
	req := httptest.NewRequest(http.MethodGet, "http://routerd.test/", nil)
	req.RemoteAddr = "172.18.0.50:43210"
	if got := serveHTTPStatus(handler, req); got != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", got, http.StatusNoContent)
	}
}

func TestManagementWebConsoleAdmissionRejectsUnavailableCurrentRouter(t *testing.T) {
	handler := managementWebConsoleAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), nil, func() *api.Router { return nil })
	req := httptest.NewRequest(http.MethodGet, "http://routerd.test/", nil)
	req.RemoteAddr = "192.168.123.130:43210"
	if got := serveHTTPStatus(handler, req); got != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", got, http.StatusForbidden)
	}
}

func testManagementAccessRouter(allowSourceCIDRs []string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "ManagementAccess"},
		Metadata: api.ObjectMeta{Name: "mgmt"},
		Spec:     api.ManagementAccessSpec{AllowSourceCIDRs: allowSourceCIDRs},
	}}}}
}

func serveHTTPStatus(handler http.Handler, req *http.Request) int {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}
