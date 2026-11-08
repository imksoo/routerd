// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeHandlerNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, Prefix+"/runtime", nil)
	rec := httptest.NewRecorder()

	Handler{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSetLogLevelHandlerNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, Prefix+"/log-level", strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"LogLevelRequest","level":"debug"}`))
	rec := httptest.NewRecorder()

	Handler{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
}
